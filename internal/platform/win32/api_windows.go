//go:build windows

// Package win32 contains only the small native surface used by the app. All UI
// operations are performed on the application's initial locked OS thread.
package win32

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/geometry"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var dwmapi = syscall.NewLazyDLL("dwmapi.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")

func U16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

//go:uintptrescapes
func call(name string, a ...uintptr) uintptr { r, _, _ := user32.NewProc(name).Call(a...); return r }

//go:uintptrescapes
func BoolCall(name string, a ...uintptr) error {
	r, _, err := user32.NewProc(name).Call(a...)
	if r == 0 {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

type Point struct{ X, Y int32 }
type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) Geometry() geometry.Rect {
	return geometry.Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

func InitializeDPI() error {
	p := user32.NewProc("SetProcessDpiAwarenessContext")
	if err := p.Find(); err != nil {
		return fmt.Errorf("Windows 10 2004+ required: %w", err)
	}
	r, _, e := p.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	if r == 0 {
		// A manifest may already have established the correct context.
		ctx := call("GetThreadDpiAwarenessContext")
		if call("AreDpiAwarenessContextsEqual", ctx, ^uintptr(3)) == 0 {
			name := "unknown"
			for _, candidate := range []struct {
				value uintptr
				name  string
			}{
				{^uintptr(0), "unaware"}, {^uintptr(1), "system-aware"},
				{^uintptr(2), "per-monitor-v1"}, {^uintptr(4), "unaware-gdi-scaled"},
			} {
				if call("AreDpiAwarenessContextsEqual", ctx, candidate.value) != 0 {
					name = candidate.name
					break
				}
			}
			return fmt.Errorf("per-monitor DPI v2 required; current context %s (%#x): %w", name, ctx, e)
		}
	}
	var version struct {
		Size, Major, Minor, Build, Platform uint32
		CSD                                 [128]uint16
	}
	version.Size = uint32(unsafe.Sizeof(version))
	s, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&version)))
	if s != 0 || version.Major < 10 || version.Build < 19041 {
		return fmt.Errorf("Windows 10 build 19041 or newer required (detected build %d)", version.Build)
	}
	return nil
}

type Monitor struct {
	Handle  uintptr
	Bounds  geometry.Rect
	Work    geometry.Rect
	Device  string
	Primary bool
}
type monitorInfo struct {
	Size          uint32
	Monitor, Work Rect
	Flags         uint32
	Device        [32]uint16
}

func MonitorFromWindow(hwnd uintptr) uintptr { return call("MonitorFromWindow", hwnd, 2) }

// Callbacks are registered once: syscall.NewCallback slots are permanent.
// Enumeration is synchronous and restricted to the UI thread.
var monitorResults []Monitor
var monitorError error
var monitorCallback = syscall.NewCallback(func(h, dc, rc, data uintptr) uintptr {
	var info monitorInfo
	info.Size = uint32(unsafe.Sizeof(info))
	if e := BoolCall("GetMonitorInfoW", h, uintptr(unsafe.Pointer(&info))); e != nil {
		monitorError = e
		return 0
	}
	monitorResults = append(monitorResults, Monitor{Handle: h, Bounds: info.Monitor.Geometry(), Work: info.Work.Geometry(), Device: syscall.UTF16ToString(info.Device[:]), Primary: info.Flags&1 != 0})
	return 1
})

func Monitors() ([]Monitor, error) {
	monitorResults = nil
	monitorError = nil
	err := BoolCall("EnumDisplayMonitors", 0, 0, monitorCallback, 0)
	out := monitorResults
	monitorResults = nil
	if monitorError != nil {
		return nil, monitorError
	}
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Primary {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out, nil
}

type Window struct {
	Handle uintptr
	Title  string
	PID    uint32
}

var windowResults []Window
var windowCallback = syscall.NewCallback(func(h, data uintptr) uintptr {
	if call("IsWindowVisible", h) == 0 || call("GetWindow", h, 4) != 0 || !CaptureSourceAllowed(h) {
		return 1
	}
	pid, _, _ := kernel32.NewProc("GetCurrentProcessId").Call()
	var process uint32
	call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&process)))
	if uintptr(process) == pid {
		return 1
	}
	var cloaked uint32
	dwmapi.NewProc("DwmGetWindowAttribute").Call(h, 14, uintptr(unsafe.Pointer(&cloaked)), 4)
	if cloaked != 0 {
		return 1
	}
	var title [512]uint16
	if call("GetWindowTextW", h, uintptr(unsafe.Pointer(&title[0])), 512) == 0 {
		return 1
	}
	windowResults = append(windowResults, Window{h, syscall.UTF16ToString(title[:]), process})
	return 1
})

func Windows() []Window {
	windowResults = nil
	call("EnumWindows", windowCallback, 0)
	out := windowResults
	windowResults = nil
	return out
}

