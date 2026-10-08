//go:build windows

// lumatape-testcard is a real, external, interactive HWND used for manual validation
// of the overlay/capture application. It does not emulate a game or a GPU.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/locale"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	gdi32             = syscall.NewLazyDLL("gdi32.dll")
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	setDPI            = user32.NewProc("SetProcessDpiAwarenessContext")
	getDPI            = user32.NewProc("GetDpiForSystem")
	getWindowDPI      = user32.NewProc("GetDpiForWindow")
	getThreadDPI      = user32.NewProc("GetThreadDpiAwarenessContext")
	equalDPI          = user32.NewProc("AreDpiAwarenessContextsEqual")
	adjustRect        = user32.NewProc("AdjustWindowRectExForDpi")
	registerClass     = user32.NewProc("RegisterClassExW")
	unregisterClass   = user32.NewProc("UnregisterClassW")
	createWindow      = user32.NewProc("CreateWindowExW")
	destroyWindow     = user32.NewProc("DestroyWindow")
	defaultProc       = user32.NewProc("DefWindowProcW")
	showWindow        = user32.NewProc("ShowWindow")
	getMessage        = user32.NewProc("GetMessageW")
	translateMessage  = user32.NewProc("TranslateMessage")
	dispatchMessage   = user32.NewProc("DispatchMessageW")
	postQuit          = user32.NewProc("PostQuitMessage")
	loadCursor        = user32.NewProc("LoadCursorW")
	setTimer          = user32.NewProc("SetTimer")
	killTimer         = user32.NewProc("KillTimer")
	invalidateRect    = user32.NewProc("InvalidateRect")
	beginPaint        = user32.NewProc("BeginPaint")
	endPaint          = user32.NewProc("EndPaint")
	getClientRect     = user32.NewProc("GetClientRect")
	setWindowPos      = user32.NewProc("SetWindowPos")
	fillRect          = user32.NewProc("FillRect")
	getModule         = kernel32.NewProc("GetModuleHandleW")
	createDC          = gdi32.NewProc("CreateCompatibleDC")
	deleteDC          = gdi32.NewProc("DeleteDC")
	createBitmap      = gdi32.NewProc("CreateCompatibleBitmap")
	selectObject      = gdi32.NewProc("SelectObject")
	deleteObject      = gdi32.NewProc("DeleteObject")
	getStockObject    = gdi32.NewProc("GetStockObject")
	setBrushColor     = gdi32.NewProc("SetDCBrushColor")
	setPenColor       = gdi32.NewProc("SetDCPenColor")
	setTextColor      = gdi32.NewProc("SetTextColor")
	setBackgroundMode = gdi32.NewProc("SetBkMode")
	textOut           = gdi32.NewProc("TextOutW")
	createFont        = gdi32.NewProc("CreateFontW")
	moveTo            = gdi32.NewProc("MoveToEx")
	lineTo            = gdi32.NewProc("LineTo")
	ellipse           = gdi32.NewProc("Ellipse")
	bitBlt            = gdi32.NewProc("BitBlt")
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type paintStruct struct {
	DC                 uintptr
	Erase              int32
	Paint              rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type minMaxInfo struct{ Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point }

type cardState struct {
	start                      time.Time
	clicks                     int
	dc, bitmap, originalBitmap uintptr
	width, height              int
	fonts                      [4]uintptr
	colorField                 bool
	pointerTest                bool
	pointer                    pointerState
	fullscreen                 fullscreenState
	fullscreenButton           uintptr
	err                        error
}

var card cardState

// Read-only observation for the separate developer UI smoke helper. Zero means
// unavailable to an observer, so the property stores the painted count plus one.
var clickProperty, _ = syscall.UTF16PtrFromString("LumaTape.TestCard.Clicks")

func main() {
	// main executes on the initial OS thread; never hand window creation or the
	// message loop to an arbitrary goroutine that merely calls LockOSThread.
	runtime.LockOSThread()
	width := flag.Int("width", 960, "initial client width in physical pixels")
	height := flag.Int("height", 720, "initial client height in physical pixels")
	colorField := flag.Bool("color-field", false, "show static midtones for comparing VHS noise and color bleed")
	pointerTest := flag.Bool("pointer-test", false, "native corner/center targets with hover, click, capture, drag and wheel observations")
	fullscreen := flag.Bool("fullscreen", false, "start borderless on the current monitor; F11 / Alt+Enter toggle, Escape returns to window")
	flag.Parse()
	if err := run(*width, *height, *colorField, *pointerTest, *fullscreen); err != nil {
		fmt.Fprintln(os.Stderr, "LumaTape test card:", err)
		os.Exit(1)
	}
}

func run(width, height int, colorField, pointerTest, fullscreen bool) error {
	if width < 320 || height < 240 || width > 16384 || height > 16384 {
		return fmt.Errorf("initial client size must be between 320x240 and 16384x16384")
	}
	if err := setDPI.Find(); err != nil {
		return fmt.Errorf("Windows 10 1703+ with per-monitor DPI v2 is required: %w", err)
	}
	perMonitorV2 := ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	ok, _, dpiErr := setDPI.Call(perMonitorV2)
	if ok == 0 {
		// A future manifest may already set the required context.
		context, _, _ := getThreadDPI.Call()
		equal, _, _ := equalDPI.Call(context, perMonitorV2)
		if equal == 0 {
			return fmt.Errorf("set DPI v2: %w", dpiErr)
		}
	}
	instance, _, err := getModule.Call(0)
	if instance == 0 {
		return fmt.Errorf("get module: %w", err)
	}
	name, _ := syscall.UTF16PtrFromString("LumaTape.Native.TestCard")
	windowTitle := locale.Text("LumaTape — тестовая сцена", "LumaTape test card")
	if pointerTest {
		windowTitle = locale.Text("LumaTape — проверка курсора", "LumaTape pointer test")
	}
	title, _ := syscall.UTF16PtrFromString(windowTitle)
	cursor, _, _ := loadCursor.Call(0, 32512) // IDC_ARROW; one system cursor.
	class := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), Style: 0x1 | 0x2,
		Proc: syscall.NewCallback(windowProc), Instance: instance, Cursor: cursor, ClassName: name}
	atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("register testcard class: %w", err)
	}
	defer unregisterClass.Call(uintptr(unsafe.Pointer(name)), instance)
	card = cardState{start: time.Now(), colorField: colorField, pointerTest: pointerTest}
	defer card.close()
	if err := card.createFonts(); err != nil {
		return err
	}
	const style = 0x00CF0000 | 0x02000000 // WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN.
	dpi, _, _ := getDPI.Call()
	bounds := rect{Right: int32(width), Bottom: int32(height)}
	ok, _, err = adjustRect.Call(uintptr(unsafe.Pointer(&bounds)), style, 0, 0, dpi)
	if ok == 0 {
		return fmt.Errorf("adjust initial client rectangle: %w", err)
	}
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)),
		style, 0x80000000, 0x80000000, uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top),
		0, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("create testcard window: %w", err)
	}
	defer destroyWindow.Call(hwnd)
	// The shell's default placement can choose a monitor with another DPI.
	// Recompute chrome there before showing, preserving requested physical pixels.
	dpi, _, _ = getWindowDPI.Call(hwnd)
	bounds = rect{Right: int32(width), Bottom: int32(height)}
	ok, _, err = adjustRect.Call(uintptr(unsafe.Pointer(&bounds)), style, 0, 0, dpi)
	if ok == 0 {
		return fmt.Errorf("adjust monitor client rectangle: %w", err)
	}
	ok, _, err = setWindowPos.Call(hwnd, 0, 0, 0, uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), 0x2|0x4|0x10)
	if ok == 0 {
		return fmt.Errorf("size physical client rectangle: %w", err)
	}
	timer, _, err := setTimer.Call(hwnd, 1, 16, 0)
	if timer == 0 {
		return fmt.Errorf("create animation timer: %w", err)
	}
	defer killTimer.Call(hwnd, 1)
	if err := card.createFullscreenButton(hwnd, instance); err != nil {
		return err
	}
	showWindow.Call(hwnd, 5) // Normal activation is intentional for this source.
	if fullscreen {
		if err := card.setFullscreen(hwnd, true); err != nil {
			return err
		}
	}
	fmt.Printf("LumaTape test card: %dx%d physical client pixels; select title %q in LumaTape.\n", width, height, windowTitle)
	var msg message
	for {
		status, _, err := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(status) == -1 {
			return fmt.Errorf("message loop: %w", err)
		}
		if status == 0 {
			break
		}
		if card.fullscreenKey(hwnd, msg) {
			continue
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	return card.err
}

func windowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if card.pointerTest && card.pointerMessage(hwnd, msg, wparam, lparam) {
		return 0
	}
	switch msg {
	case 0x0111: // WM_COMMAND from the native fullscreen button.
		if lparam == card.fullscreenButton && card.fullscreenButton != 0 && wparam>>16 == 0 {
			card.requestFullscreen(hwnd, !card.fullscreen.active)
			user32.NewProc("SetFocus").Call(hwnd)
			return 0
		}
	case 0x007E: // WM_DISPLAYCHANGE; fullscreen follows the nearest remaining monitor.
		if card.fullscreen.active && !card.fullscreen.changing {
			card.refitFullscreen(hwnd)
		}
	case 0x0001: // WM_CREATE
		user32.NewProc("SetPropW").Call(hwnd, uintptr(unsafe.Pointer(clickProperty)), 1)
	case 0x0002: // WM_DESTROY
		if card.pointerTest {
			for _, name := range pointerPropertyNames {
				user32.NewProc("RemovePropW").Call(hwnd, uintptr(unsafe.Pointer(name)))
			}
		}
		user32.NewProc("RemovePropW").Call(hwnd, uintptr(unsafe.Pointer(clickProperty)))
		killTimer.Call(hwnd, 1)
		postQuit.Call(0)
		return 0
	case 0x0014: // WM_ERASEBKGND: the backbuffer paints the complete client area.
		return 1
	case 0x0024: // WM_GETMINMAXINFO
		info := (*minMaxInfo)(unsafe.Pointer(lparam))
		dpi, _, _ := getWindowDPI.Call(hwnd)
		minimum := rect{Right: 320, Bottom: 240}
		if !card.fullscreen.active {
			adjustRect.Call(uintptr(unsafe.Pointer(&minimum)), 0x00CF0000, 0, 0, dpi)
		}
		info.MinTrackSize = point{minimum.Right - minimum.Left, minimum.Bottom - minimum.Top}
		return 0
	case 0x0005, 0x0113: // WM_SIZE, WM_TIMER
		if msg == 0x0005 {
			card.layoutFullscreenButton(hwnd)
		}
		invalidateRect.Call(hwnd, 0, 0)
		return 0
	case 0x02E0: // WM_DPICHANGED; suggested rectangle is in desktop pixels.
		if card.fullscreen.changing {
			return 0
		}
		if card.fullscreen.active {
			card.refitFullscreen(hwnd)
			return 0
		}
		r := (*rect)(unsafe.Pointer(lparam))
		ok, _, err := setWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x4|0x10)
		if ok == 0 {
			card.err = fmt.Errorf("apply DPI rectangle: %w", err)
			destroyWindow.Call(hwnd)
		}
		return 0
	case 0x0201: // WM_LBUTTONDOWN; signed coordinates matter on Windows.
		var client rect
		getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		x, y := int(int16(lparam&0xffff)), int(int16((lparam>>16)&0xffff))
		button := buttonRect(int(client.Right), int(client.Bottom))
		if x >= int(button.Left) && x < int(button.Right) && y >= int(button.Top) && y < int(button.Bottom) {
			card.clicks++
			user32.NewProc("SetPropW").Call(hwnd, uintptr(unsafe.Pointer(clickProperty)), uintptr(card.clicks+1))
			invalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case 0x000F: // WM_PAINT
		if err := card.paint(hwnd); err != nil {
			card.err = err
			destroyWindow.Call(hwnd)
		}
		return 0
	}
	result, _, _ := defaultProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return result
}

