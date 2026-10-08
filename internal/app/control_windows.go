//go:build windows

package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/control"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

type controlSource = control.WindowSource
type controlBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func ipcBounds(r geometry.Rect) controlBounds { return controlBounds{r.X, r.Y, r.W, r.H} }

func (a *application) availableWindows() []win32.Window {
	all := win32.Windows()
	filtered := all[:0]
	for _, w := range all {
		if a.controllerPID == 0 || w.PID != a.controllerPID {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

func (a *application) controlSnapshot() any {
	type preset struct {
		Name    string         `json:"name"`
		Effects config.Effects `json:"effects"`
	}
	presets := make([]preset, 0, len(config.PresetNames)+1)
	for _, name := range config.PresetNames {
		effects, _ := config.Preset(name)
		presets = append(presets, preset{name, effects})
	}
	presets = append(presets, preset{"Custom", a.cfg.Effects})
	var source *controlSource
	if a.cfg.Target.Kind == "window" && win32.SameProcess(a.target.Handle, a.target.PID) {
		descriptor := a.sourceDescriptor(a.target)
		source = &descriptor
	}
	var deadline *time.Time
	if !a.confirmationDeadline.IsZero() && !a.displayTerminalSeen {
		value := a.confirmationDeadline
		deadline = &value
	}
	return map[string]any{"config": a.cfg, "runtime": a.RuntimeStatus(), "presets": presets, "screen_shapes": config.ScreenShapes,
		"emergency_sequence": a.emergencyBarrier.sequence,
		"source":             source, "confirmation_deadline": deadline,
		"hotkeys": map[string]any{"registered": a.tray.HotkeysRegistered(), "toggle_received": a.tray.LastToggle, "emergency_received": a.tray.LastEmergency}}
}

func (a *application) sourceDescriptor(w win32.Window) controlSource {
	created := ""
	if stamp, alive, err := win32.ProcessIdentity(w.PID); err == nil && alive {
		created = strconv.FormatUint(stamp, 10)
	}
	hwnd := fmt.Sprintf("0x%x", w.Handle)
	return controlSource{ID: fmt.Sprintf("window:%s:%d:%s", hwnd, w.PID, created), HWND: hwnd, PID: w.PID, ProcessCreated: created, Title: w.Title}
}

func (a *application) controlSources() any {
	windows := make([]controlSource, 0)
	for _, w := range a.availableWindows() {
		windows = append(windows, a.sourceDescriptor(w))
	}
	monitors := make([]any, 0, len(a.monitors))
	for i, m := range a.monitors {
		monitors = append(monitors, map[string]any{"id": "monitor:" + m.Device, "index": i, "device": m.Device, "bounds": ipcBounds(m.Bounds), "work_area": ipcBounds(m.Work), "primary": m.Primary})
	}
	var suggested *controlSource
	for _, w := range windows {
		if a.suggestedSource != nil && w.ID == a.suggestedSource.ID {
			v := w
			suggested = &v
			break
		}
	}
	if suggested == nil && len(windows) == 1 {
		v := windows[0]
		suggested = &v
	}
	return map[string]any{"windows": windows, "monitors": monitors, "suggested": suggested}
}

func (a *application) controlSelected(source controlSource) (win32.Window, error) {
	hwnd, err := strconv.ParseUint(source.HWND, 0, 64)
	if err != nil || hwnd == 0 || source.PID == 0 {
		return win32.Window{}, errors.New(locale.Text("неверная идентичность источника", "invalid source identity"))
	}
	if source.PID == a.controllerPID {
		return win32.Window{}, errors.New(locale.Text("окно управления LumaTape нельзя захватывать", "LumaTape controller cannot be captured"))
	}
	for _, w := range a.availableWindows() {
		if uint64(w.Handle) != hwnd || w.PID != source.PID {
			continue
		}
		actual := a.sourceDescriptor(w)
		if source.ProcessCreated != "" && source.ProcessCreated != actual.ProcessCreated {
			return win32.Window{}, errors.New(locale.Text("процесс источника изменился; обновите список окон", "source process identity changed; refresh the source list"))
		}
		if source.ID != "" && source.ID != actual.ID {
			return win32.Window{}, errors.New(locale.Text("источник изменился; обновите список окон", "source identity changed; refresh the source list"))
		}
		return w, nil // current enumerated title is authoritative
	}
	return win32.Window{}, errors.New(locale.Text("окно источника больше недоступно", "source window is no longer available"))
}

func (a *application) processControl() {
	select {
	case <-a.previewDone:
		a.previewBusy = false
	default:
	}
	select {
	case <-a.control.Done():
		a.quit = true
		return
	default:
	}
	// Keep rendering/message processing responsive under frequent status polls.
	for i := 0; i < 4 && !a.quit; i++ {
		r, ok := a.control.Next()
		if !ok {
			return
		}
		a.executeControl(r)
	}
}

func (a *application) executeControl(r control.Request) {
	replyError := func(code string, err error, result any) {
		a.control.Reply(r.ID, result, &control.Failure{Code: code, Message: err.Error(), Unsaved: a.unsaved})
	}
	noPayload := func() bool {
		if err := control.DecodePayload(r.Payload, &struct{}{}); err != nil {
			replyError("invalid_payload", err, nil)
			return false
		}
		return true
	}
	switch r.Type {
	case "snapshot":
		if noPayload() {
			a.control.Reply(r.ID, a.controlSnapshot(), nil)
		}
	case "sources":
		if noPayload() {
			a.control.Reply(r.ID, a.controlSources(), nil)
		}
	case "diagnostics":
		if noPayload() {
			a.control.Reply(r.ID, json.RawMessage(a.settingsDiagnostic()), nil)
		}
	case "ui_state":
		var p struct {
			HWND    string `json:"hwnd"`
			Visible bool   `json:"visible"`
		}
		if err := control.DecodePayload(r.Payload, &p); err != nil {
			replyError("invalid_payload", err, nil)
			return
		}
		if !p.Visible && p.HWND == "" {
			a.controllerHWND = 0
			a.control.Reply(r.ID, map[string]bool{"accepted": true}, nil)
			return
		}
		hwnd, err := strconv.ParseUint(p.HWND, 0, 64)
		if err != nil || !win32.SameProcess(uintptr(hwnd), a.controllerPID) {
			replyError("invalid_controller", errors.New(locale.Text("окно интерфейса не принадлежит процессу управления", "UI window does not belong to the controller process")), nil)
			return
		}
		a.controllerHWND = uintptr(hwnd)
		a.control.Reply(r.ID, map[string]bool{"accepted": true}, nil)
	case "apply":
		var p control.ApplyPayload
		if err := control.DecodePayload(r.Payload, &p); err != nil {
			replyError("invalid_payload", err, a.controlSnapshot())
			return
		}
		if failure := a.emergencyBarrier.checkApply(p, a.cfg); failure != nil {
			replyError(failure.Code, failure, a.controlSnapshot())
			return
		}
		if len(p.Config) == 0 {
			replyError("validation", errors.New(locale.Text("необходимы настройки", "config is required")), a.controlSnapshot())
			return
		}
		next, err := config.Decode(p.Config)
		if err != nil {
			replyError("validation", err, a.controlSnapshot())
			return
		}
		var selected []win32.Window
		if p.Source != nil {
			w, err := a.controlSelected(*p.Source)
			if err != nil {
				replyError("source_unavailable", err, a.controlSnapshot())
				return
			}
			if next.Target.Kind != "window" {
				replyError("validation", errors.New(locale.Text("источник-окно требует target.kind=window", "window source requires target.kind=window")), a.controlSnapshot())
				return
			}
			selected = []win32.Window{w}
		}
		a.controlMutationReply(r.ID, a.ApplyDraft(next, selected...))
	case "shaders", "shader_import", "shader_source":
		a.controlShaders(r)
	case "preview":
		a.controlPreview(r)
	case "quit":
		if !noPayload() {
			return
		}
		a.controlQuitID = r.ID
		a.quit = true // acknowledgement is emitted by Run only after cleanup
	case "toggle", "disable", "emergency", "restore", "confirm", "reload":
		if !noPayload() {
			return
		}
		var err error
		switch r.Type {
		case "toggle":
			next := a.cfg
			if a.suspended {
				next.Enabled = true
			} else {
				next.Enabled = !next.Enabled
			}
			err = a.ApplyDraft(next)
		case "disable":
			err = a.disableFilter()
		case "emergency":
			a.control.CancelQueued()
			err = a.emergency()
		case "restore":
			err = a.stopAndRestore()
			err = errors.Join(err, a.save())
		case "confirm":
			if a.displaySession == nil || a.displayTerminalSeen {
				err = errors.New(locale.Text("нет нового видеорежима для подтверждения", "no active display mode to confirm"))
			} else {
				err = a.displaySession.Confirm()
				if err == nil {
					a.confirmationDeadline = time.Time{}
				} else {
					err = errors.Join(err, a.restoreDisplay())
					a.cfg.Aspect.Enabled = false
				}
			}
		case "reload":
			err = a.reload()
		}
		a.controlMutationReply(r.ID, err)
	default:
		replyError("unknown_command", fmt.Errorf(locale.Text("команда %q не поддерживается", "unsupported command %q"), r.Type), nil)
	}
}

func (a *application) controlMutationReply(id string, err error) {
	a.control.Reply(id, a.controlSnapshot(), mutationFailure(err, a.unsaved))
}

func (a *application) controlPreview(r control.Request) {
	fail := func(code string, err error) {
		a.control.Reply(r.ID, nil, &control.Failure{Code: code, Message: err.Error()})
	}
	var p struct {
		Config json.RawMessage `json:"config"`
		Width  int             `json:"width"`
		Height int             `json:"height"`
		Time   float64         `json:"time"`
		Before bool            `json:"before"`
	}
	if err := control.DecodePayload(r.Payload, &p); err != nil {
		fail("invalid_payload", err)
		return
	}
	if p.Width < 1 || p.Width > 640 || p.Height < 1 || p.Height > 480 || math.IsNaN(p.Time) || math.IsInf(p.Time, 0) || p.Time < 0 || p.Time > 86400 {
		fail("invalid_payload", errors.New(locale.Text("предпросмотр требует размер 1..640 на 1..480 и конечное время 0..86400 секунд", "preview requires 1..640 by 1..480 and finite time 0..86400 seconds")))
		return
	}
	if a.previewBusy || time.Since(a.lastControlPreview) < 500*time.Millisecond {
		fail("rate_limited", errors.New(locale.Text("предпросмотр ограничен одним запросом за раз и двумя кадрами в секунду", "preview is limited to one outstanding request and two frames per second")))
		return
	}
	if len(p.Config) == 0 {
		fail("validation", errors.New(locale.Text("необходимы настройки предпросмотра", "preview config is required")))
		return
	}
	// The preview is a synthetic scene. It must accept a draft before a real
	// source has been chosen, without relaxing its screen/input constraints.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(p.Config, &raw); err != nil {
		fail("validation", err)
		return
	}
	if raw == nil {
		fail("validation", errors.New(locale.Text("настройки предпросмотра должны быть объектом", "preview config must be an object")))
		return
	}
	target, _ := json.Marshal(config.Target{Kind: "window", WindowTitle: "LumaTape preview"})
	raw["target"] = target
	normalized, err := json.Marshal(raw)
	if err != nil {
		fail("validation", err)
		return
	}
	draft, err := config.Decode(normalized)
	if err != nil {
		fail("validation", err)
		return
	}
	draft.Enabled = true
	if p.Before {
		draft.Effects.Intensity = 0
	}
	pixels, err := a.previewRGBA(draft, p.Width, p.Height, p.Time)
	if err != nil {
		fail("preview_failed", err)
		return
	}
	a.lastControlPreview = time.Now()
	a.previewBusy = true
	server, done := a.control, a.previewDone
	go func() {
		defer func() { done <- struct{}{} }()
		var encoded bytes.Buffer
		image := &image.RGBA{Pix: pixels, Stride: p.Width * 4, Rect: image.Rect(0, 0, p.Width, p.Height)}
		if err := png.Encode(&encoded, image); err != nil {
			server.Reply(r.ID, nil, &control.Failure{Code: "preview_failed", Message: err.Error()})
			return
		}
		server.Reply(r.ID, map[string]any{"mime": "image/png", "width": p.Width, "height": p.Height, "png_base64": base64.StdEncoding.EncodeToString(encoded.Bytes())}, nil)
	}()
}
