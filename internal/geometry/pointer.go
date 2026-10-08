package geometry

import "math"

// PointerPoint uses physical pixel coordinates, with integers at pixel centers.
// It is independent of logical/DPI-scaled window coordinates.
type PointerPoint struct{ X, Y float64 }

// PointerMap describes the same sampling transform as render/shaders/crt.frag.
// Bounds is the overlay client rectangle in physical desktop coordinates; Area
// is the displayed image rectangle relative to that client's upper-left corner.
// SourceSize is the captured client framebuffer size, not the window's outer size.
//
// Effects must already be intensity-scaled. CornerRadius is a fraction of the
// shorter Area side. Set Time to zero for frozen effects; set Jitter and Tracking
// to zero for the custom shader's preserved-coordinate contract. For the shader's
// intensity-zero bypass, pass zero for all four geometric effects.
type PointerMap struct {
	Bounds       Rect
	Area         Rect
	SourceUV     UVRect
	SourceSize   Size
	Curvature    float64
	CornerRadius float64
	Jitter       float64
	Tracking     float64
	Time         float64
}

// MapScreen returns the source-client pixel sampled at a desktop pixel. The
// half-pixel conversion matches gl_FragCoord and sampleImage's texel-center clamp:
// an identity mapping maps Bounds.X/Bounds.Y exactly to source pixel 0/0.
// Invalid mappings, black bars, warped black edges and rounded-screen coverage
// <= 0.5 return false. Color-only shader effects do not affect this mapping.
func (m PointerMap) MapScreen(x, y float64) (PointerPoint, bool) {
	if !m.valid() || !pointerFinite(x) || !pointerFinite(y) {
		return PointerPoint{}, false
	}
	// Win32 coordinates name pixels; GLSL fragment coordinates name their centers
	// measured from the framebuffer edge. Keep subpixels for the inverse and DPI
	// scaling instead of rounding until input is finally delivered to the source.
	px, py := x-float64(m.Bounds.X)+.5, y-float64(m.Bounds.Y)+.5
	if px < 0 || py < 0 || px >= float64(m.Bounds.W) || py >= float64(m.Bounds.H) {
		return PointerPoint{}, false
	}
	qx := (px - float64(m.Area.X)) / float64(m.Area.W)
	qy := (py - float64(m.Area.Y)) / float64(m.Area.H)
	if qx < 0 || qx >= 1 || qy < 0 || qy >= 1 || !m.covered(px, py) {
		return PointerPoint{}, false
	}
	cx, cy := qx*2-1, qy*2-1
	factor := 1 + m.Curvature*.09*(cx*cx+cy*cy)
	qx, qy = (cx*factor+1)*.5, (cy*factor+1)*.5
	qx += m.temporalShift(qy)
	if !pointerFinite(qx) || qx < 0 || qx > 1 || qy < 0 || qy > 1 {
		return PointerPoint{}, false
	}
	return PointerPoint{
		X: pointerClamp((m.SourceUV.X+qx*m.SourceUV.W)*float64(m.SourceSize.W)-.5, 0, float64(m.SourceSize.W-1)),
		Y: pointerClamp((m.SourceUV.Y+qy*m.SourceUV.H)*float64(m.SourceSize.H)-.5, 0, float64(m.SourceSize.H-1)),
	}, true
}

