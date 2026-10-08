package display

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

var (
	originalMode = Mode{Width: 1920, Height: 1080, RefreshHz: 144, BitsPerPixel: 32}
	targetMode   = Mode{Width: 1440, Height: 1080, RefreshHz: 144, BitsPerPixel: 32}
	externalMode = Mode{Width: 1280, Height: 720, RefreshHz: 60, BitsPerPixel: 32}
)

type fakeDriver struct {
	mu        sync.Mutex
	state     snapshot
	changes   []snapshot
	testError error
	afterTest func()
}

func freshDriver() *fakeDriver {
	return &fakeDriver{state: snapshot{fingerprint: fingerprint{Mode: originalMode, X: -1920}, native: []byte("complete original state + private driver data")}}
}
func (f *fakeDriver) current() (snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *fakeDriver) resolve(m Mode, s snapshot) (snapshot, error) {
	if m != targetMode {
		return snapshot{}, errors.New("unlisted mode")
	}
	s.Mode = m
	s.native = []byte("enumerated target state")
	return s, nil
}
func (f *fakeDriver) test(snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.afterTest != nil {
		f.afterTest()
	}
	return f.testError
}
func (f *fakeDriver) apply(s snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
	f.changes = append(f.changes, s)
	return nil
}
func (f *fakeDriver) external(m Mode) { f.mu.Lock(); defer f.mu.Unlock(); f.state.Mode = m }

