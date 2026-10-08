//go:build windows

package app

import (
	"errors"
	"fmt"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/capture"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func (a *application) findTarget(next config.Config, selected []win32.Window) (win32.Window, error) {
	if next.Target.Kind != "window" {
		if next.Target.Monitor >= len(a.monitors) {
			return win32.Window{}, fmt.Errorf(locale.Text("выбранный монитор отсутствует", "the selected monitor is unavailable"))
		}
		return win32.Window{}, nil
	}
	if len(selected) > 0 {
		w := selected[0]
		if w.PID == a.controllerPID && a.controllerPID != 0 {
			return win32.Window{}, fmt.Errorf(locale.Text("окно управления LumaTape нельзя выбрать источником", "LumaTape controller cannot be selected as a source"))
		}
		if w.Handle == 0 || !win32.SameProcess(w.Handle, w.PID) {
			return win32.Window{}, fmt.Errorf(locale.Text("выбранное окно закрыто", "the selected window has closed"))
		}
		if !win32.CaptureSourceAllowed(w.Handle) {
			return win32.Window{}, fmt.Errorf(locale.Text("служебное окно Windows или LumaTape нельзя выбрать источником; выберите окно игры", "a Windows or LumaTape helper window cannot be a capture source; select a game window"))
		}
		return w, nil
	}
	if a.cfg.Target == next.Target && win32.SameProcess(a.target.Handle, a.target.PID) && win32.CaptureSourceAllowed(a.target.Handle) {
		return a.target, nil
	}
	var target win32.Window
	for _, w := range a.availableWindows() {
		if w.Title != next.Target.WindowTitle {
			continue
		}
		if target.Handle != 0 {
			return win32.Window{}, fmt.Errorf(locale.Text("заголовок совпал с несколькими окнами; выберите конкретное окно", "the title matches multiple windows; select a specific window"))
		}
		target = w
	}
	if target.Handle == 0 {
		return target, fmt.Errorf(locale.Text("окно источника не найдено; выберите открытое окно", "source window not found; select an open window"))
	}
	return target, nil
}

func (a *application) setCaptureCapability(transfer string, err error) {
	c := CapabilityStatus{State: "available"}
	if err != nil {
		c.State, c.Reason = "failed", err.Error()
		var backend *BackendUnavailableError
		if errors.As(err, &backend) {
			c.Code, c.Reason = backend.Code, backend.Message
			if errors.Is(backend.Cause, capture.ErrGPUInteropUnavailable) {
				c.State = "unavailable"
			}
		}
	}
	if transfer == config.TransferCompatibility {
		a.cpuCapability = c
	} else {
		a.gpuCapability = c
	}
}

func (a *application) probeGPUCapability() {
	if a.library == nil {
		return
	}
	err := capture.ProbeGPUInterop(a.api.GLProc)
	a.gpuCapability = initialGPUCapability(err, a.library.SupportsCompatibility())
	detail := "prerequisites present; live capture not tested"
	if err != nil {
		detail = err.Error()
	}
	a.record("gpu_prerequisites", map[string]any{"capability": a.gpuCapability, "detail": detail})
}

func (a *application) capturePreflight(next config.Config) error {
	if next.Mode == "full" && next.Capture.Transfer == config.TransferGPU && a.gpuCapability.State == "unavailable" {
		return gpuUnavailable(capture.ErrGPUInteropUnavailable, a.library.SupportsCompatibility())
	}
	return nil
}

