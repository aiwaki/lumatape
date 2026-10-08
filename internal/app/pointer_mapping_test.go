package app

import (
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestProjectedPointerEligibilityAndDisplayedFrame(t *testing.T) {
	c := config.Default()
	c.Mode, c.Target.Kind, c.Screen.Shape = "full", "window", config.ShapeConvex
	p := presentation{Bounds: geometry.Rect{X: -960, Y: 60, W: 960, H: 720}, Area: geometry.Rect{W: 960, H: 720}, SourceUV: geometry.UVRect{W: 1, H: 1}}
	source := p.Bounds.Size()
	for _, input := range []string{"mouse-exact", "keyboard-gamepad"} {
		c.InputMode = input
		m, ok := projectedPointerMap(c, p, source, "", 12.25)
		if !ok || m.Curvature == 0 || m.Time != 12.25 {
			t.Fatalf("convex pointer unavailable for %s: %+v", input, m)
		}
		point, ok := m.SourceToScreen(40, 40)
		if !ok || point.X <= -920 || point.Y <= 100 {
			t.Fatalf("cursor was not projected inward with the image: %+v", point)
		}
	}
	c.Effects.VHS.Jitter, c.Effects.VHS.Tracking = .6, .4
	m, _ := projectedPointerMap(c, p, source, "", 11.2)
	if m.Jitter == 0 || m.Tracking == 0 || m.Time != float64(float32(11.2)) {
		t.Fatal("built-in temporal geometry lost its displayed-frame parameters")
	}
	c.Effects.FreezeNoise = true
	m, _ = projectedPointerMap(c, p, source, "", 100)
	if m.Time != 0 {
		t.Fatal("frozen shader used the live cursor clock")
	}
	c.Shader.ID = "custom"
	for _, coordinates := range []string{"", "warp"} {
		if _, ok := projectedPointerMap(c, p, source, coordinates, 0); ok {
			t.Fatal("unknown custom warp was treated as an invertible mapping")
		}
	}
	m, ok := projectedPointerMap(c, p, source, "preserve", 0)
	if !ok || m.Jitter != 0 || m.Tracking != 0 {
		t.Fatal("preserved custom wrapper does not contain built-in VHS deformation")
	}
	for _, change := range []func(*config.Config){
		func(c *config.Config) { c.Enabled = false },
		func(c *config.Config) { c.Effects.Intensity = 0 },
		func(c *config.Config) { c.Screen.Curvature = 0 },
		func(c *config.Config) { c.Screen.Shape = config.ShapeRounded },
		func(c *config.Config) { c.Screen.Shape = config.ShapeFlat },
		func(c *config.Config) { c.Mode = "overlay" },
	} {
		next := c
		change(&next)
		if _, ok := projectedPointerMap(next, p, source, "preserve", 0); ok {
			t.Fatalf("inactive geometry took ownership of the system cursor: %+v", next)
		}
	}
}