func (c *cardState) createFonts() error {
	face, _ := syscall.UTF16PtrFromString("Consolas")
	for i, height := range []int32{10, 12, 16, 22} {
		font, _, err := createFont.Call(uintptr(-height), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(face)))
		if font == 0 {
			return fmt.Errorf("create testcard font: %w", err)
		}
		c.fonts[i] = font
	}
	return nil
}

func (c *cardState) ensureBuffer(target uintptr, w, h int) error {
	if c.width == w && c.height == h && c.bitmap != 0 {
		return nil
	}
	if c.dc == 0 {
		dc, _, err := createDC.Call(target)
		if dc == 0 {
			return fmt.Errorf("create paint buffer DC: %w", err)
		}
		c.dc = dc
	}
	bitmap, _, err := createBitmap.Call(target, uintptr(w), uintptr(h))
	if bitmap == 0 {
		return fmt.Errorf("resize paint buffer: %w", err)
	}
	previous, _, err := selectObject.Call(c.dc, bitmap)
	if previous == 0 || previous == ^uintptr(0) {
		deleteObject.Call(bitmap)
		return fmt.Errorf("select paint buffer: %w", err)
	}
	if c.bitmap == 0 {
		c.originalBitmap = previous
	} else {
		deleteObject.Call(c.bitmap)
	}
	c.bitmap, c.width, c.height = bitmap, w, h
	return nil
}