func (a *application) openCandidate(next config.Config, target win32.Window) (*capture.Capture, error) {
	if err := a.capturePreflight(next); err != nil {
		return nil, err
	}
	if !win32.SameProcess(target.Handle, target.PID) {
		return nil, fmt.Errorf(locale.Text("окно источника закрыто или изменилось; выберите открытое окно", "the source window has closed or changed; select an open window"))
	}
	if a.library == nil {
		return nil, fmt.Errorf(locale.Text("Full недоступен: %v", "Full is unavailable: %v"), a.captureError)
	}
	preference := next.Capture.Transfer
	transfer := preference
	if transfer == config.TransferAuto {
		transfer = config.TransferGPU
	}
	if preference == config.TransferAuto && a.gpuCapability.State == "unavailable" && a.gpuCapability.Code == "gpu_interop_unavailable" {
		transfer = config.TransferCompatibility
	}
	open := func(kind string) (*capture.Capture, error) {
		if kind == config.TransferCompatibility && !a.library.SupportsCompatibility() {
			return nil, fmt.Errorf(locale.Text("Full CPU требует обновлённую capture DLL", "Full CPU requires an updated capture DLL"))
		}
		c := next
		c.Capture.Transfer = kind
		candidate, err := a.library.Open(target.Handle, captureTransfer(c))
		if err != nil && candidate != nil {
			if cleanup := candidate.Close(); cleanup != nil {
				a.failTerminal(cleanup, true)
				err = errors.Join(err, cleanup)
			}
			candidate = nil
		}
		if kind == config.TransferGPU && !a.quit && isMissingGPUInterop(err) {
			a.record("gpu_open_rejected", map[string]any{"detail": err.Error()})
			err = gpuUnavailable(err, a.library.SupportsCompatibility())
		}
		a.setCaptureCapability(kind, err)
		return candidate, err
	}
	candidate, err := open(transfer)
	if !a.quit && shouldFallbackCapture(preference, transfer, err) {
		a.record("capture_auto_fallback", map[string]any{"reason": err.Error(), "from": "gpu", "to": "compatibility"})
		candidate, err = open(config.TransferCompatibility)
	}
	return candidate, err
}

// ApplyDraft is the only interactive preference commit path. Full preflight
// happens before replacing a working backend. A failed request is never
// silently turned into Lightweight; only an Auto preference permits CPU fallback.
func (a *application) ApplyDraft(next config.Config, selected ...win32.Window) error {
	return a.applyPreferences(next, true, true, selected)
}

func (a *application) disableFilter() error {
	// Repeating an explicit off must not retry a suspended capture or replay
	// another setting. An existing unsaved warning remains authoritative.
	if !a.cfg.Enabled {
		return nil
	}
	next := a.cfg
	next.Enabled = false
	return a.ApplyDraft(next)
}

