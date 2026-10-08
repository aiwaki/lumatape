//go:build windows

// Developer-only, explicit-PID input smoke. Never imported by LumaTape.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/aiwaki/makc"
)

var u = syscall.NewLazyDLL("user32.dll")
var k = syscall.NewLazyDLL("kernel32.dll")

//go:uintptrescapes
func call(name string, args ...uintptr) uintptr { v, _, _ := u.NewProc(name).Call(args...); return v }

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }
type state struct {
	Bounds     rect `json:"bounds"`
	Client     rect `json:"client"`
	Clicks     *int `json:"clicks"`
	Foreground bool `json:"foreground"`
	Iconic     bool `json:"iconic"`
}
type output struct {
	Action        string `json:"action"`
	PID           uint   `json:"pid"`
	Before, After state
	Backend       string        `json:"backend"`
	OverlayBefore *overlayState `json:"overlay_before,omitempty"`
	OverlayAfter  *overlayState `json:"overlay_after,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type overlayState struct {
	PID             uint   `json:"pid"`
	Executable      string `json:"executable"`
	CreatedFileTime string `json:"created_filetime"`
	HWND            string `json:"hwnd"`
	ObservedAt      string `json:"observed_at"`
	Visible         bool   `json:"visible"`
	Iconic          bool   `json:"iconic"`
	SourceClient    rect   `json:"source_client"`
	SourceAfter     rect   `json:"source_client_after"`
	Surface         rect   `json:"surface"`
	SourceStable    bool   `json:"source_stable"`
	GeometryMatches bool   `json:"geometry_matches"`
	Error           string `json:"error,omitempty"`
}

type boundOverlay struct {
	process uintptr
	pid     uint
	exe     string
	created uint64
	hwnd    uintptr
	thread  uintptr
}

func windowTitle(h uintptr) string {
	var b [256]uint16
	call("GetWindowTextW", h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b[:])
}

func processCreation(process uintptr) (uint64, error) {
	var created, exited, kernel, user syscall.Filetime
	ok, _, err := k.NewProc("GetProcessTimes").Call(process, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return 0, fmt.Errorf("overlay creation time: %w", err)
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), nil
}

func openOverlay(pid uint, executable string) (_ *boundOverlay, err error) {
	want, err := filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	process, _, err := k.NewProc("OpenProcess").Call(0x1000|0x100000, 0, uintptr(pid))
	if process == 0 {
		return nil, fmt.Errorf("open overlay PID: %w", err)
	}
	bound := &boundOverlay{process: process, pid: pid, exe: filepath.Clean(want)}
	defer func() {
		if err != nil {
			bound.close()
		}
	}()
	if bound.created, err = processCreation(process); err != nil {
		return nil, err
	}
	var matches int
	cb := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
		var owner uint32
		thread := call("GetWindowThreadProcessId", hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == uint32(pid) && class(hwnd) == "GLFW30" && windowTitle(hwnd) == "LumaTape overlay" {
			matches++
			bound.hwnd, bound.thread = hwnd, thread
		}
		return 1
	})
	if call("EnumWindows", cb, 0) == 0 {
		return nil, errors.New("cannot enumerate overlay windows")
	}
	if matches != 1 {
		return nil, fmt.Errorf("exact overlay PID has %d matching GLFW30/LumaTape overlay windows; want 1", matches)
	}
	if err = bound.validate(); err != nil {
		return nil, err
	}
	return bound, nil
}

func (b *boundOverlay) close() {
	k.NewProc("CloseHandle").Call(b.process)
}

func (b *boundOverlay) validate() error {
	var image [32768]uint16
	length := uint32(len(image))
	ok, _, err := k.NewProc("QueryFullProcessImageNameW").Call(b.process, 0, uintptr(unsafe.Pointer(&image[0])), uintptr(unsafe.Pointer(&length)))
	if ok == 0 {
		return fmt.Errorf("overlay process path: %w", err)
	}
	if !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(image[:length])), b.exe) {
		return errors.New("overlay PID path mismatch")
	}
	if status, _, _ := k.NewProc("WaitForSingleObject").Call(b.process, 0); status != 0x102 {
		return errors.New("bound overlay process exited")
	}
	created, err := processCreation(b.process)
	if err != nil {
		return err
	}
	if created != b.created {
		return errors.New("overlay process creation identity changed")
	}
	var owner uint32
	thread := call("GetWindowThreadProcessId", b.hwnd, uintptr(unsafe.Pointer(&owner)))
	if owner != uint32(b.pid) || thread != b.thread || class(b.hwnd) != "GLFW30" || windowTitle(b.hwnd) != "LumaTape overlay" {
		return errors.New("overlay window identity changed")
	}
	return nil
}

func clientOnScreen(h uintptr) (rect, error) {
	var client rect
	if call("GetClientRect", h, uintptr(unsafe.Pointer(&client))) == 0 {
		return rect{}, errors.New("cannot read source client rectangle")
	}
	topLeft, bottomRight := point{client.Left, client.Top}, point{client.Right, client.Bottom}
	if call("ClientToScreen", h, uintptr(unsafe.Pointer(&topLeft))) == 0 || call("ClientToScreen", h, uintptr(unsafe.Pointer(&bottomRight))) == 0 {
		return rect{}, errors.New("cannot map source client to physical screen coordinates")
	}
	return rect{topLeft.X, topLeft.Y, bottomRight.X, bottomRight.Y}, nil
}

func (b *boundOverlay) sample(source uintptr) (_ *overlayState, err error) {
	s := &overlayState{PID: b.pid, Executable: b.exe, CreatedFileTime: fmt.Sprintf("%d", b.created), HWND: fmt.Sprintf("0x%x", b.hwnd), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	defer func() {
		if err != nil {
			s.Error = err.Error()
		}
	}()
	if err = b.validate(); err != nil {
		return s, err
	}
	if s.SourceClient, err = clientOnScreen(source); err != nil {
		return s, err
	}
	if call("GetWindowRect", b.hwnd, uintptr(unsafe.Pointer(&s.Surface))) == 0 {
		return s, errors.New("cannot read overlay surface rectangle")
	}
	s.Visible, s.Iconic = call("IsWindowVisible", b.hwnd) != 0, call("IsIconic", b.hwnd) != 0
	if s.SourceAfter, err = clientOnScreen(source); err != nil {
		return s, err
	}
	s.SourceStable = s.SourceClient == s.SourceAfter
	s.GeometryMatches = s.Surface == s.SourceClient && s.SourceClient.Right > s.SourceClient.Left && s.SourceClient.Bottom > s.SourceClient.Top
	if !s.Visible || s.Iconic {
		return s, errors.New("overlay is hidden or minimized at the click boundary")
	}
	if !s.SourceStable || !s.GeometryMatches {
		return s, errors.New("overlay surface does not match the stable physical source client at the click boundary")
	}
	return s, b.validate()
}

func class(h uintptr) string {
	var b [256]uint16
	call("GetClassNameW", h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b[:])
}
func inspect(h uintptr) state {
	var s state
	call("GetWindowRect", h, uintptr(unsafe.Pointer(&s.Bounds)))
	call("GetClientRect", h, uintptr(unsafe.Pointer(&s.Client)))
	name, _ := syscall.UTF16PtrFromString("LumaTape.TestCard.Clicks")
	v := call("GetPropW", h, uintptr(unsafe.Pointer(name)))
	if v > 0 {
		i := int(v - 1)
		s.Clicks = &i
	}
	s.Foreground = call("GetForegroundWindow") == h
	s.Iconic = call("IsIconic", h) != 0
	return s
}
func main() {
	runtime.LockOSThread()
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() (err error) {
	pid := flag.Uint("pid", 0, "explicit testcard PID (never killed by this tool)")
	exe := flag.String("exe", "", "exact testcard executable path")
	action := flag.String("action", "inspect", "inspect, foreground, minimize, restore, click, drag, resize, keys")
	dx := flag.Int("dx", 60, "drag/resize x delta in physical pixels")
	dy := flag.Int("dy", 40, "drag/resize y delta")
	keys := flag.String("keys", "ctrl+alt+f9", "explicit combo for keys action")
	outPath := flag.String("output", "", "UTF-8 JSON output file")
	overlayPID := flag.Uint("overlay-pid", 0, "optional exact overlay engine PID; click only, requires overlay-exe")
	overlayExe := flag.String("overlay-exe", "", "optional exact overlay engine executable path; requires overlay-pid")
	flag.Parse()
	if (*overlayPID == 0) != (*overlayExe == "") {
		return errors.New("overlay-pid and overlay-exe must be supplied together")
	}
	if *overlayPID != 0 && *action != "click" {
		return errors.New("overlay guard is only available for the own-testcard click action")
	}
	if call("SetProcessDpiAwarenessContext", ^uintptr(3)) == 0 {
		return errors.New("cannot establish per-monitor DPI v2 for physical input coordinates")
	}
	if *pid == 0 || *exe == "" {
		return errors.New("-pid and -exe required")
	}
	if *dx < -500 || *dx > 500 || *dy < -500 || *dy > 500 {
		return errors.New("motion delta must be within 500px")
	}
	want, e := filepath.Abs(*exe)
	if e != nil {
		return e
	}
	p, _, e := k.NewProc("OpenProcess").Call(0x1000|0x100000, 0, uintptr(*pid))
	if p == 0 {
		return fmt.Errorf("open PID: %w", e)
	}
	defer k.NewProc("CloseHandle").Call(p)
	validate := func() error {
		var b [32768]uint16
		n := uint32(len(b))
		ok, _, e := k.NewProc("QueryFullProcessImageNameW").Call(p, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
		if ok == 0 {
			return fmt.Errorf("process path: %w", e)
		}
		if !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(b[:n])), filepath.Clean(want)) {
			return errors.New("PID path mismatch")
		}
		v, _, _ := k.NewProc("WaitForSingleObject").Call(p, 0)
		if v != 0x102 {
			return errors.New("testcard exited")
		}
		return nil
	}
	if e = validate(); e != nil {
		return e
	}
	var h uintptr
	cb := syscall.NewCallback(func(w, l uintptr) uintptr {
		var owner uint32
		call("GetWindowThreadProcessId", w, uintptr(unsafe.Pointer(&owner)))
		if owner == uint32(*pid) && class(w) == "LumaTape.Native.TestCard" {
			h = w
			return 0
		}
		return 1
	})
	call("EnumWindows", cb, 0)
	if h == 0 {
		return errors.New("explicit PID has no LumaTape.Native.TestCard")
	}
	report := output{Action: *action, PID: *pid, Before: inspect(h)}
	defer func() {
		report.After = inspect(h)
		if err != nil {
			report.Error = err.Error()
		}
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			err = errors.Join(err, e)
			return
		}
		b = append(b, '\n')
		if *outPath != "" {
			err = errors.Join(err, os.WriteFile(*outPath, b, 0600))
		} else {
			_, e = os.Stdout.Write(b)
			err = errors.Join(err, e)
		}
	}()
	var overlay *boundOverlay
	if *overlayPID != 0 {
		if overlay, e = openOverlay(*overlayPID, *overlayExe); e != nil {
			return e
		}
		defer overlay.close()
	}
	if *action == "inspect" {
		return nil
	}
	if *action == "minimize" || *action == "restore" {
		if e = validate(); e != nil {
			return e
		}
		var owner uint32
		call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&owner)))
		if owner != uint32(*pid) || class(h) != "LumaTape.Native.TestCard" {
			return errors.New("testcard ownership changed")
		}
		command := uintptr(6)
		wantIconic := true
		if *action == "restore" {
			command = 9
			wantIconic = false
		}
		call("ShowWindow", h, command)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			after := inspect(h)
			if after.Iconic == wantIconic {
				if !wantIconic && (after.Bounds.Right <= after.Bounds.Left || after.Bounds.Bottom <= after.Bounds.Top) {
					return errors.New("restored rectangle invalid")
				}
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return errors.New("actual IsIconic did not reach the requested state")
	}
	// Explicit input never targets an arbitrary foreground window.
	call("ShowWindow", h, 5)
	call("SetForegroundWindow", h)
	time.Sleep(100 * time.Millisecond)
	guard := func() error {
		if e := validate(); e != nil {
			return e
		}
		var owner uint32
		call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&owner)))
		if owner != uint32(*pid) || class(h) != "LumaTape.Native.TestCard" {
			return errors.New("testcard window ownership changed")
		}
		if call("GetForegroundWindow") != h {
			return errors.New("testcard does not have foreground; input aborted")
		}
		return nil
	}
	if e = guard(); e != nil {
		return e
	}
	if *action == "foreground" {
		return nil
	}
	client, e := makc.Open(makc.WithMouseSendInput(), makc.WithKeyboardSendInput(), makc.WithMouseMotion(makc.MouseMotionVirtualDesk))
	if e != nil {
		return e
	}
	defer client.Close()
	report.Backend = "makc v0.2.0 / Win32 SendInput / virtual desktop"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mouseHeld := false
	var heldKeys []makc.Key
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if mouseHeld {
			err = errors.Join(err, client.Mouse.Release(cleanup, makc.ButtonLeft))
		}
		for i := len(heldKeys) - 1; i >= 0; i-- {
			err = errors.Join(err, client.Keyboard.Release(cleanup, heldKeys[i]))
		}
	}()
	switch *action {
	case "click":
		if report.Before.Clicks == nil {
			return errors.New("testcard lacks click property; rebuild current testcard")
		}
		s := inspect(h)
		pt := point{X: 164, Y: s.Client.Bottom - 54}
		if call("ClientToScreen", h, uintptr(unsafe.Pointer(&pt))) == 0 {
			return errors.New("client coordinate conversion failed")
		}
		if e = client.Mouse.MoveTo(ctx, int(pt.X), int(pt.Y)); e != nil {
			return e
		}
		if e = guard(); e != nil {
			return e
		}
		if down, e := client.Mouse.Down(ctx, makc.ButtonLeft); e != nil {
			return e
		} else if down {
			return errors.New("mouse button already held")
		}
		if overlay != nil {
			if report.OverlayBefore, e = overlay.sample(h); e != nil {
				return e
			}
		}
		mouseHeld = true
		if e = client.Mouse.Click(ctx, makc.ButtonLeft); e != nil {
			return e
		}
		mouseHeld = false
		if overlay != nil {
			if report.OverlayAfter, e = overlay.sample(h); e != nil {
				return e
			}
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			s = inspect(h)
			if s.Clicks != nil && *s.Clicks == *report.Before.Clicks+1 {
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return errors.New("actual source click count did not advance exactly once")
	case "drag", "resize":
		s := inspect(h)
		dpi := int32(call("GetDpiForWindow", h))
		if dpi == 0 {
			dpi = 96
		}
		from := point{X: (s.Bounds.Left + s.Bounds.Right) / 2, Y: s.Bounds.Top + 16*dpi/96}
		hit := uintptr(2)
		if *action == "resize" {
			from = point{s.Bounds.Right - 2, s.Bounds.Bottom - 2}
			hit = 17
		}
		packed := uintptr(uint32(uint16(from.X)) | uint32(uint16(from.Y))<<16)
		var ht uintptr
		ok, _, _ := u.NewProc("SendMessageTimeoutW").Call(h, 0x84, 0, packed, 0x22, 1000, uintptr(unsafe.Pointer(&ht)))
		if ok == 0 || ht != hit {
			return fmt.Errorf("start point hit-test got %d want %d", ht, hit)
		}
		if down, e := client.Mouse.Down(ctx, makc.ButtonLeft); e != nil {
			return e
		} else if down {
			return errors.New("mouse button already held")
		}
		if e = client.Mouse.MoveTo(ctx, int(from.X), int(from.Y)); e != nil {
			return e
		}
		mouseHeld = true
		if e = client.Mouse.Press(ctx, makc.ButtonLeft); e != nil {
			return e
		}
		for i := 1; i <= 24; i++ {
			if e = guard(); e != nil {
				return e
			}
			if e = client.Mouse.MoveTo(ctx, int(from.X)+*dx*i/24, int(from.Y)+*dy*i/24); e != nil {
				return e
			}
			time.Sleep(25 * time.Millisecond)
		}
		if e = client.Mouse.Release(ctx, makc.ButtonLeft); e != nil {
			return e
		}
		mouseHeld = false
		time.Sleep(250 * time.Millisecond)
		after := inspect(h)
		if after.Bounds == s.Bounds {
			return errors.New("actual window rectangle unchanged")
		}
		if *action == "drag" && (after.Bounds.Right-after.Bounds.Left != s.Bounds.Right-s.Bounds.Left || after.Bounds.Bottom-after.Bounds.Top != s.Bounds.Bottom-s.Bounds.Top) {
			return errors.New("drag unexpectedly resized source")
		}
		return nil
	case "keys":
		var combo []makc.Key
		for _, name := range strings.Split(*keys, "+") {
			key, e := makc.ParseKey(strings.TrimSpace(name))
			if e != nil {
				return e
			}
			down, e := client.Keyboard.Down(ctx, key)
			if e != nil {
				return e
			}
			if down {
				return fmt.Errorf("key %s already held", name)
			}
			combo = append(combo, key)
		}
		if len(combo) < 1 || len(combo) > 4 {
			return errors.New("combo must contain 1..4 keys")
		}
		heldKeys = combo
		if e = guard(); e != nil {
			return e
		}
		if e = client.Keyboard.Combo(ctx, combo...); e != nil {
			return e
		}
		heldKeys = nil
		time.Sleep(150 * time.Millisecond)
		return nil
	default:
		return fmt.Errorf("unknown action %q", *action)
	}
}
