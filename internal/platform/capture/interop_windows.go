//go:build windows

package capture

import (
	"bytes"
	"fmt"
	"syscall"
	"unsafe"
)

// ProbeGPUInterop must run on the owning GL thread with its context current.
// It only reads WGL extensions and resolves function addresses; no bindings,
// textures, devices, capture sessions or display/window settings are changed.
func ProbeGPUInterop(getProc func(string) uintptr) error {
	gl := syscall.NewLazyDLL("opengl32.dll")
	context, _, _ := gl.NewProc("wglGetCurrentContext").Call()
	dc, _, _ := gl.NewProc("wglGetCurrentDC").Call()
	if context == 0 || dc == 0 || getProc == nil {
		return fmt.Errorf("GPU probe requires the owning current WGL context and DC")
	}
	extensions, err := readWGLExtensions(getProc, dc)
	if err != nil {
		return err
	}
	return checkGPUInterop(extensions, getProc)
}

func readWGLExtensions(getProc func(string) uintptr, dc uintptr) (string, error) {
	var pointer uintptr
	// Match native Interop::load's ARB-first behavior, including a NULL result.
	if proc := getProc("wglGetExtensionsStringARB"); validGLProc(proc) {
		pointer, _, _ = syscall.SyscallN(proc, dc)
	} else if proc := getProc("wglGetExtensionsStringEXT"); validGLProc(proc) {
		pointer, _, _ = syscall.SyscallN(proc)
	}
	if pointer == 0 {
		return "", fmt.Errorf("GPU probe could not read the driver's WGL extension string")
	}
	// The WGL API owns this NUL-terminated string for the current context.
	// Copy through the native API rather than converting an integer ABI return
	// into a Go-managed pointer. An unreadable or truncated result stays unknown.
	const limit = 64 << 10
	data := make([]byte, limit)
	copyString := syscall.NewLazyDLL("kernel32.dll").NewProc("lstrcpynA")
	copied, _, _ := copyString.Call(uintptr(unsafe.Pointer(&data[0])), pointer, uintptr(len(data)))
	if copied == 0 {
		return "", fmt.Errorf("GPU probe could not copy the driver's WGL extension string")
	}
	if end := bytes.IndexByte(data, 0); end >= 0 && end < limit-1 {
		return string(data[:end]), nil
	}
	return "", fmt.Errorf("GPU probe WGL extension string exceeds %d bytes", limit-1)
}
