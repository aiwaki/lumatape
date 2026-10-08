//go:build windows

package win32

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/locale"
)

type SettingsEvent struct {
	ID   int
	Code uint16
}
type ControlSpec struct {
	ID          int
	Class, Text string
	X, Y, W, H  int
	Style       uintptr
	Page        int
}
type settingsControl struct {
	spec ControlSpec
	hwnd uintptr
}
type SettingsWindow struct {
	HWND             uintptr
	callback         uintptr
	controls         map[int]settingsControl
	events           []SettingsEvent
	font, icon       uintptr
	iconOwned        bool
	dpi              int
	page             int
	muted            bool
	pixels           []byte
	imageW, imageH   int
	preview          Rect
	Notice           string
	defaultID        int
	lastFocus        uintptr
	scrollX, scrollY int // physical pixels in the virtual client
	wheelX, wheelY   int
	layoutActive     bool
}

// SCROLLINFO keeps thumb positions at full 32-bit precision (WM_*SCROLL has
// only 16 position bits). Its fields have the same layout on x86 and x64.
type settingsScrollInfo struct {
	Size, Mask    uint32
	Min, Max      int32
	Page          uint32
	Pos, TrackPos int32
}

const (
	StyleTab      = 0x10000
	StyleEdit     = 0x800000 | 0x80 // border, auto horizontal scroll
	StyleCombo    = 0x200000 | 3    // vertical scroll, dropdown list
	StyleCheck    = 3
	StyleReadOnly = 0x800 | 4 | 0x40 | 0x200000 // multiline edit
)

