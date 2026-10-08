//go:build windows

package app

import (
	"syscall"
	"testing"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func TestMissingSourcePrecedesFocusAndMonitorResolution(t *testing.T) {
	for _, closed := range []bool{false, true} {
		cfg := config.Default()
		cfg.Target.Kind, cfg.Enabled = "window", true
		a := &application{cfg: cfg, phase: "paused-settings", tray: &win32.Tray{}}
		if closed {
			// Invalid HWND: no messages or input are sent to another process.
			a.target = win32.Window{Handle: ^uintptr(0), PID: ^uint32(0)}
		}
		// No monitors or render surface: a missing/closed source must be
		// diagnosed before either is needed, even after a tray focus pause.
		if err := a.frame(); err != nil {
			t.Fatalf("closed=%v: %v", closed, err)
		}
		if a.phase != "waiting-source" || a.target.Handle != 0 || a.cfg != cfg || a.visible {
			t.Fatalf("closed=%v: unexpected phase %q or changed source intent", closed, a.phase)
		}
		if status := a.RuntimeStatus(); status.EffectActive || status.SurfaceVisible {
			t.Fatal("missing source reported an active effect")
		}
		if err := a.frame(); err != nil || a.phase != "waiting-source" {
			t.Fatalf("repeated missing-source frame changed state: %v", err)
		}
	}
}

func TestDisabledEffectDoesNotAskForSource(t *testing.T) {
	cfg := config.Default()
	cfg.Target.Kind, cfg.Enabled, cfg.Aspect.Enabled = "window", false, false
	a := &application{cfg: cfg}
	if err := a.frame(); err != nil || a.phase != "disabled" {
		t.Fatalf("disabled startup asked for a source: phase=%q err=%v", a.phase, err)
	}
}

func TestSuspendedFailurePrecedesMissingSource(t *testing.T) {
	cfg := config.Default()
	cfg.Target.Kind, cfg.Enabled = "window", true
	a := &application{cfg: cfg, suspended: true, phase: "error"}
	if err := a.frame(); err != nil || a.phase != "error" {
		t.Fatalf("missing source masked a suspended failure: phase=%q err=%v", a.phase, err)
	}
}

func TestMissingSourcePrecedesControllerForeground(t *testing.T) {
	// Observe the current foreground only; do not activate or send it input.
	hwnd := win32.Foreground()
	var pid uint32
	syscall.NewLazyDLL("user32.dll").NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if hwnd == 0 || pid == 0 {
		t.Skip("requires a desktop with an existing foreground window")
	}
	cfg := config.Default()
	cfg.Target.Kind, cfg.Enabled = "window", true
	a := &application{cfg: cfg, controllerPID: pid}
	if err := a.frame(); err != nil || a.phase != "waiting-source" {
		t.Fatalf("controller focus masked a missing source: phase=%q err=%v", a.phase, err)
	}
}
