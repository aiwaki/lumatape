package pointer

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestLeaseExpiresAndCannotBeRevivedByQueuedFrames(t *testing.T) {
	now := time.Unix(200, 0)
	var s leaseState
	fresh := func(seq uint64, at time.Time) request {
		return request{Command: "frame", Sequence: seq, SentUnixNano: at.UnixNano()}
	}
	if !s.accept(fresh(1, now), now) || !s.current(now.Add(Lease-time.Nanosecond)) || s.current(now.Add(Lease)) {
		t.Fatal("lease boundary")
	}
	if !s.accept(request{Command: "stop", Sequence: 3}, now) || s.active {
		t.Fatal("stop did not disarm")
	}
	for _, seq := range []uint64{1, 2, 3} {
		if s.accept(fresh(seq, now), now) || s.active {
			t.Fatal("stale frame resurrected a stopped pointer")
		}
	}
	if !s.accept(fresh(4, now), now) || !s.active {
		t.Fatal("new session intent was rejected")
	}
	if s.accept(fresh(5, now.Add(-Lease)), now) || s.active {
		t.Fatal("old delayed pipe data renewed lease")
	}
	if s.accept(fresh(6, now.Add(time.Nanosecond)), now) || s.active {
		t.Fatal("future timestamp accepted")
	}
}
func TestTransportDelayConsumesLease(t *testing.T) {
	now := time.Unix(200, 0)
	var s leaseState
	s.accept(request{Command: "frame", Sequence: 1, SentUnixNano: now.Add(-400 * time.Millisecond).UnixNano()}, now)
	if !s.current(now.Add(99*time.Millisecond)) || s.current(now.Add(100*time.Millisecond)) {
		t.Fatal("transport delay extended native suppression")
	}
}
func TestCursorAlphaPreservesColorAndMonochromeMasks(t *testing.T) {
	tests := []struct {
		name       string
		b, w, want []byte
	}{
		{"transparent", []byte{0, 0, 0, 0}, []byte{255, 255, 255, 0}, []byte{0, 0, 0, 0}},
		{"black", []byte{0, 0, 0, 0}, []byte{0, 0, 0, 0}, []byte{0, 0, 0, 255}},
		{"white", []byte{255, 255, 255, 0}, []byte{255, 255, 255, 0}, []byte{255, 255, 255, 255}},
		{"alpha", []byte{20, 30, 40, 0}, []byte{147, 157, 167, 0}, []byte{20, 30, 40, 128}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := composeCursorAlpha(tc.b, tc.w); err != nil {
				t.Fatal(err)
			}
			for i := range tc.b {
				if tc.b[i] != tc.want[i] {
					t.Fatalf("got%v want%v", tc.b, tc.want)
				}
			}
		})
	}
	if composeCursorAlpha([]byte{255, 255, 255, 0}, []byte{0, 0, 0, 0}) == nil {
		t.Fatal("XOR cursor silently corrupted")
	}
	if composeCursorAlpha([]byte{0}, []byte{0}) == nil {
		t.Fatal("badbuffer accepted")
	}
}

type fakeDriver struct {
	invalidateError, closeError          error
	mu                                   sync.Mutex
	ticks, restored, invalidated, closed int
	alive                                bool
	tickError                            error
	active                               bool
}

func (d *fakeDriver) Tick(Frame) (Status, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ticks++
	d.active = true
	return Status{Active: true, Window: 99}, d.tickError
}
func (d *fakeDriver) Restore() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.restored++
	d.active = false
	return nil
}
func (d *fakeDriver) Invalidate() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.invalidated++
	d.active = false
	return d.invalidateError
}
func (d *fakeDriver) Alive() bool  { d.mu.Lock(); defer d.mu.Unlock(); return d.alive }
func (d *fakeDriver) Pump()        {}
func (d *fakeDriver) Close() error { d.mu.Lock(); defer d.mu.Unlock(); d.closed++; return nil }
func (d *fakeDriver) counts() (int, int, int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ticks, d.restored, d.invalidated, d.active
}

