//go:build windows

package app

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

const (
	cmdToggle = 10 + iota
	cmdEmergency
	cmdOverlay
	cmdFull
	cmdIntensityDown
	cmdIntensityUp
	cmdFormatOff
	cmdMask
	cmdWindow43
	cmdSystem43
	cmdConfirmMode
	cmdRestore
	cmdMouse
	cmdGamepad
	cmdFit
	cmdCrop
	cmdStretch
	cmdDARAuto
	cmdDAR43
	cmdSettings
	cmdReload
	cmdStatus
	cmdLog
	cmdQuit
	cmdFullCompatibility
)

// Selection menus are snapshots. The handle/PID pair is validated again when a
// queued command is applied; HWNDs are never restored from the settings file.
var menuWindows []win32.Window
var menuMonitors []win32.Monitor

func (a *application) menu() []win32.MenuItem {
	menuWindows = a.availableWindows()
	menuMonitors = append([]win32.Monitor(nil), a.monitors...)
	targets := make([]win32.MenuItem, 0, len(menuWindows)+len(menuMonitors))
	for i, w := range menuWindows {
		if i >= 800 {
			break
		}
		targets = append(targets, win32.MenuItem{ID: 1000 + i, Label: truncate(w.Title, 64), Checked: w.Handle == a.target.Handle && a.cfg.Target.Kind == "window"})
	}
	if len(targets) == 0 {
		targets = append(targets, win32.MenuItem{Label: locale.Text("Нет доступных окон", "No available windows"), Disabled: true})
	}
	monitorItems := []win32.MenuItem{}
	for i, m := range menuMonitors {
		monitorItems = append(monitorItems, win32.MenuItem{ID: 2000 + i, Label: fmt.Sprintf("%d: %s · %d×%d", i, m.Device, m.Bounds.W, m.Bounds.H), Checked: a.cfg.Target.Kind == "monitor" && m.Device == a.monitorDevice})
	}
	presets := []win32.MenuItem{}
	for i, p := range config.PresetNames {
		presets = append(presets, win32.MenuItem{ID: 100 + i, Label: p, Checked: a.cfg.Preset == p})
	}
	fullLabel := locale.Text("Full CRT/VHS — GPU (WGC окна)", "Full CRT/VHS — GPU (window WGC)")
	compatibilityLabel := locale.Text("Full CRT/VHS — совместимость (CPU, до 30 кадров/с)", "Full CRT/VHS — compatibility (CPU, up to 30 FPS)")
	if a.library == nil || a.gpuCapability.State == "unavailable" {
		fullLabel += locale.Text(" — недоступен", " — unavailable")
	}
	if !a.library.SupportsCompatibility() {
		compatibilityLabel += locale.Text(" — недоступен", " — unavailable")
	}
	exact := a.cfg.InputMode == "mouse-exact"
	state := locale.Text("Фильтр включён", "Filter enabled")
	if a.suspended {
		state = locale.Text("Возобновить оверлей", "Resume overlay")
	}
	items := []win32.MenuItem{
		{Label: "LumaTape · SDR · windowed / borderless", Disabled: true},
		{ID: cmdToggle, Label: state + "\t" + a.cfg.Hotkeys.Toggle, Checked: a.cfg.Enabled && !a.suspended},
		{ID: cmdEmergency, Label: locale.Text("Аварийно отключить и восстановить\t", "Emergency disable and restore\t") + a.cfg.Hotkeys.Emergency},
		{},
		{Label: locale.Text("Привязать к окну игры", "Select game window"), Children: targets},
		{Label: locale.Text("Весь монитор (Lightweight)", "Entire monitor (Lightweight)"), Children: monitorItems},
		{ID: cmdOverlay, Label: locale.Text("Lightweight — прозрачный эффект", "Lightweight — transparent effect"), Checked: a.cfg.Mode == "overlay"},
		{ID: cmdFull, Label: fullLabel, Checked: a.cfg.Mode == "full" && a.cfg.Capture.Transfer == config.TransferGPU, Disabled: a.library == nil || a.gpuCapability.State == "unavailable" || a.cfg.Target.Kind != "window" || a.target.Handle == 0},
		{ID: cmdFullCompatibility, Label: compatibilityLabel, Checked: compatibilityCapture(a.cfg), Disabled: !a.library.SupportsCompatibility() || a.cfg.Target.Kind != "window" || a.target.Handle == 0},
		{Label: locale.Text("Пресет: ", "Preset: ") + a.cfg.Preset, Children: presets},
		{Label: fmt.Sprintf(locale.Text("Интенсивность: %.0f%%", "Intensity: %.0f%%"), a.cfg.Effects.Intensity*100), Children: []win32.MenuItem{{ID: cmdIntensityDown, Label: "−10%", Disabled: a.cfg.Effects.Intensity <= 0}, {ID: cmdIntensityUp, Label: "+10%", Disabled: a.cfg.Effects.Intensity >= 1}}},
		{Label: locale.Text("Формат 4:3", "4:3 format"), Children: []win32.MenuItem{
			{ID: cmdFormatOff, Label: locale.Text("Выключить и восстановить", "Disable and restore"), Checked: !a.cfg.Aspect.Enabled},
			{ID: cmdMask, Label: locale.Text("Маска/рамка 4:3 (не меняет рендер игры)", "4:3 mask/frame (does not change game rendering)"), Checked: a.cfg.Aspect.Enabled && a.cfg.Aspect.Method == "mask"},
			{ID: cmdWindow43, Label: locale.Text("Разместить клиентское окно 4:3", "Resize client window to 4:3"), Disabled: a.cfg.Target.Kind != "window" || a.target.Handle == 0, Checked: a.cfg.Aspect.Enabled && a.cfg.Aspect.Method == "window"},
			{ID: cmdSystem43, Label: locale.Text("Временный системный режим 4:3…", "Temporary 4:3 display mode…"), Checked: a.displaySession != nil},
			{ID: cmdConfirmMode, Label: locale.Text("Подтвердить новый видеорежим", "Confirm new display mode"), Disabled: a.confirmationDeadline.IsZero()},
			{ID: cmdRestore, Label: locale.Text("Восстановить исходные окно/видеорежим", "Restore original window/display mode"), Disabled: a.windowRestore == nil && a.displaySession == nil},
		}},
		{Label: locale.Text("Искажения изображения и масштаб Full", "Image distortion and Full scaling"), Children: []win32.MenuItem{
			{ID: cmdMouse, Label: locale.Text("Точные клики", "Accurate clicks"), Checked: exact},
			{ID: cmdGamepad, Label: locale.Text("Разрешить произвольные искажения", "Allow arbitrary distortion"), Checked: !exact},
			{},
			{ID: cmdFit, Label: locale.Text("Fit — сохранить пропорции", "Fit — preserve proportions"), Checked: a.cfg.Aspect.Scale == geometry.Fit, Disabled: exact},
			{ID: cmdCrop, Label: locale.Text("Crop — обрезать", "Crop — trim edges"), Checked: a.cfg.Aspect.Scale == geometry.Crop, Disabled: exact},
			{ID: cmdStretch, Label: locale.Text("Stretch — исказить", "Stretch — distort"), Checked: a.cfg.Aspect.Scale == geometry.Stretch, Disabled: exact},
			{ID: cmdDARAuto, Label: locale.Text("DAR: квадратные пиксели", "DAR: square pixels"), Checked: a.cfg.Aspect.SourceDAR == 0, Disabled: exact},
			{ID: cmdDAR43, Label: locale.Text("DAR: трактовать исходник как 4:3", "DAR: interpret source as 4:3"), Checked: a.cfg.Aspect.SourceDAR == 4.0/3.0, Disabled: exact},
		}},
		{},
		{ID: cmdSettings, Label: locale.Text("Настройки LumaTape…", "LumaTape settings…")},
		{ID: cmdReload, Label: locale.Text("Перечитать настройки", "Reload settings")},
		{ID: cmdStatus, Label: locale.Text("Статус и измерения…", "Status and measurements…")},
		{ID: cmdLog, Label: locale.Text("Открыть журнал…", "Open log…")},
		{ID: cmdQuit, Label: locale.Text("Выход с восстановлением", "Quit and restore")},
	}
	if a.cfg.Target.Kind == "window" {
		// Opening the tray takes focus from the source. Make that deliberate
		// visibility rule apparent while the user compares presets.
		items = slices.Insert(items, 1, win32.MenuItem{Label: locale.Text("Вернитесь в окно игры, чтобы увидеть эффект", "Return to the game window to see the effect"), Disabled: true})
	}
	return items
}

