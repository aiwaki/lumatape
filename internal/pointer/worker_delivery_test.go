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

type terminalObservation struct {
	response
	restored, closed bool
}

// Gate the actual Write, not the response queue. An enqueue is insufficient
// evidence that a parent received the diagnostic before worker exit.
type terminalGateOutput struct {
	driver  *fakeDriver
	started chan terminalObservation
	release chan struct{}
	written chan response
	unblock func()
}

type terminalDeliveryDriver struct{ *fakeDriver }

func (d terminalDeliveryDriver) Close() error {
	return errors.Join(d.fakeDriver.Close(), d.closeError)
}

func (w *terminalGateOutput) Write(data []byte) (int, error) {
	var r response
	if err := json.Unmarshal(data, &r); err != nil {
		return 0, err
	}
	if r.Event == "error" {
		observed := terminalObservation{response: r}
		if w.driver != nil {
			w.driver.mu.Lock()
			observed.restored = w.driver.invalidated > 0 && !w.driver.active
			observed.closed = w.driver.closed > 0
			w.driver.mu.Unlock()
		}
		w.started <- observed
		<-w.release
		w.written <- r
	}
	return len(data), nil
}

func startTerminalDeliveryWorker(t *testing.T, d *fakeDriver, startupErr error) (*terminalGateOutput, <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	w := &terminalGateOutput{driver: d, started: make(chan terminalObservation, 1), release: make(chan struct{}), written: make(chan response, 1)}
	w.unblock = sync.OnceFunc(func() { close(w.release) })
	done := make(chan error, 1)
	go func() {
		done <- runWorker(inR, w, func(identity, func(string)) (cursorDriver, error) {
			if startupErr != nil {
				return nil, startupErr
			}
			return terminalDeliveryDriver{d}, nil
		})
		_ = inR.Close()
	}()
	t.Cleanup(func() { w.unblock(); _ = inW.Close(); _ = inR.Close() })
	enc := json.NewEncoder(inW)
	if err := enc.Encode(request{Command: "hello", Parent: identity{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if startupErr == nil {
		if err := enc.Encode(request{Command: "frame", Sequence: 1, SentUnixNano: time.Now().UnixNano()}); err != nil {
			t.Fatal(err)
		}
	}
	return w, done
}

func TestWorkerDeliversTerminalCauseOnlyAfterNativeCleanup(t *testing.T) {
	cause := errors.New("cursor pixel preparation failed")
	cleanup := errors.New("native cleanup detail")
	d := &fakeDriver{alive: true, tickError: cause, closeError: cleanup}
	w, done := startTerminalDeliveryWorker(t, d, nil)
	var observed terminalObservation
	select {
	case observed = <-w.started:
	case <-time.After(time.Second):
		t.Fatal("terminal cause was discarded before writing")
	}
	if !observed.restored || !observed.closed {
		t.Fatal("terminal I/O started before native restoration and teardown")
	}
	if !strings.Contains(observed.Error, cause.Error()) || !strings.Contains(observed.Error, cleanup.Error()) {
		t.Fatalf("terminal diagnostic lost the original or cleanup cause: %q", observed.Error)
	}
	select {
	case err := <-done:
		t.Fatalf("worker exited before its terminal write completed: %v", err)
	default:
	}
	w.unblock()
	select {
	case <-w.written:
	case <-time.After(time.Second):
		t.Fatal("terminal write did not complete")
	}
	select {
	case err := <-done:
		if !errors.Is(err, cause) || !errors.Is(err, cleanup) {
			t.Fatalf("worker return lost its causes: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("completed terminal diagnostic prevented exit")
	}
}

func TestWorkerBlockedTerminalWriteCannotHoldRecoveryOrExit(t *testing.T) {
	cause := errors.New("cursor presentation failed")
	d := &fakeDriver{alive: true, tickError: cause}
	w, done := startTerminalDeliveryWorker(t, d, nil)
	select {
	case observed := <-w.started:
		if !observed.restored || !observed.closed {
			t.Fatal("blocked diagnostic delayed native recovery")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal write was not attempted")
	}
	// Leave Write blocked until cleanup. The terminal delivery budget is bounded
	// independently of the parent, so no ACK or reader is needed for safe exit.
	select {
	case err := <-done:
		if !errors.Is(err, cause) {
			t.Fatalf("blocked diagnostic replaced the native failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked terminal writer kept the worker alive")
	}
}

func TestWorkerInitializationFailureIsDeliveredBeforeExit(t *testing.T) {
	cause := errors.New("native cursor initialization failed")
	w, done := startTerminalDeliveryWorker(t, nil, cause)
	select {
	case observed := <-w.started:
		if observed.Error != cause.Error() {
			t.Fatalf("startup diagnostic = %q", observed.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("startup cause was discarded")
	}
	select {
	case err := <-done:
		t.Fatalf("worker exited before the startup diagnostic was written: %v", err)
	default:
	}
	w.unblock()
	select {
	case err := <-done:
		if !errors.Is(err, cause) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after startup diagnostic")
	}
}
