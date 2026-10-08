//go:build windows

// Developer-only native pointer qualification. All input is delivered with makc
// SendInput to an exact, identity-bound testcard; observations are read-only.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/aiwaki/makc"
)

var user = syscall.NewLazyDLL("user32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")

//go:uintptrescapes
func call(name string, args ...uintptr) uintptr {
	v, _, _ := user.NewProc(name).Call(args...)
	return v
}

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }
type floatPoint struct{ X, Y float64 }
type cursorInfo struct {
	Size, Flags uint32
	Cursor      uintptr
	Position    point
}
type iconInfo struct {
	Icon               int32
	HotspotX, HotspotY uint32
	Mask, Color        uintptr
}
type guiThreadInfo struct {
	Size, Flags                                        uint32
	Active, Focus, Capture, MenuOwner, MoveSize, Caret uintptr
	CaretRect                                          rect
}
type identity struct {
	PID        uint   `json:"pid"`
	Executable string `json:"executable"`
	Created    uint64 `json:"created_filetime"`
	HWND       string `json:"hwnd"`
	Class      string `json:"class"`
	Title      string `json:"title"`
}
type bound struct {
	identity
	process, hwnd, thread uintptr
}
type sourceState struct {
	At            string         `json:"observed_at"`
	Client        rect           `json:"client_screen"`
	Cursor        point          `json:"get_cursor_pos"`
	CursorFlags   uint32         `json:"get_cursor_info_flags"`
	NativeCapture string         `json:"native_capture_hwnd"`
	Foreground    bool           `json:"foreground"`
	Values        map[string]int `json:"values"`
}
type projectionState struct {
	Visible       bool   `json:"visible"`
	Active        bool   `json:"active"`
	NativeHidden  bool   `json:"native_hidden"`
	Bounds        rect   `json:"window_rect"`
	Hotspot       point  `json:"hotspot"`
	WindowHotspot point  `json:"window_plus_native_hotspot"`
	Real          point  `json:"real_cursor"`
	Serial        int    `json:"serial"`
	Observation   int    `json:"observation"`
	SourceHWND    string `json:"source_hwnd"`
	SourcePID     uint   `json:"source_pid"`
}
type step struct {
	Action         string           `json:"action"`
	Target         int              `json:"target,omitempty"`
	WantSource     point            `json:"want_source"`
	WantProjection floatPoint       `json:"want_projection"`
	Source         sourceState      `json:"source"`
	Projection     *projectionState `json:"projection,omitempty"`
}
type report struct {
	Action  string      `json:"action"`
	Backend string      `json:"backend,omitempty"`
	Source  identity    `json:"source_identity"`
	Worker  *identity   `json:"worker_identity,omitempty"`
	Before  sourceState `json:"before"`
	After   sourceState `json:"after"`
	Steps   []step      `json:"steps"`
	Error   string      `json:"error,omitempty"`
}

