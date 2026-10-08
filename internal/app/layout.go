package app

import (
	"fmt"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

type presentation struct {
	Bounds, Area geometry.Rect
	SourceUV     geometry.UVRect
}

// calculatePresentation deliberately keeps input geometry separate from styling.
// The click-through surface always covers the real source client. Keyboard /
// gamepad permits resampling within it, not drawing a larger, non-interactive
// copy above the desktop. In exact mouse mode even a 4:3 mask only clips pixels;
// it never resamples them.
func calculatePresentation(c config.Config, source, monitor geometry.Rect) (presentation, error) {
	if err := source.Validate(); err != nil {
		return presentation{}, err
	}
	if err := monitor.Validate(); err != nil {
		return presentation{}, fmt.Errorf("monitor: %w", err)
	}
	p := presentation{Bounds: source, Area: source, SourceUV: geometry.UVRect{W: 1, H: 1}}
	if c.Mode == "full" && c.InputMode == "keyboard-gamepad" {
		if c.Aspect.Enabled {
			var err error
			p.Area, err = geometry.Centered4x3(source)
			if err != nil {
				return presentation{}, err
			}
		}
		m, e := geometry.Layout(p.Area, source.Size(), c.Aspect.SourceDAR, c.Aspect.Scale)
		if e != nil {
			return presentation{}, e
		}
		p.Area = m.Viewport
		p.SourceUV = m.SourceUV
	} else {
		if c.Mode == "full" && (c.Aspect.SourceDAR != 0 || c.Aspect.Scale != geometry.Fit) {
			return presentation{}, fmt.Errorf(locale.Text("DAR/Crop/Stretch требуют произвольных искажений; точные клики сохраняют физические пиксели источника", "DAR/crop/stretch require arbitrary distortion; accurate clicks preserve physical source pixels"))
		}
		// A window-format transaction already changed the actual client size.
		// Keep its decorations and the surrounding desktop uncovered.
		if c.Aspect.Enabled && c.Aspect.Method == "mask" {
			var e error
			p.Area, e = geometry.Centered4x3(source)
			if e != nil {
				return presentation{}, e
			}
			p.SourceUV = geometry.UVRect{X: float64(p.Area.X-source.X) / float64(source.W), Y: float64(p.Area.Y-source.Y) / float64(source.H), W: float64(p.Area.W) / float64(source.W), H: float64(p.Area.H) / float64(source.H)}
		}
	}
	p.Area.X -= p.Bounds.X
	p.Area.Y -= p.Bounds.Y
	return p, nil
}
