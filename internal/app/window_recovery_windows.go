//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func replaceRecoveryFile(source, destination string) error {
	src, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	ok, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 1|8)
	if ok == 0 {
		return fmt.Errorf(locale.Text("замена записи восстановления окна: %w", "replace window recovery: %w"), e)
	}
	return nil
}

func recoverInterruptedWindow(path string) error {
	t, err := readWindowRecovery(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf(locale.Text("чтение записи восстановления окна: %w", "read window recovery token: %w"), err)
	}
	ownerCreated, ownerAlive, err := win32.ProcessIdentity(t.Owner.PID)
	if err != nil {
		return err
	}
	ownerAlive = ownerAlive && ownerCreated == t.Owner.Created
	if ownerAlive {
		return errors.New(locale.Text("процесс-владелец восстановления окна ещё работает", "window recovery owner is still running"))
	}
	hwnd := uintptr(t.Window)
	targetSame := win32.SameProcess(hwnd, t.Target.PID)
	if targetSame {
		created, alive, err := win32.ProcessIdentity(t.Target.PID)
		if err != nil {
			return err
		}
		targetSame = alive && created == t.Target.Created
	}
	markerSame := targetSame && win32.HasRecoveryMarker(hwnd, t.Nonce)
	var outer, client geometry.Rect
	if markerSame {
		if outer, err = win32.WindowBounds(hwnd); err != nil {
			return err
		}
		if client, err = win32.ClientBounds(hwnd); err != nil {
			return err
		}
	}
	decision, err := decideWindowRecovery(t, ownerAlive, targetSame, markerSame, outer, client)
	if err != nil {
		return err
	}
	if decision == recoveryRestore {
		// Repeat identity immediately before mutation. Never adopt another
		// window just because it now occupies the expected rectangle.
		if !win32.SameProcess(hwnd, t.Target.PID) || !win32.HasRecoveryMarker(hwnd, t.Nonce) {
			return errors.New(locale.Text("источник восстановления изменился при проверке", "recovery source changed during verification"))
		}
		moving, err := win32.InMoveSize(hwnd)
		if err != nil {
			return err
		}
		if moving {
			return errors.New(locale.Text("окно источника движется; завершите перемещение и повторите восстановление", "source is moving; finish its move before retrying window recovery"))
		}
		currentOuter, err := win32.WindowBounds(hwnd)
		if err != nil {
			return err
		}
		currentClient, err := win32.ClientBounds(hwnd)
		if err != nil {
			return err
		}
		if currentOuter != outer || currentClient != client {
			return errors.New(locale.Text("геометрия источника изменилась при восстановлении; окно не изменено", "source geometry changed during recovery; no window mutation performed"))
		}
		if err = win32.RestoreWindowState(hwnd, decodeRecoveryState(t)); err != nil {
			return err
		}
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	win32.RemoveRecoveryMarker(hwnd, t.Nonce)
	return nil
}

func encodeRecoveryPlacement(s win32.WindowState) recoveryPlacement {
	p := s.Placement
	return recoveryPlacement{Flags: p.Flags, ShowCommand: p.ShowCommand, Minimum: [2]int32{p.Minimum.X, p.Minimum.Y}, Maximum: [2]int32{p.Maximum.X, p.Maximum.Y}, Normal: [4]int32{p.Normal.Left, p.Normal.Top, p.Normal.Right, p.Normal.Bottom}}
}
func decodeRecoveryState(t windowRecoveryToken) win32.WindowState {
	p := t.Placement
	return win32.WindowState{Bounds: t.Original, Placement: win32.WindowPlacement{Length: uint32(unsafe.Sizeof(win32.WindowPlacement{})), Flags: p.Flags, ShowCommand: p.ShowCommand, Minimum: win32.Point{X: p.Minimum[0], Y: p.Minimum[1]}, Maximum: win32.Point{X: p.Maximum[0], Y: p.Maximum[1]}, Normal: win32.Rect{Left: p.Normal[0], Top: p.Normal[1], Right: p.Normal[2], Bottom: p.Normal[3]}}}
}