func (a *application) command(id int) {
	if id == 0 {
		return
	}
	a.hide()
	next := a.cfg
	var selected []win32.Window
	var err error
	apply := true
	switch {
	case id >= 1000 && id < 1800:
		i := id - 1000
		if i >= len(menuWindows) {
			return
		}
		selected = []win32.Window{menuWindows[i]}
		next.Target.Kind, next.Target.WindowTitle = "window", menuWindows[i].Title
		next.Aspect.Enabled = false
	case id >= 2000:
		i := id - 2000
		if i >= len(menuMonitors) {
			return
		}
		next.Target.Kind, next.Target.Monitor = "monitor", i
		next.Mode, next.Aspect.Enabled = "overlay", false
	case id >= 100 && id < 100+len(config.PresetNames):
		next.Preset = config.PresetNames[id-100]
		intensity := next.Effects.Intensity
		next.Effects, _ = config.Preset(next.Preset)
		next.Shader = config.ShaderConfig{}
		next.Effects.Intensity = intensity
	default:
		switch id {
		case cmdToggle:
			if a.suspended {
				next.Enabled = true
			} else {
				next.Enabled = !next.Enabled
			}
		case cmdEmergency:
			a.emergency()
			return
		case cmdOverlay:
			next.Mode = "overlay"
		case cmdFull, cmdFullCompatibility:
			next.Mode, next.Capture.Transfer = "full", config.TransferGPU
			if id == cmdFullCompatibility {
				next.Capture.Transfer = config.TransferCompatibility
			}
		case cmdIntensityDown:
			next.Effects.Intensity = math.Max(0, math.Round((next.Effects.Intensity-.1)*10)/10)
		case cmdIntensityUp:
			next.Effects.Intensity = math.Min(1, math.Round((next.Effects.Intensity+.1)*10)/10)
		case cmdFormatOff:
			next.Aspect.Enabled = false
		case cmdRestore:
			apply = false
			if err = a.closeCapture(); err == nil {
				err = a.restore()
			}
			a.save()
		case cmdMask:
			next.Aspect.Enabled, next.Aspect.Method = true, "mask"
		case cmdWindow43:
			next.Aspect.Enabled, next.Aspect.Method = true, "window"
		case cmdSystem43:
			next.Aspect.Enabled, next.Aspect.Method = true, "system"
		case cmdConfirmMode:
			apply = false
			if a.displaySession != nil && !a.displayTerminalSeen {
				err = a.displaySession.Confirm()
				if err == nil {
					a.confirmationDeadline = time.Time{}
				} else {
					err = errors.Join(err, a.restoreDisplay())
					a.cfg.Aspect.Enabled = false
				}
			}
		case cmdMouse:
			next.InputMode, next.Aspect.SourceDAR, next.Aspect.Scale = "mouse-exact", 0, geometry.Fit
		case cmdGamepad:
			next.InputMode = "keyboard-gamepad"
		case cmdFit:
			next.Aspect.Scale = geometry.Fit
		case cmdCrop:
			next.Aspect.Scale = geometry.Crop
		case cmdStretch:
			next.Aspect.Scale = geometry.Stretch
		case cmdDARAuto:
			next.Aspect.SourceDAR = 0
		case cmdDAR43:
			next.Aspect.SourceDAR = 4.0 / 3.0
		case cmdSettings:
			a.openSettings()
			return
		case cmdLog:
			err, apply = win32.OpenFile(a.logPath), false
		case cmdStatus:
			apply = false
			s := a.RuntimeStatus()
			backend := s.Backend
			if backend == "" {
				backend = locale.Text("нет активного вывода", "no active output")
			}
			win32.Message(locale.Text("LumaTape — статус", "LumaTape — status"), fmt.Sprintf(locale.Text("Запрошено: %s / %s\nДействует: %s\nСостояние: %s — %s\nПресет: %s · %.0f%%\nGPU: %s — %s\nCPU: %s — %s\nВосстановление требует внимания: %v\n\nПоследние измерения: CPU submit %.3f ms; capture age %.3f ms.\nЭто не input-to-photon latency.\n\nПоследняя ошибка: %s\nЖурнал: %s", "Requested: %s / %s\nApplied: %s\nState: %s — %s\nPreset: %s · %.0f%%\nGPU: %s — %s\nCPU: %s — %s\nRecovery needs attention: %v\n\nLatest measurements: CPU submit %.3f ms; capture age %.3f ms.\nThis is not input-to-photon latency.\n\nLast error: %s\nLog: %s"), s.RequestedMode, s.RequestedTransfer, backend, s.Phase, s.Reason, s.Preset, s.EffectiveIntensity*100, s.GPU.State, s.GPU.Reason, s.Compatibility.State, s.Compatibility.Reason, s.RecoveryPending, a.cpuMS, a.ageMS, s.LastError, a.logPath))
		case cmdReload:
			err, apply = a.reload(), false
		case cmdQuit:
			a.quit = true
			return
		default:
			return
		}
	}
	if apply {
		err = a.ApplyDraft(next, selected...)
	}
	if err != nil {
		a.lastError = err.Error()
		var applied *AppliedSettingsError
		if errors.As(err, &applied) {
			a.issue(locale.Text("Применено, но не сохранено", "Applied but not saved"), err.Error())
		} else {
			a.issue(locale.Text("Действие не выполнено", "Action failed"), err.Error())
		}
		return
	}
	if id == cmdGamepad {
		a.issue(locale.Text("Произвольные искажения", "Arbitrary distortion"), locale.Text("Full следует клиентской области игры. Произвольный шейдер может сместить видимые места кликов.", "Full follows the game client area. Arbitrary shader distortion can misalign clicks."))
	}
	if id == cmdMask {
		a.issue(locale.Text("Маска 4:3", "4:3 mask"), locale.Text("В Lightweight и режиме точной мыши закрываются края, исходные пиксели не растягиваются.", "In Lightweight and Accurate clicks, edges are covered; source pixels are not stretched."))
	}
}

func (a *application) reload() error {
	next, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	return a.applyPreferences(next, false, false, nil)
}
