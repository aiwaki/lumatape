//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Explicit developer-only control. It does not intercept input, hide a window,
// or suspend its UI thread. The normal testcard does not accept this message.
var captureSmokeProperty, _ = syscall.UTF16PtrFromString("LumaTape.TestCard.Animation.v1")
var captureSmokeMessage uint32

func registerCaptureSmoke() error {
	message, _, err := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(captureSmokeProperty)))
	if message == 0 {
		return fmt.Errorf("register capture smoke control: %w", err)
	}
	captureSmokeMessage = uint32(message)
	return nil
}

func (s *cardState) setCaptureAnimation(hwnd, animate uintptr) uintptr {
	if animate > 1 {
		return 0
	}
	if animate == 0 {
		killTimer.Call(hwnd, 1)
		s.animationPaused = true
	} else {
		timer, _, _ := setTimer.Call(hwnd, 1, 16, 0)
		if timer == 0 {
			return 0
		}
		s.animationPaused = false
	}
	value := animate + 1 // 1=static, 2=animated; absent means not opted in.
	stored, _, _ := user32.NewProc("SetPropW").Call(hwnd, uintptr(unsafe.Pointer(captureSmokeProperty)), value)
	if stored == 0 {
		return 0
	}
	return value
}
