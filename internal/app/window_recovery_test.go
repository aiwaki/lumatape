package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiwaki/lumatape/internal/geometry"
)

func recoveryFixture() windowRecoveryToken {
	return windowRecoveryToken{Version: 1, Owner: recoveryProcess{100, 1000}, Target: recoveryProcess{200, 2000}, Window: 1234, Nonce: 5678,
		Original: geometry.Rect{X: 100, Y: 100, W: 800, H: 639}, Client: geometry.Rect{X: -1200, Y: 31, W: 960, H: 720}, Placement: recoveryPlacement{ShowCommand: 1, Normal: [4]int32{100, 100, 900, 739}}}
}

func TestRecoveryJournalAtomicRoundTripAndMalformedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "window-recovery.json")
	want := recoveryFixture()
	if err := writeWindowRecovery(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := readWindowRecovery(path)
	if err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	invalid := want
	invalid.Nonce = 0
	if err := writeWindowRecovery(path, invalid); err == nil {
		t.Fatal("saved invalid token")
	}
	got, err = readWindowRecovery(path)
	if err != nil || got != want {
		t.Fatal("invalid save destroyed existing recovery")
	}
	want.Applied = true
	if err := writeWindowRecovery(path, want); err != nil {
		t.Fatal(err)
	}
	got, err = readWindowRecovery(path)
	if err != nil || got != want {
		t.Fatal("atomic replacement failed")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary files leaked")
	}
	for _, data := range []string{"{}", "{", `{"Version":1,"Unexpected":true}`, strings.Repeat(" ", 65537), "null", `{} {}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowRecovery(path); err == nil {
			t.Fatalf("accepted malformed recovery: %.64q", data)
		}
	}
}

func TestRecoveryNeverOverwritesReusedIdentityOrExternalGeometry(t *testing.T) {
	token := recoveryFixture()
	changed := geometry.Rect{X: -1208, Y: 0, W: 976, H: 759}
	for _, applied := range []bool{false, true} {
		token.Applied = applied
		for _, ownerAlive := range []bool{false, true} {
			for _, targetSame := range []bool{false, true} {
				for _, markerSame := range []bool{false, true} {
					for _, geometryCase := range []string{"original", "ours", "external"} {
						outer, client := changed, token.Client
						if geometryCase == "original" {
							outer = token.Original
						}
						if geometryCase == "external" {
							client.X++
						}
						decision, err := decideWindowRecovery(token, ownerAlive, targetSame, markerSame, outer, client)
						want := recoveryForget
						if ownerAlive {
							want = recoveryKeep
						} else if targetSame && markerSame && geometryCase != "original" {
							if geometryCase == "ours" {
								want = recoveryRestore
							} else if !applied {
								want = recoveryKeep
							}
						}
						if decision != want || (err != nil) != (want == recoveryKeep) {
							t.Fatalf("applied=%v owner=%v target=%v marker=%v geometry=%s: %s %v", applied, ownerAlive, targetSame, markerSame, geometryCase, decision, err)
						}
					}
				}
			}
		}
	}
}

type durableWindowFake struct {
	*fakeWindowMutation
	prepared, committed, cleared    bool
	prepareErr, commitErr, clearErr bool
}

func (f *durableWindowFake) Prepare(geometry.Rect) error {
	if f.mutations != 0 {
		panic("journal prepared after mutation")
	}
	f.prepared = true
	if f.prepareErr {
		return testWindowError
	}
	return nil
}
func (f *durableWindowFake) Commit() error {
	f.committed = true
	if f.commitErr {
		return testWindowError
	}
	return nil
}
func (f *durableWindowFake) Clear() error {
	if f.clearErr {
		return testWindowError
	}
	f.cleared = true
	return nil
}
func (f *durableWindowFake) ResizeClient(r geometry.Rect) error {
	if !f.prepared {
		panic("mutation without durable token")
	}
	return f.fakeWindowMutation.ResizeClient(r)
}

func TestDurableWindowTransactionFailuresAndOwnership(t *testing.T) {
	for _, stage := range []string{"prepare", "resize", "commit", "clear", "success", "external"} {
		t.Run(stage, func(t *testing.T) {
			f := &durableWindowFake{fakeWindowMutation: freshWindowMutation()}
			original := f.outer
			switch stage {
			case "prepare":
				f.prepareErr = true
			case "resize":
				f.resizeErr = true
			case "commit":
				f.commitErr = true
			case "clear":
				f.clearErr = true
			}
			tx, err := beginWindowTransaction(f, geometry.Rect{W: 960, H: 720})
			if stage == "prepare" || stage == "resize" || stage == "commit" {
				if err == nil || tx != nil || !f.cleared || f.outer != original {
					t.Fatalf("durable rollback failed: %+v %v", tx, err)
				}
				if stage == "prepare" && (f.mutations != 0 || f.restores != 0) {
					t.Fatal("journal failure mutated source")
				}
				return
			}
			if err != nil || !f.committed {
				t.Fatalf("prepare failed: %v", err)
			}
			if stage == "external" {
				f.outer.X++
			}
			err = tx.Restore()
			if stage == "clear" {
				if err == nil || !tx.active || !tx.recoveryOnly {
					t.Fatal("clear failure lost recovery")
				}
				f.clearErr = false
				if err = tx.Restore(); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !f.cleared || tx.active {
				t.Fatal("finished recovery kept token")
			}
			if stage == "external" && f.restores != 0 {
				t.Fatal("external move overwritten")
			}
		})
	}
}
