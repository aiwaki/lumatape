//go:build windows

package pointer

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/geometry"
)

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	kernel32   = syscall.NewLazyDLL("kernel32.dll")
	gdi32      = syscall.NewLazyDLL("gdi32.dll")
	mag        = syscall.NewLazyDLL("Magnification.dll")
	magInit    = mag.NewProc("MagInitialize")
	magShow    = mag.NewProc("MagShowSystemCursor")
	magUninit  = mag.NewProc("MagUninitialize")
	cursorProc = syscall.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case 0x0084:
			return ^uintptr(0) // HTTRANSPARENT
		case 0x0021:
			return 3 // MA_NOACTIVATE
		case 0x0014:
			return 1
		}
		r, _, _ := user32.NewProc("DefWindowProcW").Call(h, uintptr(msg), w, l)
		return r
	})
)

func configureProcess(cmd *exec.Cmd) {
	// CREATE_NO_WINDOW suppresses the console. HideWindow would additionally
	// pass STARTF_USESHOWWINDOW/SW_HIDE to our actual native cursor window.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000 | 0x00000200}
}
func currentIdentity() (identity, error) {
	pid := uint32(os.Getpid())
	h, created, err := openProcess(pid)
	if h != 0 {
		kernel32.NewProc("CloseHandle").Call(h)
	}
	return identity{pid, created}, err
}
func projectionMutex() (uintptr, error) {
	h, _, e := kernel32.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(u16("Local\\LumaTape.PointerProjection"))))
	if h == 0 {
		return 0, fmt.Errorf("cursor ownership mutex: %w", e)
	}
	status, _, _ := kernel32.NewProc("WaitForSingleObject").Call(h, 0)
	if status != 0 && status != 0x80 {
		kernel32.NewProc("CloseHandle").Call(h)
		return 0, errors.New("another cursor worker still owns projection recovery")
	}
	return h, nil
}
func releaseProjectionMutex(h uintptr) {
	if h != 0 {
		kernel32.NewProc("ReleaseMutex").Call(h)
		kernel32.NewProc("CloseHandle").Call(h)
	}
}
func recoverCursor() error {
	// Magnification owns native runtime objects. Keep their initialization and
	// teardown out of GLFW's message queue, on an OS thread discarded at exit.
	// This does not consume or suppress any messages belonging to the engine.
	done := make(chan error, 1)
	go func() { runtime.LockOSThread(); done <- recoverCursorOnThread() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return errors.New("native cursor restoration timed out")
	}
}
func recoverCursorOnThread() error {
	mutex, err := projectionMutex()
	if err != nil {
		return err
	}
	defer releaseProjectionMutex(mutex)
	if err := magInit.Find(); err != nil {
		return err
	}
	ok, _, e := magInit.Call()
	if ok == 0 {
		return fmt.Errorf("cursor recovery initialize: %w", e)
	}
	defer magUninit.Call()
	ok, _, e = magShow.Call(1)
	if ok == 0 {
		return fmt.Errorf("cursor recovery show: %w", e)
	}
	return nil
}
func openProcess(pid uint32) (uintptr, uint64, error) {
	h, _, e := kernel32.NewProc("OpenProcess").Call(0x00100000|0x1000, 0, uintptr(pid))
	if h == 0 {
		return 0, 0, fmt.Errorf("cursor bind process %d: %w", pid, e)
	}
	var created, exit, kernel, user uint64
	ok, _, e := kernel32.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		kernel32.NewProc("CloseHandle").Call(h)
		return 0, 0, fmt.Errorf("cursor process creation: %w", e)
	}
	return h, created, nil
}
func processAlive(h uintptr) bool {
	result, _, _ := kernel32.NewProc("WaitForSingleObject").Call(h, 0)
	return result == 258
}
func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

//go:uintptrescapes
func uc(name string, args ...uintptr) uintptr {
	r, _, _ := user32.NewProc(name).Call(args...)
	return r
}

//go:uintptrescapes
func gc(name string, args ...uintptr) uintptr { r, _, _ := gdi32.NewProc(name).Call(args...); return r }

