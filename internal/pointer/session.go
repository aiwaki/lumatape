// Package pointer projects the system cursor through the displayed image while
// leaving all mouse input and the real cursor position under Windows' control.
package pointer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/aiwaki/lumatape/internal/geometry"
)

const Lease = 500 * time.Millisecond

var ErrUnsupported = errors.New("cursor projection requires Windows")

type Frame struct {
	Mapping       geometry.PointerMap
	SourceHWND    uintptr
	SourcePID     uint32
	SourceCreated uint64
	OverlayHWND   uintptr
	Serial        uint64 // assigned by Session; read-only observation of the presented map
}

type Status struct {
	Active    bool
	Window    uintptr
	SourcePID uint32
}

type identity struct {
	PID     uint32
	Created uint64
}
type request struct {
	Command      string
	Sequence     uint64
	SentUnixNano int64
	Parent       identity
	Frame        Frame
}
type response struct {
	Event    string
	Sequence uint64
	Error    string
	Stage    string `json:",omitempty"`
	Status   Status
}

type Session struct {
	op                   sync.Mutex
	mu                   sync.Mutex
	in                   io.WriteCloser
	out                  io.ReadCloser
	frames               chan request
	commands             chan request
	replies              chan response
	done                 chan struct{}
	processDone          chan struct{}
	terminal             error
	closed               bool
	sequence             uint64
	status               Status
	startupStage         string
	everUpdated          bool
	restorationConfirmed bool
}

// Start creates an independent process. It has no cursor side effects until a
// valid, recently presented frame arrives. The pipe is private and inherited.
func Start(watchdogPath string) (*Session, error) {
	parent, err := currentIdentity()
	if err != nil {
		return nil, err
	}
	if watchdogPath == "" {
		exe, e := os.Executable()
		if e != nil {
			return nil, e
		}
		watchdogPath = filepath.Join(filepath.Dir(exe), "lumatape-watchdog.exe")
	}
	cmd := exec.Command(watchdogPath, "--pointer-stdio")
	configureProcess(cmd)
	cmd.Stderr = os.Stderr
	return startProcess(cmd, parent, 2*time.Second)
}

// startProcess keeps the real inherited-pipe and child-exit lifecycle testable
// without initializing native cursor state in the test process.
func startProcess(cmd *exec.Cmd, parent identity, readyTimeout time.Duration) (*Session, error) {
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
		return nil, fmt.Errorf("start cursor worker: %w", err)
	}
	s := &Session{in: in, out: out, frames: make(chan request, 1), commands: make(chan request, 2), replies: make(chan response, 4), done: make(chan struct{}), processDone: make(chan struct{}), restorationConfirmed: true, startupStage: "waiting for worker initialization"}
	go s.writeLoop()
	go func() { s.readLoop(); _ = cmd.Wait(); close(s.processDone) }()
	s.commands <- request{Command: "hello", Parent: parent}
	if err = s.awaitFor("ready", 0, readyTimeout); err != nil {
		s.fail(err)
		return nil, s.abortStartup(cmd, err)
	}
	return s, nil
}

func (s *Session) abortStartup(cmd *exec.Cmd, cause error) error {
	// Start has not returned and no Frame has ever been sent. Initialization may
	// be stuck inside a native call, but it cannot have suppressed the cursor.
	// Unlike an armed worker, this child can be terminated safely. Leaving it
	// behind could let it acquire projection ownership after the failed launch.
	s.closed = true
	_ = s.in.Close()
	_ = s.out.Close()
	select {
	case <-s.processDone:
		return cause
	default:
	}
	killErr := cmd.Process.Kill()
	select {
	case <-s.processDone:
		return cause
	case <-time.After(time.Second):
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		return errors.Join(cause, killErr, errors.New("cursor worker exit after cancelled startup is unconfirmed; no cursor frame was sent"))
	}
}

