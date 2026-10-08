package pointer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"
)

type cursorDriver interface {
	Tick(Frame) (Status, error)
	Restore() error
	Invalidate() error
	Alive() bool
	Pump()
	Close() error
}

type leaseState struct {
	sequence uint64
	frame    Frame
	deadline time.Time
	active   bool
}

func (s *leaseState) accept(r request, now time.Time) bool {
	if r.Sequence <= s.sequence {
		return false
	}
	s.sequence = r.Sequence
	if r.Command == "stop" {
		s.active = false
		s.deadline = time.Time{}
		return true
	}
	if r.Command != "frame" {
		return false
	}
	age := now.Sub(time.Unix(0, r.SentUnixNano))
	// Both processes share the same clock. A future timestamp or stale data cannot
	// extend cursor suppression after the renderer has stopped producing frames.
	if age < 0 || age >= Lease {
		s.active = false
		return false
	}
	s.frame = r.Frame
	s.deadline = now.Add(Lease - age)
	s.active = true
	return true
}
func (s *leaseState) current(now time.Time) bool { return s.active && now.Before(s.deadline) }

// RunWorker owns every native call on its initial OS thread. A reader goroutine
// only parses bounded messages; a missing renderer lease independently restores
// the OS cursor even if the producer or its GL thread stalls.
func RunWorker(input io.Reader, output io.Writer) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return runWorker(input, output, newNativeDriver)
}
func runWorker(input io.Reader, output io.Writer, factory func(identity) (cursorDriver, error)) (result error) {
	finished := make(chan struct{})
	defer close(finished)
	commands := make(chan request, 8)
	readError := make(chan error, 1)
	go func() {
		scan := bufio.NewScanner(input)
		scan.Buffer(make([]byte, 4096), 16384)
		for scan.Scan() {
			var r request
			if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
				readError <- err
				return
			}
			select {
			case commands <- r:
			case <-finished:
				return
			}
		}
		err := scan.Err()
		if err == nil {
			err = io.EOF
		}
		readError <- err
	}()
	outputs := make(chan response, 8)
	writeError := make(chan error, 1)
	go func() {
		enc := json.NewEncoder(output)
		for {
			select {
			case <-finished:
				return
			case r := <-outputs:
				if err := enc.Encode(r); err != nil {
					writeError <- err
					return
				}
			}
		}
	}()
	send := func(r response) error {
		select {
		case outputs <- r:
			return nil
		default:
			return errors.New("cursor worker output queue full")
		}
	}
	var hello request
	select {
	case hello = <-commands:
	case err := <-readError:
		return err
	case <-time.After(2 * time.Second):
		return errors.New("cursor worker missing parent")
	}
	if hello.Command != "hello" || hello.Parent.PID == 0 || hello.Parent.Created == 0 {
		return errors.New("cursor worker needs bound parent identity")
	}
	d, err := factory(hello.Parent)
	if err != nil {
		_ = send(response{Event: "error", Error: err.Error()})
		return err
	}
	// Losing our already-bound parent is a normal recovery trigger, whether the
	// first observable symptom is pipe loss or a disappearing render HWND. Keep
	// restoration failures separate so this classification cannot erase them.
	var restorationErrors error
	restore := func(invalidate bool) error {
		var e error
		if invalidate {
			e = d.Invalidate()
		} else {
			e = d.Restore()
		}
		restorationErrors = errors.Join(restorationErrors, e)
		return e
	}
	defer func() {
		if !d.Alive() {
			result = nil
		}
		result = errors.Join(result, restorationErrors, d.Invalidate(), d.Close())
	}()
	if err = send(response{Event: "ready"}); err != nil {
		return err
	}
	ticker := time.NewTicker(8 * time.Millisecond)
	defer ticker.Stop()
	var lease leaseState
	var last Status
	publish := func(status Status) error {
		if status == last {
			return nil
		}
		last = status
		return send(response{Event: "status", Status: status})
	}
	for {
		d.Pump()
		select {
		case err := <-writeError:
			return err
		case err := <-readError:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case r := <-commands:
			switch r.Command {
			case "frame":
				wasActive := lease.active
				lease.accept(r, time.Now())
				if wasActive && !lease.active {
					if err = restore(true); err != nil {
						return err
					}
					if err = publish(Status{}); err != nil {
						return err
					}
				}
			case "stop":
				accepted := lease.accept(r, time.Now())
				if accepted {
					if err = restore(false); err != nil {
						_ = send(response{Event: "error", Error: err.Error()})
						return err
					}
					last = Status{}
				}
				if err = send(response{Event: "stopped", Sequence: r.Sequence}); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown cursor worker command %q", r.Command)
			}
		case <-ticker.C:
			if !d.Alive() {
				return nil
			}
			if !lease.current(time.Now()) {
				if !lease.active {
					continue
				}
				lease.active = false
				if err = restore(true); err != nil {
					_ = send(response{Event: "error", Error: err.Error()})
					return err
				}
				if err = publish(Status{}); err != nil {
					return err
				}
				continue
			}
			status, tickErr := d.Tick(lease.frame)
			if tickErr != nil {
				restoreErr := restore(true)
				err = errors.Join(tickErr, restoreErr)
				_ = send(response{Event: "error", Error: err.Error()})
				return err
			}
			if err = publish(status); err != nil {
				return err
			}
		}
	}
}
