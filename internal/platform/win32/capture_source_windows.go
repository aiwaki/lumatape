//go:build windows

package win32

import (
	"syscall"
	"unsafe"
)

// CaptureSourceAllowed is shared by enumeration and the capture/selection
// boundary. The shell and projection worker live in other processes, so a
// current-PID exclusion alone cannot reject their service surfaces. Titles,
// visibility and cloaking do not affect this structural capture boundary.
func CaptureSourceAllowed(hwnd uintptr) bool {
	return captureWindowInfo(hwnd).captureAllowed()
}

func captureWindowInfo(hwnd uintptr) sourceWindowInfo {
	var class [256]uint16
	if call("GetClassNameW", hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class))) == 0 {
		return sourceWindowInfo{}
	}
	return sourceWindowInfo{
		className:     syscall.UTF16ToString(class[:]),
		style:         call("GetWindowLongPtrW", hwnd, ^uintptr(15)), // GWL_STYLE
		extendedStyle: call("GetWindowLongPtrW", hwnd, ^uintptr(19)), // GWL_EXSTYLE
		shellWindow:   hwnd == call("GetShellWindow") || hwnd == call("GetDesktopWindow"),
	}
}
