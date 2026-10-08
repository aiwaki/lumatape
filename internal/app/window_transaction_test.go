package app

import (
	"errors"
	"testing"

	"github.com/aiwaki/lumatape/internal/geometry"
)

var testWindowError = errors.New("injected window failure")

type fakeWindowMutation struct {
	outer, client                                            geometry.Rect
	snapshotErr, resizeErr, boundsErr, clientErr, restoreErr bool
	refuse                                                   bool
	mutations, restores                                      int
}

func (f *fakeWindowMutation) Snapshot() (geometry.Rect, func() error, error) {
	if f.snapshotErr {
		return geometry.Rect{}, nil, testWindowError
	}
	original, client := f.outer, f.client
	return original, func() error {
		f.restores++
		if f.restoreErr {
			return testWindowError
		}
		f.outer, f.client = original, client
		return nil
	}, nil
}
func (f *fakeWindowMutation) ResizeClient(r geometry.Rect) error {
	f.mutations++
	f.client = r
	if f.refuse {
		f.client.W -= 4
	}
	f.outer = geometry.Rect{X: r.X - 8, Y: r.Y - 31, W: r.W + 16, H: r.H + 39}
	if f.resizeErr {
		return testWindowError
	}
	return nil
}
func (f *fakeWindowMutation) Bounds() (geometry.Rect, error) {
	if f.boundsErr {
		return geometry.Rect{}, testWindowError
	}
	return f.outer, nil
}
func (f *fakeWindowMutation) ClientBounds() (geometry.Rect, error) {
	if f.clientErr {
		return geometry.Rect{}, testWindowError
	}
	return f.client, nil
}
func freshWindowMutation() *fakeWindowMutation {
	return &fakeWindowMutation{outer: geometry.Rect{X: 100, Y: 100, W: 800, H: 639}, client: geometry.Rect{X: 108, Y: 131, W: 784, H: 600}}
}

func TestWindowTransactionRollsBackEveryPartialFailure(t *testing.T) {
	for _, stage := range []string{"snapshot", "resize", "bounds", "client", "refused"} {
		t.Run(stage, func(t *testing.T) {
			f := freshWindowMutation()
			original := f.outer
			switch stage {
			case "snapshot":
				f.snapshotErr = true
			case "resize":
				f.resizeErr = true
			case "bounds":
				f.boundsErr = true
			case "client":
				f.clientErr = true
			case "refused":
				f.refuse = true
			}
			tx, err := beginWindowTransaction(f, geometry.Rect{X: 10, Y: 31, W: 960, H: 720})
			if err == nil || tx != nil || f.outer != original {
				t.Fatalf("partial failure left a changed window: stage=%s tx=%+v err=%v actual=%+v", stage, tx, err, f.outer)
			}
			if stage == "snapshot" {
				if f.mutations != 0 || f.restores != 0 {
					t.Fatal("snapshot failure changed window")
				}
			} else if f.restores != 1 {
				t.Fatal("failed mutation was not restored exactly once")
			}
		})
	}
}

func TestWindowTransactionRestoreIsIdempotentAndRespectsExternalMove(t *testing.T) {
	for _, external := range []bool{false, true} {
		f := freshWindowMutation()
		original := f.outer
		tx, err := beginWindowTransaction(f, geometry.Rect{X: -1000, Y: 31, W: 960, H: 720})
		if err != nil {
			t.Fatal(err)
		}
		if external {
			f.outer.X -= 100
		}
		observed := f.outer
		if err = tx.Restore(); err != nil {
			t.Fatal(err)
		}
		if err = tx.Restore(); err != nil {
			t.Fatal(err)
		}
		if external {
			if f.outer != observed || f.restores != 0 {
				t.Fatal("overwrote external window move")
			}
		} else if f.outer != original || f.restores != 1 {
			t.Fatal("original placement was not restored exactly once")
		}
	}
}

func TestWindowRollbackFailureRetainsRecoveryState(t *testing.T) {
	f := freshWindowMutation()
	original := f.outer
	f.clientErr, f.restoreErr = true, true
	tx, err := beginWindowTransaction(f, geometry.Rect{X: 10, Y: 31, W: 960, H: 720})
	if err == nil || tx == nil || !tx.active || !tx.recoveryOnly {
		t.Fatal("lost recovery token after failed rollback")
	}
	f.restoreErr = false
	if err = tx.Restore(); err != nil || tx.active || f.outer != original {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestUnknownPartialRecoveryDoesNotOverwriteExternalState(t *testing.T) {
	f := freshWindowMutation()
	original := f.outer
	f.boundsErr, f.restoreErr = true, true
	tx, err := beginWindowTransaction(f, geometry.Rect{X: 10, Y: 31, W: 960, H: 720})
	if err == nil || tx == nil {
		t.Fatal("expected recovery failure")
	}
	f.boundsErr, f.restoreErr = false, false
	f.outer.X = 777
	if err = tx.Restore(); err == nil || f.outer.X != 777 {
		t.Fatal("unknown mutation overwrote external state")
	}
	f.outer = original // user restores the window manually
	if err = tx.Restore(); err != nil || tx.active {
		t.Fatalf("manual restoration was not recognized: %v", err)
	}
}

func TestLaterRestorationFailureRetainsRecoveryAttention(t *testing.T) {
	f := freshWindowMutation()
	tx, err := beginWindowTransaction(f, geometry.Rect{W: 960, H: 720})
	if err != nil {
		t.Fatal(err)
	}
	f.restoreErr = true
	if err := tx.Restore(); err == nil || !tx.active || !tx.recoveryOnly {
		t.Fatal("failed restoration lost visible recovery state")
	}
	f.restoreErr = false
	if err := tx.Restore(); err != nil || tx.active {
		t.Fatalf("recovery failed: %v", err)
	}
}

func TestInvalidWindowFormatNeverMutatesSource(t *testing.T) {
	for _, r := range []geometry.Rect{{}, {W: 1280, H: 1024}, {W: 1920, H: 1080}} {
		f := freshWindowMutation()
		if tx, err := beginWindowTransaction(f, r); err == nil || tx != nil || f.mutations != 0 || f.restores != 0 {
			t.Fatalf("invalid 4:3 request mutated source: %+v", r)
		}
	}
}
