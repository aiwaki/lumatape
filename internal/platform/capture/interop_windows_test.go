//go:build windows

package capture

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestWGLProbeReadFailureDoesNotClaimUnsupportedGPU(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ProbeGPUInterop(nil); err == nil || errors.Is(err, ErrGPUInteropUnavailable) {
		t.Fatalf("missing owning context/resolver must remain unknown: %v", err)
	}
	for _, pointer := range []uintptr{0, 1, 2, 3, ^uintptr(0)} {
		_, err := readWGLExtensions(func(string) uintptr { return pointer }, 42)
		if err == nil || errors.Is(err, ErrGPUInteropUnavailable) {
			t.Fatalf("missing query API must remain unknown: %#x %v", pointer, err)
		}
	}
}

func TestWGLStringProbeUsesActualDCAndDoesNotFallbackAfterARBNull(t *testing.T) {
	data := append([]byte("WGL_NV_DX_interop WGL_NV_DX_interop2"), 0)
	var actualDC uintptr
	arb := syscall.NewCallback(func(dc uintptr) uintptr { actualDC = dc; return uintptr(unsafe.Pointer(&data[0])) })
	got, err := readWGLExtensions(func(name string) uintptr {
		if name == "wglGetExtensionsStringARB" {
			return arb
		}
		t.Fatal("unexpected EXT fallback")
		return 0
	}, 42)
	if err != nil || got != string(data[:len(data)-1]) || actualDC != 42 {
		t.Fatalf("wrong ARB ABI/string: %q, DC=%d, error=%v", got, actualDC, err)
	}
	runtime.KeepAlive(data)
	nullARB := syscall.NewCallback(func(uintptr) uintptr { return 0 })
	_, err = readWGLExtensions(func(name string) uintptr {
		if name != "wglGetExtensionsStringARB" {
			t.Fatal("native ABI does not fallback after ARB returned NULL")
		}
		return nullARB
	}, 42)
	if err == nil || errors.Is(err, ErrGPUInteropUnavailable) {
		t.Fatalf("NULL driver result must remain unknown: %v", err)
	}
}

func TestWGLStringProbeRejectsTruncation(t *testing.T) {
	data := append([]byte(strings.Repeat("X", 64<<10)), 0)
	ext := syscall.NewCallback(func() uintptr { return uintptr(unsafe.Pointer(&data[0])) })
	_, err := readWGLExtensions(func(name string) uintptr {
		if name == "wglGetExtensionsStringEXT" {
			return ext
		}
		return 0
	}, 42)
	if err == nil || errors.Is(err, ErrGPUInteropUnavailable) {
		t.Fatalf("truncation must remain unknown: %v", err)
	}
	runtime.KeepAlive(data)
}
