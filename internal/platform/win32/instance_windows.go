//go:build windows

package win32

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/locale"
)

type Instance struct{ handle uintptr }

func AcquireInstance() (*Instance, bool, error) {
	h, _, err := kernel32.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(U16("Local\\LumaTape.Instance"))))
	if h == 0 {
		return nil, false, fmt.Errorf("single instance: %w", err)
	}
	if err == syscall.Errno(183) {
		kernel32.NewProc("CloseHandle").Call(h)
		// A primary instance may still be initializing its controller window.
		for i := 0; i < 20; i++ {
			w := call("FindWindowW", uintptr(unsafe.Pointer(U16("LumaTape.Control"))), 0)
			if w != 0 {
				var pid uint32
				call("GetWindowThreadProcessId", w, uintptr(unsafe.Pointer(&pid)))
				call("AllowSetForegroundWindow", uintptr(pid))
				call("PostMessageW", w, 0x8002, 0, 0)
				return nil, true, nil
			}
			kernel32.NewProc("Sleep").Call(50)
		}
		return nil, true, fmt.Errorf(locale.Text("LumaTape уже запускается; повторите через несколько секунд", "LumaTape is already starting; try again in a few seconds"))
	}
	return &Instance{h}, false, nil
}
func (i *Instance) Close() {
	if i != nil && i.handle != 0 {
		kernel32.NewProc("CloseHandle").Call(i.handle)
		i.handle = 0
	}
}

func LoadAppIcon(small bool) (uintptr, bool) {
	size := uintptr(32)
	if small {
		size = call("GetSystemMetrics", 49)
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	h := call("LoadImageW", instance, 101, 1, size, size, 0)
	if h != 0 {
		return h, true
	}
	return call("LoadIconW", 0, 32512), false
}

func CopyText(owner uintptr, text string) error {
	if call("OpenClipboard", owner) == 0 {
		return fmt.Errorf(locale.Text("буфер обмена занят; попробуйте снова", "clipboard is busy; try again"))
	}
	defer call("CloseClipboard")
	data := syscall.StringToUTF16(text)
	h, _, e := kernel32.NewProc("GlobalAlloc").Call(0x42, uintptr(len(data)*2))
	if h == 0 {
		return e
	}
	p, _, e := kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
		return e
	}
	kernel32.NewProc("RtlMoveMemory").Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	kernel32.NewProc("GlobalUnlock").Call(h)
	call("EmptyClipboard")
	if call("SetClipboardData", 13, h) == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
		return fmt.Errorf(locale.Text("не удалось записать диагностику в буфер обмена", "could not copy diagnostics to the clipboard"))
	}
	return nil
}
