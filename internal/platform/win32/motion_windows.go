//go:build windows

package win32

import (
	"fmt"
	"unsafe"
)

// GUITHREADINFO: six HWNDs followed by RECT; 72 bytes on Windows x64.
// https://learn.microsoft.com/windows/win32/api/winuser/ns-winuser-guithreadinfo
type guiThreadInfo struct {
	Size, Flags                       uint32
	Active, Focus, Capture, MenuOwner uintptr
	MoveSize, Caret                   uintptr
	CaretRect                         Rect
}

// InMoveSize observes the target's own UI thread without hooks or input
// injection. Fail closed if Windows cannot describe that thread.
func InMoveSize(hwnd uintptr) (bool, error) {
	thread := call("GetWindowThreadProcessId", hwnd, 0)
	if thread == 0 {
		return false, fmt.Errorf("cannot query source window thread")
	}
	info := guiThreadInfo{Size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	if err := BoolCall("GetGUIThreadInfo", thread, uintptr(unsafe.Pointer(&info))); err != nil {
		return false, err
	}
	if info.Flags&0x00000002 == 0 { // GUI_INMOVESIZE
		return false, nil
	}
	// During activation transitions the handle may be zero even though the
	// thread flag is set. Pausing is safer than painting at uncertain geometry.
	return info.MoveSize == 0 || info.MoveSize == hwnd || call("GetAncestor", info.MoveSize, 2) == hwnd, nil
}