func NewSettingsWindow() (*SettingsWindow, error) {
	s := &SettingsWindow{controls: make(map[int]settingsControl), dpi: 96}
	s.callback = syscall.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case 0x10:
			s.hide()
			return 0
		case 0x111:
			if w&0xffff == 2 {
				s.hide()
				return 0
			}
			if !s.muted {
				s.events = append(s.events, SettingsEvent{int(w & 0xffff), uint16(w >> 16)})
			}
			return 0
		case 0x400: // DM_GETDEFID: this ordinary window uses IsDialogMessage.
			if s.defaultID != 0 {
				return uintptr(s.defaultID) | 0x534b0000 // DC_HASDEFID
			}
			return 0
		case 0x401: // DM_SETDEFID
			s.SetDefault(int(w & 0xffff))
			return 1
		case 0x2e0:
			oldDPI := s.dpi
			s.dpi = int(w & 0xffff)
			if s.dpi == 0 {
				s.dpi = 96
			}
			s.scrollX = s.scrollX * s.dpi / oldDPI
			s.scrollY = s.scrollY * s.dpi / oldDPI
			var r Rect
			kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&r)), l, unsafe.Sizeof(r))
			// Moving to a smaller/high-DPI monitor must not put the bottom of the
			// window outside its work area. The virtual client will scroll instead.
			var monitor monitorInfo
			monitor.Size = uint32(unsafe.Sizeof(monitor))
			hm := call("MonitorFromRect", uintptr(unsafe.Pointer(&r)), 2)
			if call("GetMonitorInfoW", hm, uintptr(unsafe.Pointer(&monitor))) != 0 {
				width := min(r.Right-r.Left, monitor.Work.Right-monitor.Work.Left)
				height := min(r.Bottom-r.Top, monitor.Work.Bottom-monitor.Work.Top)
				r.Left = max(monitor.Work.Left, min(r.Left, monitor.Work.Right-width))
				r.Top = max(monitor.Work.Top, min(r.Top, monitor.Work.Bottom-height))
				r.Right, r.Bottom = r.Left+width, r.Top+height
			}
			call("SetWindowPos", h, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x14)
			s.makeFont()
			s.layout()
			s.ensureFocusVisible()
			return 0
		case 5:
			s.layout()
			if !s.layoutActive {
				s.ensureFocusVisible()
			}
			return 0
		case 0x24: // Small windows scroll; do not force the entire form off screen.
			var p [10]int32
			kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&p)), l, unsafe.Sizeof(p))
			p[6] = int32(s.px(320))
			p[7] = int32(s.px(240))
			var m monitorInfo
			m.Size = uint32(unsafe.Sizeof(m))
			if call("GetMonitorInfoW", MonitorFromWindow(h), uintptr(unsafe.Pointer(&m))) != 0 {
				p[6] = min(p[6], m.Work.Right-m.Work.Left)
				p[7] = min(p[7], m.Work.Bottom-m.Work.Top)
			}
			kernel32.NewProc("RtlMoveMemory").Call(l, uintptr(unsafe.Pointer(&p)), unsafe.Sizeof(p))
			return 0
		case 0x114, 0x115: // WM_HSCROLL / WM_VSCROLL
			if l == 0 {
				s.scroll(int(msg-0x114), int(w&0xffff))
			}
			return 0
		case 0x20a, 0x20e: // wheel, including horizontal wheel / Shift+wheel
			horizontal := msg == 0x20e || w&4 != 0
			s.wheel(int(int16(w>>16)), horizontal, msg == 0x20e)
			return 0
		case 0xf:
			s.paint()
			return 0
		}
		return call("DefWindowProcW", h, uintptr(msg), w, l)
	})
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	s.icon, s.iconOwned = LoadAppIcon(false)
	wc := windowClass{Proc: s.callback, Instance: instance, Class: U16("LumaTape.Settings"), Cursor: call("LoadCursorW", 0, 32512), Background: 6, Icon: s.icon, SmallIcon: s.icon}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if call("RegisterClassExW", uintptr(unsafe.Pointer(&wc))) == 0 {
		return nil, fmt.Errorf("register settings window failed")
	}
	s.HWND = call("CreateWindowExW", 0x10000, uintptr(unsafe.Pointer(wc.Class)), uintptr(unsafe.Pointer(U16(locale.Text("LumaTape — настройки", "LumaTape — settings")))), 0x2cf0000, 0x80000000, 0x80000000, 960, 680, 0, 0, instance, 0)
	if s.HWND == 0 {
		return nil, fmt.Errorf("create settings window failed")
	}
	if p := user32.NewProc("GetDpiForWindow"); p.Find() == nil {
		d, _, _ := p.Call(s.HWND)
		if d > 0 {
			s.dpi = int(d)
		}
	}
	s.makeFont()
	// Scale the initial window on the selected monitor, keeping its nonclient area inside work bounds.
	monitors, _ := Monitors()
	m := MonitorFromWindow(s.HWND)
	for _, v := range monitors {
		if v.Handle == m {
			w, h := s.px(940), s.px(660)
			if w > v.Work.W {
				w = v.Work.W
			}
			if h > v.Work.H {
				h = v.Work.H
			}
			call("SetWindowPos", s.HWND, 0, uintptr(v.Work.X+(v.Work.W-w)/2), uintptr(v.Work.Y+(v.Work.H-h)/2), uintptr(w), uintptr(h), 0x14)
		}
	}
	return s, nil
}
func (s *SettingsWindow) px(n int) int { return n * s.dpi / 96 }
func (s *SettingsWindow) makeFont() {
	old := s.font
	gdi := syscall.NewLazyDLL("gdi32.dll")
	h, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(int64(-s.px(14))), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(U16("Segoe UI"))))
	s.font = h
	for _, c := range s.controls {
		call("SendMessageW", c.hwnd, 0x30, h, 1)
	}
	if old != 0 {
		gdi.NewProc("DeleteObject").Call(old)
	}
}
func (s *SettingsWindow) Add(c ControlSpec) {
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	style := uintptr(0x40000000|0x10000000) | c.Style
	h := call("CreateWindowExW", 0, uintptr(unsafe.Pointer(U16(c.Class))), uintptr(unsafe.Pointer(U16(c.Text))), style, 0, 0, 0, 0, s.HWND, uintptr(c.ID), instance, 0)
	s.controls[c.ID] = settingsControl{c, h}
	call("SendMessageW", h, 0x30, s.font, 1)
	s.layout()
}
func (s *SettingsWindow) layout() {
	if s.HWND == 0 || s.layoutActive {
		return
	}
	s.layoutActive = true
	defer func() { s.layoutActive = false }()
	var r Rect
	call("GetClientRect", s.HWND, uintptr(unsafe.Pointer(&r)))
	style := call("GetWindowLongPtrW", s.HWND, ^uintptr(15)) // GWL_STYLE (-16)
	metric := func(index int) int {
		if proc := user32.NewProc("GetSystemMetricsForDpi"); proc.Find() == nil {
			value, _, _ := proc.Call(uintptr(index), uintptr(s.dpi))
			return int(value)
		}
		return int(call("GetSystemMetrics", uintptr(index)))
	}
	barW, barH := metric(2), metric(3)
	baseW, baseH := int(r.Right), int(r.Bottom)
	if style&0x200000 != 0 {
		baseW += barW
	}
	if style&0x100000 != 0 {
		baseH += barH
	}
	virtualW, virtualH := s.px(912), s.px(610)
	// One scrollbar may make the other necessary. Start without either so both
	// disappear again when the form fits after a resize or a DPI change.
	horizontal, vertical := false, false
	for i := 0; i < 3; i++ {
		w, h := baseW, baseH
		if vertical {
			w -= barW
		}
		if horizontal {
			h -= barH
		}
		horizontal = virtualW > w
		vertical = virtualH > h
	}
	for bar, needed := range []bool{horizontal, vertical} {
		visible := uintptr(0)
		if needed {
			visible = 1
		}
		call("ShowScrollBar", s.HWND, uintptr(bar), visible)
	}
	call("GetClientRect", s.HWND, uintptr(unsafe.Pointer(&r)))
	clientW, clientH := max(1, int(r.Right)), max(1, int(r.Bottom))
	contentW, contentH := max(virtualW, clientW), max(virtualH, clientH)
	s.scrollX = max(0, min(s.scrollX, contentW-clientW))
	s.scrollY = max(0, min(s.scrollY, contentH-clientH))
	for bar, values := range [][3]int{{contentW, clientW, s.scrollX}, {contentH, clientH, s.scrollY}} {
		info := settingsScrollInfo{Mask: 7, Max: int32(values[0] - 1), Page: uint32(values[1]), Pos: int32(values[2])}
		info.Size = uint32(unsafe.Sizeof(info))
		call("SetScrollInfo", s.HWND, uintptr(bar), uintptr(unsafe.Pointer(&info)), 1)
	}
	for _, c := range s.controls {
		call("MoveWindow", c.hwnd, uintptr(s.px(c.spec.X)-s.scrollX), uintptr(s.px(c.spec.Y)-s.scrollY), uintptr(s.px(c.spec.W)), uintptr(s.px(c.spec.H)), 1)
		show := uintptr(0)
		if c.spec.Page == 0 || c.spec.Page == s.page {
			show = 5
		}
		call("ShowWindow", c.hwnd, show)
	}
	s.preview = Rect{int32(s.px(450) - s.scrollX), int32(s.px(95) - s.scrollY), int32(contentW - s.px(18) - s.scrollX), int32(s.px(370) - s.scrollY)}
	call("InvalidateRect", s.HWND, 0, 1)
}

