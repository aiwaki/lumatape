package pointer

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

type visibilityFault struct {
	err   error
	apply bool // A failing native operation may still change visible state.
}

type fakeVisibilityBackend struct {
	system, projection bool
	faults             map[int]visibilityFault
	calls              []string
	overlap            bool
}

func (b *fakeVisibilityBackend) change(system, visible bool) error {
	name := "projection"
	if system {
		name = "system"
	}
	b.calls = append(b.calls, fmt.Sprintf("%s:%t", name, visible))
	fault, fails := b.faults[len(b.calls)]
	if !fails || fault.apply {
		if system {
			b.system = visible
		} else {
			b.projection = visible
		}
	}
	b.overlap = b.overlap || b.system && b.projection
	return fault.err
}

func (b *fakeVisibilityBackend) setSystemCursorVisible(visible bool) error {
	return b.change(true, visible)
}
func (b *fakeVisibilityBackend) setProjectionVisible(visible bool) error {
	return b.change(false, visible)
}

func TestCursorVisibilityHandoffNeverOverlapsOnSuccess(t *testing.T) {
	b := &fakeVisibilityBackend{system: true}
	var s cursorVisibility
	for i := 0; i < 2; i++ {
		if err := s.project(b); err != nil {
			t.Fatal(err)
		}
		if !s.hidden || !s.visible || b.system || !b.projection {
			t.Fatalf("active frame %d: state=%+v backend=%+v", i, s, b)
		}
	}
	for i := 0; i < 2; i++ {
		if err := s.restore(b); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"system:false", "projection:true", "projection:true", "projection:false", "system:true", "projection:false"}
	if !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("handoff order: got %v want %v", b.calls, want)
	}
	if b.overlap || !b.system || b.projection || s.hidden || s.visible {
		t.Fatalf("unsuccessful restoration or overlapping cursors: state=%+v backend=%+v", s, b)
	}
}

func TestCursorVisibilityProjectFailureRestoresPartialSideEffects(t *testing.T) {
	for _, step := range []int{1, 2} {
		for _, apply := range []bool{false, true} {
			t.Run(fmt.Sprintf("step_%d_partial_%t", step, apply), func(t *testing.T) {
				failure := errors.New("native handoff failed")
				b := &fakeVisibilityBackend{system: true, faults: map[int]visibilityFault{step: {failure, apply}}}
				var s cursorVisibility
				if err := s.project(b); !errors.Is(err, failure) {
					t.Fatalf("lost native failure: %v", err)
				}
				if !b.system || b.projection || s.hidden || s.visible || b.overlap {
					t.Fatalf("partial operation was not restored: state=%+v backend=%+v", s, b)
				}
				if b.calls[len(b.calls)-2] != "projection:false" || b.calls[len(b.calls)-1] != "system:true" {
					t.Fatalf("failure cleanup order: %v", b.calls)
				}
			})
		}
	}
}

func TestCursorVisibilityRestoreDoesNotTrustCachedProjectionFlag(t *testing.T) {
	// Native ShowWindow succeeded, but a later readback failed before the old
	// implementation updated its cached visible flag.
	b := &fakeVisibilityBackend{projection: true}
	s := cursorVisibility{hidden: true, visible: false}
	if err := s.restore(b); err != nil {
		t.Fatal(err)
	}
	if b.overlap || b.projection || !b.system || s.hidden || s.visible {
		t.Fatalf("partially shown projection survived cleanup: state=%+v backend=%+v", s, b)
	}
}

func TestCursorVisibilityFailedRestoreRetainsOwnershipForRetry(t *testing.T) {
	for _, failProjection := range []bool{false, true} {
		for _, failSystem := range []bool{false, true} {
			if !failProjection && !failSystem {
				continue
			}
			for _, apply := range []bool{false, true} {
				t.Run(fmt.Sprintf("projection_%t_system_%t_partial_%t", failProjection, failSystem, apply), func(t *testing.T) {
					projectionErr, systemErr := errors.New("projection hide failed"), errors.New("system show failed")
					b := &fakeVisibilityBackend{projection: true, faults: map[int]visibilityFault{}}
					if failProjection {
						b.faults[1] = visibilityFault{projectionErr, apply}
					}
					if failSystem {
						b.faults[2] = visibilityFault{systemErr, apply}
					}
					s := cursorVisibility{hidden: true, visible: true}
					err := s.restore(b)
					if errors.Is(err, projectionErr) != failProjection || errors.Is(err, systemErr) != failSystem {
						t.Fatalf("restore errors not preserved: %v", err)
					}
					if s.visible != failProjection || s.hidden != failSystem {
						t.Fatalf("uncertain native state lost retry flags: %+v", s)
					}
					if !reflect.DeepEqual(b.calls, []string{"projection:false", "system:true"}) {
						t.Fatalf("projection failure prevented native restoration: %v", b.calls)
					}
					if err := s.restore(b); err != nil {
						t.Fatal(err)
					}
					if s.visible || s.hidden || b.projection || !b.system {
						t.Fatalf("retry did not restore the cursor: state=%+v backend=%+v", s, b)
					}
				})
			}
		}
	}
}

func TestCursorVisibilityProjectFailureIncludesCleanupErrors(t *testing.T) {
	showErr, hideErr, restoreErr := errors.New("show"), errors.New("hide"), errors.New("restore")
	b := &fakeVisibilityBackend{system: true, faults: map[int]visibilityFault{
		2: {showErr, true},
		3: {hideErr, false},
		4: {restoreErr, false},
	}}
	var s cursorVisibility
	err := s.project(b)
	for _, want := range []error{showErr, hideErr, restoreErr} {
		if !errors.Is(err, want) {
			t.Fatalf("lost failure %v in %v", want, err)
		}
	}
	if !s.hidden || !s.visible {
		t.Fatalf("failed cleanup lost ownership: %+v", s)
	}
	if err := s.restore(b); err != nil {
		t.Fatal(err)
	}
	if !b.system || b.projection || s.hidden || s.visible {
		t.Fatalf("retry did not finish failed activation: state=%+v backend=%+v", s, b)
	}
}
