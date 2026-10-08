//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

type inputReport struct {
	Action        string `json:"action"`
	PID           uint32 `json:"pid"`
	Before, After struct {
		Clicks     *int `json:"clicks"`
		Foreground bool `json:"foreground"`
		Iconic     bool `json:"iconic"`
	}
	Backend string `json:"backend"`
	Error   string `json:"error"`
}

func runInputHelper(helper, target string, pid uint32, action, keys, name, dir string) (inputReport, error) {
	var report inputReport
	output := filepath.Join(dir, name+".json")
	args := []string{"-pid", strconv.FormatUint(uint64(pid), 10), "-exe", target, "-action", action, "-output", output}
	if keys != "" {
		args = append(args, "-keys", keys)
	}
	stderr, err := os.Create(filepath.Join(dir, name+"-stderr.txt"))
	if err != nil {
		return report, err
	}
	stdout, err := os.Create(filepath.Join(dir, name+"-stdout.txt"))
	if err != nil {
		stderr.Close()
		return report, err
	}
	cmd := exec.Command(helper, args...)
	cmd.Dir = filepath.Dir(helper)
	// CREATE_NO_WINDOW prevents a console. HideWindow instead sets the child's
	// first-ShowWindow override to SW_HIDE: source-input's first ShowWindow is
	// on the testcard, so that override would hide the very source being tested.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return report, err
	}
	emit("input_process_started", map[string]any{"pid": cmd.Process.Pid, "target_pid": pid, "action": action, "keys": keys, "report": output})
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		stdout.Close()
		stderr.Close()
		done <- err
	}()
	select {
	case err = <-done:
		if err != nil {
			return report, fmt.Errorf("input helper %s: %w (see %s-stderr.txt)", name, err, name)
		}
	case <-time.After(15 * time.Second):
		// Never kill an input helper while its deferred key/button releases may
		// still be needed. Its own operation timeout is five seconds plus cleanup.
		return report, fmt.Errorf("input helper PID %d did not finish; no force-kill or further input performed", cmd.Process.Pid)
	}
	f, err := os.Open(output)
	if err != nil {
		return report, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(b) > 64<<10 {
		return report, errors.Join(errors.New("input report unavailable or exceeds 64 KiB"), err)
	}
	if err = json.Unmarshal(b, &report); err != nil {
		return report, err
	}
	if report.PID != pid || report.Action != action || report.Error != "" || !report.After.Foreground || report.After.Iconic {
		return report, fmt.Errorf("input report identity/action/foreground failure: %+v", report)
	}
	if action != "foreground" && report.Backend != "makc v0.2.0 / Win32 SendInput / virtual desktop" {
		return report, fmt.Errorf("unexpected input backend %q", report.Backend)
	}
	return report, nil
}

func observation(s snapshot) inputObservation {
	enabled, _ := s.Config["enabled"].(bool)
	return inputObservation{Registered: s.Hotkeys.Registered, ConfigEnabled: enabled, RuntimeEnabled: s.Runtime.Enabled,
		FormatRequested: s.Runtime.FormatRequested, FormatActive: s.Runtime.FormatActive, RecoveryPending: s.Runtime.RecoveryPending,
		Toggle: s.Hotkeys.Toggle, Emergency: s.Hotkeys.Emergency, Sequence: s.EmergencySequence, Backend: s.Runtime.Backend,
		Phase: s.Runtime.Phase, EffectiveIntensity: s.Runtime.EffectiveIntensity}
}

func inputWindowProbe(hwnd uintptr) map[string]any {
	var pid uint32
	thread, _, _ := user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	valid, _, _ := user32.NewProc("IsWindow").Call(hwnd)
	visible, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd)
	iconic, _, _ := user32.NewProc("IsIconic").Call(hwnd)
	return map[string]any{"hwnd": fmt.Sprintf("0x%x", hwnd), "pid": pid, "thread_id": thread,
		"is_window": valid != 0, "visible": visible != 0, "iconic": iconic != 0}
}

