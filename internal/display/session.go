package display

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type request struct {
	Command   string
	Device    string
	Mode      Mode
	TimeoutMS int64
}
type response struct {
	Event, Error     string
	DeadlineUnixNano int64
}

// Session is a pipe to a separate watchdog process. No file journal or public
// IPC endpoint is needed: the only writer belongs to the main process. Losing
// that writer (including TerminateProcess) produces EOF and restores the mode.
type Session struct {
	mu          sync.Mutex
	in          io.WriteCloser
	out         io.ReadCloser
	replies     chan response
	closed      bool
	terminal    bool
	terminalErr error
	deadline    time.Time
}

// DefaultWatchdogPath returns the required companion executable beside the app.
func DefaultWatchdogPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "lumatape-watchdog.exe"), nil
}

// StartSession waits for an independent, ready watchdog before requesting any
// side effect. The watchdog captures the original full mode, re-enumerates the
// candidate, tests it and applies it. Call Confirm only after explicit user
// confirmation, and defer Restore immediately after successful return.
func StartSession(watchdogPath, device string, mode Mode, confirmationTimeout time.Duration) (*Session, error) {
	if !supported() {
		return nil, ErrUnsupported
	}
	if confirmationTimeout < 5*time.Second || confirmationTimeout > 60*time.Second {
		return nil, errors.New("display confirmation timeout must be between 5 and 60 seconds")
	}
	if device == "" {
		return nil, errors.New("an explicit monitor device name is required")
	}
	if watchdogPath == "" {
		var err error
		watchdogPath, err = DefaultWatchdogPath()
		if err != nil {
			return nil, err
		}
	}
	cmd := exec.Command(watchdogPath, "--stdio")
	configureWatchdog(cmd)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, fmt.Errorf("start independent display watchdog: %w", err)
	}
	s := &Session{in: in, out: out, replies: make(chan response, 4)}
	go func() {
		decoder := json.NewDecoder(out)
		for {
			var reply response
			if err := decoder.Decode(&reply); err != nil {
				break
			}
			s.replies <- reply
		}
		close(s.replies)
		_ = cmd.Wait()
	}()
	if err = s.await("ready", 5*time.Second); err != nil {
		// No apply request has been sent, so termination is safe here.
		s.close()
		_ = cmd.Process.Kill()
		return nil, err
	}
	if err = s.send(request{Command: "apply", Device: device, Mode: mode, TimeoutMS: confirmationTimeout.Milliseconds()}, "applied"); err != nil {
		// Never kill a watchdog after apply: EOF instructs it to restore even
		// if a slow graphics driver finishes the in-flight call later.
		return nil, s.finishAfterError(err)
	}
	return s, nil
}

func (s *Session) await(event string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case reply, ok := <-s.replies:
		if s.receiveTerminal(reply, ok) {
			if event == "restored" {
				return s.terminalErr
			}
			return errors.Join(fmt.Errorf("display session ended before %s", event), s.terminalErr)
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		if reply.Event != event {
			return fmt.Errorf("display watchdog: expected %s, received %s", event, reply.Event)
		}
		if event == "applied" {
			if reply.DeadlineUnixNano == 0 {
				return errors.New("watchdog omitted the confirmation deadline")
			}
			s.deadline = time.Unix(0, reply.DeadlineUnixNano)
		}
		return nil
	case <-timer.C:
		return errors.New("display watchdog response timed out; restoration requested by closing its pipe")
	}
}

func (s *Session) receiveTerminal(reply response, ok bool) bool {
	if ok && reply.Event != "restored" {
		return false
	}
	s.terminal = true
	if !ok {
		s.terminalErr = errors.New("display watchdog exited without a restoration result")
	} else if reply.Error != "" {
		s.terminalErr = errors.New(reply.Error)
	}
	s.close()
	return true
}

// Poll reports completion without blocking. Completion includes automatic
// rollback and relinquishing a mode that another program/user changed. An
// error means restoration failed or its result could not be verified. Terminal
// results are retained, and pipe handles close when completion is observed.
func (s *Session) Poll() (bool, error) {
	if s == nil {
		return true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.poll()
}

func (s *Session) poll() (bool, error) {
	if s.terminal {
		return true, s.terminalErr
	}
	select {
	case reply, ok := <-s.replies:
		if !s.receiveTerminal(reply, ok) {
			s.terminal, s.terminalErr = true, fmt.Errorf("unexpected asynchronous watchdog event %q", reply.Event)
			s.close()
		}
		return true, s.terminalErr
	default:
		return false, nil
	}
}

// Deadline is the watchdog's actual confirmation deadline, established after
// successful mode application and verification. Use this for the UI countdown.
func (s *Session) Deadline() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadline
}

func (s *Session) finishAfterError(cause error) error {
	if !s.terminal {
		_ = s.in.Close() // EOF requests rollback while retaining its output pipe.
		restoreErr := s.await("restored", 15*time.Second)
		if !s.terminal {
			s.terminal, s.terminalErr = true, restoreErr
		}
	}
	s.close()
	return errors.Join(cause, s.terminalErr)
}

func (s *Session) send(req request, event string) error {
	if err := json.NewEncoder(s.in).Encode(req); err != nil {
		return fmt.Errorf("display watchdog command: %w", err)
	}
	return s.await(event, 15*time.Second)
}

func (s *Session) close() {
	if s.closed {
		return
	}
	s.closed = true
	_ = s.in.Close()
	_ = s.out.Close()
}

func (s *Session) Confirm() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.Join(errors.New("display session is closed"), s.terminalErr)
	}
	if err := s.send(request{Command: "confirm"}, "confirmed"); err != nil {
		return s.finishAfterError(err)
	}
	return nil
}

// Restore is idempotent. A different currently observed mode is left untouched.
func (s *Session) Restore() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if done, err := s.poll(); done {
		return err
	}
	defer s.close()
	// A watchdog can finish between the nonblocking poll and this write. In
	// that case its terminal reply, not a broken pipe, decides the result.
	writeErr := json.NewEncoder(s.in).Encode(request{Command: "restore"})
	err := s.await("restored", 15*time.Second)
	if s.terminal {
		return s.terminalErr
	}
	s.terminal, s.terminalErr = true, errors.Join(writeErr, err)
	return s.terminalErr
}