type point struct{ X, Y int32 }
type rect struct{ L, T, R, B int32 }
type cursorInfo struct {
	Size, Flags uint32
	Handle      uintptr
	Position    point
}
type iconInfo struct {
	Icon        int32
	HotX, HotY  uint32
	Mask, Color uintptr
}
type bitmap struct {
	Type, Width, Height, WidthBytes int32
	Planes, BitsPixel               uint16
	Bits                            uintptr
}
type bitmapHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, ImageSize uint32
	XPels, YPels           int32
	Used, Important        uint32
}
type bitmapInfo struct {
	Header bitmapHeader
	Color  uint32
}
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
type blend struct{ Operation, Flags, Alpha, Format uint8 }
type size struct{ W, H int32 }

type nativeDriver struct {
	cursorVisibility
	ownership                  uintptr
	observation                uint64
	parent                     identity
	parentHandle, sourceHandle uintptr
	source                     identity
	window, instance           uintptr
	initialized                bool
	frame                      Frame
	dc, dib, oldBitmap         uintptr
	pixels                     []byte
	width, height              int
}

func newNativeDriver(parent identity) (cursorDriver, error) {
	d := &nativeDriver{parent: parent}
	if err := d.initialize(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}
func (d *nativeDriver) initialize() error {
	var err error
	d.ownership, err = projectionMutex()
	if err != nil {
		return err
	}
	var created uint64
	d.parentHandle, created, err = openProcess(d.parent.PID)
	if err != nil {
		return err
	}
	if created != d.parent.Created {
		return errors.New("cursor parent identity changed")
	}
	if uc("SetProcessDpiAwarenessContext", ^uintptr(3)) == 0 {
		if uc("AreDpiAwarenessContextsEqual", uc("GetThreadDpiAwarenessContext"), ^uintptr(3)) == 0 {
			return errors.New("cursor projection requires physical-pixel DPI context")
		}
	}
	if err = magInit.Find(); err != nil {
		return err
	}
	ok, _, e := magInit.Call()
	if ok == 0 {
		return fmt.Errorf("cursor magnification runtime: %w", e)
	}
	d.initialized = true
	d.instance, _, _ = kernel32.NewProc("GetModuleHandleW").Call(0)
	cls := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), Proc: cursorProc, Instance: d.instance, ClassName: u16("LumaTape.PointerProjection")}
	if uc("RegisterClassExW", uintptr(unsafe.Pointer(&cls))) == 0 {
		return errors.New("register cursor projection window")
	}
	// The worker is intentionally not foreground and must never steal focus.
	// SetWindowPos promotion of a normal window depends on foreground permission;
	// create this surface in the documented topmost band from the beginning.
	d.window = uc("CreateWindowExW", 0x00080000|0x20|0x08000000|0x80|0x8, uintptr(unsafe.Pointer(cls.ClassName)), uintptr(unsafe.Pointer(u16("LumaTape pointer"))), 0x80000000, 0, 0, 1, 1, 0, 0, d.instance, 0)
	if d.window == 0 {
		return errors.New("create cursor projection window")
	}
	if ex := uc("GetWindowLongPtrW", d.window, ^uintptr(19)); ex&0x080800a8 != 0x080800a8 {
		return fmt.Errorf("cursor creation styles were not applied: exstyle=%#x", ex)
	}
	if uc("SetWindowDisplayAffinity", d.window, 0x11) == 0 {
		return errors.New("exclude projected cursor from capture")
	}
	var affinity uint32
	if uc("GetWindowDisplayAffinity", d.window, uintptr(unsafe.Pointer(&affinity))) == 0 || affinity != 0x11 {
		return errors.New("cursor capture exclusion was not applied")
	}
	return nil
}
func (d *nativeDriver) Alive() bool { return d.parentHandle != 0 && processAlive(d.parentHandle) }
func (d *nativeDriver) Pump() {
	var msg message
	for uc("PeekMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1) != 0 {
		uc("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
		uc("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
	}
}
func windowPID(h uintptr) uint32 {
	var pid uint32
	uc("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&pid)))
	return pid
}
func windowClassName(h uintptr) string {
	var text [128]uint16
	uc("GetClassNameW", h, uintptr(unsafe.Pointer(&text[0])), 128)
	return syscall.UTF16ToString(text[:])
}
func windowTitle(h uintptr) string {
	var text [128]uint16
	uc("GetWindowTextW", h, uintptr(unsafe.Pointer(&text[0])), 128)
	return syscall.UTF16ToString(text[:])
}
func windowClient(h uintptr) (geometry.Rect, bool) {
	var r rect
	var origin point
	if uc("GetClientRect", h, uintptr(unsafe.Pointer(&r))) == 0 || uc("ClientToScreen", h, uintptr(unsafe.Pointer(&origin))) == 0 {
		return geometry.Rect{}, false
	}
	return geometry.Rect{X: int(origin.X), Y: int(origin.Y), W: int(r.R - r.L), H: int(r.B - r.T)}, true
}
func (d *nativeDriver) overlayOwned() bool {
	return d.Alive() && d.frame.OverlayHWND != 0 && uc("IsWindow", d.frame.OverlayHWND) != 0 && windowPID(d.frame.OverlayHWND) == d.parent.PID && windowClassName(d.frame.OverlayHWND) == "GLFW30" && windowTitle(d.frame.OverlayHWND) == "LumaTape overlay"
}
func (d *nativeDriver) Invalidate() error {
	if d.overlayOwned() {
		uc("ShowWindowAsync", d.frame.OverlayHWND, 0)
	}
	return d.Restore()
}
func (d *nativeDriver) bindSource(f Frame) error {
	wanted := identity{f.SourcePID, f.SourceCreated}
	if wanted == d.source && d.sourceHandle != 0 {
		return nil
	}
	if d.sourceHandle != 0 {
		kernel32.NewProc("CloseHandle").Call(d.sourceHandle)
		d.sourceHandle = 0
	}
	h, created, err := openProcess(f.SourcePID)
	if err != nil {
		return err
	}
	if created != f.SourceCreated {
		kernel32.NewProc("CloseHandle").Call(h)
		return errors.New("cursor source process identity changed")
	}
	d.sourceHandle = h
	d.source = wanted
	return nil
}
func (d *nativeDriver) Tick(f Frame) (Status, error) {
	observed := d.beginObservation()
	defer d.endObservation(observed)
	if f.SourceHWND == 0 || f.SourcePID == 0 || f.SourceCreated == 0 || f.OverlayHWND == 0 {
		return Status{}, errors.New("invalid cursor frame identity")
	}
	if f.Mapping.SourceSize.W != f.Mapping.Bounds.W || f.Mapping.SourceSize.H != f.Mapping.Bounds.H {
		return Status{}, errors.New("cursor source framebuffer does not match its physical client")
	}
	if d.frame.SourceHWND != 0 && (d.frame.SourceHWND != f.SourceHWND || d.frame.OverlayHWND != f.OverlayHWND || d.frame.SourcePID != f.SourcePID) {
		if err := d.Invalidate(); err != nil {
			return Status{}, err
		}
	}
	d.frame = f
	if err := d.bindSource(f); err != nil {
		return Status{}, err
	}
	if !d.Alive() || !processAlive(d.sourceHandle) || uc("IsWindow", f.SourceHWND) == 0 || windowPID(f.SourceHWND) != f.SourcePID {
		return Status{}, d.Invalidate()
	}
	client, ok := windowClient(f.SourceHWND)
	overlay, overlayOK := windowClient(f.OverlayHWND)
	foreground := uc("GetAncestor", uc("GetForegroundWindow"), 2)
	if !ok || client != f.Mapping.Bounds || !d.overlayOwned() || !overlayOK || overlay != client || foreground != f.SourceHWND || uc("IsWindowVisible", f.SourceHWND) == 0 || uc("IsIconic", f.SourceHWND) != 0 {
		return Status{}, d.Invalidate()
	}
	if uc("IsWindowVisible", f.OverlayHWND) == 0 {
		// The renderer publishes the newly swapped mapping before showing the
		// surface. Do not queue another asynchronous hide against that Show:
		// an already hidden overlay only needs the ordinary cursor restored.
		return Status{}, d.Restore()
	}
	var ci cursorInfo
	ci.Size = uint32(unsafe.Sizeof(ci))
	if uc("GetCursorInfo", uintptr(unsafe.Pointer(&ci))) == 0 {
		return Status{}, errors.New("read current system cursor")
	}
	if ci.Flags != 1 || ci.Handle == 0 {
		return Status{}, d.Restore()
	}
	p, visible := f.Mapping.SourceToScreen(float64(ci.Position.X)-float64(client.X), float64(ci.Position.Y)-float64(client.Y))
	if !visible {
		// Crop and rounded corners can legitimately hide a source pixel. There
		// is no projected cursor there, but the displayed frame is still valid.
		// Hiding the overlay would race the renderer's next Show and make its
		// black bars flicker while the mouse rests in that clipped region.
		return Status{}, d.Restore()
	}
	if err := d.prepareCursor(ci.Handle, p); err != nil {
		return Status{}, errors.Join(err, d.Restore())
	}
	// Bitmap preparation can take long enough for the pointer to leave the game.
	// Recheck eligibility before suppressing the real cursor or showing a stale
	// projected sprite. A changed position inside the image is handled next tick.
	latest := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
	if uc("GetCursorInfo", uintptr(unsafe.Pointer(&latest))) == 0 {
		return Status{}, errors.Join(errors.New("recheck current system cursor"), d.Restore())
	}
	_, stillInside := f.Mapping.SourceToScreen(float64(latest.Position.X)-float64(client.X), float64(latest.Position.Y)-float64(client.Y))
	if latest.Flags != 1 || latest.Handle == 0 || !stillInside || uc("GetAncestor", uc("GetForegroundWindow"), 2) != f.SourceHWND || uc("IsWindowVisible", f.OverlayHWND) == 0 {
		return Status{}, d.Restore()
	}
	if err := d.cursorVisibility.project(d); err != nil {
		d.publishCursorVisibility(false)
		return Status{}, err
	}
	d.prop("CursorFlags", uintptr(ci.Flags))
	d.prop("CursorHandle", ci.Handle)
	d.prop("RealX", uintptr(int64(ci.Position.X)))
	d.prop("RealY", uintptr(int64(ci.Position.Y)))
	d.prop("ScreenX", uintptr(int64(math.Round(p.X))))
	d.prop("ScreenY", uintptr(int64(math.Round(p.Y))))
	d.prop("Serial", uintptr(f.Serial))
	d.prop("SourceHWND", f.SourceHWND)
	d.prop("SourcePID", uintptr(f.SourcePID))
	d.publishCursorVisibility(true)
	return Status{Active: true, Window: d.window, SourcePID: f.SourcePID}, nil
}
func (d *nativeDriver) beginObservation() bool {
	if d.observation%2 != 0 {
		return false
	}
	d.observation++
	if d.window != 0 {
		d.prop("Observation", uintptr(d.observation))
	}
	return true
}
func (d *nativeDriver) endObservation(owned bool) {
	if owned {
		d.observation++
		if d.window != 0 {
			d.prop("Observation", uintptr(d.observation))
		}
	}
}
func (d *nativeDriver) prop(name string, value uintptr) {
	uc("SetPropW", d.window, uintptr(unsafe.Pointer(u16("LumaTape.Pointer."+name))), value)
}
func (d *nativeDriver) Restore() error {
	observed := d.beginObservation()
	defer d.endObservation(observed)
	err := d.cursorVisibility.restore(d)
	d.publishCursorVisibility(false)
	return err
}
func (d *nativeDriver) publishCursorVisibility(active bool) {
	if d.window == 0 {
		return
	}
	var activeValue, hiddenValue uintptr
	if active {
		activeValue = 1
	}
	if d.hidden {
		hiddenValue = 1
	}
	d.prop("Active", activeValue)
	d.prop("Hidden", hiddenValue)
}
func (d *nativeDriver) setSystemCursorVisible(show bool) error {
	var value uintptr
	if show {
		value = 1
	}
	ok, _, e := magShow.Call(value)
	if ok == 0 {
		return fmt.Errorf("set system cursor visible=%t: %w", show, e)
	}
	return nil
}
func (d *nativeDriver) buffer(w, h int) error {
	if d.width == w && d.height == h && d.dib != 0 {
		return nil
	}
	d.freeBuffer()
	if w < 1 || h < 1 || w > 512 || h > 512 {
		return errors.New("unsupported system cursor dimensions")
	}
	d.dc = gc("CreateCompatibleDC", 0)
	if d.dc == 0 {
		return errors.New("create cursor drawing DC")
	}
	info := bitmapInfo{Header: bitmapHeader{Size: 40, Width: int32(w), Height: -int32(h * 2), Planes: 1, BitCount: 32}}
	var bits unsafe.Pointer
	d.dib = gc("CreateDIBSection", d.dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if d.dib == 0 || bits == nil {
		return errors.New("create cursor pixels")
	}
	d.oldBitmap = gc("SelectObject", d.dc, d.dib)
	if d.oldBitmap == 0 {
		return errors.New("select cursor pixels")
	}
	d.pixels = unsafe.Slice((*byte)(bits), w*h*8)
	d.width = w
	d.height = h
	return nil
}

// prepareCursor updates pixels/position but never shows a hidden projection.
// Native visibility is handed off only after this fallible work has succeeded.
func (d *nativeDriver) prepareCursor(hcursor uintptr, p geometry.PointerPoint) error {
	// Copy the shared cursor while drawing. GetIconInfo gives us owned bitmap
	// copies, all of which are released on every branch.
	copy := uc("CopyIcon", hcursor)
	if copy == 0 {
		return errors.New("copy active system cursor")
	}
	defer uc("DestroyIcon", copy)
	var info iconInfo
	if uc("GetIconInfo", copy, uintptr(unsafe.Pointer(&info))) == 0 {
		return errors.New("read cursor hotspot")
	}
	defer func() {
		if info.Mask != 0 {
			gc("DeleteObject", info.Mask)
		}
		if info.Color != 0 {
			gc("DeleteObject", info.Color)
		}
	}()
	var bm bitmap
	hbitmap := info.Color
	if hbitmap == 0 {
		hbitmap = info.Mask
	}
	if gc("GetObjectW", hbitmap, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))) == 0 {
		return errors.New("read cursor dimensions")
	}
	w, h := int(bm.Width), int(bm.Height)
	if info.Color == 0 {
		h /= 2
	}
	if err := d.buffer(w, h); err != nil {
		return err
	}
	// Drawing against black and white preserves alpha and ordinary monochrome
	// masks without assuming that GDI wrote meaningful alpha into its DIB.
	half := w * h * 4
	for i := 0; i < half; i += 4 {
		d.pixels[i] = 0
		d.pixels[i+1] = 0
		d.pixels[i+2] = 0
		d.pixels[i+3] = 255
		d.pixels[half+i] = 255
		d.pixels[half+i+1] = 255
		d.pixels[half+i+2] = 255
		d.pixels[half+i+3] = 255
	}
	if uc("DrawIconEx", d.dc, 0, 0, copy, uintptr(w), uintptr(h), 0, 0, 3) == 0 || uc("DrawIconEx", d.dc, 0, uintptr(h), copy, uintptr(w), uintptr(h), 0, 0, 3) == 0 {
		return errors.New("draw system cursor")
	}
	gc("GdiFlush")
	if err := composeCursorAlpha(d.pixels[:half], d.pixels[half:]); err != nil {
		return err
	}

	dest := point{int32(math.Round(p.X)) - int32(info.HotX), int32(math.Round(p.Y)) - int32(info.HotY)}
	dimensions := size{int32(w), int32(h)}
	origin := point{}
	blending := blend{Alpha: 255, Format: 1}
	if uc("UpdateLayeredWindow", d.window, 0, uintptr(unsafe.Pointer(&dest)), uintptr(unsafe.Pointer(&dimensions)), d.dc, uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blending)), 2) == 0 {
		return errors.New("present projected system cursor")
	}
	return nil
}

