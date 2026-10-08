package win32

import "testing"

func TestSourceWindowEligibility(t *testing.T) {
	game := sourceWindowInfo{
		className: "GameWindow", style: 0x00cf0000, visible: true,
		processID: 42, currentProcessID: 7, title: "Game",
	}
	type testCase struct {
		name          string
		change        func(*sourceWindowInfo)
		capture, menu bool
	}
	tests := []testCase{
		{"windowed game", func(w *sourceWindowInfo) {}, true, true},
		{"borderless popup", func(w *sourceWindowInfo) { w.style = 0x80000000 }, true, true},
		{"topmost borderless", func(w *sourceWindowInfo) { w.style, w.extendedStyle = 0x80000000, 0x8 }, true, true},
		{"minimized game", func(w *sourceWindowInfo) { w.style |= 0x20000000 }, true, true},
		{"testcard", func(w *sourceWindowInfo) { w.className, w.title = "GLFW30", "LumaTape testcard" }, true, true},
		{"Explorer folder", func(w *sourceWindowInfo) { w.className = "CabinetWClass" }, true, true},
		{"unrelated title matches shell", func(w *sourceWindowInfo) { w.title = "Program Manager" }, true, true},
		{"unrelated title matches tray", func(w *sourceWindowInfo) { w.title = "System tray overflow window." }, true, true},
		{"unrelated title matches helper", func(w *sourceWindowInfo) { w.title = "LumaTape pointer" }, true, true},
		{"untitled source", func(w *sourceWindowInfo) { w.title = "" }, true, false},
		{"hidden game", func(w *sourceWindowInfo) { w.visible = false }, true, false},
		{"DWM cloaked", func(w *sourceWindowInfo) { w.cloaked = true }, true, false},
		{"owned dialog", func(w *sourceWindowInfo) { w.owned = true }, true, false},
		{"own process", func(w *sourceWindowInfo) { w.processID = w.currentProcessID }, true, false},
		{"missing process", func(w *sourceWindowInfo) { w.processID = 0 }, true, false},
		{"missing class", func(w *sourceWindowInfo) { w.className = "" }, false, false},
		{"shell HWND", func(w *sourceWindowInfo) { w.shellWindow = true }, false, false},
		{"child window", func(w *sourceWindowInfo) { w.style |= 0x40000000 }, false, false},
		{"tool window", func(w *sourceWindowInfo) { w.extendedStyle = 0x80 }, false, false},
		{"no activate window", func(w *sourceWindowInfo) { w.extendedStyle = 0x08000000 }, false, false},
		{"overlay", func(w *sourceWindowInfo) { w.className, w.extendedStyle = "GLFW30", 0x080800a0 }, false, false},
	}
	for _, class := range []string{
		"Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd",
		"NotifyIconOverflowWindow", "TopLevelWindowForOverflowXamlIsland",
		"LumaTape.PointerProjection", "LumaTape.Control", "LumaTape.Settings",
		"PROGMAN", "lumatape.pointerprojection",
	} {
		tests = append(tests, testCase{class, func(w *sourceWindowInfo) { w.className, w.title = class, "Renamed service window" }, false, false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := game
			tt.change(&w)
			if got := w.captureAllowed(); got != tt.capture {
				t.Errorf("captureAllowed = %v, want %v", got, tt.capture)
			}
			if got := w.listed(); got != tt.menu {
				t.Errorf("listed = %v, want %v", got, tt.menu)
			}
		})
	}
}
