package pointer

import "errors"

// cursorVisibilityBackend changes only cursor visibility. Preparing the cursor
// image must finish before project is called, so preparation failure cannot
// suppress the system cursor.
type cursorVisibilityBackend interface {
	setSystemCursorVisible(bool) error
	setProjectionVisible(bool) error
}

// The flags retain ownership of possibly unfinished native operations. A failed
// call can have side effects, so they are deliberately conservative until a
// successful restoration confirms the result.
type cursorVisibility struct {
	hidden, visible bool
}

func (s *cursorVisibility) project(b cursorVisibilityBackend) error {
	if !s.hidden {
		s.hidden = true
		if err := b.setSystemCursorVisible(false); err != nil {
			return errors.Join(err, s.restore(b))
		}
	}
	// Reassert the projection on active frames: the renderer can have changed
	// native z-order. This does not toggle the system cursor on every frame.
	s.visible = true
	if err := b.setProjectionVisible(true); err != nil {
		return errors.Join(err, s.restore(b))
	}
	return nil
}

func (s *cursorVisibility) restore(b cursorVisibilityBackend) error {
	// Always hide the owned HWND, including when a failed show/readback left
	// cached visibility false. Never expose both cursors during a successful
	// handoff. Failure to hide must not prevent restoring the system cursor.
	projectionErr := b.setProjectionVisible(false)
	s.visible = projectionErr != nil
	var systemErr error
	if s.hidden {
		systemErr = b.setSystemCursorVisible(true)
		if systemErr == nil {
			s.hidden = false
		}
	}
	return errors.Join(projectionErr, systemErr)
}