func awaitInputState(p *peer, targetHWND uintptr, toggle, emergency, sequence uint64, enabled, formatted bool, name, dir string) (s snapshot, result error) {
	var lastRaw json.RawMessage
	var snapshotAt time.Time
	defer func() {
		if result == nil {
			return
		}
		// Failure evidence only: do not refocus the source or weaken the actual
		// backend assertion. HWND/PID distinguish focus theft from wrong source
		// identity without collecting any unrelated window titles.
		foreground, _, _ := user32.NewProc("GetForegroundWindow").Call()
		diagnostic := map[string]any{"error": result.Error(), "snapshot_observed_at": snapshotAt,
			"windows_observed_at": time.Now(), "last_snapshot_available": len(lastRaw) > 0,
			"expected_target": inputWindowProbe(targetHWND), "foreground": inputWindowProbe(foreground),
			"foreground_matches_target": foreground == targetHWND, "smoke_pid": os.Getpid(), "engine_pid": p.cmd.Process.Pid}
		if len(lastRaw) > 0 {
			path := filepath.Join(dir, name+"-last-snapshot.json")
			diagnostic["last_snapshot_path"] = path
			result = errors.Join(result, os.WriteFile(path, lastRaw, 0600))
		}
		data, err := json.MarshalIndent(diagnostic, "", "  ")
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name+"-failure.json"), data, 0600)
		}
		result = errors.Join(result, err)
		emit(name+"_failure_diagnostics", diagnostic)
	}()
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		r, err := p.request("snapshot", map[string]any{})
		if err != nil {
			return s, err
		}
		lastRaw, snapshotAt = append(lastRaw[:0], r.Result...), time.Now()
		s, err = decodeSnapshot(r.Result)
		if err != nil {
			return s, err
		}
		last = verifyInputObservation(observation(s), toggle, emergency, sequence, enabled, formatted)
		if last == nil {
			if err = os.WriteFile(filepath.Join(dir, name+"-snapshot.json"), r.Result, 0600); err != nil {
				return s, err
			}
			emit(name+"_verified", map[string]any{"runtime": s.Runtime, "hotkeys": s.Hotkeys, "emergency_sequence": s.EmergencySequence})
			return s, nil
		}
		// Receipt count overshoot/another emergency cannot settle back to the
		// expected value; do not issue more input into an ambiguous test state.
		if s.Hotkeys.Toggle > toggle || s.Hotkeys.Emergency > emergency || s.EmergencySequence > sequence {
			return s, last
		}
		time.Sleep(100 * time.Millisecond)
	}
	return s, fmt.Errorf("%s did not settle: %w", name, last)
}

