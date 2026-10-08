//go:build windows

package app

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

type nativeWindowMutation struct {
	target       win32.Window
	recoveryPath string
	token        windowRecoveryToken
}

func (w *nativeWindowMutation) Snapshot() (geometry.Rect, func() error, error) {
	if !win32.SameProcess(w.target.Handle, w.target.PID) {
		return geometry.Rect{}, nil, fmt.Errorf(locale.Text("окно источника закрыто", "source window closed"))
	}
	if _, err := os.Stat(w.recoveryPath); err == nil {
		return geometry.Rect{}, nil, errors.New(locale.Text("предыдущее восстановление окна ещё не завершено", "an unresolved window recovery token already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return geometry.Rect{}, nil, err
	}
	owner, alive, err := win32.ProcessIdentity(uint32(os.Getpid()))
	if err != nil || !alive {
		return geometry.Rect{}, nil, errors.Join(errors.New(locale.Text("не удалось определить владельца восстановления", "cannot identify recovery owner")), err)
	}
	target, alive, err := win32.ProcessIdentity(w.target.PID)
	if err != nil || !alive {
		return geometry.Rect{}, nil, errors.Join(errors.New(locale.Text("не удалось определить процесс источника", "cannot identify source process")), err)
	}
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return geometry.Rect{}, nil, err
	}
	w.token = windowRecoveryToken{Version: 1, Owner: recoveryProcess{uint32(os.Getpid()), owner}, Target: recoveryProcess{w.target.PID, target}, Window: uint64(w.target.Handle), Nonce: binary.LittleEndian.Uint64(nonce[:]) | 1}
	if err := win32.SetRecoveryMarker(w.target.Handle, w.token.Nonce); err != nil {
		return geometry.Rect{}, nil, err
	}
	saved, err := win32.SaveWindowState(w.target.Handle)
	if err != nil || !win32.SameProcess(w.target.Handle, w.target.PID) || !win32.HasRecoveryMarker(w.target.Handle, w.token.Nonce) {
		win32.RemoveRecoveryMarker(w.target.Handle, w.token.Nonce)
		return geometry.Rect{}, nil, errors.Join(errors.New(locale.Text("источник изменился при сохранении положения окна", "source changed while saving window placement")), err)
	}
	w.token.Original, w.token.Placement = saved.Bounds, encodeRecoveryPlacement(saved)
	return saved.Bounds, func() error {
		if !win32.SameProcess(w.target.Handle, w.target.PID) || !win32.HasRecoveryMarker(w.target.Handle, w.token.Nonce) {
			return nil // a closed source no longer has window state to restore
		}
		return win32.RestoreWindowState(w.target.Handle, saved)
	}, nil
}
func (w *nativeWindowMutation) Prepare(r geometry.Rect) error {
	if _, err := os.Stat(w.recoveryPath); err == nil {
		return errors.New(locale.Text("предыдущее восстановление окна ещё не завершено", "an unresolved window recovery token already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	w.token.Client = r
	if err := writeWindowRecovery(w.recoveryPath, w.token); err != nil {
		return err
	}
	if !win32.SameProcess(w.target.Handle, w.target.PID) || !win32.HasRecoveryMarker(w.target.Handle, w.token.Nonce) {
		return errors.New(locale.Text("источник изменился до изменения окна", "source changed before window mutation"))
	}
	return nil
}
func (w *nativeWindowMutation) Commit() error {
	w.token.Applied = true
	return writeWindowRecovery(w.recoveryPath, w.token)
}
func (w *nativeWindowMutation) Clear() error {
	t, err := readWindowRecovery(w.recoveryPath)
	if errors.Is(err, os.ErrNotExist) {
		win32.RemoveRecoveryMarker(w.target.Handle, w.token.Nonce)
		return nil
	}
	if err != nil {
		return err
	}
	if t.Nonce != w.token.Nonce {
		return errors.New(locale.Text("нельзя удалить запись восстановления другого окна", "refusing to remove another window recovery token"))
	}
	if err = os.Remove(w.recoveryPath); err != nil {
		return err
	}
	win32.RemoveRecoveryMarker(w.target.Handle, w.token.Nonce)
	return nil
}
func (w *nativeWindowMutation) ResizeClient(r geometry.Rect) error {
	if !win32.SameProcess(w.target.Handle, w.target.PID) || !win32.HasRecoveryMarker(w.target.Handle, w.token.Nonce) {
		return fmt.Errorf(locale.Text("окно источника закрыто", "source window closed"))
	}
	return win32.ResizeNormalClient(w.target.Handle, r)
}
func (w *nativeWindowMutation) Bounds() (geometry.Rect, error) {
	return win32.WindowBounds(w.target.Handle)
}
func (w *nativeWindowMutation) ClientBounds() (geometry.Rect, error) {
	return win32.ClientBounds(w.target.Handle)
}