func windowText(hwnd uintptr, method string) string {
	var value [256]uint16
	call(method, hwnd, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)))
	return syscall.UTF16ToString(value[:])
}
func creation(process uintptr) (uint64, error) {
	var created, exited, kt, ut syscall.Filetime
	if ok, _, err := kernel.NewProc("GetProcessTimes").Call(process, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kt)), uintptr(unsafe.Pointer(&ut))); ok == 0 {
		return 0, fmt.Errorf("GetProcessTimes: %w", err)
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), nil
}
func bind(pid uint, executable, class, title string) (_ *bound, err error) {
	want, err := filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	handle, _, err := kernel.NewProc("OpenProcess").Call(0x1000|0x100000, 0, uintptr(pid))
	if handle == 0 {
		return nil, fmt.Errorf("OpenProcess %d: %w", pid, err)
	}
	b := &bound{identity: identity{PID: pid, Executable: filepath.Clean(want), Class: class, Title: title}, process: handle}
	defer func() {
		if err != nil {
			b.close()
		}
	}()
	if b.Created, err = creation(handle); err != nil {
		return nil, err
	}
	matches := 0
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		thread := call("GetWindowThreadProcessId", hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == uint32(pid) && windowText(hwnd, "GetClassNameW") == class && windowText(hwnd, "GetWindowTextW") == title {
			b.hwnd, b.thread, matches = hwnd, thread, matches+1
		}
		return 1
	})
	if call("EnumWindows", callback, 0) == 0 || matches != 1 {
		return nil, fmt.Errorf("PID %d: found %d windows for %s/%s, want 1", pid, matches, class, title)
	}
	b.HWND = fmt.Sprintf("0x%x", b.hwnd)
	if err = b.validate(); err != nil {
		return nil, err
	}
	return b, nil
}
func (b *bound) close() { kernel.NewProc("CloseHandle").Call(b.process) }
func (b *bound) validate() error {
	if status, _, _ := kernel.NewProc("WaitForSingleObject").Call(b.process, 0); status != 0x102 {
		return errors.New("bound process exited")
	}
	var name [32768]uint16
	length := uint32(len(name))
	if ok, _, err := kernel.NewProc("QueryFullProcessImageNameW").Call(b.process, 0, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&length))); ok == 0 {
		return fmt.Errorf("process image: %w", err)
	}
	if !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(name[:length])), b.Executable) {
		return errors.New("exact executable identity mismatch")
	}
	created, err := creation(b.process)
	if err != nil {
		return err
	}
	if created != b.Created {
		return errors.New("process creation identity changed")
	}
	var owner uint32
	thread := call("GetWindowThreadProcessId", b.hwnd, uintptr(unsafe.Pointer(&owner)))
	if owner != uint32(b.PID) || thread != b.thread || windowText(b.hwnd, "GetClassNameW") != b.Class || windowText(b.hwnd, "GetWindowTextW") != b.Title {
		return errors.New("exact HWND identity changed")
	}
	return nil
}
func prop(hwnd uintptr, name string) (int, error) {
	wide, _ := syscall.UTF16PtrFromString(name)
	value := call("GetPropW", hwnd, uintptr(unsafe.Pointer(wide)))
	if value == 0 {
		return 0, fmt.Errorf("missing observation %s", name)
	}
	return int(int64(value) - 2147483649), nil
}
func clientRect(hwnd uintptr) (rect, error) {
	var bounds rect
	if call("GetClientRect", hwnd, uintptr(unsafe.Pointer(&bounds))) == 0 {
		return rect{}, errors.New("GetClientRect failed")
	}
	a, b := point{bounds.Left, bounds.Top}, point{bounds.Right, bounds.Bottom}
	if call("ClientToScreen", hwnd, uintptr(unsafe.Pointer(&a))) == 0 || call("ClientToScreen", hwnd, uintptr(unsafe.Pointer(&b))) == 0 {
		return rect{}, errors.New("ClientToScreen failed")
	}
	return rect{a.X, a.Y, b.X, b.Y}, nil
}
func (b *bound) source() (sourceState, error) {
	s := sourceState{At: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := b.validate(); err != nil {
		return s, err
	}
	var err error
	if s.Client, err = clientRect(b.hwnd); err != nil {
		return s, err
	}
	if call("GetCursorPos", uintptr(unsafe.Pointer(&s.Cursor))) == 0 {
		return s, errors.New("GetCursorPos failed")
	}
	ci := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
	if call("GetCursorInfo", uintptr(unsafe.Pointer(&ci))) == 0 {
		return s, errors.New("GetCursorInfo failed")
	}
	s.CursorFlags = ci.Flags
	gui := guiThreadInfo{Size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	if call("GetGUIThreadInfo", b.thread, uintptr(unsafe.Pointer(&gui))) == 0 {
		return s, errors.New("GetGUIThreadInfo failed")
	}
	s.NativeCapture = fmt.Sprintf("0x%x", gui.Capture)
	s.Foreground = call("GetForegroundWindow") == b.hwnd
	keys := []string{"Version", "Hover", "Down", "Up", "Clicked", "DragX", "DragY", "Wheel", "Captured", "Held", "ClientX", "ClientY", "CursorX", "CursorY", "LastDownTarget", "LastUpTarget"}
	for i := 1; i <= 5; i++ {
		keys = append(keys, fmt.Sprintf("Clicks%d", i), fmt.Sprintf("Target%dX", i), fmt.Sprintf("Target%dY", i))
	}
	for attempt := 0; attempt < 20; attempt++ {
		seq, err := prop(b.hwnd, "LumaTape.TestCard.Pointer.Sequence")
		if err != nil {
			return s, err
		}
		if seq%2 != 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		s.Values = make(map[string]int, len(keys)+1)
		for _, key := range keys {
			value, err := prop(b.hwnd, "LumaTape.TestCard.Pointer."+key)
			if err != nil {
				return s, err
			}
			s.Values[key] = value
		}
		end, err := prop(b.hwnd, "LumaTape.TestCard.Pointer.Sequence")
		if err != nil {
			return s, err
		}
		if end == seq {
			s.Values["Sequence"] = seq
			if s.Values["Version"] != 1 {
				return s, fmt.Errorf("unsupported pointer-test observation version %d", s.Values["Version"])
			}
			return s, nil
		}
	}
	return s, errors.New("source observation kept changing")
}

// This independent radial oracle applies only to the explicitly tested no-crop,
// static-curvature scenario. It does not call product mapping code or query a
// product-reported expected position.
func projected(bounds rect, source point, curvature float64) floatPoint {
	w, h := float64(bounds.Right-bounds.Left), float64(bounds.Bottom-bounds.Top)
	x, y := 2*(float64(source.X)+.5)/w-1, 2*(float64(source.Y)+.5)/h-1
	radius := math.Hypot(x, y)
	if radius > 0 {
		lo, hi := 0., radius
		for i := 0; i < 60; i++ {
			r := (lo + hi) / 2
			if r+curvature*.09*r*r*r < radius {
				lo = r
			} else {
				hi = r
			}
		}
		x, y = x*(lo+hi)/(2*radius), y*(lo+hi)/(2*radius)
	}
	return floatPoint{float64(bounds.Left) + (x+1)*w/2 - .5, float64(bounds.Top) + (y+1)*h/2 - .5}
}

func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (err error) {
	pid := flag.Uint("pid", 0, "exact pointer-test source PID")
	exe := flag.String("exe", "", "exact source executable path")
	workerPID := flag.Uint("worker-pid", 0, "exact pointer worker PID")
	workerExe := flag.String("worker-exe", "", "exact pointer worker executable path")
	action := flag.String("action", "inspect", "inspect, move, motion, targets, drag, wheel, held-disable")
	duration := flag.Duration("duration", 60*time.Second, "motion probe duration, 1s..180s")
	target := flag.Int("target", 1, "target ID 1..5 for move/drag/wheel/held-disable")
	curvature := flag.Float64("curvature", .85, "static shader curvature [0,1], no crop or temporal warp")
	projection := flag.String("projection", "visible", "visible, hidden, ignore")
	dx := flag.Int("dx", 80, "source drag delta X")
	dy := flag.Int("dy", 40, "source drag delta Y")
	disableKeys := flag.String("disable-keys", "ctrl+shift+0", "hotkey while left mouse is held")
	output := flag.String("output", "", "JSON output file")
	flag.Parse()
	switch *action {
	case "inspect", "move", "motion", "targets", "drag", "wheel", "held-disable":
	default:
		return fmt.Errorf("unknown action %q", *action)
	}
	if *pid == 0 || *exe == "" || *target < 1 || *target > 5 || *curvature < 0 || *curvature > 1 || math.IsNaN(*curvature) || math.IsInf(*curvature, 0) {
		return errors.New("require exact pid/exe, target 1..5 and finite curvature [0,1]")
	}
	if (*workerPID == 0) != (*workerExe == "") {
		return errors.New("worker-pid and worker-exe are required together")
	}
	if *projection != "visible" && *projection != "hidden" && *projection != "ignore" {
		return errors.New("projection must be visible, hidden or ignore")
	}
	if *action == "motion" && (*duration < time.Second || *duration > 180*time.Second || *projection != "ignore") {
		return errors.New("motion requires duration 1s..180s and projection=ignore; it measures native input, not a cropped projection oracle")
	}
	if *workerPID == 0 && *projection != "ignore" {
		return errors.New("projection verification requires exact worker-pid/exe; use projection=ignore only for baseline")
	}
	if *action == "held-disable" && *workerPID == 0 {
		return errors.New("held-disable requires worker identity to verify projection deactivation")
	}
	if *dx < -500 || *dx > 500 || *dy < -500 || *dy > 500 {
		return errors.New("drag deltas exceed 500 physical pixels")
	}
	if call("SetProcessDpiAwarenessContext", ^uintptr(3)) == 0 {
		return errors.New("cannot establish per-monitor-v2 physical coordinates")
	}
	source, e := bind(*pid, *exe, "LumaTape.Native.TestCard", "LumaTape pointer test")
	if e != nil {
		return e
	}
	defer source.close()
	r := report{Action: *action, Source: source.identity}
	defer func() {
		var e error
		r.After, e = source.source()
		err = errors.Join(err, e)
		if err != nil {
			r.Error = err.Error()
		}
		data, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			err = errors.Join(err, e)
			return
		}
		data = append(data, '\n')
		if *output != "" {
			e = os.WriteFile(*output, data, 0600)
		} else {
			_, e = os.Stdout.Write(data)
		}
		err = errors.Join(err, e)
	}()
	if r.Before, e = source.source(); e != nil {
		return e
	}
	var worker *bound
	if *workerPID != 0 {
		worker, e = bind(*workerPID, *workerExe, "LumaTape.PointerProjection", "LumaTape pointer")
		if e != nil {
			return e
		}
		defer worker.close()
		r.Worker = &worker.identity
	}
	if *action == "inspect" {
		p := point{r.Before.Cursor.X - r.Before.Client.Left, r.Before.Cursor.Y - r.Before.Client.Top}
		s := step{Action: "inspect", WantSource: p, WantProjection: projected(r.Before.Client, p, *curvature), Source: r.Before}
		if worker != nil {
			s.Projection, e = worker.projection()
			if e != nil {
				return e
			}
		}
		r.Steps = append(r.Steps, s)
		return nil
	}
	call("ShowWindow", source.hwnd, 5)
	call("SetForegroundWindow", source.hwnd)
	time.Sleep(100 * time.Millisecond)
	guard := func() error {
		if e := source.validate(); e != nil {
			return e
		}
		if call("GetForegroundWindow") != source.hwnd {
			return errors.New("source lost foreground; input stopped")
		}
		return nil
	}
	if e = guard(); e != nil {
		return e
	}
	client, e := makc.Open(makc.WithMouseSendInput(), makc.WithKeyboardSendInput(), makc.WithMouseMotion(makc.MouseMotionVirtualDesk))
	if e != nil {
		return e
	}
	defer client.Close()
	r.Backend = "makc v0.2.0 / Win32 SendInput / virtual desktop"
	timeout := 20 * time.Second
	if *action == "motion" {
		timeout = *duration + 5*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	held := false
	var heldKeys []makc.Key
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if held {
			err = errors.Join(err, client.Mouse.Release(cleanup, makc.ButtonLeft))
		}
		for i := len(heldKeys) - 1; i >= 0; i-- {
			err = errors.Join(err, client.Keyboard.Release(cleanup, heldKeys[i]))
		}
	}()
	if down, e := client.Mouse.Down(ctx, makc.ButtonLeft); e != nil {
		return e
	} else if down {
		return errors.New("left mouse already held")
	}
	observe := func(label string, id int, want point, mode string) (sourceState, error) {
		var last step
		deadline := time.Now().Add(2 * time.Second)
		for {
			s, e := source.source()
			if e != nil {
				return s, e
			}
			last = step{Action: label, Target: id, WantSource: want, WantProjection: projected(s.Client, want, *curvature), Source: s}
			err := guard()
			if err == nil && (s.Cursor.X != s.Client.Left+want.X || s.Cursor.Y != s.Client.Top+want.Y) {
				err = fmt.Errorf("real cursor moved: got %+v want source %+v", s.Cursor, want)
			}
			if err == nil && (s.Values["ClientX"] != int(want.X) || s.Values["ClientY"] != int(want.Y)) {
				err = fmt.Errorf("native client event mismatch: got %d,%d want %+v", s.Values["ClientX"], s.Values["ClientY"], want)
			}
			if err == nil && (s.Values["CursorX"] != int(s.Cursor.X) || s.Values["CursorY"] != int(s.Cursor.Y)) {
				err = errors.New("source event GetCursorPos differs from physical cursor")
			}
			// A repeated move may already match the previous event's coordinates
			// after WM_MOUSELEAVE cleared Hover. Wait for the source's new move
			// event to publish the target, not just for GetCursorPos to settle.
			if err == nil && label == "hover" && s.Values["Hover"] != id {
				err = fmt.Errorf("native hover=%d want %d", s.Values["Hover"], id)
			}
			if worker != nil {
				last.Projection, e = worker.projection()
				if err == nil {
					err = e
				}
				if err == nil {
					if mode == "visible" && (last.Projection.SourcePID != source.PID || last.Projection.SourceHWND != source.HWND) {
						err = errors.New("worker projection is bound to a different source")
					}
				}
				if err == nil {
					err = checkProjection(last, mode)
				}
			}
			if err == nil {
				r.Steps = append(r.Steps, last)
				return s, nil
			}
			if time.Now().After(deadline) {
				r.Steps = append(r.Steps, last)
				return s, err
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	move := func(id int) (point, sourceState, error) {
		if e := guard(); e != nil {
			return point{}, sourceState{}, e
		}
		s, e := source.source()
		if e != nil {
			return point{}, s, e
		}
		p := point{int32(s.Values[fmt.Sprintf("Target%dX", id)]), int32(s.Values[fmt.Sprintf("Target%dY", id)])}
		if e = client.Mouse.MoveTo(ctx, int(s.Client.Left+p.X), int(s.Client.Top+p.Y)); e != nil {
			return p, s, e
		}
		s, e = observe("hover", id, p, *projection)
		return p, s, e
	}
	if *action == "motion" {
		// Exercise continuous compositor/capture updates with real guest mouse
		// movement. Each step stays in the pinned testcard and aborts on focus,
		// geometry or button-state changes; this probe never presses a button.
		started, observed := time.Now(), time.Time{}
		for time.Since(started) < *duration {
			if e := guard(); e != nil {
				return e
			}
			s, e := source.source()
			if e != nil {
				return e
			}
			if s.Client != r.Before.Client || s.Values["Down"] != r.Before.Values["Down"] || s.Values["Up"] != r.Before.Values["Up"] || s.Values["Held"] != 0 {
				return errors.New("motion probe source geometry or buttons changed")
			}
			t := time.Since(started).Seconds()
			p := point{int32(float64(s.Client.Right-s.Client.Left) * (.5 + .44*math.Sin(t*1.7))), int32(float64(s.Client.Bottom-s.Client.Top) * (.5 + .4*math.Sin(t*2.3)))}
			if e = client.Mouse.MoveTo(ctx, int(s.Client.Left+p.X), int(s.Client.Top+p.Y)); e != nil {
				return e
			}
			if time.Since(observed) >= time.Second {
				if _, e = observe("motion", 0, p, "ignore"); e != nil {
					return e
				}
				observed = time.Now()
			}
			time.Sleep(16 * time.Millisecond)
		}
		return nil
	}
	if *action == "targets" {
		for id := 1; id <= 5; id++ {
			p, before, e := move(id)
			if e != nil {
				return e
			}
			if e = guard(); e != nil {
				return e
			}
			held = true
			if e = client.Mouse.Press(ctx, makc.ButtonLeft); e != nil {
				return e
			}
			time.Sleep(60 * time.Millisecond)
			down, e := observe("down", id, p, *projection)
			if e != nil {
				return e
			}
			if down.Values["Captured"] != 1 || down.Values["Held"] != 1 || down.NativeCapture != source.HWND {
				return errors.New("source did not own native mouse capture while held")
			}
			if e = client.Mouse.Release(ctx, makc.ButtonLeft); e != nil {
				return e
			}
			held = false
			time.Sleep(60 * time.Millisecond)
			after, e := observe("click", id, p, *projection)
			if e != nil {
				return e
			}
			if after.Values["Down"] != before.Values["Down"]+1 || after.Values["Up"] != before.Values["Up"]+1 || after.Values[fmt.Sprintf("Clicks%d", id)] != before.Values[fmt.Sprintf("Clicks%d", id)]+1 || after.Values["Clicked"] != id || after.Values["Captured"] != 0 || after.Values["Held"] != 0 || after.NativeCapture != "0x0" {
				return fmt.Errorf("target %d did not receive exactly one native complete click", id)
			}
		}
		return nil
	}
	p, before, e := move(*target)
	if e != nil {
		return e
	}
	switch *action {
	case "move":
		return nil
	case "wheel":
		if e = guard(); e != nil {
			return e
		}
		if e = client.Mouse.Wheel(ctx, 1); e != nil {
			return e
		}
		time.Sleep(80 * time.Millisecond)
		after, e := observe("wheel", *target, p, *projection)
		if e != nil {
			return e
		}
		if after.Values["Wheel"] != before.Values["Wheel"]+120 {
			return errors.New("native wheel delta did not advance by 120")
		}
		return nil
	case "drag", "held-disable":
		held = true
		if e = client.Mouse.Press(ctx, makc.ButtonLeft); e != nil {
			return e
		}
		time.Sleep(60 * time.Millisecond)
		down, e := observe("down", *target, p, *projection)
		if e != nil {
			return e
		}
		if down.Values["Captured"] != 1 || down.Values["Held"] != 1 || down.NativeCapture != source.HWND {
			return errors.New("source did not own native capture")
		}
		mode := *projection
		if *action == "held-disable" {
			for _, name := range strings.Split(*disableKeys, "+") {
				key, e := makc.ParseKey(strings.TrimSpace(name))
				if e != nil {
					return e
				}
				if down, e := client.Keyboard.Down(ctx, key); e != nil {
					return e
				} else if down {
					return fmt.Errorf("hotkey %s already held", name)
				}
				heldKeys = append(heldKeys, key)
			}
			if len(heldKeys) < 1 || len(heldKeys) > 4 {
				return errors.New("hotkey must contain 1..4 keys")
			}
			if e = guard(); e != nil {
				return e
			}
			if e = client.Keyboard.Combo(ctx, heldKeys...); e != nil {
				return e
			}
			heldKeys = nil
			mode = "hidden"
			time.Sleep(150 * time.Millisecond)
			after, e := observe("disabled-while-held", *target, p, mode)
			if e != nil {
				return e
			}
			if after.Values["Held"] != 1 || after.Values["Captured"] != 1 || after.Values["Up"] != before.Values["Up"] || after.NativeCapture != source.HWND {
				return errors.New("disable changed source capture/button state")
			}
		} else {
			end := point{p.X + int32(*dx), p.Y + int32(*dy)}
			if end.X < 0 || end.Y < 0 || end.X >= before.Client.Right-before.Client.Left || end.Y >= before.Client.Bottom-before.Client.Top {
				return errors.New("drag endpoint outside source client")
			}
			for i := 1; i <= 12; i++ {
				if e = guard(); e != nil {
					return e
				}
				if e = client.Mouse.MoveTo(ctx, int(before.Client.Left+p.X)+*dx*i/12, int(before.Client.Top+p.Y)+*dy*i/12); e != nil {
					return e
				}
				time.Sleep(20 * time.Millisecond)
			}
			p = end
			if _, e = observe("drag-held", *target, p, mode); e != nil {
				return e
			}
		}
		if e = client.Mouse.Release(ctx, makc.ButtonLeft); e != nil {
			return e
		}
		held = false
		time.Sleep(80 * time.Millisecond)
		after, e := observe("released", *target, p, mode)
		if e != nil {
			return e
		}
		if after.Values["Down"] != before.Values["Down"]+1 || after.Values["Up"] != before.Values["Up"]+1 || after.Values["Captured"] != 0 || after.Values["Held"] != 0 || after.NativeCapture != "0x0" {
			return errors.New("source did not receive exactly one down/up and release capture")
		}
		if *action == "drag" && (after.Values["DragX"] != *dx || after.Values["DragY"] != *dy) {
			return errors.New("native source drag delta differs")
		}
		return nil
	default:
		return fmt.Errorf("unknown action %q", *action)
	}
}

func (b *bound) projection() (*projectionState, error) {
	s := &projectionState{}
	if err := b.validate(); err != nil {
		return s, err
	}
	get := func(name string) uintptr {
		wide, _ := syscall.UTF16PtrFromString("LumaTape.Pointer." + name)
		return call("GetPropW", b.hwnd, uintptr(unsafe.Pointer(wide)))
	}
	for attempt := 0; attempt < 20; attempt++ {
		seq := get("Observation")
		if seq == 0 || seq%2 != 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		s.Visible = call("IsWindowVisible", b.hwnd) != 0
		if call("GetWindowRect", b.hwnd, uintptr(unsafe.Pointer(&s.Bounds))) == 0 {
			return s, errors.New("pointer window rectangle unavailable")
		}
		s.Active, s.NativeHidden = get("Active") != 0, get("Hidden") != 0
		s.Real = point{int32(get("RealX")), int32(get("RealY"))}
		s.Hotspot = point{int32(get("ScreenX")), int32(get("ScreenY"))}
		s.Serial = int(get("Serial"))
		s.SourceHWND = fmt.Sprintf("0x%x", get("SourceHWND"))
		s.SourcePID = uint(get("SourcePID"))
		ci := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
		if call("GetCursorInfo", uintptr(unsafe.Pointer(&ci))) == 0 {
			return s, errors.New("GetCursorInfo for native hotspot failed")
		}
		var icon iconInfo
		if ci.Cursor == 0 || call("GetIconInfo", ci.Cursor, uintptr(unsafe.Pointer(&icon))) == 0 {
			return s, errors.New("GetIconInfo for native hotspot failed")
		}
		s.WindowHotspot = point{s.Bounds.Left + int32(icon.HotspotX), s.Bounds.Top + int32(icon.HotspotY)}
		gdi := syscall.NewLazyDLL("gdi32.dll")
		if icon.Mask != 0 {
			gdi.NewProc("DeleteObject").Call(icon.Mask)
		}
		if icon.Color != 0 {
			gdi.NewProc("DeleteObject").Call(icon.Color)
		}
		if end := get("Observation"); end == seq {
			s.Observation = int(seq)
			return s, nil
		}
	}
	return s, errors.New("pointer observation unavailable or kept changing")
}
func checkProjection(s step, mode string) error {
	p := s.Projection
	if mode == "ignore" {
		return nil
	}
	if mode == "hidden" {
		if p.Visible || p.Active || p.NativeHidden {
			return errors.New("projection remained visible/active or native cursor remained hidden after disable")
		}
		return nil
	}
	if !p.Visible || !p.Active || !p.NativeHidden {
		return errors.New("projected pointer is not visible/active with native cursor hidden")
	}
	if p.Serial <= 0 || p.Observation <= 0 {
		return errors.New("projected pointer has no mapping/publication serial")
	}
	if p.Real != s.Source.Cursor {
		return errors.New("worker sampled a different real cursor")
	}
	if math.Abs(float64(p.Hotspot.X)-s.WantProjection.X) > 2 || math.Abs(float64(p.Hotspot.Y)-s.WantProjection.Y) > 2 {
		return fmt.Errorf("projection hotspot %+v does not match independent oracle %+v", p.Hotspot, s.WantProjection)
	}
	if p.WindowHotspot != p.Hotspot {
		return fmt.Errorf("actual cursor window plus native hotspot %+v disagrees with published hotspot %+v", p.WindowHotspot, p.Hotspot)
	}
	if p.Hotspot.X < s.Projection.Bounds.Left || p.Hotspot.X >= s.Projection.Bounds.Right || p.Hotspot.Y < s.Projection.Bounds.Top || p.Hotspot.Y >= s.Projection.Bounds.Bottom {
		return errors.New("projection hotspot is outside the actual cursor window")
	}
	return nil
}
