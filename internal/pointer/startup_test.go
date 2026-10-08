package pointer

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// This child deliberately outlives stdin EOF, like an initialization call stuck
// inside a graphics driver. No native API or user's cursor state is involved.
func TestCursorStartupChild(t *testing.T) {
	mode := os.Getenv("LUMATAPE_TEST_CURSOR_STARTUP")
	if mode == "" {
		return
	}
	if mode == "ready" {
		if err := runWorker(os.Stdin, os.Stdout, func(identity, func(string)) (cursorDriver, error) {
			return &fakeDriver{alive: true}, nil
		}); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	if mode == "blocked-native" {
		var hello request
		if err := json.NewDecoder(os.Stdin).Decode(&hello); err != nil || hello.Command != "hello" {
			os.Exit(4)
		}
		if err := json.NewEncoder(os.Stdout).Encode(response{Event: "initializing", Stage: "MagInitialize"}); err != nil {
			os.Exit(5)
		}
	}
	// A timer keeps the runtime alive without CPU spin or the deadlock detector.
	for {
		time.Sleep(time.Hour)
	}
}

func cursorStartupCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestCursorStartupChild$")
	// A race-instrumented helper must not add the runtime's artificial one-second
	// exit sleep to the production worker's one-second EOF cleanup deadline.
	cmd.Env = append(os.Environ(), "LUMATAPE_TEST_CURSOR_STARTUP="+mode, "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
	configureProcess(cmd)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return cmd
}

func TestFailedStartupReapsUnarmedChild(t *testing.T) {
	for _, tc := range []struct{ mode, stage string }{
		{"blocked-before-handshake", "waiting for worker initialization"},
		{"blocked-native", "MagInitialize"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			cmd := cursorStartupCommand(t, tc.mode)
			session, err := startProcess(cmd, identity{1, 2}, time.Second)
			if session != nil || err == nil || !strings.Contains(err.Error(), "ready acknowledgement timed out") || !strings.Contains(err.Error(), tc.stage) {
				t.Fatalf("startup result = %v, %v; want timeout at %s", session, err, tc.stage)
			}
			if strings.Contains(err.Error(), "lease") || strings.Contains(err.Error(), "unconfirmed") {
				t.Fatalf("unarmed child reported as live recovery owner: %v", err)
			}
			if cmd.ProcessState == nil {
				t.Fatal("failed Start returned before its own child was reaped")
			}
		})
	}
}

func TestStartupProcessCanAcknowledgeAndStopNormally(t *testing.T) {
	cmd := cursorStartupCommand(t, "ready")
	session, err := startProcess(cmd, identity{1, 2}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatalf("normal startup/close did not exit cleanly: %v", cmd.ProcessState)
	}
}

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