func (c *cardState) close() {
	if c.dc != 0 {
		if c.originalBitmap != 0 {
			selectObject.Call(c.dc, c.originalBitmap)
		}
		if c.bitmap != 0 {
			deleteObject.Call(c.bitmap)
		}
		deleteDC.Call(c.dc)
	}
	for _, font := range c.fonts {
		if font != 0 {
			deleteObject.Call(font)
		}
	}
}

func buttonRect(w, h int) rect {
	return rect{24, int32(max(0, h-76)), int32(min(w-24, 304)), int32(max(0, h-76) + 44)}
}

func (c *cardState) paint(hwnd uintptr) error {
	var ps paintStruct
	dc, _, err := beginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return fmt.Errorf("begin paint: %w", err)
	}
	defer endPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var client rect
	ok, _, err := getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	if ok == 0 {
		return fmt.Errorf("get client bounds: %w", err)
	}
	w, h := int(client.Right), int(client.Bottom)
	if w <= 0 || h <= 0 { // Minimized windows have no framebuffer to paint.
		return nil
	}
	if err := c.ensureBuffer(dc, w, h); err != nil {
		return err
	}
	c.draw(w, h)
	ok, _, err = bitBlt.Call(dc, 0, 0, uintptr(w), uintptr(h), c.dc, 0, 0, 0x00CC0020) // SRCCOPY
	if ok == 0 {
		return fmt.Errorf("present paint buffer: %w", err)
	}
	return nil
}

