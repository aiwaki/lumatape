//go:build windows

package app

import (
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func TestOffscreenProjectionPausesWithoutDisablingAndCanResume(t *testing.T) {
	cfg := config.Default()
	cfg.Mode, cfg.Target.Kind, cfg.Screen.Shape = "full", "window", config.ShapeConvex
	cfg.InputMode, cfg.Aspect.SourceDAR = "keyboard-gamepad", 4.0/3
	monitor := geometry.Rect{W: 1920, H: 1080}
	a := &application{cfg: cfg, monitors: []win32.Monitor{{Bounds: monitor}}}
	source := geometry.Rect{X: -100, Y: 100, W: 960, H: 720}
	p, err := calculatePresentation(cfg, source, monitor)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := a.pauseProjectedSourceOutsideDesktop(p, source.Size())
	if err != nil || !paused || a.phase != "paused-offscreen" || !a.cfg.Enabled || a.suspended || a.pointerSession != nil {
		t.Fatalf("offscreen pause changed intent or started cursor ownership: paused=%v error=%v phase=%s enabled=%v", paused, err, a.phase, a.cfg.Enabled)
	}
	source.X = 100
	p, err = calculatePresentation(cfg, source, monitor)
	if err != nil {
		t.Fatal(err)
	}
	if paused, err = a.pauseProjectedSourceOutsideDesktop(p, source.Size()); err != nil || paused {
		t.Fatalf("moving the source onscreen still requires manual re-enable: paused=%v error=%v", paused, err)
	}
}

func TestOffscreenProjectionAllowsAdjacentDisplaysAndLeavesFlatUnchanged(t *testing.T) {
	cfg := config.Default()
	cfg.Mode, cfg.Target.Kind, cfg.Screen.Shape = "full", "window", config.ShapeConvex
	monitor := geometry.Rect{W: 1920, H: 1080}
	source := geometry.Rect{X: -100, Y: 100, W: 960, H: 720}
	p, err := calculatePresentation(cfg, source, monitor)
	if err != nil {
		t.Fatal(err)
	}
	a := &application{cfg: cfg, monitors: []win32.Monitor{{Bounds: monitor}, {Bounds: geometry.Rect{X: -1920, W: 1920, H: 1080}}}}
	if paused, err := a.pauseProjectedSourceOutsideDesktop(p, source.Size()); err != nil || paused {
		t.Fatalf("adjacent monitor is reachable: paused=%v error=%v", paused, err)
	}
	a.monitors = a.monitors[:1]
	for _, shape := range []string{config.ShapeFlat, config.ShapeRounded} {
		a.cfg.Screen.Shape = shape
		if paused, err := a.pauseProjectedSourceOutsideDesktop(p, source.Size()); err != nil || paused {
			t.Fatalf("unprojected %s cursor was paused: paused=%v error=%v", shape, paused, err)
		}
	}
}