func (s *SettingsWindow) scroll(bar, command int) {
	info := settingsScrollInfo{Mask: 0x17} // range, page, position, full track position
	info.Size = uint32(unsafe.Sizeof(info))
	if call("GetScrollInfo", s.HWND, uintptr(bar), uintptr(unsafe.Pointer(&info))) == 0 {
		return
	}
	pos := int(info.Pos)
	switch command {
	case 0:
		pos -= s.px(24)
	case 1:
		pos += s.px(24)
	case 2:
		pos -= max(s.px(24), int(info.Page)-s.px(24))
	case 3:
		pos += max(s.px(24), int(info.Page)-s.px(24))
	case 4, 5:
		pos = int(info.TrackPos)
	case 6:
		pos = int(info.Min)
	case 7:
		pos = int(info.Max)
	default:
		return
	}
	if bar == 0 {
		s.scrollX = pos
	} else {
		s.scrollY = pos
	}
	s.layout()
}

func (s *SettingsWindow) wheel(delta int, horizontal, nativeHorizontal bool) {
	bar := 1
	accumulator := &s.wheelY
	if horizontal {
		bar, accumulator = 0, &s.wheelX
	}
	*accumulator += delta
	steps := *accumulator / 120
	*accumulator %= 120
	if steps == 0 {
		return
	}
	var lines uint32 = 3
	call("SystemParametersInfoW", 0x68, 0, uintptr(unsafe.Pointer(&lines)), 0) // SPI_GETWHEELSCROLLLINES
	if lines == 0 {
		return
	}
	distance := s.px(24) * int(lines)
	if lines == ^uint32(0) {
		var r Rect
		call("GetClientRect", s.HWND, uintptr(unsafe.Pointer(&r)))
		distance = int(r.Bottom)
		if horizontal {
			distance = int(r.Right)
		}
	}
	if !nativeHorizontal {
		steps = -steps
	} // positive horizontal-wheel delta means right
	if bar == 0 {
		s.scrollX += steps * distance
	} else {
		s.scrollY += steps * distance
	}
	s.layout()
}

