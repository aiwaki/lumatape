package capture

import (
	"errors"
	"strings"
	"testing"
)

func TestInteropRequiresBothExactTokens(t *testing.T) {
	for _, list := range []string{"", "WGL_NV_DX_interop", "WGL_NV_DX_interop2", "WGL_NV_DX_interop WGL_NV_DX_interop20", "WGL_NV_DX_interop_extra WGL_NV_DX_interop2", "prefixWGL_NV_DX_interop WGL_NV_DX_interop2"} {
		calls := 0
		err := checkGPUInterop(list, func(string) uintptr { calls++; return 4 })
		if !errors.Is(err, ErrGPUInteropUnavailable) || calls != 0 {
			t.Fatalf("invalid extension list passed or invoked entries: %q, %v, calls=%d", list, err, calls)
		}
	}
	var resolved []string
	if err := checkGPUInterop("OTHER  WGL_NV_DX_interop2 WGL_NV_DX_interop ", func(name string) uintptr { resolved = append(resolved, name); return 4 }); err != nil || len(resolved) != 6 {
		t.Fatalf("exact supported prerequisite probe failed: %v %v", resolved, err)
	}
}

func TestInteropRejectsEveryMissingFunctionAndWGLSentinel(t *testing.T) {
	for _, missing := range interopEntryPoints {
		for _, address := range []uintptr{0, 1, 2, 3, ^uintptr(0)} {
			err := checkGPUInterop("WGL_NV_DX_interop WGL_NV_DX_interop2", func(name string) uintptr {
				if name == missing {
					return address
				}
				return 4
			})
			if !errors.Is(err, ErrGPUInteropUnavailable) || !strings.Contains(err.Error(), missing) {
				t.Fatalf("accepted %s address %#x: %v", missing, address, err)
			}
		}
	}
}
