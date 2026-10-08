package pointer

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartupProgressReportsBlockedStageWithoutGrantingReady(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	resume := make(chan struct{})
	resumeWorker := sync.OnceFunc(func() { close(resume) })
	d := &fakeDriver{alive: true}
	workerDone := make(chan error, 1)
	s := &Session{in: inW, out: outR, commands: make(chan request, 2), replies: make(chan response, 4), done: make(chan struct{}), restorationConfirmed: true}
	go s.readLoop()
	go func() {
		workerDone <- runWorker(inR, outW, func(_ identity, progress func(string)) (cursorDriver, error) {
			progress("MagInitialize")
			<-resume // model a blocked native startup, before any frame can arrive
			return d, nil
		})
		_ = inR.Close()
		_ = outW.Close()
	}()
	t.Cleanup(func() { resumeWorker(); _ = inW.Close(); _ = outR.Close() })
	if err := json.NewEncoder(inW).Encode(request{Command: "hello", Parent: identity{1, 2}}); err != nil {
		t.Fatal(err)
	}
	waitCondition(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.startupStage == "MagInitialize"
	})
	err := s.awaitFor("ready", 0, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "ready acknowledgement timed out") || !strings.Contains(err.Error(), "MagInitialize") {
		t.Fatalf("startup error lost its precise stage: %v", err)
	}
	if !errors.Is(s.Poll(), err) || !s.RestorationConfirmed() {
		t.Fatal("startup timeout was not latched or claimed a cursor mutation")
	}
	if stopErr := s.Stop(); !errors.Is(stopErr, err) || len(s.commands) != 0 {
		t.Fatalf("failed startup waited for another impossible acknowledgement: %v", stopErr)
	}
	if ticks, _, _, active := d.counts(); ticks != 0 || active {
		t.Fatal("worker touched cursor before a Frame")
	}
	_ = inW.Close()
	resumeWorker()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("startup worker did not clean up after parent pipe closure")
	}
}

func TestInitializationProgressIsNotAReadyAcknowledgement(t *testing.T) {
	outR, outW := io.Pipe()
	s := &Session{out: outR, replies: make(chan response, 4), done: make(chan struct{})}
	go s.readLoop()
	t.Cleanup(func() { _ = outW.Close(); _ = outR.Close() })
	go func() {
		encoder := json.NewEncoder(outW)
		_ = encoder.Encode(response{Event: "initializing", Stage: "cursor capture exclusion"})
		_ = encoder.Encode(response{Event: "ready"})
	}()
	if err := s.awaitFor("ready", 0, time.Second); err != nil {
		t.Fatalf("progress confused the ready handshake: %v", err)
	}
}
