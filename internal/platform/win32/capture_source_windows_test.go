//go:build windows

package win32_test

import (
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/platform/capture"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func TestPointerSurfaceCannotBeCapturedRegardlessOfTitle(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user := syscall.NewLazyDLL("user32.dll")
	kernel := syscall.NewLazyDLL("kernel32.dll")
	instance, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
	var cls struct {
		Size, Style                        uint32
		Proc                               uintptr
		ClsExtra, WndExtra                 int32
		Instance, Icon, Cursor, Background uintptr
		Menu, Class                        *uint16
		SmallIcon                          uintptr
	}
	cls.Size, cls.Instance, cls.Class = uint32(unsafe.Sizeof(cls)), instance, win32.U16("LumaTape.PointerProjection")
	cls.Proc = syscall.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		result, _, _ := user.NewProc("DefWindowProcW").Call(h, uintptr(msg), w, l)
		return result
	})
	if atom, _, err := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&cls))); atom == 0 {
		t.Fatalf("register native fixture: %v", err)
	}
	defer user.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(cls.Class)), instance)
	var windows []uintptr
	defer func() {
		for _, hwnd := range windows {
			user.NewProc("DestroyWindow").Call(hwnd)
		}
	}()
	create := func(class, title string) uintptr {
		t.Helper()
		// Hidden native windows are enough to test the capture boundary. No
		// focus, cursor visibility, magnification or input state is changed.
		hwnd, _, err := user.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(win32.U16(class))), uintptr(unsafe.Pointer(win32.U16(title))), 0x80000000, 0, 0, 1, 1, 0, 0, instance, 0)
		if hwnd == 0 {
			t.Fatalf("create %s fixture: %v", class, err)
		}
		windows = append(windows, hwnd)
		return hwnd
	}
	// Selection must use the class identity, not a translated/renamed title.
	pointer := create("LumaTape.PointerProjection", "Renamed helper")
	if win32.CaptureSourceAllowed(pointer) {
		t.Fatal("projection surface accepted as a capture source")
	}
	for _, transfer := range []capture.Transfer{capture.TransferGPU, capture.TransferCompatibility} {
		// A nil library proves the rejection happens before any capture DLL
		// entry point (including CPU capability checks) can be touched.
		var library *capture.Library
		if opened, err := library.Open(pointer, transfer); opened != nil || err == nil || !strings.Contains(err.Error(), "helper window") {
			t.Fatalf("capture boundary accepted projection surface: %v %v", opened, err)
		}
	}
	for _, title := range []string{"LumaTape pointer", "LumaTape testcard", "LumaTape preview", "Ordinary game"} {
		if !win32.CaptureSourceAllowed(create("STATIC", title)) {
			t.Fatalf("ordinary source excluded by title %q", title)
		}
	}
	if win32.CaptureSourceAllowed(0) {
		t.Fatal("invalid HWND accepted")
	}
}