func (s *Session) fail(err error) {
	s.mu.Lock()
	if s.terminal == nil {
		s.terminal = err
		close(s.done)
	}
	s.mu.Unlock()
}
func (s *Session) writeLoop() {
	enc := json.NewEncoder(s.in)
	for {
		var r request
		select {
		case <-s.done:
			return
		case r = <-s.commands:
		default:
			select {
			case <-s.done:
				return
			case r = <-s.commands:
			case r = <-s.frames:
			}
		}
		if err := enc.Encode(r); err != nil {
			s.fail(fmt.Errorf("cursor worker write: %w", err))
			return
		}
	}
}
func (s *Session) readLoop() {
	scan := bufio.NewScanner(s.out)
	scan.Buffer(make([]byte, 4096), 16384)
	for scan.Scan() {
		var r response
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			s.fail(fmt.Errorf("cursor worker protocol: %w", err))
			return
		}
		if r.Event == "status" {
			s.mu.Lock()
			s.status = r.Status
			s.mu.Unlock()
			continue
		}
		if r.Event == "initializing" {
			s.mu.Lock()
			s.startupStage = r.Stage
			s.mu.Unlock()
			continue
		}
		if r.Error != "" {
			s.fail(errors.New(r.Error))
			return
		}
		select {
		case s.replies <- r:
		case <-s.done:
			return
		}
	}
	err := scan.Err()
	if err == nil {
		err = io.EOF
	}
	s.fail(fmt.Errorf("cursor worker closed: %w", err))
}
func (s *Session) await(event string, seq uint64) error {
	return s.awaitFor(event, seq, 2*time.Second)
}
func (s *Session) awaitFor(event string, seq uint64, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case r := <-s.replies:
			if r.Event == event && r.Sequence == seq {
				s.mu.Lock()
				s.status = r.Status
				s.mu.Unlock()
				return nil
			}
			return fmt.Errorf("cursor worker: unexpected %s sequence %d", r.Event, r.Sequence)
		case <-s.done:
			return s.Poll()
		case <-timer.C:
			s.mu.Lock()
			stage := s.startupStage
			s.mu.Unlock()
			err := fmt.Errorf("cursor worker %s acknowledgement timed out", event)
			if event == "ready" && stage != "" {
				err = fmt.Errorf("%w (last initialization stage: %s)", err, stage)
			}
			s.fail(err)
			return err
		}
	}
}

// Update is bounded and does not perform pipe I/O. Only the latest unsent frame
// is retained; the timestamp makes a delayed transport unable to renew a lease.
func (s *Session) Update(frame Frame) error {
	s.op.Lock()
	defer s.op.Unlock()
	if s.closed {
		return errors.New("cursor session is closed")
	}
	if err := s.Poll(); err != nil {
		return err
	}
	if frame.SourceHWND == 0 || frame.SourcePID == 0 || frame.SourceCreated == 0 || frame.OverlayHWND == 0 {
		return errors.New("cursor frame needs exact source and overlay identities")
	}
	s.sequence++
	s.everUpdated = true
	s.mu.Lock()
	s.restorationConfirmed = false
	s.mu.Unlock()
	frame.Serial = s.sequence
	r := request{Command: "frame", Sequence: s.sequence, SentUnixNano: time.Now().UnixNano(), Frame: frame}
	select {
	case s.frames <- r:
	default:
		select {
		case <-s.frames:
		default:
		}
		select {
		case s.frames <- r:
		default:
		}
	}
	return nil
}

// Stop is a sequence barrier: its ACK means the system cursor was restored and
// the projection hidden. Older queued frames cannot reactivate the worker.
func (s *Session) Stop() error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.stopLocked()
}
func (s *Session) stopLocked() error {
	if s.closed {
		return nil
	}
	if err := s.Poll(); err != nil {
		return err
	}
	s.sequence++
	select {
	case <-s.frames:
	default:
	}
	r := request{Command: "stop", Sequence: s.sequence}
	select {
	case s.commands <- r:
	case <-s.done:
		return s.Poll()
	}
	if err := s.await("stopped", r.Sequence); err != nil {
		// EOF is the worker's restoration instruction even if its ACK got lost.
		_ = s.in.Close()
		s.fail(err)
		return err
	}
	s.mu.Lock()
	s.restorationConfirmed = true
	s.mu.Unlock()
	return nil
}

// Poll latches a worker failure. The renderer should hide its effect surface on
// error. Recovery is restricted to sessions that could have hidden our cursor.
func (s *Session) Poll() error {
	s.mu.Lock()
	err := s.terminal
	s.mu.Unlock()
	return err
}
func (s *Session) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.status }

// RestorationConfirmed is stronger than Status.Active == false. It is true
// only before the first frame, after a Stop barrier was acknowledged, or after
// confirmed worker exit and a successful native fallback restore. Update clears
// it. Worker/transport errors remain available through Poll and Close even when
// cursor restoration succeeded, so callers can log the cause and safely stay on.
func (s *Session) RestorationConfirmed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restorationConfirmed
}
func (s *Session) Close() error {
	s.op.Lock()
	defer s.op.Unlock()
	if s.closed {
		return nil
	}
	err := s.stopLocked()
	s.closed = true
	_ = s.in.Close()
	_ = s.out.Close()
	// Do not restore global visibility while a live worker could subsequently
	// finish an in-flight hide. EOF/lease restoration belongs to that worker;
	// fallback recovery runs only after confirmed process exit.
	exited := false
	select {
	case <-s.processDone:
		exited = true
	case <-time.After(time.Second):
		err = errors.Join(err, errors.New("cursor worker did not exit after pipe close; its lease must restore the cursor"))
	}
	if err != nil && s.everUpdated && exited {
		recoveryError := recoverCursor()
		if recoveryError == nil {
			s.mu.Lock()
			s.restorationConfirmed = true
			s.mu.Unlock()
		}
		err = errors.Join(err, recoveryError)
	}
	s.fail(errors.New("cursor session closed"))
	return err
}
