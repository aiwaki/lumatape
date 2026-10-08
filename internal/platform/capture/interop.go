package capture

import (
	"errors"
	"fmt"
	"strings"
)

// ErrGPUInteropUnavailable identifies missing prerequisites in the current GL
// context. It says nothing about WGC or the explicitly selected CPU path.
var ErrGPUInteropUnavailable = errors.New("GPU interop prerequisites unavailable")

var interopEntryPoints = [...]string{
	"wglDXOpenDeviceNV", "wglDXCloseDeviceNV", "wglDXRegisterObjectNV",
	"wglDXUnregisterObjectNV", "wglDXLockObjectsNV", "wglDXUnlockObjectsNV",
}

func validGLProc(p uintptr) bool { return p > 3 && p != ^uintptr(0) }

// Check exactly the same prerequisites as native Interop::load. Resolving the
// entry points does not call them or open a device. Success is not capture
// qualification: the native bridge still probes BGRA sharing and WGC on Open.
func checkGPUInterop(extensions string, getProc func(string) uintptr) error {
	tokens := strings.Split(extensions, " ")
	for _, required := range []string{"WGL_NV_DX_interop", "WGL_NV_DX_interop2"} {
		found := false
		for _, token := range tokens {
			found = found || token == required
		}
		if !found {
			return fmt.Errorf("%w: missing %s", ErrGPUInteropUnavailable, required)
		}
	}
	for _, name := range interopEntryPoints {
		if getProc == nil || !validGLProc(getProc(name)) {
			return fmt.Errorf("%w: missing entry point %s", ErrGPUInteropUnavailable, name)
		}
	}
	return nil
}