// ensureFocusVisible makes keyboard navigation useful even when the focused
// control is beyond the scroll viewport. It is not called for mouse scrolling.
func (s *SettingsWindow) ensureFocusVisible() {
	if s.HWND == 0 || s.layoutActive {
		return
	}
	focus := call("GetFocus")
	if focus == 0 || call("IsChild", s.HWND, focus) == 0 {
		return
	}
	var bounds, client Rect
	if call("GetWindowRect", focus, uintptr(unsafe.Pointer(&bounds))) == 0 {
		return
	}
	call("MapWindowPoints", 0, s.HWND, uintptr(unsafe.Pointer(&bounds)), 2)
	call("GetClientRect", s.HWND, uintptr(unsafe.Pointer(&client)))
	margin := s.px(8)
	x, y := s.scrollX, s.scrollY
	if bounds.Right > client.Right-int32(margin) {
		s.scrollX += int(bounds.Right-client.Right) + margin
	}
	if bounds.Left < int32(margin) || bounds.Right-bounds.Left > client.Right-int32(2*margin) {
		s.scrollX = x + int(bounds.Left) - margin
	}
	if bounds.Bottom > client.Bottom-int32(margin) {
		s.scrollY += int(bounds.Bottom-client.Bottom) + margin
	}
	if bounds.Top < int32(margin) || bounds.Bottom-bounds.Top > client.Bottom-int32(2*margin) {
		s.scrollY = y + int(bounds.Top) - margin
	}
	if x != s.scrollX || y != s.scrollY {
		s.layout()
	}
}

// SetDefault selects the button invoked by Enter when focus is in an edit.
// Focused push buttons retain normal native Enter/Space behavior.
func (s *SettingsWindow) SetDefault(id int) {
	c, ok := s.controls[id]
	if !ok || c.spec.Class != "BUTTON" || c.spec.Style&0xf > 1 {
		return
	}
	if old, ok := s.controls[s.defaultID]; ok && s.defaultID != id {
		call("SendMessageW", old.hwnd, 0xf4, 0, 1) // BM_SETSTYLE, BS_PUSHBUTTON
	}
	s.defaultID = id
	call("SendMessageW", c.hwnd, 0xf4, 1, 1) // BS_DEFPUSHBUTTON
}

// PreviewSize is the physical 16:9 render size. Whole 16x9 units avoid GDI
// resampling at ordinary sizes; the upper bound limits synchronous GL readback.
func (s *SettingsWindow) PreviewSize() (int, int) {
	units := min(int(s.preview.Right-s.preview.Left)/16, int(s.preview.Bottom-s.preview.Top)/9, 80)
	units = max(8, units)
	return units * 16, units * 9
}