func (c *cardState) fill(r rect, color uint32) {
	brush, _, _ := getStockObject.Call(18) // DC_BRUSH: cached system object.
	setBrushColor.Call(c.dc, uintptr(color))
	fillRect.Call(c.dc, uintptr(unsafe.Pointer(&r)), brush)
}

func (c *cardState) line(x1, y1, x2, y2 int) {
	moveTo.Call(c.dc, uintptr(x1), uintptr(y1), 0)
	lineTo.Call(c.dc, uintptr(x2), uintptr(y2))
}

func (c *cardState) text(x, y, fontIndex int, color uint32, value string) {
	encoded, _ := syscall.UTF16FromString(value)
	old, _, _ := selectObject.Call(c.dc, c.fonts[fontIndex])
	setTextColor.Call(c.dc, uintptr(color))
	textOut.Call(c.dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)-1))
	selectObject.Call(c.dc, old)
}

func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

// Pointer observations are a developer-only read interface. Every value uses
// a signed-int32 bias plus one, reserving a null property for missing data.
// Sequence is odd during a publication and even after a coherent snapshot.
var pointerPropertyNames = map[string]*uint16{}

type pointerState struct {
	sequence, hover, down, up, clicked, wheel int
	client, cursor, anchor, drag              point
	held                                      bool
	lastDownTarget, lastUpTarget              int
	clicks                                    [5]int
}

