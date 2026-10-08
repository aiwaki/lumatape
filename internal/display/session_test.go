package display

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeSessionWriter struct {
	closed  bool
	writes  int
	onWrite func()
}

func (w *fakeSessionWriter) Write([]byte) (int, error) {
	w.writes++
	if w.onWrite != nil {
		w.onWrite()
	}
	return 0, io.ErrClosedPipe
}
func (w *fakeSessionWriter) Close() error { w.closed = true; return nil }

type fakeSessionReader struct{ closed bool }

func (r *fakeSessionReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *fakeSessionReader) Close() error             { r.closed = true; return nil }

func fakeSession() (*Session, *fakeSessionWriter, *fakeSessionReader) {
	w, r := &fakeSessionWriter{}, &fakeSessionReader{}
	return &Session{in: w, out: r, replies: make(chan response, 1)}, w, r
}

func TestRestoreAcceptsAlreadyCompletedWatchdog(t *testing.T) {
	s, w, r := fakeSession()
	s.replies <- response{Event: "restored"}
	close(s.replies)
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if w.writes != 0 || !w.closed || !r.closed {
		t.Fatal("terminal restoration wrote to a dead watchdog or leaked handles")
	}
}

func TestRestoreTerminalReplyWinsRaceWithBrokenPipe(t *testing.T) {
	s, w, r := fakeSession()
	w.onWrite = func() { s.replies <- response{Event: "restored"}; close(s.replies) }
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if w.writes != 1 || !w.closed || !r.closed {
		t.Fatal("broken-pipe race was not cleaned up")
	}
}

func TestPollPreservesTerminalRestorationFailure(t *testing.T) {
	s, w, r := fakeSession()
	s.replies <- response{Event: "restored", Error: "restore original mode: driver failed"}
	close(s.replies)
	for i := 0; i < 2; i++ {
		done, err := s.Poll()
		if !done || err == nil || !strings.Contains(err.Error(), "driver failed") {
			t.Fatalf("done=%v err=%v", done, err)
		}
	}
	if err := s.Restore(); err == nil {
		t.Fatal("Restore swallowed an earlier restoration failure")
	}
	if w.writes != 0 || !w.closed || !r.closed {
		t.Fatal("terminal failure leaked handles")
	}
}

func TestPollDoesNotInferRestorationFromEOF(t *testing.T) {
	s, _, _ := fakeSession()
	close(s.replies)
	done, err := s.Poll()
	if !done || err == nil {
		t.Fatal("process exit was incorrectly reported as successful restoration")
	}
}

func TestPollPendingIsNonblocking(t *testing.T) {
	s, w, r := fakeSession()
	done, err := s.Poll()
	if done || err != nil || w.writes != 0 || w.closed || r.closed {
		t.Fatal("pending Poll changed session state")
	}
}

func TestSessionReceivesExactConfirmationDeadline(t *testing.T) {
	s, _, _ := fakeSession()
	deadline := time.Now().Add(15 * time.Second)
	s.replies <- response{Event: "applied", DeadlineUnixNano: deadline.UnixNano()}
	if err := s.await("applied", time.Second); err != nil {
		t.Fatal(err)
	}
	if !s.Deadline().Equal(deadline) {
		t.Fatalf("got %v want %v", s.Deadline(), deadline)
	}
}

type slowApplyDriver struct{ *fakeDriver }

func (d slowApplyDriver) apply(s snapshot) error {
	time.Sleep(40 * time.Millisecond)
	return d.fakeDriver.apply(s)
}
func TestConfirmationWindowStartsAfterSuccessfulApply(t *testing.T) {
	g := guard{driver: slowApplyDriver{freshDriver()}}
	const timeout = 100 * time.Millisecond
	if err := g.start(targetMode, time.Now(), timeout); err != nil {
		t.Fatal(err)
	}
	remaining := time.Until(g.deadline)
	if remaining < timeout-10*time.Millisecond {
		t.Fatalf("driver setup consumed confirmation window: %v", remaining)
	}
}

func TestRestoreReportsErrorFromTerminalEvenAfterWriteFailure(t *testing.T) {
	s, w, _ := fakeSession()
	w.onWrite = func() { s.replies <- response{Event: "restored", Error: "original mode unavailable"}; close(s.replies) }
	err := s.Restore()
	if err == nil || !strings.Contains(err.Error(), "original mode unavailable") {
		t.Fatalf("lost recovery error: %v", err)
	}
	if errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("transport error hid actual recovery result")
	}
}

func TestSessionPollObservesWatchdogExternalChange(t *testing.T) {
	d := freshDriver()
	childInput, parentInput := io.Pipe()
	parentOutput, childOutput := io.Pipe()
	defer childInput.Close()
	defer childOutput.Close()
	s := &Session{in: parentInput, out: parentOutput, replies: make(chan response, 4)}
	defer s.close()
	watchdogDone := make(chan error, 1)
	go func() {
		watchdogDone <- runWatchdog(childInput, childOutput, func(string) (driver, error) { return d, nil })
	}()
	go func() {
		defer close(s.replies)
		decoder := json.NewDecoder(parentOutput)
		for {
			var reply response
			if decoder.Decode(&reply) != nil {
				return
			}
			s.replies <- reply
		}
	}()
	if err := s.await("ready", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.send(request{Command: "apply", Device: `\\.\DISPLAY1`, Mode: targetMode, TimeoutMS: 15000}, "applied"); err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(s.Deadline()); remaining < 14*time.Second || remaining > 15*time.Second {
		t.Fatalf("invalid wire deadline: %v", remaining)
	}
	if err := s.Confirm(); err != nil {
		t.Fatal(err)
	}
	d.external(externalMode)
	deadline := time.Now().Add(time.Second)
	for {
		done, err := s.Poll()
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Poll did not observe watchdog relinquishing changed mode")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if err := <-watchdogDone; err != nil {
		t.Fatal(err)
	}
	state, _ := d.current()
	if state.Mode != externalMode {
		t.Fatal("overwrote the external mode while finishing the session")
	}
}
