//go:build windows

// Package glfw is a deliberately small GLFW 3.4 ABI binding for Windows x64.
// No callbacks or GL calls cross a Go worker thread. Integer/pointer ABI only.
package glfw

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

type API struct {
	dll     *syscall.DLL
	procs   map[string]*syscall.Proc
	started bool
}
type Window struct {
	API          *API
	Handle, HWND uintptr
}

var currentDC = syscall.NewLazyDLL("opengl32.dll").NewProc("wglGetCurrentDC")
var swapBuffers = syscall.NewLazyDLL("gdi32.dll").NewProc("SwapBuffers")

func Open(directory string) (*API, error) {
	if runtime.GOARCH != "amd64" {
		return nil, fmt.Errorf("this release supports Windows x64 only")
	}
	dll, e := syscall.LoadDLL(filepath.Join(directory, "glfw3.dll"))
	if e != nil {
		return nil, fmt.Errorf("load bundled glfw3.dll: %w", e)
	}
	a := &API{dll: dll, procs: map[string]*syscall.Proc{}}
	for _, n := range []string{"glfwInit", "glfwTerminate", "glfwGetVersion", "glfwGetError", "glfwWindowHint", "glfwCreateWindow", "glfwDestroyWindow", "glfwMakeContextCurrent", "glfwGetWin32Window", "glfwGetFramebufferSize", "glfwGetWindowAttrib", "glfwShowWindow", "glfwHideWindow", "glfwPollEvents", "glfwSwapBuffers", "glfwSwapInterval", "glfwWindowShouldClose", "glfwGetProcAddress"} {
		p, e := dll.FindProc(n)
		if e != nil {
			a.Close()
			return nil, e
		}
		a.procs[n] = p
	}
	var major, minor, rev int32
	a.Call("glfwGetVersion", uintptr(unsafe.Pointer(&major)), uintptr(unsafe.Pointer(&minor)), uintptr(unsafe.Pointer(&rev)))
	if major != 3 || minor < 4 {
		a.Close()
		return nil, fmt.Errorf("GLFW 3.4+ required; got %d.%d.%d", major, minor, rev)
	}
	if a.Call("glfwInit") == 0 {
		e := a.Error("glfwInit")
		a.Close()
		return nil, e
	}
	a.started = true
	return a, nil
}

//go:uintptrescapes
func (a *API) Call(n string, args ...uintptr) uintptr { r, _, _ := a.procs[n].Call(args...); return r }
func nativeString(p *byte) string {
	if p == nil {
		return ""
	}
	b := make([]byte, 0, 128)
	for i := uintptr(0); i < 16384; i++ {
		c := *(*byte)(unsafe.Add(unsafe.Pointer(p), i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}
func (a *API) Error(context string) error {
	var desc *byte
	code := a.Call("glfwGetError", uintptr(unsafe.Pointer(&desc)))
	return fmt.Errorf("%s: GLFW 0x%x %s", context, code, nativeString(desc))
}
func (a *API) Hint(key int, value int) { a.Call("glfwWindowHint", uintptr(key), uintptr(value)) }
func (a *API) NewOverlay() (*Window, error) {
	for _, h := range [][2]int{
		{0x00020004, 0},                  // VISIBLE
		{0x00020005, 0},                  // DECORATED
		{0x00020001, 0},                  // FOCUSED
		{0x0002000c, 0},                  // FOCUS_ON_SHOW
		{0x00020007, 1},                  // FLOATING
		{0x0002000a, 1},                  // TRANSPARENT_FRAMEBUFFER
		{0x0002000d, 1},                  // MOUSE_PASSTHROUGH
		{0x00022002, 3}, {0x00022003, 3}, // GL version
		{0x00022008, 0x00032001},         // OPENGL_CORE_PROFILE
		{0x00021005, 0}, {0x00021006, 0}, // no depth/stencil
		{0x00021004, 8}, // alpha bits
		{0x0002100e, 0}, // framebuffer SRGB off: shaders encode SDR output explicitly
	} {
		a.Hint(h[0], h[1])
	}
	title := append([]byte("LumaTape overlay"), 0)
	h := a.Call("glfwCreateWindow", 64, 64, uintptr(unsafe.Pointer(&title[0])), 0, 0)
	if h == 0 {
		return nil, a.Error("create overlay")
	}
	w := &Window{API: a, Handle: h, HWND: a.Call("glfwGetWin32Window", h)}
	a.Call("glfwMakeContextCurrent", h)
	if a.Call("glfwGetWindowAttrib", h, 0x0002000a) == 0 {
		w.Close()
		return nil, fmt.Errorf("transparent framebuffer unavailable; overlay was not shown")
	}
	if a.Call("glfwGetWindowAttrib", h, 0x0002000d) == 0 {
		w.Close()
		return nil, fmt.Errorf("mouse passthrough unavailable")
	}
	a.Call("glfwSwapInterval", 1)
	return w, nil
}
func (a *API) GLProc(name string) uintptr {
	b := append([]byte(name), 0)
	return a.Call("glfwGetProcAddress", uintptr(unsafe.Pointer(&b[0])))
}

// GLFW's WGL backend intentionally ignores the driver's swap-control BOOL.
// Check the actual extension result/readback before relying on VSync pacing.
func (a *API) ConfigureVSync() bool {
	set, get := a.GLProc("wglSwapIntervalEXT"), a.GLProc("wglGetSwapIntervalEXT")
	valid := func(p uintptr) bool { return p > 3 && p != ^uintptr(0) }
	if !valid(set) || !valid(get) {
		a.Call("glfwSwapInterval", 0)
		return false
	}
	a.Call("glfwSwapInterval", 1)
	ok, _, _ := syscall.SyscallN(set, 1)
	interval, _, _ := syscall.SyscallN(get)
	if ok == 0 || int32(interval) != 1 {
		a.Call("glfwSwapInterval", 0)
		return false
	}
	return true
}

func (a *API) DisableVSync() { a.Call("glfwSwapInterval", 0) }
func (a *API) Poll()         { a.Call("glfwPollEvents") }
func (w *Window) Framebuffer() (int, int) {
	var x, y int32
	w.API.Call("glfwGetFramebufferSize", w.Handle, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y)))
	return int(x), int(y)
}
func (w *Window) Show() { w.API.Call("glfwShowWindow", w.Handle) }
func (w *Window) Hide() { w.API.Call("glfwHideWindow", w.Handle) }

// On the supported Windows 10+ path GLFW calls exactly SwapBuffers but discards
// its return value. Check it directly so a failed presentation hides the window.
func (w *Window) Swap() error {
	dc, _, e := currentDC.Call()
	if dc == 0 {
		return fmt.Errorf("current WGL DC unavailable: %w", e)
	}
	ok, _, e := swapBuffers.Call(dc)
	if ok == 0 {
		return fmt.Errorf("SwapBuffers failed: %w", e)
	}
	return nil
}
func (w *Window) ShouldClose() bool { return w.API.Call("glfwWindowShouldClose", w.Handle) != 0 }
func (w *Window) Close() {
	if w.Handle != 0 {
		w.API.Call("glfwDestroyWindow", w.Handle)
		w.Handle = 0
		w.HWND = 0
	}
}
func (a *API) Close() {
	if a.started {
		a.Call("glfwTerminate")
		a.started = false
	}
	if a.dll != nil {
		a.dll.Release()
		a.dll = nil
	}
}