func pointerProperty(hwnd uintptr, name string, value int) {
	key := pointerPropertyNames[name]
	if key == nil {
		key, _ = syscall.UTF16PtrFromString("LumaTape.TestCard.Pointer." + name)
		pointerPropertyNames[name] = key
	}
	user32.NewProc("SetPropW").Call(hwnd, uintptr(unsafe.Pointer(key)), uintptr(int64(value)+2147483649))
}

func pointerTargets(w, h int) [5]rect {
	// Targets sit near each edge but inside the CRT mask at supported sizes.
	size := max(36, min(88, min(w, h)/6))
	inset := max(size/2+8, min(w, h)/10)
	centers := [5]point{{int32(inset), int32(inset)}, {int32(w - inset - 1), int32(inset)},
		{int32(inset), int32(h - inset - 1)}, {int32(w - inset - 1), int32(h - inset - 1)}, {int32(w / 2), int32(h / 2)}}
	var targets [5]rect
	for i, p := range centers {
		targets[i] = rect{p.X - int32(size/2), p.Y - int32(size/2), p.X + int32(size/2), p.Y + int32(size/2)}
	}
	return targets
}

func (c *cardState) publishPointer(hwnd uintptr) {
	p := &c.pointer
	p.sequence++
	pointerProperty(hwnd, "Sequence", p.sequence)
	values := map[string]int{"Version": 1, "Hover": p.hover, "Down": p.down, "Up": p.up, "Clicked": p.clicked,
		"DragX": int(p.drag.X), "DragY": int(p.drag.Y), "Wheel": p.wheel,
		"ClientX": int(p.client.X), "ClientY": int(p.client.Y), "CursorX": int(p.cursor.X), "CursorY": int(p.cursor.Y),
		"LastDownTarget": p.lastDownTarget, "LastUpTarget": p.lastUpTarget, "Held": 0, "Captured": 0}
	if p.held {
		values["Held"] = 1
	}
	if owner, _, _ := user32.NewProc("GetCapture").Call(); owner == hwnd {
		values["Captured"] = 1
	}
	var bounds rect
	getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	for i, target := range pointerTargets(int(bounds.Right), int(bounds.Bottom)) {
		values[fmt.Sprintf("Target%dX", i+1)] = int((target.Left + target.Right) / 2)
		values[fmt.Sprintf("Target%dY", i+1)] = int((target.Top + target.Bottom) / 2)
		values[fmt.Sprintf("Clicks%d", i+1)] = p.clicks[i]
	}
	for name, value := range values {
		pointerProperty(hwnd, name, value)
	}
	p.sequence++
	pointerProperty(hwnd, "Sequence", p.sequence)
}

func (c *cardState) pointerMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) bool {
	p := &c.pointer
	switch msg {
	case 0x0001, 0x0005: // WM_CREATE, WM_SIZE
		c.publishPointer(hwnd)
		return false
	case 0x0215: // WM_CAPTURECHANGED; source ownership only, never an input hook.
		if lparam != hwnd {
			p.held = false
		}
		c.publishPointer(hwnd)
		return false
	case 0x02A3: // WM_MOUSELEAVE
		p.hover = 0
		c.publishPointer(hwnd)
		invalidateRect.Call(hwnd, 0, 0)
		return true
	case 0x0200, 0x0201, 0x0202, 0x020A: // MOVE, LEFT DOWN/UP, WHEEL
		p.client = point{int32(int16(lparam & 0xffff)), int32(int16(lparam >> 16))}
		if msg == 0x020A { // Wheel LPARAM is in desktop coordinates.
			user32.NewProc("ScreenToClient").Call(hwnd, uintptr(unsafe.Pointer(&p.client)))
		}
		user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p.cursor)))
		var bounds rect
		getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
		p.hover = 0
		for i, r := range pointerTargets(int(bounds.Right), int(bounds.Bottom)) {
			if p.client.X >= r.Left && p.client.X < r.Right && p.client.Y >= r.Top && p.client.Y < r.Bottom {
				p.hover = i + 1
			}
		}
		switch msg {
		case 0x0200:
			type trackMouseEvent struct {
				Size, Flags uint32
				Window      uintptr
				HoverTime   uint32
			}
			tracking := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: 2, Window: hwnd}
			user32.NewProc("TrackMouseEvent").Call(uintptr(unsafe.Pointer(&tracking)))
			if p.held {
				p.drag = point{p.client.X - p.anchor.X, p.client.Y - p.anchor.Y}
			}
		case 0x0201:
			p.down++
			p.lastDownTarget, p.anchor, p.drag, p.held = p.hover, p.client, point{}, true
			user32.NewProc("SetCapture").Call(hwnd)
		case 0x0202:
			p.up++
			p.lastUpTarget = p.hover
			if p.held {
				p.drag = point{p.client.X - p.anchor.X, p.client.Y - p.anchor.Y}
				if p.hover > 0 && p.hover == p.lastDownTarget && abs32(p.drag.X) <= 3 && abs32(p.drag.Y) <= 3 {
					p.clicks[p.hover-1]++
					p.clicked = p.hover
				}
			}
			p.held = false
			user32.NewProc("ReleaseCapture").Call()
		case 0x020A:
			p.wheel += int(int16(wparam >> 16))
		}
		c.publishPointer(hwnd)
		invalidateRect.Call(hwnd, 0, 0)
		return true
	}
	return false
}

