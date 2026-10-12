//go:build windows

package pointer

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/geometry"
)

// Exercise real GDI black/white rendering and layered-window positioning without
// ever showing the test HWND, changing focus or hiding the user's cursor.
func TestNativeCursorShapesAndHotspot(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous := uc("SetThreadDpiAwarenessContext", ^uintptr(3))
	if previous == 0 {
		t.Fatal("set physical-pixel test context")
	}
	defer uc("SetThreadDpiAwarenessContext", previous)
	d := &nativeDriver{}
	d.window = uc("CreateWindowExW", 0x00080000|0x20|0x08000000|0x80, uintptr(unsafe.Pointer(u16("STATIC"))), uintptr(unsafe.Pointer(u16("LumaTape cursor render test"))), 0x80000000, 0, 0, 1, 1, 0, 0, 0, 0)
	if d.window == 0 {
		t.Fatal("create hidden layered test window")
	}
	defer uc("DestroyWindow", d.window)
	defer d.freeBuffer()
	// Explicit XOR cursor; native schemes can replace the built-in I-beam with
	// alpha cursors, so testing only LoadCursor would not cover the regression.
	andMask, xorMask := make([]byte, 128), make([]byte, 128)
	for i := range andMask {
		andMask[i] = 255
	}
	for y := 4; y < 28; y++ {
		xorMask[y*4+2] = 0x80
	}
	xor := uc("CreateCursor", 0, 16, 16, 32, 32, uintptr(unsafe.Pointer(&andMask[0])), uintptr(unsafe.Pointer(&xorMask[0])))
	if xor == 0 {
		t.Fatal("create XOR test cursor")
	}
	defer uc("DestroyCursor", xor)
	clearMask := make([]byte, 128)
	clearCursor := uc("CreateCursor", 0, 16, 16, 32, 32, uintptr(unsafe.Pointer(&andMask[0])), uintptr(unsafe.Pointer(&clearMask[0])))
	if clearCursor == 0 {
		t.Fatal("create transparent test cursor")
	}
	defer uc("DestroyCursor", clearCursor)
	shapes := []struct {
		name   string
		handle uintptr
	}{
		{"arrow", uc("LoadCursorW", 0, 32512)}, {"ibeam", uc("LoadCursorW", 0, 32513)},
		{"resize", uc("LoadCursorW", 0, 32644)}, {"xor", xor}, {"transparent-after-xor", clearCursor}, {"arrow-again", uc("LoadCursorW", 0, 32512)},
	}
	var xorDIB uintptr
	for _, tc := range shapes {
		t.Logf("cursor: %s", tc.name)
		if tc.handle == 0 {
			t.Fatal("load test cursor")
		}
		if err := d.prepareCursor(tc.handle, geometry.PointerPoint{X: 240, Y: 180}); err != nil {
			t.Fatal(err)
		}
		var icon iconInfo
		if uc("GetIconInfo", tc.handle, uintptr(unsafe.Pointer(&icon))) == 0 {
			t.Fatal("read original hotspot")
		}
		defer gc("DeleteObject", icon.Mask)
		if icon.Color != 0 {
			defer gc("DeleteObject", icon.Color)
		}
		var bounds rect
		if uc("GetWindowRect", d.window, uintptr(unsafe.Pointer(&bounds))) == 0 {
			t.Fatal("read actual projection position")
		}
		if bounds.L+int32(icon.HotX)+1 != 240 || bounds.T+int32(icon.HotY)+1 != 180 {
			t.Fatalf("hotspot moved: %+v / %+v", bounds, icon)
		}
		if uc("IsWindowVisible", d.window) != 0 {
			t.Fatal("test unexpectedly showed cursor window")
		}
		for i := 0; i < d.width*d.height*4; i += 4 {
			for c := 0; c < 3; c++ {
				if d.pixels[i+c] > d.pixels[i+3] {
					t.Fatal("invalid premultiplied pixel")
				}
			}
		}
		if tc.name == "xor" {
			xorDIB = d.dib
			center := (10*d.width + 17) * 4
			border := (10*d.width + 16) * 4
			if d.pixels[center] != 255 || d.pixels[center+3] != 255 || d.pixels[border] != 0 || d.pixels[border+3] != 255 {
				t.Fatal("native XOR shape/outline missing")
			}
		}
		if tc.name == "transparent-after-xor" {
			if d.dib != xorDIB {
				t.Fatal("fixture did not reuse native pixel buffer")
			}
			for i := 0; i < d.width*d.height*4; i++ {
				if d.pixels[i] != 0 {
					t.Fatal("previous XOR cursor pixels survived buffer reuse")
				}
			}
		}
	}
}
