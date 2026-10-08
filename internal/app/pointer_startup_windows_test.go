//go:build windows

package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func TestPointerStartupFailureDisablesAndPersistsWithoutClosingTray(t *testing.T) {
	cfg := config.Default()
	cfg.Mode, cfg.Target.Kind, cfg.Screen.Shape = "full", "window", config.ShapeConvex
	cfg.Enabled, cfg.Aspect.Enabled = true, true
	a := &application{cfg: cfg, directory: t.TempDir(), configPath: filepath.Join(t.TempDir(), "config.json"), tray: &win32.Tray{}}
	p := presentation{Bounds: geometry.Rect{W: 960, H: 720}, Area: geometry.Rect{W: 960, H: 720}, SourceUV: geometry.UVRect{W: 1, H: 1}}
	// The real Start path fails because the isolated directory has no companion.
	// This touches neither native cursor visibility nor the user's configuration.
	if err := a.updateProjectedPointer(p, p.Bounds.Size(), 0); err != nil {
		t.Fatalf("startup error escaped the recoverable frame boundary: %v", err)
	}
	if a.pointerSession != nil || !a.projectedPointerFailed() || !a.cfg.Enabled {
		t.Fatal("startup failure did not defer recovery until the frame boundary")
	}
	if err := a.recoverProjectedPointerAtBoundary(); err != nil {
		t.Fatal(err)
	}
	if a.quit || a.cfg.Enabled || a.cfg.Aspect.Enabled || !a.suspended || a.pointerFailure != nil || a.phase != "error" || a.lastError == "" {
		t.Fatal("startup failure did not leave a usable, disabled tray state")
	}
	if a.emergencyBarrier.sequence != 1 {
		t.Fatal("stale controller intent can re-enable the failed effect")
	}
	saved, err := config.Load(a.configPath)
	if err != nil || saved.Enabled || saved.Aspect.Enabled || saved.Screen.Shape != config.ShapeConvex {
		t.Fatalf("failed startup was not persisted OFF with the chosen shape intact: %+v, %v", saved, err)
	}
	if err = a.recoverProjectedPointerAtBoundary(); err != nil || a.emergencyBarrier.sequence != 1 {
		t.Fatal("the same startup failure was replayed")
	}
}

func TestMissingPointerSessionCannotConfirmAnArmedWorkerRecovery(t *testing.T) {
	a := &application{}
	cause := errors.New("armed worker failure")
	if err := a.recoverProjectedPointer(cause); !errors.Is(err, cause) {
		t.Fatalf("missing recovery session erased an unconfirmed armed failure: %v", err)
	}
}