func abs32(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

func (c *cardState) drawPointer(w, h int) {
	c.fill(rect{0, 0, int32(w), int32(h)}, rgb(18, 23, 30))
	setBackgroundMode.Call(c.dc, 1)
	for i, r := range pointerTargets(w, h) {
		color := rgb(42, 74, 89)
		if c.pointer.hover == i+1 {
			color = rgb(92, 139, 67)
		}
		if c.pointer.held && c.pointer.lastDownTarget == i+1 {
			color = rgb(178, 97, 44)
		}
		c.fill(r, color)
		c.text(int(r.Left)+8, int(r.Top)+8, 2, rgb(255, 255, 255), fmt.Sprintf("%d", i+1))
		c.text(int(r.Left)+8, int(r.Top)+28, 1, rgb(230, 241, 240), fmt.Sprintf("n=%d", c.pointer.clicks[i]))
		// The small center cross remains visible under the cursor hotspot.
		cx, cy := (r.Left+r.Right)/2, (r.Top+r.Bottom)/2
		c.fill(rect{cx - 5, cy, cx + 6, cy + 1}, rgb(255, 255, 255))
		c.fill(rect{cx, cy - 5, cx + 1, cy + 6}, rgb(255, 255, 255))
	}
	c.text(max(8, w/2-145), max(110, h/2-94), 2, rgb(234, 241, 246), locale.Text("Проверка курсора", "Native pointer test"))
	c.text(max(8, w/2-145), max(132, h/2-68), 1, rgb(181, 198, 207), fmt.Sprintf("hover=%d down=%d up=%d wheel=%d", c.pointer.hover, c.pointer.down, c.pointer.up, c.pointer.wheel))
	c.text(max(8, w/2-145), min(h-20, h/2+64), 1, rgb(181, 198, 207), fmt.Sprintf("client=%d,%d drag=%d,%d held=%t", c.pointer.client.X, c.pointer.client.Y, c.pointer.drag.X, c.pointer.drag.Y, c.pointer.held))
}

func (c *cardState) draw(w, h int) {
	if c.pointerTest {
		c.drawPointer(w, h)
		return
	}
	c.fill(rect{0, 0, int32(w), int32(h)}, rgb(12, 17, 20))
	if c.colorField && h >= 480 {
		// Static, unfiltered GDI source: midtones make signal noise visible and
		// the vertical boundaries expose chroma delay without moving hit targets.
		field := []uint32{rgb(62, 101, 154), rgb(81, 128, 154), rgb(89, 145, 132), rgb(128, 151, 105), rgb(167, 147, 91), rgb(177, 114, 91), rgb(159, 96, 128), rgb(116, 96, 150)}
		for i, color := range field {
			c.fill(rect{int32(24 + (w-48)*i/len(field)), 180, int32(24 + (w-48)*(i+1)/len(field)), int32(h - 208)}, color)
		}
		for i := 0; i < 16; i++ {
			v := uint32(24 + i*14)
			c.fill(rect{int32(24 + (w-48)*i/16), int32(h - 204), int32(24 + (w-48)*(i+1)/16), int32(h - 182)}, rgb(v, v, v))
		}
	}
	pen, _, _ := getStockObject.Call(19) // DC_PEN, color set without allocation.
	oldPen, _, _ := selectObject.Call(c.dc, pen)
	defer selectObject.Call(c.dc, oldPen)
	setPenColor.Call(c.dc, uintptr(rgb(38, 52, 58)))
	for x := 0; x < w; x += 32 {
		c.line(x, 0, x, h)
	}
	for y := 0; y < h; y += 32 {
		c.line(0, y, w, y)
	}
	setBackgroundMode.Call(c.dc, 1) // TRANSPARENT text background.
	title := locale.Text("LumaTape — тестовая сцена", "LumaTape test card")
	if w < 520 {
		title = "LumaTape"
	}
	c.text(24, 14, 3, rgb(232, 242, 235), title)
	c.text(24, 40, 1, rgb(171, 188, 189), fmt.Sprintf(locale.Text("%d x %d физических пикселей | пропорции %.4f | время %.1fс", "%d x %d physical pixels | aspect %.4f | elapsed %.1fs"), w, h, float64(w)/float64(h), time.Since(c.start).Seconds()))
	colors := []uint32{rgb(235, 235, 235), rgb(235, 235, 0), rgb(0, 235, 235), rgb(0, 235, 0), rgb(235, 0, 235), rgb(235, 0, 0), rgb(0, 0, 235), rgb(0, 0, 0)}
	for i, color := range colors {
		c.fill(rect{int32(24 + (w-48)*i/8), 66, int32(24 + (w-48)*(i+1)/8), 120}, color)
	}
	radius := max(12, min(w, h)/6)
	cx, cy := w/2, h/2
	setPenColor.Call(c.dc, uintptr(rgb(242, 242, 235)))
	nullBrush, _, _ := getStockObject.Call(5)
	oldBrush, _, _ := selectObject.Call(c.dc, nullBrush)
	ellipse.Call(c.dc, uintptr(cx-radius), uintptr(cy-radius), uintptr(cx+radius), uintptr(cy+radius))
	selectObject.Call(c.dc, oldBrush)
	c.line(cx-radius-12, cy, cx+radius+13, cy)
	c.line(cx, cy-radius-12, cx, cy+radius+13)
	c.text(max(24, cx-radius), cy+radius+20, 1, rgb(225, 233, 219), locale.Text("Радиус X/Y одинаков: круг должен оставаться круглым", "Equal X/Y radius: circle must stay round"))
	c.text(24, 136, 0, rgb(245, 245, 245), "10px: 0123456789 The quick brown fox / Il1 O0")
	c.text(24, 150, 1, rgb(245, 245, 245), locale.Text("12px: мелкий текст в движении должен читаться", "12px: Fine text + motion should remain readable"))
	// Exact single-pixel alternating stripes expose moire and resampling.
	for x := max(24, w-184); x < w-24; x++ {
		color := rgb(0, 0, 0)
		if x%2 == 0 {
			color = rgb(255, 255, 255)
		}
		c.fill(rect{int32(x), int32(max(172, h-176)), int32(x + 1), int32(max(172, h-176) + 32)}, color)
	}
	// Position is derived from elapsed monotonic time, never from WM_TIMER count.
	t := time.Since(c.start).Seconds()
	travel := max(1, w-80)
	mx := 24 + int((math.Sin(t*1.2)+1)*0.5*float64(travel))
	my := max(180, h-124)
	c.fill(rect{int32(mx), int32(my), int32(mx + 24), int32(my + 24)}, rgb(255, 151, 57))
	button := buttonRect(w, h)
	c.fill(button, rgb(57, 106, 81))
	c.text(int(button.Left)+12, int(button.Top)+12, 2, rgb(255, 255, 255), fmt.Sprintf(locale.Text("НАЖМИТЕ  |  кликов: %d", "CLICK HERE  |  count: %d"), c.clicks))
	footer := locale.Text("F11 / Alt+Enter: весь экран | Esc: окно | Alt+Tab: смена окна", "F11 / Alt+Enter: fullscreen | Esc: window | Alt+Tab: switch app")
	c.text(24, h-22, 1, rgb(171, 188, 189), footer)
}