type testWorker struct {
	in      *io.PipeWriter
	out     *io.PipeReader
	enc     *json.Encoder
	replies chan response
	done    chan error
	d       *fakeDriver
}

func startTestWorker(t *testing.T) *testWorker {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	d := &fakeDriver{alive: true}
	w := &testWorker{in: inW, out: outR, enc: json.NewEncoder(inW), replies: make(chan response, 16), done: make(chan error, 1), d: d}
	go func() {
		w.done <- runWorker(inR, outW, func(identity) (cursorDriver, error) { return d, nil })
		_ = outW.Close()
		_ = inR.Close()
	}()
	go func() {
		dec := json.NewDecoder(outR)
		for {
			var r response
			if dec.Decode(&r) != nil {
				return
			}
			w.replies <- r
		}
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outR.Close() })
	if err := w.enc.Encode(request{Command: "hello", Parent: identity{1, 2}}); err != nil {
		t.Fatal(err)
	}
	w.wait(t, "ready")
	return w
}
func (w *testWorker) wait(t *testing.T, event string) response {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-w.replies:
			if r.Event == event {
				return r
			}
		case <-timer.C:
			t.Fatalf("waiting for%s", event)
		}
	}
}
func (w *testWorker) frame(t *testing.T, seq uint64) {
	t.Helper()
	if err := w.enc.Encode(request{Command: "frame", Sequence: seq, SentUnixNano: time.Now().UnixNano()}); err != nil {
		t.Fatal(err)
	}
}
func waitCondition(t *testing.T, f func() bool) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestWorkerStopAcknowledgesRestoreAndDoesNotHideNewFlatFrames(t *testing.T) {
	w := startTestWorker(t)
	w.frame(t, 1)
	w.wait(t, "status")
	if err := w.enc.Encode(request{Command: "stop", Sequence: 2}); err != nil {
		t.Fatal(err)
	}
	stopped := w.wait(t, "stopped")
	_, r, before, active := w.d.counts()
	if stopped.Sequence != 2 || r == 0 || active {
		t.Fatal("ACK preceded restoration")
	}
	// A stopped worker must not keep hiding the parent surface as it later draws
	// flat content. It also must not reactivate for the pre-barrier queued frame.
	w.frame(t, 1)
	time.Sleep(40 * time.Millisecond)
	_, _, after, active := w.d.counts()
	if before != after || active {
		t.Fatal("idle worker mutated surface or accepted stale frame")
	}
	w.frame(t, 3)
	waitCondition(t, func() bool { _, _, _, active := w.d.counts(); return active })
}
func TestWorkerLeaseExpiryHidesOwnSurfaceOnce(t *testing.T) {
	w := startTestWorker(t)
	w.frame(t, 1)
	w.wait(t, "status")
	waitCondition(t, func() bool { _, _, invalidated, active := w.d.counts(); return invalidated > 0 && !active })
	_, _, before, _ := w.d.counts()
	time.Sleep(40 * time.Millisecond)
	_, _, after, _ := w.d.counts()
	if before != after {
		t.Fatal("expired lease repeatedly hid a new surface")
	}
}
func TestWorkerEOFRestoresBeforeClose(t *testing.T) {
	w := startTestWorker(t)
	w.frame(t, 1)
	w.wait(t, "status")
	_ = w.in.Close()
	select {
	case err := <-w.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker ignored EOF")
	}
	w.d.mu.Lock()
	defer w.d.mu.Unlock()
	if w.d.invalidated == 0 || w.d.closed == 0 || w.d.active {
		t.Fatal("worker left projection active")
	}
}
func TestWorkerRenderFailureRestores(t *testing.T) {
	w := startTestWorker(t)
	w.d.mu.Lock()
	w.d.tickError = errors.New("renderfailed")
	w.d.mu.Unlock()
	w.frame(t, 1)
	select {
	case err := <-w.done:
		if err == nil {
			t.Fatal("render failure lost")
		}
	case <-time.After(time.Second):
		t.Fatal("worker ignored renderer error")
	}
	_, _, restored, active := w.d.counts()
	if restored == 0 || active {
		t.Fatal("render failure kept hiding cursor")
	}
}

