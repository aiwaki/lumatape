//go:build windows

package app

import (
	"errors"
	"testing"
)

func TestTerminalFailureSurvivesSuccessfulLaterCleanup(t *testing.T) {
	a := &application{}
	closeFailure := errors.New("candidate close failed")
	a.failTerminal(closeFailure, true)
	// Mirrors the final cleanup's nil result; successful cleanup must not
	// erase the reason that made rendering/normal operation unsafe.
	var runErr, cleanupErr error
	runErr = errors.Join(runErr, cleanupErr, a.terminalError)
	if !a.quit || !a.abandonGPU || !errors.Is(runErr, closeFailure) {
		t.Fatal("fatal candidate failure could be acknowledged as clean")
	}
	a.failTerminal(nil, false)
	if !errors.Is(a.terminalError, closeFailure) || !a.abandonGPU {
		t.Fatal("successful subsequent operation cleared fatal state")
	}
}

func TestTerminalHotkeyFailureRetainsEveryCause(t *testing.T) {
	a := &application{}
	first, second := errors.New("hotkey rollback failed"), errors.New("capture release failed")
	a.failTerminal(first, false)
	a.failTerminal(second, true)
	if !a.quit || !a.abandonGPU || !errors.Is(a.terminalError, first) || !errors.Is(a.terminalError, second) {
		t.Fatal("fatal shutdown discarded an earlier cause")
	}
}
