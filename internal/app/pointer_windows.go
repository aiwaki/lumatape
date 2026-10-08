//go:build windows

package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/win32"
	"github.com/aiwaki/lumatape/internal/pointer"
)

// A failed Start has never sent a Frame, so even an initialization timeout
// cannot leave this session responsible for hidden cursor state.
type projectedPointerStartupError struct{ cause error }

func (e *projectedPointerStartupError) Error() string { return e.cause.Error() }
func (e *projectedPointerStartupError) Unwrap() error { return e.cause }

func (a *application) updateProjectedPointer(p presentation, source geometry.Size, frameTime float64) error {
	coordinates := ""
	if a.shaderProgram != nil {
		coordinates = a.shaderProgram.Metadata.Coordinates
	}
	mapping, enabled := projectedPointerMap(a.cfg, p, source, coordinates, frameTime)
	if !enabled {
		return a.stopProjectedPointer()
	}
	if a.pointerSession == nil {
		session, err := pointer.Start(filepath.Join(a.directory, "lumatape-watchdog.exe"))
		if err != nil {
			cause := fmt.Errorf(locale.Text("подготовка курсора для выпуклого экрана: %w", "preparing cursor for the convex screen: %w"), err)
			if a.snapshotPath != "" {
				return cause // an explicit image qualification must still fail
			}
			a.pointerFailure = &projectedPointerStartupError{cause}
			a.hide()
			return nil // persist OFF and recover at the normal frame boundary
		}
		a.pointerSession = session
		a.record("pointer_worker_ready", nil)
	}
	if a.pointerTarget != a.target {
		created, alive, err := win32.ProcessIdentity(a.target.PID)
		if err != nil || !alive {
			return errors.Join(errors.New(locale.Text("источник курсора закрыт или недоступен", "cursor source is closed or unavailable")), err)
		}
		a.pointerTarget, a.pointerSourceCreated = a.target, created
	}
	if err := a.pointerSession.Update(pointer.Frame{Mapping: mapping, SourceHWND: a.target.Handle,
		SourcePID: a.target.PID, SourceCreated: a.pointerSourceCreated, OverlayHWND: a.window.HWND}); err != nil {
		return fmt.Errorf(locale.Text("проекция курсора недоступна: %w", "cursor projection unavailable: %w"), err)
	}
	if !a.pointerEngaged {
		a.record("pointer_projection_requested", map[string]any{"shape": a.cfg.Screen.Shape, "input_unchanged": true})
	}
	a.pointerEngaged = true
	return nil
}

func (a *application) stopProjectedPointer() error {
	if a.pointerSession == nil || !a.pointerEngaged {
		return nil
	}
	// Clear before the acknowledgement so a later cleanup boundary never replays
	// Stop recursively. The worker has a generation barrier for queued frames.
	a.pointerEngaged = false
	if err := a.pointerSession.Stop(); err != nil {
		// Recover a failed worker before any capture/device/window teardown,
		// which can itself be blocked inside a driver.
		return fmt.Errorf(locale.Text("восстановление системного курсора: %w", "restoring system cursor: %w"), errors.Join(err, a.pointerSession.Close()))
	}
	a.record("pointer_projection_stopped", nil)
	return nil
}

func (a *application) closeProjectedPointer() error {
	if a.pointerSession == nil {
		return nil
	}
	err := errors.Join(a.pointerFailure, a.stopProjectedPointer())
	err = errors.Join(err, a.pointerSession.Close())
	a.pointerSession = nil
	a.pointerFailure = nil
	return err
}

// This check is safe inside frame(): hide restores the cursor immediately but
// only latches its failure. It cannot close capture or restore source geometry.
func (a *application) projectedPointerFailed() bool {
	if a.pointerFailure != nil {
		return true
	}
	if a.pointerSession != nil {
		if err := a.pointerSession.Poll(); err != nil {
			a.pointerFailure = err
			a.hide()
			return true
		}
	}
	return false
}

// Call only from the main loop, after frame() has released any acquired texture.
// Keep emergency cleanup outside hide(), which is also used during a frame.
func (a *application) recoverProjectedPointerAtBoundary() error {
	if !a.projectedPointerFailed() {
		return nil
	}
	cause := a.pointerFailure
	a.pointerFailure = nil
	return a.recoverProjectedPointer(cause)
}

// A failed companion is recoverable only after it can no longer hide the
// cursor and restoration has been positively acknowledged. Keep the tray usable
// for choosing another screen shape, and never replay the failed effect on launch.
func (a *application) recoverProjectedPointer(cause error) error {
	session := a.pointerSession
	var closeErr error
	var confirmed bool
	if session == nil {
		var startup *projectedPointerStartupError
		if !errors.As(cause, &startup) {
			return errors.Join(cause, errors.New(locale.Text("сеанс восстановления системного курсора недоступен", "system cursor recovery session is unavailable")))
		}
		confirmed = true // Start never returned a session that could receive Frame
	} else {
		closeErr = session.Close()
		confirmed = session.RestorationConfirmed()
	}
	a.pointerEngaged = false
	a.pointerSession = nil
	a.hide()
	a.cfg.Enabled, a.cfg.Aspect.Enabled = false, false
	if !confirmed {
		return errors.Join(cause, closeErr, a.save(), errors.New(locale.Text("восстановление системного курсора не подтверждено", "system cursor recovery is unconfirmed")))
	}
	a.record("pointer_projection_error", errors.Join(cause, closeErr).Error())
	if err := a.emergency(); err != nil {
		return errors.Join(cause, err)
	}
	a.lastError = locale.Text("Проекция курсора недоступна. Эффект выключен; выберите плоский или скруглённый экран и включите снова.", "Cursor projection is unavailable. Effect disabled; select a flat or rounded screen and enable it again.")
	a.setPhase("error", a.lastError)
	a.issue(locale.Text("Эффект выключен", "Effect disabled"), a.lastError)
	return nil
}
