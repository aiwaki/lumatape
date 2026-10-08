//go:build windows

package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreWindowRetriesStartupRecoveryWithoutLiveTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-recovery.json")
	a := &application{windowRecoveryPath: path, windowRecoveryError: errors.New("startup recovery failed")}
	// A missing journal is already recovered; retry must clear the stale error
	// that otherwise keeps the tray in recovery_pending indefinitely.
	if err := a.restoreWindow(); err != nil || a.windowRecoveryError != nil {
		t.Fatalf("completed recovery stayed pending: returned=%v pending=%v", err, a.windowRecoveryError)
	}
	if err := a.restoreWindow(); err != nil {
		t.Fatalf("repeated restore must be harmless: %v", err)
	}
}

func TestRestoreWindowRetainsFailedStartupRecoveryJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-recovery.json")
	data := []byte("unfinished recovery journal")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	a := &application{windowRecoveryPath: path, windowRecoveryError: errors.New("startup recovery failed")}
	for range 2 {
		if err := a.restoreWindow(); err == nil || a.windowRecoveryError == nil {
			t.Fatal("failed recovery lost its retryable error")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(data) {
			t.Fatalf("failed retry changed recovery journal: %q, %v", got, err)
		}
	}
	// Simulate the recovery being completed externally, then retry the same
	// instance. No native window mutation is needed for these journal outcomes.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreWindow(); err != nil || a.windowRecoveryError != nil {
		t.Fatalf("successful retry did not clear recovery: returned=%v pending=%v", err, a.windowRecoveryError)
	}
}