func (s *SettingsWindow) Page(page int) { s.page = page; s.layout() }
func (s *SettingsWindow) Visible() bool {
	return s != nil && s.HWND != 0 && call("IsWindowVisible", s.HWND) != 0
}
func (s *SettingsWindow) Show() {
	call("ShowWindow", s.HWND, 9)
	call("SetForegroundWindow", s.HWND)
	if call("IsChild", s.HWND, call("GetFocus")) == 0 {
		focus := s.lastFocus
		if focus == 0 || call("IsWindowVisible", focus) == 0 || call("IsWindowEnabled", focus) == 0 {
			focus = call("GetNextDlgTabItem", s.HWND, 0, 0)
		}
		if focus != 0 {
			call("SetFocus", focus)
		}
	}
	s.ensureFocusVisible()
}
func (s *SettingsWindow) hide() {
	if focus := call("GetFocus"); call("IsChild", s.HWND, focus) != 0 {
		s.lastFocus = focus
	}
	call("ShowWindow", s.HWND, 0)
}
func (s *SettingsWindow) Events() []SettingsEvent { r := s.events; s.events = nil; return r }
func (s *SettingsWindow) SetText(id int, text string) {
	s.muted = true
	defer func() { s.muted = false }()
	call("SetWindowTextW", s.controls[id].hwnd, uintptr(unsafe.Pointer(U16(text))))
}
func (s *SettingsWindow) Text(id int) string {
	h := s.controls[id].hwnd
	n := call("GetWindowTextLengthW", h)
	if n > 65536 {
		n = 65536
	}
	b := make([]uint16, n+1)
	call("GetWindowTextW", h, uintptr(unsafe.Pointer(&b[0])), n+1)
	return syscall.UTF16ToString(b)
}
func (s *SettingsWindow) Options(id int, labels []string, selected int) {
	s.muted = true
	defer func() { s.muted = false }()
	h := s.controls[id].hwnd
	call("SendMessageW", h, 0x14b, 0, 0)
	for _, label := range labels {
		call("SendMessageW", h, 0x143, 0, uintptr(unsafe.Pointer(U16(label))))
	}
	call("SendMessageW", h, 0x14e, uintptr(selected), 0)
}
func (s *SettingsWindow) Select(id, selected int) {
	s.muted = true
	defer func() { s.muted = false }()
	call("SendMessageW", s.controls[id].hwnd, 0x14e, uintptr(selected), 0)
}
func (s *SettingsWindow) Selected(id int) int {
	return int(int32(call("SendMessageW", s.controls[id].hwnd, 0x147, 0, 0)))
}
func (s *SettingsWindow) Check(id int, checked bool) {
	v := uintptr(0)
	if checked {
		v = 1
	}
	call("SendMessageW", s.controls[id].hwnd, 0xf1, v, 0)
}
func (s *SettingsWindow) Checked(id int) bool {
	return call("SendMessageW", s.controls[id].hwnd, 0xf0, 0, 0) == 1
}
func (s *SettingsWindow) Enable(id int, enabled bool) {
	v := uintptr(0)
	if enabled {
		v = 1
	}
	call("EnableWindow", s.controls[id].hwnd, v)
}
func (s *SettingsWindow) SetPreview(rgba []byte, width, height int) {
	if width <= 0 || height <= 0 || width > 1920 || height > 1080 || len(rgba) != width*height*4 {
		return
	}
	s.imageW = width
	s.imageH = height
	s.pixels = make([]byte, len(rgba))
	for i := 0; i < len(rgba); i += 4 {
		s.pixels[i] = rgba[i+2]
		s.pixels[i+1] = rgba[i+1]
		s.pixels[i+2] = rgba[i]
		s.pixels[i+3] = 255
	}
	call("InvalidateRect", s.HWND, uintptr(unsafe.Pointer(&s.preview)), 0)
}
func (s *SettingsWindow) paint() {
	if s.HWND == 0 {
		return
	}
	var ps struct {
		DC                 uintptr
		Erase              int32
		Paint              Rect
		Restore, IncUpdate int32
		Reserved           [32]byte
	}
	dc := call("BeginPaint", s.HWND, uintptr(unsafe.Pointer(&ps)))
	defer call("EndPaint", s.HWND, uintptr(unsafe.Pointer(&ps)))
	if len(s.pixels) == 0 {
		return
	}
	info := struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, SizeImage uint32
		X, Y                   int32
		Used, Important        uint32
	}{Size: 40, Width: int32(s.imageW), Height: -int32(s.imageH), Planes: 1, Bits: 32}
	r := s.preview
	call("FillRect", dc, uintptr(unsafe.Pointer(&r)), 6)
	// Keep circles round while the settings window is resized.
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if s.imageW*9 == s.imageH*16 {
		units := min(w/16, h/9)
		nw, nh := units*16, units*9
		r.Left += (w - nw) / 2
		r.Top += (h - nh) / 2
		r.Right, r.Bottom = r.Left+nw, r.Top+nh
	} else if int(w)*s.imageH > int(h)*s.imageW {
		nw := int32(int(h) * s.imageW / s.imageH)
		r.Left += (w - nw) / 2
		r.Right = r.Left + nw
	} else {
		nh := int32(int(w) * s.imageH / s.imageW)
		r.Top += (h - nh) / 2
		r.Bottom = r.Top + nh
	}
	syscall.NewLazyDLL("gdi32.dll").NewProc("StretchDIBits").Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0, 0, uintptr(s.imageW), uintptr(s.imageH), uintptr(unsafe.Pointer(&s.pixels[0])), uintptr(unsafe.Pointer(&info)), 0, 0xcc0020)
}
func (s *SettingsWindow) Close() {
	if s == nil || s.HWND == 0 {
		return
	}
	call("DestroyWindow", s.HWND)
	s.HWND = 0
	if s.font != 0 {
		syscall.NewLazyDLL("gdi32.dll").NewProc("DeleteObject").Call(s.font)
	}
	if s.iconOwned {
		call("DestroyIcon", s.icon)
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	call("UnregisterClassW", uintptr(unsafe.Pointer(U16("LumaTape.Settings"))), instance)
}

// IsDialogMessage provides native Tab/Shift+Tab, mnemonics and arrow navigation
// for standard controls before GLFW's message pump consumes their messages.
func PumpSettingsMessages(s *SettingsWindow) {
	if !s.Visible() {
		return
	}
	var msg struct {
		Hwnd    uintptr
		Message uint32
		W, L    uintptr
		Time    uint32
		Point   Point
		Private uint32
	}
	for call("PeekMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1) != 0 {
		focus := call("GetFocus")
		if call("IsDialogMessageW", s.HWND, uintptr(unsafe.Pointer(&msg))) == 0 {
			call("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
			call("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
		}
		if call("GetFocus") != focus {
			s.ensureFocusVisible()
		}
	}
}
