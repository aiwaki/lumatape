package app

import (
	"testing"

	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestSourceAlignmentDistinguishesFocusFromMovement(t *testing.T) {
	bounds := geometry.Rect{X: -3440, Y: 40, W: 1920, H: 1440}
	changed := bounds
	changed.W--
	for _, sample := range []struct {
		name                                 string
		visible, foreground, moving, settled bool
		captured, current                    geometry.Rect
		want                                 sourceAlignment
	}{
		{"stable", true, true, false, true, bounds, bounds, sourceAligned},
		{"hidden", false, false, false, true, bounds, bounds, sourceHidden},
		{"desktop foreground", true, false, false, true, bounds, bounds, sourceUnfocused},
		{"focus lost during move", true, false, true, false, changed, bounds, sourceUnfocused},
		{"native drag", true, true, true, false, bounds, bounds, sourceMoving},
		{"source resized during transfer", true, true, false, true, bounds, changed, sourceGeometry},
		{"old captured size", true, true, false, true, changed, bounds, sourceGeometry},
		{"settling after movement", true, true, false, false, bounds, bounds, sourceSettling},
	} {
		t.Run(sample.name, func(t *testing.T) {
			got := classifySourceAlignment(sample.visible, sample.foreground, sample.moving, sample.settled, bounds, sample.captured, sample.current)
			if got != sample.want {
				t.Fatalf("got %s, want %s", got, sample.want)
			}
		})
	}
}