func (d *nativeDriver) setProjectionVisible(show bool) error {
	if !show {
		// A partial show/readback failure may leave a visible HWND without a
		// successful cached state. Always hide our actual window during cleanup.
		if d.window != 0 {
			if uc("IsWindowVisible", d.window) != 0 {
				uc("ShowWindow", d.window, 0)
			}
			if uc("IsWindowVisible", d.window) != 0 {
				return errors.New("projected cursor remained visible after hide")
			}
		}
		return nil
	}
	if d.window == 0 {
		return errors.New("projected cursor window is missing")
	}
	// Reassert order after a renderer show/resize. Only this worker's window
	// is touched synchronously; never wait for the renderer's thread.
	if uc("IsWindowVisible", d.window) == 0 {
		// Explicit native show, separate from positioning, avoids depending on the
		// initial-process ShowWindow policy of a shell/console launch path.
		uc("ShowWindow", d.window, 4) // SW_SHOWNOACTIVATE; WS_EX_NOACTIVATE also applies.
	}
	setOK, _, setError := user32.NewProc("SetWindowPos").Call(d.window, ^uintptr(0), 0, 0, 0, 0, 0x1|0x2|0x10|0x40)
	visible := uc("IsWindowVisible", d.window)
	ex, _, styleError := user32.NewProc("GetWindowLongPtrW").Call(d.window, ^uintptr(19))
	aboveEffect := d.aboveEffect()
	if setOK == 0 || visible == 0 || ex&0x8 == 0 || !aboveEffect {
		return fmt.Errorf("projected cursor show/readback failed: hwnd=%#x set_ok=%d visible=%d exstyle=%#x above_effect=%t set_error=%v style_error=%v", d.window, setOK, visible, ex, aboveEffect, setError, styleError)
	}
	return nil
}

