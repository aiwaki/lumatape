//go:build windows

package win32

import (
	"fmt"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/geometry"
)

type WindowPlacement struct {
	Length, Flags, ShowCommand uint32
	Minimum, Maximum           Point
	Normal                     Rect
}

type WindowState struct {
	Bounds    geometry.Rect
	Placement WindowPlacement
}

func SaveWindowState(hwnd uintptr) (WindowState, error) {
	s := WindowState{Placement: WindowPlacement{Length: uint32(unsafe.Sizeof(WindowPlacement{}))}}
	if err := BoolCall("GetWindowPlacement", hwnd, uintptr(unsafe.Pointer(&s.Placement))); err != nil {
		return WindowState{}, err
	}
	var err error
	s.Bounds, err = WindowBounds(hwnd)
	return s, err
}

func RestoreWindowState(hwnd uintptr, s WindowState) error {
	if err := BoolCall("SetWindowPlacement", hwnd, uintptr(unsafe.Pointer(&s.Placement))); err != nil {
		return err
	}
	actual, err := WindowBounds(hwnd)
	if err != nil {
		return err
	}
	if actual != s.Bounds {
		return fmt.Errorf("the window did not restore its original placement")
	}
	return nil
}

func ClientInsets(hwnd uintptr) (left, top, right, bottom int, err error) {
	style := call("GetWindowLongPtrW", hwnd, ^uintptr(15)) &^ (0x01000000 | 0x20000000) // normal, not maximized/minimized
	ex := call("GetWindowLongPtrW", hwnd, ^uintptr(19))
	dpi := call("GetDpiForWindow", hwnd)
	if dpi == 0 {
		return 0, 0, 0, 0, fmt.Errorf("cannot query source DPI")
	}
	menu := uintptr(0)
	if call("GetMenu", hwnd) != 0 {
		menu = 1
	}
	var r Rect
	if err = BoolCall("AdjustWindowRectExForDpi", uintptr(unsafe.Pointer(&r)), style, menu, ex, dpi); err != nil {
		return
	}
	return -int(r.Left), -int(r.Top), int(r.Right), int(r.Bottom), nil
}

func ResizeNormalClient(hwnd uintptr, client geometry.Rect) error {
	if call("IsZoomed", hwnd) != 0 || Minimized(hwnd) {
		call("ShowWindow", hwnd, 9) // SW_RESTORE; original placement was saved first
	}
	return ResizeClient(hwnd, client)
}
