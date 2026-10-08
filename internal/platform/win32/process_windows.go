//go:build windows

package win32

import (
	"fmt"
	"syscall"
	"unsafe"
)

// ProcessIdentity returns creation time only for a currently live process.
// Access denied is an error, never evidence that an owner has exited.
func ProcessIdentity(pid uint32) (created uint64, alive bool, err error) {
	h, _, e := kernel32.NewProc("OpenProcess").Call(0x1000|0x100000, 0, uintptr(pid))
	if h == 0 {
		if e == syscall.Errno(87) {
			return 0, false, nil
		} // no such process
		return 0, false, fmt.Errorf("OpenProcess identity: %w", e)
	}
	defer kernel32.NewProc("CloseHandle").Call(h)
	wait, _, e := kernel32.NewProc("WaitForSingleObject").Call(h, 0)
	if wait == 0 {
		return 0, false, nil
	}
	if wait != 258 {
		return 0, false, fmt.Errorf("query process lifetime: %w", e)
	}
	var creation, exit, kernel, user syscall.Filetime
	ok, _, e := kernel32.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return 0, false, fmt.Errorf("query process creation time: %w", e)
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), true, nil
}

const recoveryProperty = "LumaTape.WindowRecovery.v1"

func SetRecoveryMarker(hwnd uintptr, nonce uint64) error {
	return BoolCall("SetPropW", hwnd, uintptr(unsafe.Pointer(U16(recoveryProperty))), uintptr(nonce))
}
func HasRecoveryMarker(hwnd uintptr, nonce uint64) bool {
	return nonce != 0 && call("GetPropW", hwnd, uintptr(unsafe.Pointer(U16(recoveryProperty)))) == uintptr(nonce)
}
func RemoveRecoveryMarker(hwnd uintptr, nonce uint64) {
	if HasRecoveryMarker(hwnd, nonce) {
		call("RemovePropW", hwnd, uintptr(unsafe.Pointer(U16(recoveryProperty))))
	}
}

// WakeController is safe from a transport reader goroutine. It only posts a
// message; native/UI work still runs on the initial locked application thread.
func WakeController(hwnd uintptr) { call("PostMessageW", hwnd, 0, 0, 0) }

// Grant foreground permission for an explicit tray/settings action. It does
// not activate anything; the desktop host decides whether to show its window.
func AllowForeground(pid uint32) {
	if pid != 0 {
		call("AllowSetForegroundWindow", uintptr(pid))
	}
}