// SourceToScreen locates a source-client pixel on the displayed image at Time.
// It returns false for pixels outside the visible source crop or screen shape.
// The radial transform is monotonic for nonnegative curvature, so a bounded
// bisection provides a stable inverse without a Newton iteration near an edge.
//
// Texel clamping makes the forward transform many-to-one at source edges. This
// method chooses the texel-center preimage; it does not recover every original
// screen position that was clamped to that source pixel.
func (m PointerMap) SourceToScreen(x, y float64) (PointerPoint, bool) {
	if !m.valid() || !pointerFinite(x) || !pointerFinite(y) ||
		x < 0 || y < 0 || x > float64(m.SourceSize.W-1) || y > float64(m.SourceSize.H-1) {
		return PointerPoint{}, false
	}
	qx := ((x+.5)/float64(m.SourceSize.W) - m.SourceUV.X) / m.SourceUV.W
	qy := ((y+.5)/float64(m.SourceSize.H) - m.SourceUV.Y) / m.SourceUV.H
	if qx < 0 || qx > 1 || qy < 0 || qy > 1 {
		return PointerPoint{}, false
	}
	// The shader's temporal effects move only X after the radial warp. Therefore
	// its final Y is sufficient to reverse both jitter and the tracking band.
	qx -= m.temporalShift(qy)
	if !pointerFinite(qx) {
		return PointerPoint{}, false
	}
	cx, cy := qx*2-1, qy*2-1
	radius := math.Hypot(cx, cy)
	if radius > 0 && m.Curvature > 0 {
		lo, hi := 0.0, radius
		k := m.Curvature * .09
		for i := 0; i < 56; i++ {
			mid := (lo + hi) * .5
			if mid*(1+k*mid*mid) < radius {
				lo = mid
			} else {
				hi = mid
			}
		}
		scale := ((lo + hi) * .5) / radius
		cx, cy = cx*scale, cy*scale
	}
	point := PointerPoint{
		X: float64(m.Bounds.X) + float64(m.Area.X) + (cx+1)*.5*float64(m.Area.W) - .5,
		Y: float64(m.Bounds.Y) + float64(m.Area.Y) + (cy+1)*.5*float64(m.Area.H) - .5,
	}
	// Reuse the forward visibility rules, including rounded coverage and pixels
	// shifted outside the display. A source pixel need not have a visible preimage.
	if _, ok := m.MapScreen(point.X, point.Y); !ok {
		return PointerPoint{}, false
	}
	return point, true
}

func (m PointerMap) valid() bool {
	if m.Bounds.Validate() != nil || m.Area.Validate() != nil || m.SourceSize.Validate() != nil ||
		m.Area.X < 0 || m.Area.Y < 0 || m.Area.X+m.Area.W > m.Bounds.W || m.Area.Y+m.Area.H > m.Bounds.H {
		return false
	}
	u := m.SourceUV
	if !pointerFinite(u.X) || !pointerFinite(u.Y) || !pointerFinite(u.W) || !pointerFinite(u.H) ||
		u.X < 0 || u.Y < 0 || u.W <= 0 || u.H <= 0 || u.X+u.W > 1 || u.Y+u.H > 1 {
		return false
	}
	// These are the shader-ready normalized controls, not unconstrained warp
	// coefficients. In particular, negative curvature would need another inverse.
	for _, v := range []float64{m.Curvature, m.CornerRadius, m.Jitter, m.Tracking} {
		if !pointerFinite(v) || v < 0 || v > 1 {
			return false
		}
	}
	return pointerFinite(m.Time)
}

func (m PointerMap) covered(px, py float64) bool {
	// Force rounding here: a fused radius multiply/subtract can turn exact
	// distance zero into a tiny negative value and admit coverage exactly 0.5.
	radius := float64(m.CornerRadius * float64(min(m.Area.W, m.Area.H)))
	if radius <= 0 {
		return true
	}
	x := math.Abs(px-float64(m.Area.X)-float64(m.Area.W)*.5) - float64(m.Area.W)*.5 + radius
	y := math.Abs(py-float64(m.Area.Y)-float64(m.Area.H)*.5) - float64(m.Area.H)*.5 + radius
	outsideX, outsideY := max(x, 0), max(y, 0)
	distance := math.Sqrt(outsideX*outsideX+outsideY*outsideY) + min(max(x, y), 0) - radius
	// 1-smoothstep(-.75,.75,distance) > .5 exactly when distance < 0.
	return distance < 0
}

func (m PointerMap) temporalShift(qy float64) float64 {
	shift := 0.0
	if m.Jitter != 0 {
		shift = m.Jitter * .002 * math.Sin(m.Time*3.7+qy*9)
	}
	// GLSL mod/fract use floor, unlike Go's remainder for negative time.
	if m.Tracking != 0 && m.Time-12*math.Floor(m.Time/12) >= 10.8 {
		band := m.Time*.3 - math.Floor(m.Time*.3)
		distance := (qy - band) * 70
		shift += m.Tracking * .012 * math.Exp(-distance*distance)
	}
	return shift
}

func pointerFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func pointerClamp(v, lo, hi float64) float64 { return min(max(v, lo), hi) }
