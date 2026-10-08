//go:build windows

package win32

import (
	"os"
	"runtime"
	"testing"
	"unsafe"
)

func TestSettingsPreviewPhysicalSize(t *testing.T) {
	for _, dpi := range []int{96, 120, 144, 192, 288, 384} {
		s := SettingsWindow{dpi: dpi}
		s.preview = Rect{0, 0, int32(s.px(444)), int32(s.px(275))}
		w, h := s.PreviewSize()
		if w*9 != h*16 || w > 1280 || h > 720 || w < 128 || h < 72 {
			t.Fatalf("DPI %d: invalid render dimensions %dx%d", dpi, w, h)
		}
		if w > int(s.preview.Right) || h > int(s.preview.Bottom) {
			t.Fatalf("DPI %d: render exceeds preview area", dpi)
		}
		if w%16 != 0 || h%9 != 0 {
			t.Fatalf("DPI %d: preview would need fractional 16:9 scaling", dpi)
		}
	}
}

func TestSettingsScrollInfoABI(t *testing.T) {
	var info settingsScrollInfo
	if unsafe.Sizeof(info) != 28 || unsafe.Offsetof(info.TrackPos) != 24 {
		t.Fatal("SCROLLINFO no longer matches Win32 ABI")
	}
}

// This test creates a real native window and drives its normal message pump.
// Run on an interactive Windows desktop with LUMATAPE_NATIVE_UI_TEST=1; a
// headless service is not evidence for native keyboard/focus behavior.
func TestSettingsNativeScrollKeyboard(t *testing.T) {
	if os.Getenv("LUMATAPE_NATIVE_UI_TEST") != "1" {
		t.Skip("set LUMATAPE_NATIVE_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := InitializeDPI(); err != nil {
		t.Fatal(err)
	}
	s, err := NewSettingsWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const edit, hidden, stop, apply = 101, 102, 103, 104
	s.Add(ControlSpec{ID: edit, Class: "EDIT", X: 18, Y: 94, W: 240, H: 26, Page: 1, Style: StyleEdit | StyleTab})
	s.Add(ControlSpec{ID: hidden, Class: "EDIT", X: 18, Y: 130, W: 240, H: 26, Page: 2, Style: StyleEdit | StyleTab})
	s.Add(ControlSpec{ID: stop, Class: "BUTTON", Text: "STOP test", X: 625, Y: 562, W: 269, H: 30, Style: StyleTab})
	s.Add(ControlSpec{ID: apply, Class: "BUTTON", Text: "Apply test", X: 18, Y: 562, W: 135, H: 30, Style: StyleTab})
	s.SetDefault(apply)
	s.Page(1)
	s.Show()
	resize := func(width, height int) {
		t.Helper()
		if call("SetWindowPos", s.HWND, 0, 0, 0, uintptr(s.px(width)), uintptr(s.px(height)), 0x16) == 0 {
			t.Fatal("resize failed")
		}
	}
	inside := func(id int) {
		t.Helper()
		var r, client Rect
		call("GetWindowRect", s.controls[id].hwnd, uintptr(unsafe.Pointer(&r)))
		call("MapWindowPoints", 0, s.HWND, uintptr(unsafe.Pointer(&r)), 2)
		call("GetClientRect", s.HWND, uintptr(unsafe.Pointer(&client)))
		if r.Left < 0 || r.Top < 0 || r.Right > client.Right || r.Bottom > client.Bottom {
			t.Fatalf("control %d is clipped: bounds %+v, viewport %+v", id, r, client)
		}
	}
	key := func(vk uintptr) {
		t.Helper()
		if call("PostMessageW", call("GetFocus"), 0x100, vk, 1) == 0 {
			t.Fatal("post key failed")
		}
		PumpSettingsMessages(s)
	}
	resize(480, 360)
	if style := call("GetWindowLongPtrW", s.HWND, ^uintptr(15)); style&0x300000 != 0x300000 {
		t.Fatalf("small form lacks both scrollbars: style %#x", style)
	}
	call("SendMessageW", s.HWND, 0x114, 7, 0) // right edge
	call("SendMessageW", s.HWND, 0x115, 7, 0) // bottom edge
	inside(stop)
	call("SendMessageW", s.HWND, 0x114, 6, 0)
	call("SendMessageW", s.HWND, 0x115, 6, 0)
	call("SetFocus", s.controls[edit].hwnd)
	key(9) // hidden page must be skipped, off-viewport STOP must become visible
	if call("GetFocus") != s.controls[stop].hwnd {
		t.Fatal("Tab did not skip hidden page to STOP")
	}
	inside(stop)
	s.Events()
	key(13)
	if events := s.Events(); len(events) != 1 || events[0].ID != stop {
		t.Fatalf("Enter on focused button: %+v", events)
	}
	call("SetFocus", s.controls[edit].hwnd)
	s.ensureFocusVisible()
	s.Events()
	key(13)
	if events := s.Events(); len(events) != 1 || events[0].ID != apply {
		t.Fatalf("Enter in edit must invoke Apply: %+v", events)
	}
	resize(1100, 800)
	if s.scrollX != 0 || s.scrollY != 0 {
		t.Fatal("scroll offset survived a size where form fits")
	}
	if style := call("GetWindowLongPtrW", s.HWND, ^uintptr(15)); style&0x300000 != 0 {
		t.Fatal("scrollbars survived a size where form fits")
	}
	// Exercise the handler with an explicit DPI message; a real cross-monitor
	// transition remains a separate manual qualification.
	r := Rect{Left: 20, Top: 20, Right: 740, Bottom: 560}
	call("SendMessageW", s.HWND, 0x2e0, 144|(144<<16), uintptr(unsafe.Pointer(&r)))
	if s.dpi != 144 {
		t.Fatal("WM_DPICHANGED was not applied")
	}
	call("SetFocus", s.controls[stop].hwnd)
	s.ensureFocusVisible()
	inside(stop)
	key(27)
	if s.Visible() {
		t.Fatal("Escape did not hide settings")
	}
}