func TestUpdateQueueRetainsOnlyLatestAndNeverWaitsForPipe(t *testing.T) {
	s := &Session{frames: make(chan request, 1), done: make(chan struct{})}
	f := Frame{SourceHWND: 1, SourcePID: 2, SourceCreated: 3, OverlayHWND: 4}
	for i := 0; i < 10000; i++ {
		if err := s.Update(f); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.frames) != 1 {
		t.Fatal("unbounded frames")
	}
	got := <-s.frames
	if got.Sequence != 10000 || got.Frame.Serial != 10000 {
		t.Fatalf("retained stale sequence%d", got.Sequence)
	}
}

type blockingOutput struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingOutput) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}
func TestBlockedOutputDoesNotBlockLeaseRestoration(t *testing.T) {
	inR, inW := io.Pipe()
	out := &blockingOutput{entered: make(chan struct{}), release: make(chan struct{})}
	d := &fakeDriver{alive: true}
	done := make(chan error, 1)
	go func() {
		done <- runWorker(inR, out, func(identity) (cursorDriver, error) { return d, nil })
		_ = inR.Close()
	}()
	defer func() { _ = inW.Close(); close(out.release) }()
	enc := json.NewEncoder(inW)
	if err := enc.Encode(request{Command: "hello", Parent: identity{1, 2}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-out.entered:
	case <-time.After(time.Second):
		t.Fatal("writer never blocked")
	}
	if err := enc.Encode(request{Command: "frame", Sequence: 1, SentUnixNano: time.Now().UnixNano()}); err != nil {
		t.Fatal(err)
	}
	waitCondition(t, func() bool { _, _, _, active := d.counts(); return active })
	waitCondition(t, func() bool { _, _, restored, active := d.counts(); return restored > 0 && !active })
	_ = inW.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked stdout prevented EOF cleanup")
	}
}