func TestModeSelection(t *testing.T) {
	modes := []Mode{
		{Width: 1280, Height: 1024, RefreshHz: 144, BitsPerPixel: 32},
		{Width: 1600, Height: 1200, RefreshHz: 60, BitsPerPixel: 32},
		{Width: 1024, Height: 768, RefreshHz: 144, BitsPerPixel: 32},
		targetMode,
		{Width: 1920, Height: 1440, RefreshHz: 144, BitsPerPixel: 32},
	}
	got, err := Choose43(modes, originalMode)
	if err != nil || got != targetMode {
		t.Fatalf("got %v, %v", got, err)
	}
	if modes[0].Is43() {
		t.Fatal("5:4 was classified as 4:3")
	}
	t.Run("refresh preserved even above desktop size", func(t *testing.T) {
		want := Mode{Width: 1600, Height: 1200, RefreshHz: 144, BitsPerPixel: 32}
		got, err := Choose43([]Mode{{Width: 1024, Height: 768, RefreshHz: 60, BitsPerPixel: 32}, want}, originalMode)
		if err != nil || got != want {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("no unsafe candidates", func(t *testing.T) {
		if _, err := Choose43([]Mode{{Width: 1024, Height: 768, RefreshHz: 60, BitsPerPixel: 16}, {Width: 1024, Height: 768, RefreshHz: 60, BitsPerPixel: 32, DisplayFlags: 2}}, originalMode); err == nil {
			t.Fatal("accepted interlaced/16-bit mode")
		}
	})
}

func TestGuardRestoresOnUnconfirmedTimeout(t *testing.T) {
	d := freshDriver()
	g := guard{driver: d}
	now := time.Unix(100, 0)
	if err := g.start(targetMode, now, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	finished, err := g.tick(g.deadline)
	if !finished || err != nil || d.state.Mode != originalMode {
		t.Fatalf("finished=%v err=%v mode=%v", finished, err, d.state.Mode)
	}
	if !bytes.Equal(d.state.native, []byte("complete original state + private driver data")) {
		t.Fatal("original private state lost")
	}
}

func TestConfirmedModeSurvivesDeadlineButRestoresOnExit(t *testing.T) {
	d := freshDriver()
	g := guard{driver: d}
	now := time.Unix(100, 0)
	if err := g.start(targetMode, now, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := g.confirm(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if finished, err := g.tick(now.Add(time.Hour)); finished || err != nil {
		t.Fatalf("confirmed session stopped: %v", err)
	}
	if err := g.restore(); err != nil {
		t.Fatal(err)
	}
	if err := g.restore(); err != nil {
		t.Fatal(err)
	}
	if len(d.changes) != 2 || d.state.Mode != originalMode {
		t.Fatalf("changes=%v", d.changes)
	}
}

func TestGuardDoesNotRestoreOverExternalChange(t *testing.T) {
	for _, returnToTarget := range []bool{false, true} {
		t.Run(map[bool]string{false: "different mode", true: "observed change then same target"}[returnToTarget], func(t *testing.T) {
			d := freshDriver()
			g := guard{driver: d}
			if err := g.start(targetMode, time.Now(), 15*time.Second); err != nil {
				t.Fatal(err)
			}
			d.external(externalMode)
			if err := g.observe(); err != nil {
				t.Fatal(err)
			}
			if returnToTarget {
				d.external(targetMode)
			}
			if err := g.restore(); err != nil {
				t.Fatal(err)
			}
			if len(d.changes) != 1 || !g.relinquished {
				t.Fatal("overwrote an independent change")
			}
		})
	}
}

func TestGuardDoesNotRestoreOverChangedMonitorPosition(t *testing.T) {
	d := freshDriver()
	g := guard{driver: d}
	if err := g.start(targetMode, time.Now(), 15*time.Second); err != nil {
		t.Fatal(err)
	}
	d.state.X = 0
	if err := g.restore(); err != nil {
		t.Fatal(err)
	}
	if len(d.changes) != 1 {
		t.Fatal("overwrote monitor layout")
	}
}

func TestGuardNoSideEffectOnFailedTestOrConcurrentChange(t *testing.T) {
	for _, testFailure := range []bool{false, true} {
		d := freshDriver()
		g := guard{driver: d}
		if testFailure {
			d.testError = errors.New("unsupported")
		} else {
			d.afterTest = func() { d.state.Mode = externalMode }
		}
		if err := g.start(targetMode, time.Now(), 15*time.Second); err == nil {
			t.Fatal("expected refusal")
		}
		if len(d.changes) != 0 {
			t.Fatal("changed display after failed preparation")
		}
	}
}

func TestRestoreRechecksAfterTestingOriginalMode(t *testing.T) {
	d := freshDriver()
	g := guard{driver: d}
	if err := g.start(targetMode, time.Now(), 15*time.Second); err != nil {
		t.Fatal(err)
	}
	d.afterTest = func() { d.state.Mode = externalMode }
	if err := g.restore(); err != nil {
		t.Fatal(err)
	}
	if len(d.changes) != 1 || d.state.Mode != externalMode {
		t.Fatal("overwrote a change during restoration CDS_TEST")
	}
}

type refusedRestoreDriver struct {
	*fakeDriver
	wrongState snapshot
}

func (d *refusedRestoreDriver) apply(s snapshot) error {
	if s.Mode == originalMode {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.state = d.wrongState
		d.changes = append(d.changes, s)
		return nil // a success response is not proof of the resulting mode
	}
	return d.fakeDriver.apply(s)
}

func TestRestorationRequiresActualOriginalDisplayState(t *testing.T) {
	for _, external := range []bool{false, true} {
		d := &refusedRestoreDriver{fakeDriver: freshDriver()}
		g := guard{driver: d}
		if err := g.start(targetMode, time.Now(), time.Second); err != nil {
			t.Fatal(err)
		}
		d.wrongState = g.applied
		if external {
			d.wrongState.Mode = externalMode
		}
		if err := g.restore(); err == nil {
			t.Fatal("unverified restoration was reported as success")
		}
		if external {
			if g.active || !g.relinquished {
				t.Fatal("unexpected driver state must not be overwritten on cleanup")
			}
		} else if !g.active {
			t.Fatal("failed restoration lost ownership of unchanged applied state")
		}
	}
}

func TestLateConfirmationRestores(t *testing.T) {
	d := freshDriver()
	g := guard{driver: d}
	now := time.Now()
	if err := g.start(targetMode, now, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := g.confirm(g.deadline); err == nil {
		t.Fatal("late confirmation accepted")
	}
	if d.state.Mode != originalMode {
		t.Fatal("late confirmation left changed mode")
	}
}

func TestWatchdogParentPipeLossRestores(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before confirmation", true: "after confirmation"}[confirmed], func(t *testing.T) {
			d := freshDriver()
			input, writer := io.Pipe()
			output, reader := io.Pipe()
			defer input.Close()
			defer reader.Close()
			defer output.Close()
			done := make(chan error, 1)
			go func() { done <- runWatchdog(input, reader, func(string) (driver, error) { return d, nil }) }()
			decode := json.NewDecoder(output)
			read := func(event string) {
				t.Helper()
				var r response
				if err := decode.Decode(&r); err != nil || r.Event != event || r.Error != "" {
					t.Fatalf("%s: %#v %v", event, r, err)
				}
			}
			read("ready")
			encoder := json.NewEncoder(writer)
			if err := encoder.Encode(request{Command: "apply", Device: `\\.\DISPLAY1`, Mode: targetMode, TimeoutMS: 15000}); err != nil {
				t.Fatal(err)
			}
			read("applied")
			if confirmed {
				if err := encoder.Encode(request{Command: "confirm"}); err != nil {
					t.Fatal(err)
				}
				read("confirmed")
			}
			// A crashed process loses both pipe endpoints. Its watchdog can
			// restore even though nobody remains to receive the terminal reply.
			output.Close()
			writer.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("watchdog did not react to pipe EOF")
			}
			state, _ := d.current()
			if state.Mode != originalMode {
				t.Fatal("parent crash left temporary mode")
			}
		})
	}
}
