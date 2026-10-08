package app

import (
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

// projectedPointerMap describes the frame actually submitted to the renderer.
// Only the built-in screen deformation has a known inverse. A custom RGB-only
// warp cannot describe where its interactive content went.
func projectedPointerMap(c config.Config, p presentation, source geometry.Size, coordinates string, frameTime float64) (geometry.PointerMap, bool) {
	e, screen := c.Effective(), c.EffectiveScreen()
	if c.Mode != "full" || c.Target.Kind != "window" || !c.Enabled || e.Intensity <= 0 ||
		screen.Shape != config.ShapeConvex || screen.Curvature <= 0 ||
		(c.Shader.ID != "" && coordinates != "preserve") {
		return geometry.PointerMap{}, false
	}
	// Match the control uniform precision. Time belongs to the displayed frame, never
	// the independently animated cursor worker's clock.
	f32 := func(v float64) float64 { return float64(float32(v)) }
	m := geometry.PointerMap{
		Bounds: p.Bounds, Area: p.Area, SourceSize: source,
		SourceUV:  p.SourceUV,
		Curvature: f32(screen.Curvature), CornerRadius: f32(screen.CornerRadius), Time: f32(frameTime),
	}
	if c.Shader.ID == "" {
		m.Jitter, m.Tracking = f32(e.VHS.Jitter), f32(e.VHS.Tracking)
	}
	if e.FreezeNoise {
		m.Time = 0
	}
	return m, true
}
