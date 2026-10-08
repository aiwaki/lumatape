//go:build windows

package win32

import (
	"syscall"
	"unsafe"
)

// CaptureSourceAllowed is shared by enumeration and the capture/selection
// boundary. The projection worker runs in another process, so a current-PID
// exclusion alone does not prevent recursive capture of our own cursor surface.
// Do not filter titles: the testcard and games may have any user-visible name.
func CaptureSourceAllowed(hwnd uintptr) bool {
	var class [256]uint16
	if call("GetClassNameW", hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class))) == 0 {
		return false
	}
	return syscall.UTF16ToString(class[:]) != "LumaTape.PointerProjection"
}