func Foreground() uintptr     { return call("GetForegroundWindow") }
func IsWindow(h uintptr) bool { return call("IsWindow", h) != 0 }
func SameProcess(h uintptr, pid uint32) bool {
	var actual uint32
	call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&actual)))
	return pid != 0 && pid == actual && IsWindow(h)
}
func Minimized(h uintptr) bool { return call("IsIconic", h) != 0 }
func Visible(h uintptr) bool {
	if call("IsWindowVisible", h) == 0 || Minimized(h) {
		return false
	}
	var cloaked uint32
	dwmapi.NewProc("DwmGetWindowAttribute").Call(h, 14, uintptr(unsafe.Pointer(&cloaked)), 4)
	return cloaked == 0
}
func ClientBounds(h uintptr) (geometry.Rect, error) {
	var r Rect
	var p Point
	if err := BoolCall("GetClientRect", h, uintptr(unsafe.Pointer(&r))); err != nil {
		return geometry.Rect{}, err
	}
	if err := BoolCall("ClientToScreen", h, uintptr(unsafe.Pointer(&p))); err != nil {
		return geometry.Rect{}, err
	}
	return geometry.Rect{X: int(p.X), Y: int(p.Y), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}, nil
}
func WindowBounds(h uintptr) (geometry.Rect, error) {
	var r Rect
	if e := BoolCall("GetWindowRect", h, uintptr(unsafe.Pointer(&r))); e != nil {
		return geometry.Rect{}, e
	}
	return r.Geometry(), nil
}

func Position(h uintptr, r geometry.Rect) error {
	return BoolCall("SetWindowPos", h, 0, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), 0x10|0x4) // NOACTIVATE | NOZORDER
}
func ResizeClient(h uintptr, r geometry.Rect) error {
	style := call("GetWindowLongPtrW", h, ^uintptr(15)) // GWL_STYLE (-16)
	ex := call("GetWindowLongPtrW", h, ^uintptr(19))    // GWL_EXSTYLE (-20)
	dpi := call("GetDpiForWindow", h)
	rc := Rect{0, 0, int32(r.W), int32(r.H)}
	menu := uintptr(0)
	if call("GetMenu", h) != 0 {
		menu = 1
	}
	if e := BoolCall("AdjustWindowRectExForDpi", uintptr(unsafe.Pointer(&rc)), style, menu, ex, dpi); e != nil {
		return e
	}
	return Position(h, geometry.Rect{X: r.X + int(rc.Left), Y: r.Y + int(rc.Top), W: int(rc.Right - rc.Left), H: int(rc.Bottom - rc.Top)})
}

func OverlayStyles(h uintptr) error {
	ex := call("GetWindowLongPtrW", h, ^uintptr(19))
	ex = (ex &^ 0x40000) | 0x08000000 | 0x80 // no appwindow, NOACTIVATE, TOOLWINDOW
	// GLFW MOUSE_PASSTHROUGH owns WS_EX_TRANSPARENT/LAYERED.
	call("SetWindowLongPtrW", h, ^uintptr(19), ex)
	got := call("GetWindowLongPtrW", h, ^uintptr(19))
	if got&0x080800a0 != 0x080800a0 {
		return fmt.Errorf("no-activate/click-through styles could not be set")
	}
	return BoolCall("SetWindowPos", h, ^uintptr(0), 0, 0, 0, 0, 0x1|0x2|0x10|0x20) // TOPMOST, no move/size/activate, frame changed
}
func ExcludeCapture(h uintptr) error {
	if e := BoolCall("SetWindowDisplayAffinity", h, 0x11); e != nil {
		return e
	}
	var affinity uint32
	if e := BoolCall("GetWindowDisplayAffinity", h, uintptr(unsafe.Pointer(&affinity))); e != nil {
		return e
	}
	if affinity != 0x11 {
		return fmt.Errorf("capture exclusion readback 0x%x, expected 0x11", affinity)
	}
	return nil
}
func WaitMessages(ms uint32) { call("MsgWaitForMultipleObjectsEx", 0, 0, uintptr(ms), 0x04ff, 4) }
func Message(title, body string) {
	call("MessageBoxW", 0, uintptr(unsafe.Pointer(U16(body))), uintptr(unsafe.Pointer(U16(title))), 0x40)
}
func OpenFile(path string) error {
	var system [260]uint16
	n, _, e := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&system[0])), 260)
	if n == 0 || n >= 260 {
		return fmt.Errorf("locate editor: %w", e)
	}
	cmd := exec.Command(syscall.UTF16ToString(system[:])+`\notepad.exe`, path)
	if e := cmd.Start(); e != nil {
		return fmt.Errorf("open editor: %w", e)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