// WS_EX_TOPMOST alone is insufficient: another topmost surface can cover the
// cursor. Walk only native z-order handles, with a hard bound for races/cycles.
func (d *nativeDriver) aboveEffect() bool {
	if d.frame.OverlayHWND == 0 {
		return false
	}
	h := uc("GetWindow", d.window, 2) // GW_HWNDNEXT goes downward in z order.
	for n := 0; h != 0 && n < 4096; n++ {
		if h == d.frame.OverlayHWND {
			return true
		}
		next := uc("GetWindow", h, 2)
		if next == h {
			return false
		}
		h = next
	}
	return false
}

func (d *nativeDriver) freeBuffer() {
	if d.dc != 0 && d.oldBitmap != 0 {
		gc("SelectObject", d.dc, d.oldBitmap)
	}
	if d.dib != 0 {
		gc("DeleteObject", d.dib)
	}
	if d.dc != 0 {
		gc("DeleteDC", d.dc)
	}
	d.dc = 0
	d.dib = 0
	d.oldBitmap = 0
	d.pixels = nil
	d.width = 0
	d.height = 0
}
func (d *nativeDriver) Close() error {
	err := d.Restore()
	d.freeBuffer()
	if d.window != 0 {
		uc("DestroyWindow", d.window)
		d.window = 0
	}
	if d.instance != 0 {
		uc("UnregisterClassW", uintptr(unsafe.Pointer(u16("LumaTape.PointerProjection"))), d.instance)
	}
	if d.initialized {
		magUninit.Call()
		d.initialized = false
	}
	if d.sourceHandle != 0 {
		kernel32.NewProc("CloseHandle").Call(d.sourceHandle)
		d.sourceHandle = 0
	}
	if d.parentHandle != 0 {
		kernel32.NewProc("CloseHandle").Call(d.parentHandle)
		d.parentHandle = 0
	}
	if d.ownership != 0 {
		releaseProjectionMutex(d.ownership)
		d.ownership = 0
	}
	return err
}
