package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
)

func TestFirstDesktopProfileStartsDisabledWithoutChangingCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-profile", "config.json")
	want := config.Default()
	for _, controlled := range []bool{false, true} {
		got, err := loadStartupConfig(path, controlled)
		if err != nil {
			t.Fatal(err)
		}
		want.Enabled = !controlled
		if controlled {
			want.Capture.Transfer = config.TransferAuto
			want.Hotkeys = config.Hotkeys{Toggle: "Ctrl+Shift+9", Emergency: "Ctrl+Shift+0"}
		}
		if got != want {
			t.Fatalf("controlled=%v unexpected first profile: got=%+v want=%+v", controlled, got, want)
		}
	}
	// Loading does not create a profile before the normal startup save boundary.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("loading unexpectedly wrote a profile: %v", err)
	}
	legacy, err := config.Load(path)
	if err != nil || legacy != config.Default() {
		t.Fatalf("legacy CLI/reload missing-file defaults changed: %+v, %v", legacy, err)
	}
}

func TestDesktopStartupPreservesExistingProfilesAndErrors(t *testing.T) {
	for _, data := range []string{`{}`, `{"enabled":true}`, `{"enabled":false}`, `{"enabled":true,"preset":"VHS Tape","hotkeys":{"toggle":"Ctrl+Shift+9","emergency":"Ctrl+Shift+0"}}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		want, err := config.Decode([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, controlled := range []bool{false, true} {
			got, err := loadStartupConfig(path, controlled)
			if err != nil || got != want {
				t.Fatalf("existing profile changed for controlled=%v: %s, got=%+v err=%v", controlled, data, got, err)
			}
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != data {
			t.Fatalf("startup rewrote existing profile: %q, %v", after, err)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"enabled":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadStartupConfig(path, true); err == nil {
		t.Fatal("corrupt existing desktop profile was silently replaced with defaults")
	}
	if _, err := loadStartupConfig(t.TempDir(), true); err == nil {
		t.Fatal("unreadable profile directory was treated as missing")
	}
}