func TestRestorationConfirmationRequiresStopBarrierAndSurvivesClose(t *testing.T) {
	s := &Session{frames: make(chan request, 1), commands: make(chan request, 2), replies: make(chan response, 4), done: make(chan struct{}), restorationConfirmed: true}
	if !s.RestorationConfirmed() {
		t.Fatal("unstarted session cannot have hidden a cursor")
	}
	f := Frame{SourceHWND: 1, SourcePID: 2, SourceCreated: 3, OverlayHWND: 4}
	if err := s.Update(f); err != nil {
		t.Fatal(err)
	}
	if s.RestorationConfirmed() {
		t.Fatal("queued frame must invalidate restoration confirmation")
	}
	// A standby status can later reactivate using the same unexpired frame.
	s.mu.Lock()
	s.status = Status{}
	s.mu.Unlock()
	if s.RestorationConfirmed() {
		t.Fatal("standby status is not a stop barrier")
	}
	go func() { r := <-s.commands; s.replies <- response{Event: "stopped", Sequence: r.Sequence} }()
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if !s.RestorationConfirmed() {
		t.Fatal("successful restoration acknowledgement was lost")
	}
	if err := s.Update(f); err != nil {
		t.Fatal(err)
	}
	if s.RestorationConfirmed() {
		t.Fatal("new frame retained old stop confirmation")
	}
	go func() { r := <-s.commands; s.replies <- response{Event: "stopped", Sequence: r.Sequence} }()
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	// Idempotent Close must not erase a completed restoration result or replace
	// the original error. Avoid native calls: this represents an already-closed
	// session after its acknowledged stop and worker teardown.
	s.closed = true
	s.fail(errors.New("original failure"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.RestorationConfirmed() || s.Poll() == nil || s.Poll().Error() != "original failure" {
		t.Fatal("close erased evidence")
	}
}
func TestFailureBeforeStopAckDoesNotConfirmRestoration(t *testing.T) {
	s := &Session{frames: make(chan request, 1), commands: make(chan request, 2), replies: make(chan response, 4), done: make(chan struct{}), restorationConfirmed: true}
	if err := s.Update(Frame{SourceHWND: 1, SourcePID: 2, SourceCreated: 3, OverlayHWND: 4}); err != nil {
		t.Fatal(err)
	}
	s.fail(errors.New("worker died"))
	if s.Stop() == nil || s.RestorationConfirmed() {
		t.Fatal("dead worker was called restored without native recovery")
	}
}

type parentLossOutput struct {
	driver      *fakeDriver
	parentAlive bool
}

func (w parentLossOutput) Write([]byte) (int, error) {
	w.driver.mu.Lock()
	w.driver.alive = w.parentAlive
	w.driver.mu.Unlock()
	return 0, io.ErrClosedPipe
}
func TestWorkerParentDeathClassifiesTransportLossButKeepsCleanupErrors(t *testing.T) {
	cleanupFailure := errors.New("native restore failed")
	cases := []struct {
		name      string
		alive     bool
		cleanup   error
		wantError bool
	}{
		{"dead parent", false, nil, false},
		{"live parent transport fault", true, nil, true},
		{"dead parent cleanup failure", false, cleanupFailure, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inR, inW := io.Pipe()
			defer inR.Close()
			defer inW.Close()
			d := &fakeDriver{alive: true, invalidateError: tc.cleanup}
			done := make(chan error, 1)
			go func() {
				done <- runWorker(inR, parentLossOutput{d, tc.alive}, func(identity) (cursorDriver, error) { return d, nil })
			}()
			if err := json.NewEncoder(inW).Encode(request{Command: "hello", Parent: identity{1, 2}}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if (err != nil) != tc.wantError {
					t.Fatalf("result%v wantError%t", err, tc.wantError)
				}
				if tc.cleanup != nil && !errors.Is(err, tc.cleanup) {
					t.Fatalf("cleanup failure erased: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("worker ignored transport loss")
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.invalidated == 0 || d.closed == 0 {
				t.Fatal("parent loss skipped recovery")
			}
		})
	}
}

type deathDuringTick struct {
	*fakeDriver
	cleanup error
}

func (d *deathDuringTick) Tick(Frame) (Status, error) {
	d.mu.Lock()
	d.alive = false
	d.ticks++
	d.mu.Unlock()
	return Status{}, errors.New("render HWND vanished")
}
func (d *deathDuringTick) Invalidate() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.invalidated++
	d.active = false
	if d.invalidated == 1 {
		return d.cleanup
	}
	return nil // later recovery cannot erase the first restoration failure
}
func TestWorkerParentDeathClassifiesWindowRaceButKeepsEarlierRestoreFailure(t *testing.T) {
	for _, cleanup := range []error{nil, errors.New("restore after vanished HWND failed")} {
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		d := &deathDuringTick{fakeDriver: &fakeDriver{alive: true}, cleanup: cleanup}
		done := make(chan error, 1)
		go func() {
			done <- runWorker(inR, outW, func(identity) (cursorDriver, error) { return d, nil })
			_ = outW.Close()
		}()
		enc := json.NewEncoder(inW)
		_ = enc.Encode(request{Command: "hello", Parent: identity{1, 2}})
		var ready response
		if err := json.NewDecoder(outR).Decode(&ready); err != nil {
			t.Fatal(err)
		}
		if ready.Event != "ready" {
			t.Fatal("worker did not bind before race")
		}
		_ = enc.Encode(request{Command: "frame", Sequence: 1, SentUnixNano: time.Now().UnixNano()})
		select {
		case err := <-done:
			if cleanup == nil && err != nil {
				t.Fatalf("parent HWND destruction treated as a failure:%v", err)
			}
			if cleanup != nil && !errors.Is(err, cleanup) {
				t.Fatalf("earlier restore error was erased:%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("native race blocked cleanup")
		}
		_ = inW.Close()
		_ = inR.Close()
		_ = outR.Close()
	}
}
