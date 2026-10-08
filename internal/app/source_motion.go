package app

import (
	"time"

	"github.com/aiwaki/lumatape/internal/geometry"
)

const sourceSettleTime = 150 * time.Millisecond

// A captured image must not chase a moving, still-interactive source window.
// Native drag/resize pauses even if its bounds have not changed yet; observed
// bounds changes also cover programmatic movement outside a native size loop.
type sourceMotionGate struct {
	target      uintptr
	bounds      geometry.Rect
	stableSince time.Time
	observed    bool
}

func (g *sourceMotionGate) Observe(target uintptr, bounds geometry.Rect, moving bool, now time.Time) bool {
	if !g.observed || g.target != target || g.bounds != bounds || moving || now.Before(g.stableSince) {
		g.target, g.bounds, g.stableSince, g.observed = target, bounds, now, true
		return false
	}
	return target != 0 && bounds.W > 0 && bounds.H > 0 && now.Sub(g.stableSince) >= sourceSettleTime
}

func (g *sourceMotionGate) Reset() { *g = sourceMotionGate{} }

func sourceGeometryMatches(before, captured, current geometry.Rect) bool {
	return before.W > 0 && before.H > 0 && before == captured && before == current
}
