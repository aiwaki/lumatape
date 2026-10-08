//go:build windows

package app

import "github.com/aiwaki/lumatape/internal/platform/win32"

// Observe only foreground transitions. Never retarget capture, enable an effect
// or take focus; this is a suggestion for the next explicit UI selection.
func (a *application) observeForegroundSource() {
	hwnd := win32.Foreground()
	if hwnd == a.lastForeground {
		return
	}
	a.lastForeground = hwnd
	if hwnd == 0 || (a.controllerPID != 0 && win32.SameProcess(hwnd, a.controllerPID)) {
		return
	}
	for _, w := range a.availableWindows() {
		if w.Handle == hwnd {
			descriptor := a.sourceDescriptor(w)
			a.suggestedSource = &descriptor
			return
		}
	}
}
