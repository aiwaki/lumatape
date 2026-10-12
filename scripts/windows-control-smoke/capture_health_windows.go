//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/platform/win32"
)

const animationControlName = "LumaTape.TestCard.Animation.v1"

type captureHealthSample struct {
	At         time.Time    `json:"at"`
	Interval   string       `json:"interval"`
	Phase      string       `json:"phase"`
	Visible    bool         `json:"visible"`
	Active     bool         `json:"active"`
	Recovery   string       `json:"recovery"`
	Foreground bool         `json:"foreground"`
	Rates      captureRates `json:"frame_rates"`
}

// Every mutating message rechecks HWND, PID, creation time and the actual image
// path. An opt-in property additionally prevents controlling a normal testcard.
func verifyCaptureFixture(target source, hwnd uintptr, executable string) error {
	if !win32.SameProcess(hwnd, target.PID) {
		return errors.New("capture fixture HWND/PID changed")
	}
	stamp, alive, err := win32.ProcessIdentity(target.PID)
	if err != nil || !alive || target.Created == "" || strconv.FormatUint(stamp, 10) != target.Created {
		return errors.Join(errors.New("capture fixture creation identity changed"), err)
	}
	var class [128]uint16
	n, _, err := user32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	if n == 0 || syscall.UTF16ToString(class[:]) != "LumaTape.Native.TestCard" {
		return errors.Join(errors.New("target is not the native testcard"), err)
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	h, _, err := kernel.NewProc("OpenProcess").Call(0x1000, 0, uintptr(target.PID))
	if h == 0 {
		return fmt.Errorf("open capture fixture: %w", err)
	}
	defer kernel.NewProc("CloseHandle").Call(h)
	var image [32768]uint16
	size := uint32(len(image))
	ok, _, err := kernel.NewProc("QueryFullProcessImageNameW").Call(h, 0, uintptr(unsafe.Pointer(&image[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return fmt.Errorf("read fixture executable: %w", err)
	}
	actual, err := os.Stat(syscall.UTF16ToString(image[:size]))
	if err != nil {
		return err
	}
	expected, err := os.Stat(executable)
	if err != nil || !os.SameFile(actual, expected) {
		return errors.Join(errors.New("capture fixture executable differs"), err)
	}
	property := win32.U16(animationControlName)
	value, _, _ := user32.NewProc("GetPropW").Call(hwnd, uintptr(unsafe.Pointer(property)))
	if value != 1 && value != 2 {
		return errors.New("testcard was not started with --capture-smoke")
	}
	return nil
}

func setFixtureAnimation(target source, hwnd uintptr, executable string, enabled bool) error {
	if err := verifyCaptureFixture(target, hwnd, executable); err != nil {
		return err
	}
	name := win32.U16(animationControlName)
	message, _, err := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(name)))
	if message == 0 {
		return err
	}
	var value, result uintptr
	if enabled {
		value = 1
	}
	ok, _, err := user32.NewProc("SendMessageTimeoutW").Call(hwnd, message, value, 0, 0x2, 1000, uintptr(unsafe.Pointer(&result)))
	if ok == 0 || result != value+1 {
		return errors.Join(errors.New("testcard did not acknowledge animation command"), err)
	}
	observed, _, _ := user32.NewProc("GetPropW").Call(hwnd, uintptr(unsafe.Pointer(name)))
	if observed != result {
		return errors.New("animation receipt differs from actual property")
	}
	return nil
}

func captureSnapshot(p *peer) (snapshot, error) {
	e, err := p.request("snapshot", map[string]any{})
	if err != nil {
		return snapshot{}, err
	}
	return decodeSnapshot(e.Result)
}

func awaitCaptureState(p *peer, match func(snapshot) bool) (snapshot, error) {
	deadline := time.Now().Add(10 * time.Second)
	var s snapshot
	for time.Now().Before(deadline) {
		var err error
		s, err = captureSnapshot(p)
		if err != nil {
			return s, err
		}
		if match(s) {
			return s, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return s, fmt.Errorf("capture state deadline: phase=%s visible=%v active=%v rates=%s", s.Runtime.Phase, s.Runtime.SurfaceVisible, s.Runtime.EffectActive, s.Runtime.FrameRates.State)
}

func runCaptureHealthCase(p *peer, target source, hwnd uintptr, original rect, initial snapshot, executable, dir string) (result error) {
	if err := verifyCaptureFixture(target, hwnd, executable); err != nil {
		return err
	}
	// Restoring animation on early failure touches only this still-owned fixture.
	defer func() {
		if win32.SameProcess(hwnd, target.PID) {
			result = errors.Join(result, setFixtureAnimation(target, hwnd, executable, true))
		}
	}()
	f, err := os.Create(filepath.Join(dir, "capture-health-samples.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	cfg := cloneMap(initial.Config)
	cfg["enabled"], cfg["mode"], cfg["input_mode"] = true, "full", "mouse-exact"
	cfg["target"] = map[string]any{"kind": "window", "window_title": target.Title, "monitor": 0}
	cfg["capture"] = map[string]any{"transfer": "compatibility"}
	cfg["screen"].(map[string]any)["shape"] = "flat"
	cfg["aspect"].(map[string]any)["enabled"] = false
	for _, preset := range initial.Presets {
		if preset.Name == "VHS Tape" {
			cfg["preset"], cfg["effects"] = preset.Name, preset.Effects
		}
	}
	cfg["effects"].(map[string]any)["intensity"] = 1
	cfg["effects"].(map[string]any)["freeze_noise"] = false
	if _, err = p.request("apply", map[string]any{"config": cfg, "source": target, "expected_config": initial.Config, "expected_emergency_sequence": initial.EmergencySequence}); err != nil {
		return err
	}
	if err = verifyCaptureFixture(target, hwnd, executable); err != nil {
		return err
	}
	user32.NewProc("SetForegroundWindow").Call(hwnd)
	if win32.Foreground() != hwnd {
		return errors.New("Windows did not activate the owned testcard; no input was injected")
	}
	active := func(s snapshot) bool {
		return s.Runtime.Phase == "active" && s.Runtime.SurfaceVisible && s.Runtime.EffectActive && s.Runtime.Backend == "full-compatibility" && s.Runtime.FrameRates.State == "current"
	}
	if _, err = awaitCaptureState(p, active); err != nil {
		return err
	}
	intervals := make(map[string]any)
	for _, interval := range []struct {
		name     string
		animate  bool
		duration time.Duration
	}{{"animated", true, 15 * time.Second}, {"static", false, 6 * time.Second}} {
		if err = setFixtureAnimation(target, hwnd, executable, interval.animate); err != nil {
			return err
		}
		var phases []string
		phaseOverflow := false
		p.observe = func(e envelope) {
			if e.Event == "state_changed" {
				var state struct {
					Phase string `json:"phase"`
				}
				if json.Unmarshal(e.Data, &state) == nil {
					if len(phases) < 256 {
						phases = append(phases, state.Phase)
					} else {
						phaseOverflow = true
					}
				}
			}
		}
		defer func() { p.observe = nil }()
		start := time.Now()
		idleSeen, positiveFresh := false, false
		var presentations, fresh []float64
		for time.Since(start) < interval.duration {
			s, err := captureSnapshot(p)
			if err != nil {
				return err
			}
			now := time.Now()
			sample := captureHealthSample{At: now, Interval: interval.name, Phase: s.Runtime.Phase, Visible: s.Runtime.SurfaceVisible, Active: s.Runtime.EffectActive, Recovery: s.Runtime.CaptureRecovery.State, Foreground: win32.Foreground() == hwnd, Rates: s.Runtime.FrameRates}
			if err = encoder.Encode(sample); err != nil {
				return err
			}
			if !active(s) || !sample.Foreground || sample.Recovery != "ready" {
				return fmt.Errorf("%s lost active foreground presentation: %+v", interval.name, sample)
			}
			if err = verifyCaptureRates(sample.Rates, now); err != nil {
				return fmt.Errorf("%s: %w", interval.name, err)
			}
			presentations = append(presentations, *sample.Rates.PresentFPS)
			fresh = append(fresh, *sample.Rates.FreshFrameFPS)
			positiveFresh = positiveFresh || *sample.Rates.FreshFrameFPS > 0
			idleSeen = idleSeen || captureIdleReuseObserved(sample.Rates, now)
			time.Sleep(500 * time.Millisecond)
		}
		p.observe = nil
		if phaseOverflow {
			return errors.New("capture-health state event buffer overflowed")
		}
		for _, phase := range phases {
			if phase != "active" {
				return fmt.Errorf("%s had an intervening nonactive transition: %q", interval.name, phase)
			}
		}
		if interval.animate && !positiveFresh {
			return errors.New("animated interval never produced a measured fresh frame")
		}
		sort.Float64s(presentations)
		sort.Float64s(fresh)
		intervals[interval.name] = map[string]any{"seconds": time.Since(start).Seconds(), "samples": len(fresh), "median_present_fps": presentations[len(presentations)/2], "median_fresh_frame_fps": fresh[len(fresh)/2], "idle_reuse_exercised": idleSeen, "nonactive_transitions": 0}
		emit("capture_health_interval", map[string]any{"interval": interval.name, "measurement": intervals[interval.name]})
	}
	if err = setFixtureAnimation(target, hwnd, executable, true); err != nil {
		return err
	}
	if _, err = p.request("disable", map[string]any{}); err != nil {
		return err
	}
	disabled, err := awaitCaptureState(p, func(s snapshot) bool {
		return !s.Runtime.Enabled && !s.Runtime.SurfaceVisible && !s.Runtime.EffectActive && s.Runtime.FrameRates.PresentFPS == nil
	})
	if err != nil {
		return err
	}
	if _, err = p.request("apply", map[string]any{"config": cfg, "source": target, "expected_config": disabled.Config, "expected_emergency_sequence": disabled.EmergencySequence}); err != nil {
		return err
	}
	if _, err = awaitCaptureState(p, active); err != nil {
		return err
	}
	if actual, err := getRect(hwnd, false); err != nil || actual != original {
		return errors.Join(errors.New("capture smoke changed source window geometry"), err)
	}
	if err = verifyCaptureFixture(target, hwnd, executable); err != nil {
		return err
	}
	ok, _, err := user32.NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0) // WM_CLOSE, only owned fixture.
	if ok == 0 {
		return fmt.Errorf("close owned fixture: %w", err)
	}
	closed, err := awaitCaptureState(p, func(s snapshot) bool {
		return !win32.SameProcess(hwnd, target.PID) && !s.Runtime.SurfaceVisible && !s.Runtime.EffectActive && s.Runtime.Backend == "" && s.Runtime.Phase != "active" && s.Runtime.FrameRates.PresentFPS == nil
	})
	if err != nil {
		return err
	}
	quit, err := p.request("quit", map[string]any{})
	if err != nil {
		return err
	}
	var shutdown struct {
		Clean bool `json:"clean_shutdown"`
	}
	if err = json.Unmarshal(quit.Result, &shutdown); err != nil || !shutdown.Clean {
		return errors.Join(errors.New("engine did not acknowledge clean shutdown"), err)
	}
	if err = p.waitExit(20 * time.Second); err != nil {
		return err
	}
	return saveCapabilityEvidence(dir, "capture-health-summary", map[string]any{"intervals": intervals, "disable_reenable": true, "source_closed_phase": closed.Runtime.Phase, "source_geometry_unchanged": true, "engine_exit": 0, "png_during_measurement": false, "limitations": []string{"Not a game-internal FPS measurement or physical GPU qualification", "Static sources may still produce WGC updates; idle_reuse_exercised can be false", "Snapshots and state events do not constitute a recording of every screen refresh"}})
}
