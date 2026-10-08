//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/locale"
)

// Fullscreen only changes this test window. It never changes display modes,
// clips the cursor, installs hooks, or makes the source permanently topmost.
type windowPlacement struct {
	Length, Flags, ShowCommand uint32
	Minimum, Maximum           point
	Normal                     rect
}

type fullscreenState struct {
	active, changing bool
	style            uintptr
	placement        windowPlacement
}

var (
	getWindowStyle = user32.NewProc("GetWindowLongPtrW")
	setWindowStyle = user32.NewProc("SetWindowLongPtrW")
	getPlacement   = user32.NewProc("GetWindowPlacement")
	setPlacement   = user32.NewProc("SetWindowPlacement")
)

func monitorBounds(hwnd uintptr) (rect, error) {
	monitor, _, err := user32.NewProc("MonitorFromWindow").Call(hwnd, 2)
	if monitor == 0 {
		return rect{}, fmt.Errorf("find fullscreen monitor: %w", err)
	}
	info := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, err := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return rect{}, fmt.Errorf("read fullscreen monitor: %w", err)
	}
	return info.Monitor, nil
}

func applyStyle(hwnd, style uintptr) error {
	// Both the saved overlapped style and the popup style are nonzero, so a
	// successful call always returns a nonzero previous value here.
	previous, _, err := setWindowStyle.Call(hwnd, ^uintptr(15), style) // GWL_STYLE -16
	if previous == 0 {
		return fmt.Errorf("set testcard style: %w", err)
	}
	return nil
}

func fitMonitor(hwnd uintptr, r rect) error {
	ok, _, callErr := setWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x4|0x10|0x20)
	if ok == 0 {
		return fmt.Errorf("fit fullscreen monitor: %w", callErr)
	}
	var actual rect
	ok, _, callErr = user32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&actual)))
	if ok == 0 {
		return fmt.Errorf("verify fullscreen bounds: %w", callErr)
	}
	if actual != r {
		return fmt.Errorf("fullscreen bounds rejected: got %+v, wanted %+v", actual, r)
	}
	return nil
}

func (f *fullscreenState) restore(hwnd uintptr) error {
	if err := applyStyle(hwnd, f.style); err != nil {
		return err
	}
	ok, _, err := setPlacement.Call(hwnd, uintptr(unsafe.Pointer(&f.placement)))
	if ok == 0 {
		return fmt.Errorf("restore testcard placement: %w", err)
	}
	ok, _, err = setWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x1|0x2|0x4|0x10|0x20)
	if ok == 0 {
		return fmt.Errorf("restore testcard frame: %w", err)
	}
	return nil
}

func (c *cardState) setFullscreen(hwnd uintptr, enabled bool) error {
	f := &c.fullscreen
	if f.changing || f.active == enabled {
		return nil
	}
	f.changing = true
	defer func() { f.changing = false; c.layoutFullscreenButton(hwnd) }()
	if !enabled {
		f.active = false
		return f.restore(hwnd)
	}
	style, _, err := getWindowStyle.Call(hwnd, ^uintptr(15))
	if style == 0 {
		return fmt.Errorf("save testcard style: %w", err)
	}
	placement := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	ok, _, err := getPlacement.Call(hwnd, uintptr(unsafe.Pointer(&placement)))
	if ok == 0 {
		return fmt.Errorf("save testcard placement: %w", err)
	}
	// Validate the monitor before changing a working window.
	bounds, err := monitorBounds(hwnd)
	if err != nil {
		return err
	}
	f.style, f.placement, f.active = style, placement, true
	showWindow.Call(hwnd, 9) // Clear maximized/minimized state after saving it.
	err = applyStyle(hwnd, (style&^(0x00CF0000|0x01000000|0x20000000))|0x80000000)
	if err == nil {
		err = fitMonitor(hwnd, bounds)
	}
	if err != nil {
		f.active = false
		return errors.Join(err, f.restore(hwnd))
	}
	return nil
}

func (c *cardState) requestFullscreen(hwnd uintptr, enabled bool) {
	if err := c.setFullscreen(hwnd, enabled); err != nil {
		// A test source may fail closed; no external window/display was changed.
		c.err = err
		destroyWindow.Call(hwnd)
	}
}

func (c *cardState) refitFullscreen(hwnd uintptr) {
	c.fullscreen.changing = true
	bounds, err := monitorBounds(hwnd)
	if err == nil {
		err = fitMonitor(hwnd, bounds)
	}
	c.fullscreen.changing = false
	if err != nil {
		c.err = errors.Join(err, c.setFullscreen(hwnd, false))
		destroyWindow.Call(hwnd)
	}
}

func (c *cardState) fullscreenKey(hwnd uintptr, msg message) bool {
	keyDown := msg.Message == 0x0100 || msg.Message == 0x0104
	keyUp := msg.Message == 0x0101 || msg.Message == 0x0105
	if !keyDown && !keyUp {
		return false
	}
	toggle := msg.WParam == 0x7A || (msg.WParam == 0x0D && msg.LParam&(1<<29) != 0)
	exit := msg.WParam == 0x1B && c.fullscreen.active
	if !toggle && !exit {
		return false
	}
	if keyDown && msg.LParam&(1<<30) == 0 { // Ignore held-key auto-repeat.
		c.requestFullscreen(hwnd, !exit && !c.fullscreen.active)
	}
	return true
}

func (c *cardState) createFullscreenButton(hwnd, instance uintptr) error {
	class, _ := syscall.UTF16PtrFromString("BUTTON")
	button, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), 0,
		0x50010000, 0, 0, 0, 0, hwnd, 1001, instance, 0) // CHILD | VISIBLE | TABSTOP
	if button == 0 {
		return fmt.Errorf("create fullscreen button: %w", err)
	}
	c.fullscreenButton = button
	c.layoutFullscreenButton(hwnd)
	return nil
}

func (c *cardState) layoutFullscreenButton(hwnd uintptr) {
	if c.fullscreenButton == 0 {
		return
	}
	var client rect
	getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	dpi, _, _ := getWindowDPI.Call(hwnd)
	width, height := int32(164*dpi/96), int32(30*dpi/96)
	width = min(width, client.Right/2)
	x := client.Right - width - 12
	if c.pointerTest {
		x = (client.Right - width) / 2
	}
	setWindowPos.Call(c.fullscreenButton, 0, uintptr(x), 8, uintptr(width), uintptr(height), 0x4|0x10)
	label := locale.Text("На весь экран · F11", "Fullscreen · F11")
	if c.fullscreen.active {
		label = locale.Text("В окно · Esc", "Windowed · Esc")
	}
	text, _ := syscall.UTF16PtrFromString(label)
	user32.NewProc("SetWindowTextW").Call(c.fullscreenButton, uintptr(unsafe.Pointer(text)))
}