func runInputCase(p *peer, target source, hwnd uintptr, original rect, applied snapshot, helper, targetExe, dir string) error {
	if !applied.Hotkeys.Registered {
		return errors.New("input case hotkeys are not registered")
	}
	baselineToggle, baselineEmergency, sequence := applied.Hotkeys.Toggle, applied.Hotkeys.Emergency, applied.EmergencySequence
	if _, err := runInputHelper(helper, targetExe, target.PID, "foreground", "", "input-foreground", dir); err != nil {
		return err
	}
	if _, err := awaitInputState(p, hwnd, baselineToggle, baselineEmergency, sequence, true, true, "input_full_cpu", dir); err != nil {
		return err
	}
	formattedOuter, err := getRect(hwnd, false)
	if err != nil {
		return err
	}
	formattedClient, err := getRect(hwnd, true)
	if err != nil {
		return err
	}
	if formattedClient.Right <= formattedClient.Left || formattedClient.Bottom <= formattedClient.Top ||
		(formattedClient.Right-formattedClient.Left)*3 != (formattedClient.Bottom-formattedClient.Top)*4 {
		return fmt.Errorf("input source client is not actual 4:3: %+v", formattedClient)
	}
	click, err := runInputHelper(helper, targetExe, target.PID, "click", "", "input-click", dir)
	if err != nil {
		return err
	}
	if click.Before.Clicks == nil || click.After.Clicks == nil || *click.After.Clicks != *click.Before.Clicks+1 {
		return errors.New("source click count did not advance exactly once")
	}
	if _, err = awaitInputState(p, hwnd, baselineToggle, baselineEmergency, sequence, true, true, "input_click", dir); err != nil {
		return err
	}
	emit("input_click_count_verified", map[string]any{"before": *click.Before.Clicks, "after": *click.After.Clicks, "backend": click.Backend})
	for i, enabled := range []bool{false, true} {
		name := []string{"input-toggle-off", "input-toggle-on"}[i]
		if _, err = runInputHelper(helper, targetExe, target.PID, "keys", "ctrl+shift+9", name, dir); err != nil {
			return err
		}
		current, err := awaitInputState(p, hwnd, baselineToggle+uint64(i)+1, baselineEmergency, sequence, enabled, true, name, dir)
		if err != nil {
			return err
		}
		expectedConfig := cloneMap(applied.Config)
		expectedConfig["enabled"] = enabled
		if !equalConfig(current.Config, expectedConfig) {
			return fmt.Errorf("%s changed saved effects, shape or independent format", name)
		}
		outer, err := getRect(hwnd, false)
		if err != nil || outer != formattedOuter {
			return errors.Join(fmt.Errorf("%s changed the formatted outer rectangle: expected=%+v actual=%+v", name, formattedOuter, outer), err)
		}
		client, err := getRect(hwnd, true)
		if err != nil || client != formattedClient {
			return errors.Join(fmt.Errorf("%s changed the formatted client rectangle: expected=%+v actual=%+v", name, formattedClient, client), err)
		}
		emit(name+"_format_and_preferences_preserved", map[string]any{"outer": outer, "client": client, "phase": current.Runtime.Phase, "effective_intensity": current.Runtime.EffectiveIntensity})
	}
	if _, err = runInputHelper(helper, targetExe, target.PID, "keys", "ctrl+shift+0", "input-emergency", dir); err != nil {
		return err
	}
	stopped, err := awaitInputState(p, hwnd, baselineToggle+2, baselineEmergency+1, sequence+1, false, false, "input_emergency", dir)
	if err != nil {
		return err
	}
	if stopped.Config["aspect"].(map[string]any)["enabled"] != false {
		return errors.New("physical emergency left requested format enabled")
	}
	actual, err := getRect(hwnd, false)
	if err != nil || actual != original {
		return errors.Join(fmt.Errorf("physical emergency did not restore exact rectangle: original=%+v actual=%+v", original, actual), err)
	}
	emit("input_emergency_exact_restore_verified", actual)
	stale, err := p.requestRaw("apply", map[string]any{"config": applied.Config, "source": target, "expected_emergency_sequence": sequence})
	if err != nil {
		return err
	}
	staleState, err := decodeSnapshot(stale.Result)
	if err != nil {
		return err
	}
	var failure struct {
		Code string `json:"code"`
	}
	if err = json.Unmarshal(stale.Error, &failure); err != nil {
		return err
	}
	if stale.OK || failure.Code != "stale_emergency_sequence" || staleState.EmergencySequence != sequence+1 || !equalConfig(stopped.Config, staleState.Config) {
		return errors.New("stale apply crossed the physical emergency barrier")
	}
	if actual, err = getRect(hwnd, false); err != nil || actual != original {
		return errors.Join(errors.New("stale apply changed physical emergency's restored rectangle"), err)
	}
	emit("input_stale_apply_rejected", map[string]any{"sequence": staleState.EmergencySequence, "rect": actual})
	quit, err := p.request("quit", map[string]any{})
	if err != nil {
		return err
	}
	var shutdown struct {
		Clean bool `json:"clean_shutdown"`
	}
	if err = json.Unmarshal(quit.Result, &shutdown); err != nil || !shutdown.Clean {
		return errors.Join(errors.New("quit did not acknowledge cleanup"), err)
	}
	if err = p.waitExit(20 * time.Second); err != nil {
		return err
	}
	emit("input_quit_cleanup_ack_and_exit0_verified", true)
	return nil
}
