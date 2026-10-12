package app

import "github.com/aiwaki/lumatape/internal/geometry"

type sourceAlignment string

const (
	sourceAligned   sourceAlignment = "aligned"
	sourceHidden    sourceAlignment = "hidden"
	sourceUnfocused sourceAlignment = "focus"
	sourceMoving    sourceAlignment = "moving"
	sourceSettling  sourceAlignment = "settling"
	sourceGeometry  sourceAlignment = "geometry"
)

// Keep loss of focus distinct from resize. Both require immediate hiding,
// but the user action and diagnostics must describe what actually happened.
func classifySourceAlignment(visible, foreground, moving, settled bool, before, captured, current geometry.Rect) sourceAlignment {
	if !visible {
		return sourceHidden
	}
	if !foreground {
		return sourceUnfocused
	}
	if moving {
		return sourceMoving
	}
	if !sourceGeometryMatches(before, captured, current) {
		return sourceGeometry
	}
	if !settled {
		return sourceSettling
	}
	return sourceAligned
}
