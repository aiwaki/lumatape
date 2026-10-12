//go:build windows

package app

import (
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

// The projected cursor changes only its drawing, never its native position.
// If the source is partly outside the physical displays, its offscreen pixels
// may still appear inside the warped image but cannot receive a real click.
// Pause instead of drawing an unreachable pointer; resume when the user moves
// the complete client back onto the displays. Never move their window for them.
func (a *application) pauseProjectedSourceOutsideDesktop(p presentation, source geometry.Size) (bool, error) {
	coordinates := ""
	if a.shaderProgram != nil {
		coordinates = a.shaderProgram.Metadata.Coordinates
	}
	if _, enabled := projectedPointerMap(a.cfg, p, source, coordinates, 0); !enabled {
		return false, nil
	}
	displays := make([]geometry.Rect, 0, len(a.monitors))
	for _, monitor := range a.monitors {
		displays = append(displays, monitor.Bounds)
	}
	if geometry.CoveredBy(p.Bounds, displays) {
		return false, nil
	}
	a.hide()
	a.setPhase("paused-offscreen", locale.Text("Часть окна за пределами экрана. Переместите его целиком на экран — эффект возобновится.", "Part of the window is offscreen. Move it fully onto the screen to resume the effect."))
	const reason sourceAlignment = "offscreen"
	if !a.sourceMotionPaused || a.sourcePauseReason != reason {
		a.sourceMotionPaused, a.sourcePauseReason = true, reason
		a.record("source_alignment_paused", map[string]any{"cause": reason})
	}
	return true, a.closeCapture()
}