func (a *application) applyPreferences(next config.Config, explicitGeometry, persist bool, selected []win32.Window) (err error) {
	defer func() {
		if err != nil {
			a.lastError = err.Error()
			var applied *AppliedSettingsError
			if errors.As(err, &applied) {
				a.record("save_error", err.Error())
			} else {
				var backend *BackendUnavailableError
				if errors.As(err, &backend) {
					detail := ""
					if backend.Cause != nil {
						detail = backend.Cause.Error()
					}
					a.record("settings_rejected", map[string]any{"code": backend.Code, "message": backend.Message, "detail": detail})
				} else {
					a.record("settings_rejected", err.Error())
				}
			}
		}
	}()
	loaded := next
	if len(selected) > 0 && next.Target.Kind == "window" {
		next.Target.WindowTitle = selected[0].Title
	}
	if !explicitGeometry && next.Aspect.Enabled && next.Aspect.Method != "mask" {
		owned := next.Target == a.cfg.Target && next.Aspect.Method == a.cfg.Aspect.Method && a.cfg.Aspect.Enabled &&
			((next.Aspect.Method == "window" && a.windowRestore != nil) || (next.Aspect.Method == "system" && a.displaySession != nil && !a.displayTerminalSeen))
		if !owned {
			next.Aspect.Enabled = false
			a.issue(locale.Text("Формат 4:3", "4:3 format"), locale.Text("Оконный или системный формат применяется только явным действием в настройках.", "Window or display format changes require an explicit action in settings."))
		}
	}
	plan, err := planTransition(a.cfg, next, explicitGeometry)
	if err != nil {
		return err
	}
	// Load/validate/compile before any OS mutation or capture replacement.
	shader, ownsShader, err := a.prepareLiveShader(next)
	if err != nil {
		return err
	}
	defer func() {
		if ownsShader {
			shader.Close()
		}
	}()
	activatingCapture := (!a.cfg.Enabled && next.Enabled) || (!a.cfg.Aspect.Enabled && next.Aspect.Enabled)
	resumingCapture := next == a.cfg && a.suspended && (next.Enabled || next.Aspect.Enabled)
	if plan.Capture || activatingCapture || resumingCapture || plan.ApplyWindow || plan.ApplySystem {
		// Reject a proven unsupported backend before resolving sources,
		// registering keys, hiding presentation or restoring/changing geometry.
		if err = a.capturePreflight(next); err != nil {
			return err
		}
	}
	if !a.tray.HotkeysRegistered() {
		plan.Hotkeys = true
	}
	// Preferences alone do not select a new source. In particular, a closed
	// game must not be silently replaced by a different same-title window.
	target := a.target
	if needsSourceResolution(plan, len(selected) > 0) {
		target, err = a.findTarget(next, selected)
		if err != nil {
			return err
		}
	}
	if target.Handle != a.target.Handle {
		plan.Source, plan.Capture, plan.RestoreFormat = true, true, true
		if explicitGeometry && next.Aspect.Enabled {
			plan.ApplyWindow, plan.ApplySystem = next.Aspect.Method == "window", next.Aspect.Method == "system"
		}
	}
	var candidate *capture.Capture
	resolved := a.resolvedTransfer
	if next.Mode == "full" && (plan.Capture || (resumingCapture && target.Handle != 0) || (activatingCapture && a.capture == nil)) {
		candidate, err = a.openCandidate(next, target)
		if err != nil {
			return err
		}
	}
	if candidate != nil {
		resolved = transferName(candidate.Transfer())
	}
	defer func() {
		if candidate != nil {
			if cleanup := candidate.Close(); cleanup != nil {
				a.failTerminal(cleanup, true)
				err = errors.Join(err, cleanup)
			}
		}
	}()
	previous, previousTarget := a.cfg, a.target
	previousMonitor := a.monitorDevice
	hadHotkeys := a.tray.HotkeysRegistered()
	if plan.Hotkeys {
		if err = a.registerHotkeys(next); err != nil {
			return err
		}
	}
	committed := false
	defer func() {
		if !committed && plan.Hotkeys {
			if !hadHotkeys {
				a.cfg.Hotkeys = next.Hotkeys // retain the first working emergency binding
				a.unsaved = true
				return
			}
			if rollback := a.registerHotkeys(previous); rollback != nil {
				a.hide()
				failure := fmt.Errorf(locale.Text("восстановление аварийных клавиш: %w", "restore emergency shortcuts: %w"), rollback)
				a.failTerminal(failure, false)
				err = errors.Join(err, failure)
			}
		}
	}()
	a.hide()
	if plan.RestoreFormat {
		if err = a.closeCapture(); err != nil {
			return err
		}
		if err = a.restore(); err != nil {
			return err
		}
	}
	if plan.ApplyWindow || plan.ApplySystem {
		if candidate != nil {
			if err = candidate.Close(); err != nil {
				a.failTerminal(err, true)
				return err
			}
			candidate = nil // resize must start a fresh WGC session
		}
		a.cfg.Target, a.target = next.Target, target
		if next.Target.Kind == "monitor" {
			a.monitorDevice = a.monitors[next.Target.Monitor].Device
		}
		if plan.ApplyWindow {
			err = a.applyWindow43()
		} else {
			err = a.applySystem43()
		}
		if err != nil {
			a.cfg, a.target = previous, previousTarget
			a.monitorDevice = previousMonitor
			a.cfg.Aspect.Enabled = false // the previous format was already restored
			return fmt.Errorf(locale.Text("предыдущий формат снят; новый формат не применён: %w", "the previous format was restored; the new format was not applied: %w"), err)
		}
	}
	if plan.Capture && a.capture != nil {
		if err = a.closeCapture(); err != nil {
			return err
		}
	}
	if shader != a.shaderProgram {
		a.shaderProgram.Close()
		a.shaderProgram = shader
	}
	ownsShader = false
	a.cfg, a.target = next, target
	a.resolvedTransfer = resolved
	a.resetFrameMetrics()
	if next.Target.Kind == "monitor" {
		a.monitorDevice = a.monitors[next.Target.Monitor].Device
	}
	if candidate != nil {
		a.capture, candidate = candidate, nil
	}
	a.suspended = false
	a.lastError = ""
	a.setPhase("ready", locale.Text("Настройки применены; ожидается активное окно источника", "Settings applied; waiting for the source window to become active"))
	committed = true
	if persist {
		if err = config.Save(a.configPath, a.cfg); err != nil {
			a.unsaved = true
			return &AppliedSettingsError{Err: err}
		}
		a.unsaved = false
	} else {
		a.unsaved = next != loaded
	}
	a.record("settings_applied", map[string]any{"mode": next.Mode, "transfer": next.Capture.Transfer, "preset": next.Preset, "format": next.Aspect.Method, "format_enabled": next.Aspect.Enabled})
	return nil
}
