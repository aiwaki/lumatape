//go:build windows

// Developer smoke for the real engine's local protocol. Input is opt-in and
// delegated to a separately built, explicit-testcard-PID helper.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/platform/win32"
)

type envelope struct {
	Version int             `json:"v"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Event   string          `json:"event"`
	Result  json.RawMessage `json:"result"`
	Data    json.RawMessage `json:"data"`
	Error   json.RawMessage `json:"error"`
}
type source struct {
	ID      string `json:"id"`
	HWND    string `json:"hwnd"`
	PID     uint32 `json:"pid"`
	Created string `json:"process_created"`
	Title   string `json:"title"`
}
type snapshot struct {
	EmergencySequence uint64         `json:"emergency_sequence"`
	Config            map[string]any `json:"config"`
	Presets           []struct {
		Name    string         `json:"name"`
		Effects map[string]any `json:"effects"`
	} `json:"presets"`
	Runtime struct {
		RecoveryPending    bool             `json:"recovery_pending"`
		Enabled            bool             `json:"enabled"`
		Backend            string           `json:"backend"`
		Phase              string           `json:"phase"`
		FormatActive       bool             `json:"format_active"`
		FormatRequested    bool             `json:"format_requested"`
		EffectiveIntensity *float64         `json:"effective_intensity"`
		GPU                capabilityStatus `json:"gpu"`
		Compatibility      capabilityStatus `json:"compatibility"`
		ShaderID           string           `json:"shader_id"`
		ShaderName         string           `json:"shader_name"`
	} `json:"runtime"`
	Hotkeys struct {
		Registered bool   `json:"registered"`
		Toggle     uint64 `json:"toggle_received"`
		Emergency  uint64 `json:"emergency_received"`
	} `json:"hotkeys"`
}
type peer struct {
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	frames          chan envelope
	exit            chan error
	seq             int
	finished        bool
	protocol        *os.File
	protocolFailure error
}

var user32 = syscall.NewLazyDLL("user32.dll")

func emit(event string, data any) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"time": time.Now(), "event": event, "data": data})
}
func getRect(hwnd uintptr, client bool) (rect, error) {
	var r rect
	name := "GetWindowRect"
	if client {
		name = "GetClientRect"
	}
	ok, _, err := user32.NewProc(name).Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return r, err
	}
	return r, nil
}
func equalConfig(a, b map[string]any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func decodeSnapshot(raw json.RawMessage) (snapshot, error) {
	var s snapshot
	err := json.Unmarshal(raw, &s)
	if err == nil && s.Config == nil {
		err = errors.New("snapshot omitted config")
	}
	return s, err
}
func cloneMap(m map[string]any) map[string]any {
	data, _ := json.Marshal(m)
	var c map[string]any
	_ = json.Unmarshal(data, &c)
	return c
}

func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		emit("failed", err.Error())
		os.Exit(1)
	}
}
func run() (result error) {
	engine := flag.String("engine", "", "exact engine executable path")
	targetPID := flag.Uint("target-pid", 0, "already-running test card process PID")
	targetTitle := flag.String("target-title", "", "optional exact title within target PID")
	testCase := flag.String("case", "lifecycle", "lifecycle, eof, capabilities (Parallels), shaders, presets (Parallels), or explicit input")
	inputHelper := flag.String("input-helper", "", "input case: exact absolute path to separately built source-input executable")
	targetExe := flag.String("target-exe", "", "input case: exact absolute executable path of the owned test card")
	output := flag.String("output", "", "artifact directory (default private temp directory)")
	gpuUnavailable := flag.Bool("expect-gpu-unavailable", false, "assert explicit GPU rejection preserves CPU configuration, for Parallels")
	shaderFile := flag.String("shader-file", "", "shaders case: exact Amber CRT .lumatape.glsl example path")
	secondShaderFile := flag.String("secondary-shader-file", "", "shaders case: exact Cold Bleed .lumatape.glsl example path")
	startupTimeout := flag.Duration("startup-timeout", 15*time.Second, "bound process creation independently of protocol readiness (1s..60s)")
	flag.Parse()
	if *engine == "" || *targetPID == 0 || uint64(*targetPID) > uint64(^uint32(0)) {
		return errors.New("--engine and --target-pid are required")
	}
	if *testCase != "lifecycle" && *testCase != "eof" && *testCase != "input" && *testCase != "capabilities" && *testCase != "shaders" && *testCase != "presets" {
		return errors.New("--case must be lifecycle, eof, capabilities, shaders, presets or input")
	}
	if (*testCase == "capabilities" || *testCase == "presets") && !*gpuUnavailable {
		return errors.New("capabilities/presets case requires --expect-gpu-unavailable for the explicit unsupported-GPU fixture")
	}
	if *testCase == "shaders" {
		if !*gpuUnavailable {
			return errors.New("shaders case requires --expect-gpu-unavailable for Auto-to-CPU qualification in Parallels")
		}
		for _, name := range []string{*shaderFile, *secondShaderFile} {
			if !filepath.IsAbs(name) {
				return errors.New("shaders case requires absolute --shader-file and --secondary-shader-file paths")
			}
		}
	} else if *shaderFile != "" || *secondShaderFile != "" {
		return errors.New("shader file arguments require --case shaders")
	}
	if *testCase == "input" || *testCase == "presets" || (*testCase == "shaders" && (*inputHelper != "" || *targetExe != "")) {
		for _, path := range []string{*inputHelper, *targetExe} {
			if !filepath.IsAbs(path) {
				return errors.New("input requires absolute --input-helper and --target-exe paths")
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				return fmt.Errorf("input executable %q is unavailable: %w", path, errors.Join(err, errors.New("regular executable file required")))
			}
		}
	} else if *inputHelper != "" || *targetExe != "" {
		return errors.New("--input-helper/--target-exe require explicit --case input, shaders or presets")
	}
	if *startupTimeout < time.Second || *startupTimeout > time.Minute {
		return errors.New("--startup-timeout must be between 1s and 60s")
	}
	enginePath, err := filepath.Abs(*engine)
	if err != nil {
		return err
	}
	if _, err = os.Stat(enginePath); err != nil {
		return err
	}
	if err = win32.InitializeDPI(); err != nil {
		return fmt.Errorf("physical-pixel smoke requires per-monitor DPI v2: %w", err)
	}
	dir := *output
	if dir == "" {
		dir, err = os.MkdirTemp("", "lumatape-control-smoke-")
	} else {
		dir, err = filepath.Abs(dir)
		if err == nil {
			err = os.MkdirAll(dir, 0700)
		}
	}
	if err != nil {
		return err
	}
	emit("artifacts", dir)
	// Never load or modify the user's real profile.
	profile := filepath.Join(dir, "config.json")
	if err = os.WriteFile(profile, []byte(`{"enabled":false}`), 0600); err != nil {
		return err
	}
	assetRoot := ""
	if *testCase == "shaders" {
		assetRoot = filepath.Join(dir, "local-app-data")
		if err = os.MkdirAll(assetRoot, 0700); err != nil {
			return err
		}
	}
	p, err := startPeer(enginePath, profile, dir, assetRoot, *startupTimeout)
	if err != nil {
		return err
	}
	defer func() {
		if !p.finished {
			_ = p.stdin.Close()
			result = errors.Join(result, p.waitExit(20*time.Second))
		}
		_ = p.protocol.Close()
	}()
	if err = p.ready(); err != nil {
		return err
	}
	currentReply, err := p.request("snapshot", map[string]any{})
	if err != nil {
		return err
	}
	initial, err := decodeSnapshot(currentReply.Result)
	if err != nil {
		return err
	}
	sourcesReply, err := p.request("sources", map[string]any{})
	if err != nil {
		return err
	}
	var list struct {
		Windows []source `json:"windows"`
	}
	if err = json.Unmarshal(sourcesReply.Result, &list); err != nil {
		return err
	}
	var chosen *source
	for _, w := range list.Windows {
		if w.PID == uint32(*targetPID) && (*targetTitle == "" || w.Title == *targetTitle) {
			if chosen != nil {
				return errors.New("multiple matching source windows; pass exact --target-title")
			}
			copy := w
			chosen = &copy
		}
	}
	if chosen == nil {
		return errors.New("target was not present in actual sources response")
	}
	h, err := strconv.ParseUint(chosen.HWND, 0, 64)
	if err != nil {
		return err
	}
	hwnd := uintptr(h)
	original, err := getRect(hwnd, false)
	if err != nil {
		return err
	}
	emit("source", map[string]any{"source": chosen, "original_rect": original})
	if *testCase == "presets" {
		return runPresetsCase(p, *chosen, hwnd, original, initial, *inputHelper, *targetExe, dir)
	}
	cfg := cloneMap(initial.Config)
	cfg["enabled"], cfg["mode"], cfg["input_mode"] = true, "full", "mouse-exact"
	cfg["target"] = map[string]any{"kind": "window", "window_title": chosen.Title, "monitor": 0}
	cfg["capture"] = map[string]any{"transfer": "compatibility"}
	if *testCase == "input" {
		cfg["hotkeys"] = map[string]any{"toggle": "Ctrl+Shift+9", "emergency": "Ctrl+Shift+0"}
	}
	cfg["screen"].(map[string]any)["shape"] = "rounded"
	aspect := cfg["aspect"].(map[string]any)
	aspect["enabled"], aspect["method"], aspect["scale"], aspect["source_dar"] = true, "window", "fit", 0
	for _, preset := range initial.Presets {
		if preset.Name == "VHS Tape" {
			cfg["preset"], cfg["effects"] = preset.Name, preset.Effects
		}
	}
	effects := cfg["effects"].(map[string]any)
	effects["intensity"], effects["freeze_noise"] = 1, true
	if *testCase == "capabilities" {
		return runCapabilitiesCase(p, *chosen, hwnd, original, initial, cfg, dir)
	}
	if *testCase == "shaders" {
		return runShadersCase(p, *chosen, hwnd, original, initial, cfg, *shaderFile, *secondShaderFile, *inputHelper, *targetExe, dir)
	}
	appliedReply, err := p.request("apply", map[string]any{"config": cfg, "source": chosen, "expected_emergency_sequence": initial.EmergencySequence})
	if err != nil {
		return err
	}
	applied, err := decodeSnapshot(appliedReply.Result)
	if err != nil {
		return err
	}
	client, err := getRect(hwnd, true)
	if err != nil {
		return err
	}
	if (client.Right-client.Left)*3 != (client.Bottom-client.Top)*4 {
		return fmt.Errorf("actual client is not 4:3: %+v", client)
	}
	if applied.Config["mode"] != "full" || applied.Config["capture"].(map[string]any)["transfer"] != "compatibility" {
		return errors.New("applied backend is not explicit Full CPU")
	}
	emit("full_cpu_window43_applied", map[string]any{"client": client, "config": applied.Config})
	if *testCase == "input" {
		return runInputCase(p, *chosen, hwnd, original, applied, *inputHelper, *targetExe, dir)
	}
	if *testCase == "eof" {
		if err = p.stdin.Close(); err != nil {
			return err
		}
		if err = p.waitExit(20 * time.Second); err != nil {
			return err
		}
		actual, err := getRect(hwnd, false)
		if err != nil {
			return err
		}
		if actual != original {
			return fmt.Errorf("EOF did not restore exact rectangle: original=%+v actual=%+v", original, actual)
		}
		emit("eof_exit0_and_restore_verified", actual)
		return nil
	}
	if *gpuUnavailable {
		gpu := cloneMap(applied.Config)
		gpu["capture"] = map[string]any{"transfer": "gpu"}
		rejected, err := p.requestRaw("apply", map[string]any{"config": gpu, "source": chosen, "expected_emergency_sequence": applied.EmergencySequence})
		if err != nil {
			return err
		}
		if rejected.OK {
			return errors.New("GPU unexpectedly available; this rejection fixture is not applicable")
		}
		after, err := decodeSnapshot(rejected.Result)
		if err != nil {
			return err
		}
		if !equalConfig(applied.Config, after.Config) {
			return errors.New("GPU rejection changed working CPU configuration")
		}
		emit("gpu_rejection_preserved_configuration", json.RawMessage(rejected.Error))
	}
	if err = os.WriteFile(profile, []byte(`{"version":`), 0600); err != nil {
		return err
	}
	rejected, err := p.requestRaw("reload", map[string]any{})
	if err != nil {
		return err
	}
	if rejected.OK {
		return errors.New("malformed configuration was accepted")
	}
	after, err := decodeSnapshot(rejected.Result)
	if err != nil {
		return err
	}
	if !equalConfig(applied.Config, after.Config) {
		return errors.New("invalid reload changed working configuration")
	}
	validJSON, _ := json.MarshalIndent(applied.Config, "", "  ")
	if err = os.WriteFile(profile, validJSON, 0600); err != nil {
		return err
	}
	emit("invalid_reload_preserved_configuration", true)
	var beforePNG []byte
	for i, before := range []bool{true, false} {
		if i > 0 {
			time.Sleep(550 * time.Millisecond)
		}
		preview, err := p.request("preview", map[string]any{"config": applied.Config, "width": 320, "height": 240, "time": 1, "before": before})
		if err != nil {
			return err
		}
		var image struct {
			MIME string `json:"mime"`
			PNG  string `json:"png_base64"`
		}
		if err = json.Unmarshal(preview.Result, &image); err != nil {
			return err
		}
		if image.MIME != "image/png" {
			return errors.New("preview MIME is not PNG")
		}
		data, err := base64.StdEncoding.DecodeString(image.PNG)
		if err != nil {
			return err
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return err
		}
		if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
			return errors.New("preview dimensions differ")
		}
		name := "preview-after.png"
		if before {
			name = "preview-before.png"
			beforePNG = data
		} else if bytes.Equal(beforePNG, data) {
			return errors.New("before/after shader PNGs are identical")
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
		emit("shader_preview_saved", name)
	}
	return stopAndQuit(p, *chosen, hwnd, original, applied)
}

// Shared terminal checks: both lifecycle and capability cases require exact
// restoration, rejection of stale Apply, cleanup ACK and actual exit zero.
func stopAndQuit(p *peer, target source, hwnd uintptr, original rect, applied snapshot) error {
	stopped, err := p.request("emergency", map[string]any{})
	if err != nil {
		return err
	}
	stop, err := decodeSnapshot(stopped.Result)
	if err != nil {
		return err
	}
	if stop.Config["enabled"] != false || stop.Config["aspect"].(map[string]any)["enabled"] != false || stop.Runtime.RecoveryPending {
		return errors.New("emergency did not disable filter/format or left pending recovery")
	}
	actual, err := getRect(hwnd, false)
	if err != nil {
		return err
	}
	if actual != original {
		return fmt.Errorf("emergency did not restore exact rectangle: original=%+v actual=%+v", original, actual)
	}
	emit("emergency_restore_verified", actual)
	// An old draft can arrive after emergency's queue cancellation. It must
	// still be rejected without re-enabling the filter or resizing the source.
	stale, err := p.requestRaw("apply", map[string]any{"config": applied.Config, "source": target, "expected_emergency_sequence": applied.EmergencySequence})
	if err != nil {
		return err
	}
	staleState, err := decodeSnapshot(stale.Result)
	if err != nil {
		return err
	}
	var staleFailure struct {
		Code string `json:"code"`
	}
	if err = json.Unmarshal(stale.Error, &staleFailure); err != nil {
		return err
	}
	if stale.OK || staleFailure.Code != "stale_emergency_sequence" || stop.EmergencySequence <= applied.EmergencySequence || staleState.EmergencySequence != stop.EmergencySequence || !equalConfig(staleState.Config, stop.Config) {
		return errors.New("old apply crossed the emergency barrier")
	}
	if actual, err = getRect(hwnd, false); err != nil || actual != original {
		return errors.Join(errors.New("stale apply changed restored geometry"), err)
	}
	emit("stale_apply_after_emergency_rejected", map[string]any{"emergency_sequence": staleState.EmergencySequence, "rect": actual})
	quit, err := p.request("quit", map[string]any{})
	if err != nil {
		return err
	}
	var shutdown struct {
		Clean bool `json:"clean_shutdown"`
	}
	if err = json.Unmarshal(quit.Result, &shutdown); err != nil {
		return err
	}
	if !shutdown.Clean {
		return errors.New("quit did not acknowledge cleanup")
	}
	if err = p.waitExit(20 * time.Second); err != nil {
		return err
	}
	emit("quit_cleanup_ack_and_exit0_verified", true)
	return nil
}

func startPeer(engine, profile, dir, assetRoot string, startupTimeout time.Duration) (*peer, error) {
	cmd := exec.Command(engine, "--control-stdio", "--headless-settings", "--controller-pid", strconv.Itoa(os.Getpid()), "--config", profile)
	cmd.Dir = filepath.Dir(engine)
	if assetRoot != "" {
		// --config does not control the shader library. Isolate only this child
		// (including logs) so smoke imports never enter the user's real library.
		cmd.Env = isolatedShaderEnvironment(os.Environ(), assetRoot)
	}
	// Match the Tauri host: suppress a console without overriding the engine's
	// first ShowWindow call with STARTF_USESHOWWINDOW/SW_HIDE.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	stderr, err := os.Create(filepath.Join(dir, "engine-stderr.txt"))
	if err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	cmd.Stderr = stderr
	protocol, err := os.Create(filepath.Join(dir, "protocol-summary.jsonl"))
	if err != nil {
		stderr.Close()
		in.Close()
		out.Close()
		return nil, err
	}
	emit("process_start_begin", map[string]any{"engine": engine, "directory": cmd.Dir, "timeout_ms": startupTimeout.Milliseconds(), "creation_flags": "CREATE_NO_WINDOW", "profile": profile})
	startTime := time.Now()
	err = startWithDeadline(cmd.Start, startupTimeout, func() {
		// These are parent-only pipe ends; do not close stderr/child handles
		// while CreateProcess may still be duplicating them.
		_ = in.Close()
		_ = out.Close()
	}, func(lateErr error) {
		if lateErr == nil {
			// A late child sees stdin EOF and performs its own restoration.
			_ = cmd.Wait()
		}
		_ = stderr.Close()
		_ = protocol.Close()
	})
	if errors.Is(err, errStartupTimeout) {
		stackPath := filepath.Join(dir, "process-start-timeout-stacks.txt")
		stacks := make([]byte, 1<<20)
		n := runtime.Stack(stacks, true)
		stackErr := os.WriteFile(stackPath, stacks[:n], 0600)
		emit("process_start_timeout", map[string]any{"elapsed_ms": time.Since(startTime).Milliseconds(), "error": err.Error(), "goroutine_stacks": stackPath, "stack_write_error": stackErr, "stacks_may_be_truncated": n == len(stacks)})
		return nil, err
	}
	if err != nil {
		emit("process_start_failed", map[string]any{"elapsed_ms": time.Since(startTime).Milliseconds(), "error": err.Error()})
		stderr.Close()
		protocol.Close()
		in.Close()
		out.Close()
		return nil, err
	}
	emit("process_started", map[string]any{"pid": cmd.Process.Pid, "elapsed_ms": time.Since(startTime).Milliseconds()})
	p := &peer{cmd: cmd, stdin: in, frames: make(chan envelope, 32), exit: make(chan error, 1), protocol: protocol}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(p.frames)
		protocolError := func(err error) {
			message, _ := json.Marshal(err.Error())
			p.frames <- envelope{Event: "protocol_error", Data: message}
		}
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), (2<<20)+1)
		for scanner.Scan() {
			var e envelope
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				protocolError(err)
				return
			}
			if e.Version != 1 {
				protocolError(errors.New("engine stdout is not protocol v1"))
				return
			}
			p.frames <- e
		}
		if err := scanner.Err(); err != nil {
			protocolError(err)
		}
	}()
	// StdoutPipe must be drained before Wait closes its handles; otherwise a
	// successful final quit acknowledgement can race with process exit.
	go func() { <-readDone; p.exit <- cmd.Wait(); _ = stderr.Close() }()
	return p, nil
}
func (p *peer) ready() error {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-p.frames:
			if !ok {
				return errors.New("engine exited before ready")
			}
			p.note(e)
			if e.Event == "fatal" || e.Event == "protocol_error" {
				return fmt.Errorf("engine startup: %s", e.Data)
			}
			if e.Event == "ready" {
				return nil
			}
		case <-timer.C:
			return errors.New("engine ready timeout")
		}
	}
}
func (p *peer) note(e envelope) {
	_ = json.NewEncoder(p.protocol).Encode(map[string]any{"time": time.Now(), "event": e.Event, "id": e.ID, "ok": e.OK, "error": e.Error})
	if e.Event == "protocol_error" {
		p.protocolFailure = fmt.Errorf("invalid engine protocol: %s", e.Data)
	}
}
func (p *peer) request(kind string, payload any) (envelope, error) {
	e, err := p.requestRaw(kind, payload)
	if err == nil && !e.OK {
		err = fmt.Errorf("%s rejected: %s", kind, e.Error)
	}
	return e, err
}
func (p *peer) requestRaw(kind string, payload any) (envelope, error) {
	p.seq++
	id := strconv.Itoa(p.seq)
	if err := json.NewEncoder(p.stdin).Encode(map[string]any{"v": 1, "id": id, "type": kind, "payload": payload}); err != nil {
		return envelope{}, err
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-p.frames:
			if !ok {
				return envelope{}, fmt.Errorf("engine EOF before %s response", kind)
			}
			p.note(e)
			if e.ID == id {
				return e, nil
			}
			if e.ID != "" {
				return e, fmt.Errorf("unexpected response ID %q, expected %q", e.ID, id)
			}
			if e.Event == "fatal" || e.Event == "protocol_error" {
				return e, fmt.Errorf("engine fatal: %s", e.Data)
			}
		case <-timer.C:
			return envelope{}, fmt.Errorf("%s response timeout", kind)
		}
	}
}
func (p *peer) waitExit(limit time.Duration) error {
	if p.finished {
		return nil
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		select {
		case err := <-p.exit:
			p.finished = true
			return errors.Join(err, p.protocolFailure)
		case e, ok := <-p.frames:
			if ok {
				p.note(e)
			} else {
				p.frames = nil
			}
		case <-timer.C:
			return fmt.Errorf("engine PID %d did not exit cleanly; no force-kill performed", p.cmd.Process.Pid)
		}
	}
}
