//go:build windows

package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/diagnostics"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

const (
	uiGame = 3000 + iota
	uiFormat
	uiEffects
	uiDiagnostics
	uiSource
	uiRefresh
	uiBackend
	uiPreset
	uiIntensity
	uiShape
	uiEnabled
	uiAspect
	uiInput
	uiScale
	uiDAR
	uiToggleKey
	uiEmergencyKey
	uiMacKeys
	uiProbe
	uiApply
	uiToggle
	uiEmergency
	uiRestore
	uiConfirm
	uiCompare
	uiCopy
	uiLogs
	uiJSON
	uiReload
	uiStatus
	uiHelp
	uiReport
	uiPreviewLabel
	uiProbeStatus
)

var uiEffectsLabels = []string{locale.Text("Строки CRT", "CRT scanlines"), locale.Text("Фосфорная маска", "Phosphor mask"), locale.Text("Свечение", "Bloom"), locale.Text("Мягкость", "Softness"), locale.Text("Затемнение края", "Vignette"), locale.Text("Растекание цвета", "Chroma bleed"), locale.Text("Шум VHS", "VHS noise"), locale.Text("Дрожание*", "Jitter*"), locale.Text("Трекинг*", "Tracking*"), locale.Text("Радиус углов", "Corner radius"), locale.Text("Выпуклость*", "Curvature*"), locale.Text("Стекло", "Glass")}

func (a *application) openSettings() {
	a.observeForegroundSource()
	if a.control != nil {
		win32.AllowForeground(a.controllerPID)
		a.control.Emit("show_settings", nil)
		return
	}
	if a.settings != nil {
		a.settings.Show()
		return
	}
	s, err := win32.NewSettingsWindow()
	if err != nil {
		a.issue(locale.Text("Настройки", "Settings"), err.Error())
		return
	}
	a.settings = s
	add := func(id int, class, text string, x, y, w, h, page int, style uintptr) {
		s.Add(win32.ControlSpec{ID: id, Class: class, Text: text, X: x, Y: y, W: w, H: h, Page: page, Style: style})
	}
	labelID := 4000
	label := func(text string, x, y, w, h, page int) { labelID++; add(labelID, "STATIC", text, x, y, w, h, page, 0) }
	combo := func(id int, text string, y, page int) {
		label(text, 18, y, 152, 22, page)
		add(id, "COMBOBOX", "", 175, y-3, 248, 240, page, win32.StyleCombo|win32.StyleTab)
	}
	edit := func(id int, text string, y, page int) {
		label(text, 18, y, 152, 22, page)
		add(id, "EDIT", "", 175, y-3, 248, 25, page, win32.StyleEdit|win32.StyleTab)
	}
	button := func(id int, text string, x, y, w, page int) {
		add(id, "BUTTON", text, x, y, w, 30, page, win32.StyleTab)
	}
	button(uiGame, locale.Text("&Игра и экран", "&Game and screen"), 18, 14, 140, 0)
	button(uiFormat, locale.Text("&Формат / клавиши", "&Format / hotkeys"), 165, 14, 158, 0)
	button(uiEffects, locale.Text("&Эффекты", "&Effects"), 330, 14, 110, 0)
	button(uiDiagnostics, locale.Text("&Диагностика", "&Diagnostics"), 447, 14, 145, 0)
	label(locale.Text("LumaTape · SDR · оконные игры и borderless", "LumaTape · SDR · windowed and borderless games"), 18, 54, 410, 24, 0)
	combo(uiSource, locale.Text("Окно / монитор", "Window / monitor"), 92, 1)
	button(uiRefresh, locale.Text("Обновить окна", "Refresh windows"), 270, 120, 153, 1)
	combo(uiBackend, locale.Text("Обработка", "Processing"), 170, 1)
	combo(uiPreset, locale.Text("Пресет", "Preset"), 216, 1)
	edit(uiIntensity, locale.Text("Интенсивность %", "Intensity %"), 261, 1)
	combo(uiShape, locale.Text("Форма экрана", "Screen shape"), 306, 1)
	add(uiEnabled, "BUTTON", locale.Text("Фильтр включён", "Filter enabled"), 18, 342, 390, 28, 1, win32.StyleTab|win32.StyleCheck)
	label(locale.Text("Lightweight: строки, шум и края экрана.\r\nFull: обработка пикселей — мягкость, свечение, VHS.\r\nGPU проверяется при применении. CPU — явный выбор, до 30 FPS.", "Lightweight: scanlines, noise and screen edges.\r\nFull: pixel processing — softness, bloom, VHS.\r\nGPU checked when applied. CPU is explicit, up to 30 FPS."), 18, 385, 407, 80, 1)
	label(locale.Text("Subtle CRT намеренно деликатный.\r\nНовые значения применяются кнопкой внизу.", "Subtle CRT is intentionally delicate.\r\nApply new values using the button below."), 18, 478, 407, 46, 1)
	combo(uiAspect, locale.Text("Формат 4:3", "4:3 format"), 94, 2)
	combo(uiInput, locale.Text("Искажения изображения", "Image distortion"), 135, 2)
	combo(uiScale, locale.Text("Масштаб Full", "Full scaling"), 176, 2)
	combo(uiDAR, locale.Text("Исходный DAR", "Source DAR"), 217, 2)
	label(locale.Text("Маска закрывает края. Размер окна не гарантирует\r\nFOV игры. Crop/DAR требуют произвольных искажений.", "A mask covers the edges. Resizing does not ensure\r\ncorrect FOV. Crop/DAR require arbitrary distortion."), 18, 249, 407, 42, 2)
	edit(uiToggleKey, locale.Text("Переключение", "Toggle effect"), 306, 2)
	edit(uiEmergencyKey, locale.Text("Аварийная клавиша", "Emergency shortcut"), 348, 2)
	button(uiMacKeys, locale.Text("Без Alt / Fn", "Number-key shortcuts"), 18, 385, 155, 2)
	button(uiProbe, locale.Text("Проверить клавиши", "Test hotkeys"), 180, 385, 243, 2)
	add(uiProbeStatus, "STATIC", locale.Text("Регистрация проверяется при применении. Проверка доставки — нажмите обе комбинации.", "Registration is checked on apply. Press both shortcuts to verify delivery."), 18, 422, 405, 54, 2, 0)
	label(locale.Text("Mac: Alt = Option ⌥, Control ≠ Command.\r\nF-клавишам может требоваться Fn. Доставку\r\nопределяет Parallels; хост не изменяется.", "Mac: Alt = Option ⌥, Control ≠ Command.\r\nF-keys may require Fn. Parallels handles delivery;\r\nhost settings are not changed."), 18, 479, 408, 62, 2)
	for i, text := range uiEffectsLabels {
		col, row := i/6, i%6
		x := 18 + col*208
		y := 94 + row*58
		label(text, x, y, 200, 21, 3)
		add(4100+i, "EDIT", "", x, y+23, 194, 26, 3, win32.StyleEdit|win32.StyleTab)
	}
	label(locale.Text("Значения 0–100%. Ручная правка → Custom.\r\n* Дрожание / трекинг выключены в точной мыши.\r\nВыпуклость действует только у формы «Выпуклый».\r\nРадиус: 0–20% короткой стороны экрана.", "Values 0–100%. Manual editing → Custom.\r\n* Jitter / tracking are disabled for Accurate clicks.\r\nCurvature applies only to the Convex screen shape.\r\nRadius: 0–20% of the shorter screen side."), 18, 467, 407, 80, 3)
	add(uiReport, "EDIT", "", 18, 91, 405, 366, 4, win32.StyleEdit|win32.StyleReadOnly|win32.StyleTab)
	button(uiCopy, locale.Text("Скопировать диагностику", "Copy diagnostics"), 18, 468, 405, 4)
	button(uiLogs, locale.Text("Журнал", "Log"), 18, 508, 125, 4)
	button(uiJSON, "JSON", 150, 508, 125, 4)
	button(uiReload, locale.Text("Перечитать", "Reload"), 282, 508, 141, 4)
	add(uiPreviewLabel, "STATIC", locale.Text("Предпросмотр · встроенная сцена · после", "Preview · built-in scene · after"), 450, 62, 444, 26, 0, 0)
	button(uiCompare, locale.Text("Сравнить: до / после", "Compare: before / after"), 450, 381, 235, 0)
	label(locale.Text("Предпросмотр независим от игры.\r\nФокус возвращается в игру только вашим действием.", "Preview is independent of the game.\r\nYou decide when to return focus to the game."), 450, 420, 444, 45, 0)
	add(uiStatus, "STATIC", "", 450, 474, 444, 86, 0, 0)
	button(uiApply, locale.Text("&Применить", "&Apply"), 18, 562, 135, 0)
	button(uiToggle, locale.Text("Вкл / выкл", "Enable / disable"), 160, 562, 120, 0)
	button(uiRestore, locale.Text("Восстановить", "Restore"), 287, 562, 136, 0)
	button(uiConfirm, locale.Text("Сохранить режим", "Keep display mode"), 450, 562, 168, 0)
	button(uiEmergency, locale.Text("СТОП и восстановить", "STOP and restore"), 625, 562, 269, 0)
	s.SetDefault(uiApply)
	s.Page(1)
	a.populateSettings()
	s.Show()
}

func (a *application) populateSettings() {
	s := a.settings
	if s == nil {
		return
	}
	a.settingsDraft = a.cfg
	a.settingsDirty = false
	a.refreshSettingsSources(false)
	backend := 0
	if a.cfg.Mode == "full" {
		backend = 1
		if a.cfg.Capture.Transfer == config.TransferCompatibility {
			backend = 2
		}
		if a.cfg.Capture.Transfer == config.TransferAuto {
			backend = 3
		}
	}
	s.Options(uiBackend, []string{locale.Text("Lightweight — прозрачный слой", "Lightweight — transparent layer"), "Full — GPU interop", locale.Text("Full — CPU совместимость (явно)", "Full — CPU compatibility (explicit)"), "Full — Auto (GPU / CPU)"}, backend)
	presets := append(append([]string{}, config.PresetNames...), "Custom")
	selected := len(presets) - 1
	for i, p := range presets {
		if p == a.cfg.Preset {
			selected = i
		}
	}
	s.Options(uiPreset, presets, selected)
	s.SetText(uiIntensity, fmt.Sprintf("%.0f", a.cfg.Effects.Intensity*100))
	shape := 0
	for i, v := range config.ScreenShapes {
		if a.cfg.Screen.Shape == v {
			shape = i
		}
	}
	s.Options(uiShape, []string{locale.Text("Плоский экран", "Flat screen"), locale.Text("Скруглённый CRT — без смещения", "Rounded CRT — no displacement"), locale.Text("Выпуклый CRT — Full", "Convex CRT — Full")}, shape)
	s.Check(uiEnabled, a.cfg.Enabled)
	aspect := 0
	if a.cfg.Aspect.Enabled {
		for i, v := range []string{"mask", "window", "system"} {
			if a.cfg.Aspect.Method == v {
				aspect = i + 1
			}
		}
	}
	s.Options(uiAspect, []string{locale.Text("Выключен", "Disabled"), locale.Text("Маска 4:3", "4:3 mask"), locale.Text("Клиентское окно 4:3", "4:3 client window"), locale.Text("Системный режим… (подтверждение)", "Display mode… (confirmation)")}, aspect)
	input := 0
	if a.cfg.InputMode == "keyboard-gamepad" {
		input = 1
	}
	s.Options(uiInput, []string{locale.Text("Точные клики", "Accurate clicks"), locale.Text("Разрешить произвольные искажения", "Allow arbitrary distortion")}, input)
	scale := 0
	for i, v := range []geometry.ScaleMode{geometry.Fit, geometry.Crop, geometry.Stretch} {
		if a.cfg.Aspect.Scale == v {
			scale = i
		}
	}
	s.Options(uiScale, []string{locale.Text("Fit — вписать", "Fit — fit inside"), locale.Text("Crop — обрезать", "Crop — trim edges"), locale.Text("Stretch — растянуть", "Stretch — fill")}, scale)
	dar := 0
	if a.cfg.Aspect.SourceDAR != 0 {
		dar = 1
	}
	s.Options(uiDAR, []string{locale.Text("Авто — квадратные пиксели", "Auto — square pixels"), locale.Text("4:3 — например 320×200", "4:3 — e.g. 320×200")}, dar)
	s.SetText(uiToggleKey, a.cfg.Hotkeys.Toggle)
	s.SetText(uiEmergencyKey, a.cfg.Hotkeys.Emergency)
	a.fillEffectControls(a.cfg)
	a.settingsLastRefresh = time.Time{}
}
func (a *application) refreshSettingsSources(preserve bool) {
	old := a.target
	kind := a.cfg.Target.Kind
	monitor := a.cfg.Target.Monitor
	if preserve {
		index := a.settings.Selected(uiSource)
		if index < 0 {
			kind = ""
		}
		if index >= 0 && index < len(a.settingsWindows) {
			old = a.settingsWindows[index]
			kind = "window"
		} else if index >= len(a.settingsWindows) {
			kind = "monitor"
			monitor = index - len(a.settingsWindows)
		}
	}
	s := a.settings
	a.settingsWindows = win32.Windows()
	labels := []string{}
	selected := -1
	for i, w := range a.settingsWindows {
		labels = append(labels, fmt.Sprintf("%s · PID %d", truncate(w.Title, 56), w.PID))
		if kind == "window" && w.Handle == old.Handle && w.PID == old.PID {
			selected = i
		}
	}
	for i, m := range a.monitors {
		labels = append(labels, fmt.Sprintf(locale.Text("Монитор %d · %d×%d (Lightweight)", "Monitor %d · %d×%d (Lightweight)"), i+1, m.Bounds.W, m.Bounds.H))
		if kind == "monitor" && monitor == i {
			selected = len(a.settingsWindows) + i
		}
	}
	s.Options(uiSource, labels, selected)
}
func (a *application) fillEffectControls(c config.Config) {
	e := c.Effects
	values := []float64{e.CRT.Scanlines, e.CRT.Mask, e.CRT.Bloom, e.CRT.Softness, e.CRT.Vignette, e.VHS.ChromaBleed, e.VHS.Noise, e.VHS.Jitter, e.VHS.Tracking, c.Screen.CornerRadius, c.Screen.Curvature, c.Screen.Glass}
	for i, v := range values {
		a.settings.SetText(4100+i, strconv.FormatFloat(v*100, 'f', -1, 64))
	}
}
func (a *application) readSettings() (config.Config, *win32.Window, error) {
	s := a.settings
	c := a.settingsDraft
	index := s.Selected(uiSource)
	var selected *win32.Window
	c.Target = config.Target{}
	if index >= 0 && index < len(a.settingsWindows) {
		w := a.settingsWindows[index]
		selected = &w
		c.Target = config.Target{Kind: "window", WindowTitle: w.Title}
	} else if index >= len(a.settingsWindows) && index < len(a.settingsWindows)+len(a.monitors) {
		c.Target = config.Target{Kind: "monitor", Monitor: index - len(a.settingsWindows)}
	}
	c.Mode = "overlay"
	if s.Selected(uiBackend) > 0 {
		c.Mode = "full"
		c.Capture.Transfer = config.TransferGPU
		if s.Selected(uiBackend) == 2 {
			c.Capture.Transfer = config.TransferCompatibility
		}
		if s.Selected(uiBackend) == 3 {
			c.Capture.Transfer = config.TransferAuto
		}
	}
	p := s.Selected(uiPreset)
	c.Preset = "Custom"
	if p >= 0 && p < len(config.PresetNames) {
		c.Preset = config.PresetNames[p]
	}
	percent := func(id int, name string) (float64, error) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s.Text(id)), ",", "."), 64)
		if err != nil || v < 0 || v > 100 {
			return 0, fmt.Errorf(locale.Text("%s: введите число от 0 до 100", "%s: enter a number from 0 to 100"), name)
		}
		return v / 100, nil
	}
	var err error
	c.Effects.Intensity, err = percent(uiIntensity, locale.Text("Интенсивность", "Intensity"))
	if err != nil {
		return c, nil, err
	}
	v := []*float64{&c.Effects.CRT.Scanlines, &c.Effects.CRT.Mask, &c.Effects.CRT.Bloom, &c.Effects.CRT.Softness, &c.Effects.CRT.Vignette, &c.Effects.VHS.ChromaBleed, &c.Effects.VHS.Noise, &c.Effects.VHS.Jitter, &c.Effects.VHS.Tracking, &c.Screen.CornerRadius, &c.Screen.Curvature, &c.Screen.Glass}
	for i, dst := range v {
		*dst, err = percent(4100+i, uiEffectsLabels[i])
		if err != nil {
			return c, nil, err
		}
	}
	if i := s.Selected(uiShape); i >= 0 && i < len(config.ScreenShapes) {
		c.Screen.Shape = config.ScreenShapes[i]
	}
	c.Enabled = s.Checked(uiEnabled)
	aspect := s.Selected(uiAspect)
	c.Aspect.Enabled = aspect > 0
	if aspect > 0 {
		c.Aspect.Method = []string{"mask", "window", "system"}[aspect-1]
	}
	c.InputMode = "mouse-exact"
	if s.Selected(uiInput) == 1 {
		c.InputMode = "keyboard-gamepad"
	}
	if i := s.Selected(uiScale); i >= 0 && i < 3 {
		c.Aspect.Scale = []geometry.ScaleMode{geometry.Fit, geometry.Crop, geometry.Stretch}[i]
	}
	c.Aspect.SourceDAR = 0
	if s.Selected(uiDAR) == 1 {
		c.Aspect.SourceDAR = 4.0 / 3.0
	}
	c.Hotkeys = config.Hotkeys{Toggle: strings.TrimSpace(s.Text(uiToggleKey)), Emergency: strings.TrimSpace(s.Text(uiEmergencyKey))}
	return c, selected, nil
}

func (a *application) settingsDiagnostic() string {
	status := a.RuntimeStatus()
	data := map[string]any{"build": diagnostics.BuildInfo(), "runtime": status, "config": map[string]any{"mode": a.cfg.Mode, "transfer": a.cfg.Capture.Transfer, "preset": a.cfg.Preset, "screen": a.cfg.Screen, "aspect": a.cfg.Aspect, "input": a.cfg.InputMode, "enabled": a.cfg.Enabled, "intensity": a.cfg.Effects.Intensity}, "hotkeys": map[string]any{"registered": a.tray.HotkeysRegistered(), "toggle_received": a.tray.LastToggle, "emergency_received": a.tray.LastEmergency}, "metrics": map[string]any{"cpu_submission_ms": a.cpuMS, "frame_age_ms": a.ageMS, "transfer": a.transferStats, "transfer_valid": a.transferMetricsValid, "rendered_frames": a.frameCount}}
	data["gl"] = a.renderer.DriverInfo()
	data["environment"] = win32.Environment()
	if a.log != nil {
		data["logger"] = a.log.Status()
	}
	b, err := diagnostics.SupportRecord(data)
	if err != nil {
		return locale.Text("Не удалось собрать диагностику: ", "Could not collect diagnostics: ") + err.Error()
	}
	return string(b)
}

func (a *application) updateSettings() {
	s := a.settings
	if !s.Visible() {
		return
	}
	events := s.Events()
	if len(events) == 0 && !a.settingsDirty && a.cfg != a.settingsDraft {
		a.populateSettings()
	}
	for _, ev := range events {
		if ev.Code == 1 || ev.Code == 0x300 || ev.ID == uiEnabled || ev.ID == uiMacKeys {
			s.Notice = ""
		}
		switch ev.ID {
		case uiGame, uiFormat, uiEffects, uiDiagnostics:
			s.Page(ev.ID - uiGame + 1)
		case uiRefresh:
			a.refreshSettingsSources(true)
		case uiPreset:
			if ev.Code == 1 {
				i := s.Selected(uiPreset)
				if i >= 0 && i < len(config.PresetNames) {
					a.settingsDraft, _ = draftWithPreset(a.settingsDraft, config.PresetNames[i])
					// Presets change only the signal, preserving unsaved screen geometry.
					values := draftSignalValues(a.settingsDraft.Effects)
					for k, v := range values {
						s.SetText(4100+k, strconv.FormatFloat(v*100, 'f', -1, 64))
					}
					a.settingsDirty = true
				}
			}
		case uiMacKeys:
			s.SetText(uiToggleKey, "Ctrl+Shift+9")
			s.SetText(uiEmergencyKey, "Ctrl+Shift+0")
			s.SetText(uiProbeStatus, locale.Text("Профиль подготовлен. Примените: прежние клавиши сохранятся при конфликте.", "Profile prepared. Apply it: previous hotkeys will be kept if a conflict occurs."))
			a.settingsDirty = true
		case uiProbe:
			s.SetText(uiProbeStatus, fmt.Sprintf(locale.Text("Windows: зарегистрировано = %t. Получено команд: вкл/выкл %d, СТОП %d. Нажмите комбинации; СТОП действительно восстановит окно.", "Windows: registered = %t. Commands received: toggle %d, STOP %d. Press the shortcuts; STOP will actually restore the window."), a.tray.HotkeysRegistered(), a.tray.LastToggle, a.tray.LastEmergency))
		case uiApply:
			c, w, err := a.readSettings()
			if c.Target.Kind == "" && err == nil {
				err = fmt.Errorf(locale.Text("выберите открытое окно или монитор в разделе «Игра и экран»", "select an open window or monitor under “Game and screen”"))
			}
			if err == nil {
				if w != nil {
					err = a.ApplyDraft(c, *w)
				} else {
					err = a.ApplyDraft(c)
				}
			}
			if err != nil {
				var applied *AppliedSettingsError
				if errors.As(err, &applied) {
					a.populateSettings()
					s.Notice = locale.Text("Применено, но не сохранено: ", "Applied but not saved: ") + err.Error()
				} else {
					s.Notice = locale.Text("Не применено: ", "Not applied: ") + truncate(err.Error(), 190)
					if strings.Contains(err.Error(), "WGL_NV_DX_interop2") {
						s.Notice = locale.Text("Не применено: драйвер не поддерживает GPU-перенос. Выберите Full CPU явно либо Lightweight. Рабочие настройки сохранены.", "Not applied: the driver does not support GPU transfer. Explicitly select Full CPU or Lightweight. Working settings are preserved.")
					}
				}
				s.SetText(uiStatus, s.Notice)
			} else {
				a.populateSettings()
				s.Notice = ""
				s.SetText(uiStatus, s.Notice)
			}
			a.settingsLastRefresh = time.Now()
		case uiToggle:
			a.command(cmdToggle)
			a.populateSettings()
		case uiEmergency:
			a.command(cmdEmergency)
			a.populateSettings()
		case uiRestore:
			a.command(cmdRestore)
			a.populateSettings()
		case uiConfirm:
			a.command(cmdConfirmMode)
		case uiCompare:
			a.settingsPreviewBefore = !a.settingsPreviewBefore
			a.settingsLastRefresh = time.Time{}
		case uiCopy:
			if err := win32.CopyText(s.HWND, a.settingsDiagnostic()); err != nil {
				s.Notice = err.Error()
				s.SetText(uiStatus, s.Notice)
			} else {
				s.Notice = locale.Text("Диагностика скопирована. Без снимков, названий чужих окон и полных путей.", "Diagnostics copied. No screenshots, other window titles or full paths.")
				s.SetText(uiStatus, s.Notice)
			}
			a.settingsLastRefresh = time.Now()
		case uiLogs:
			a.command(cmdLog)
		case uiJSON:
			win32.OpenFile(a.configPath)
		case uiReload:
			a.command(cmdReload)
			a.populateSettings()
		default:
			if ev.ID >= 4100 && ev.ID < 4109 && ev.Code == 0x300 {
				s.Select(uiPreset, len(config.PresetNames))
				a.settingsDirty = true
			}
			if ev.Code == 1 || ev.Code == 0x300 || ev.ID == uiEnabled {
				a.settingsDirty = true
			}
		}
	}
	if time.Since(a.settingsLastRefresh) < 500*time.Millisecond {
		return
	}
	a.settingsLastRefresh = time.Now()
	status := a.RuntimeStatus()
	pending := ""
	if a.settingsDirty {
		pending = locale.Text("\r\nЕсть неприменённые настройки.", "\r\nThere are unapplied settings.")
	}
	phase := map[string]string{"active": locale.Text("работает", "active"), "ready": locale.Text("готов", "ready"), "disabled": locale.Text("выключен", "disabled"), "bypass": locale.Text("интенсивность 0%", "intensity 0%"), "paused-focus": locale.Text("источник неактивен", "source inactive"), "paused-moving": locale.Text("пауза при движении", "paused while moving"), "waiting-frame": locale.Text("ожидание свежего кадра", "waiting for a fresh frame"), "source-closed": locale.Text("источник закрыт", "source closed"), "waiting-source": locale.Text("источник не выбран", "no source selected"), "paused-stale": locale.Text("ожидание свежего кадра", "waiting for a fresh frame"), "error": locale.Text("ошибка", "error"), "recovery-error": locale.Text("ошибка восстановления", "recovery error")}[status.Phase]
	if phase == "" {
		phase = status.Phase
	}
	if !status.Enabled {
		phase = locale.Text("выключен", "disabled")
	}
	format := locale.Text("выключен", "disabled")
	if status.FormatRequested && !status.FormatActive {
		format = locale.Text("выбран, показ приостановлен", "selected, display paused")
	}
	if status.FormatActive {
		format = map[string]string{"mask": locale.Text("маска 4:3", "4:3 mask"), "window": locale.Text("окно 4:3", "4:3 window"), "system": locale.Text("видеорежим 4:3", "4:3 display mode")}[status.FormatMethod]
	}
	if status.RecoveryPending {
		format += locale.Text(" · ожидается восстановление", " · recovery pending")
	}
	detail := status.Reason + pending
	if s.Notice != "" {
		detail = s.Notice
	}
	s.SetText(uiStatus, fmt.Sprintf(locale.Text("Фильтр: %s · %.0f%% · %s\r\nФормат: %s\r\n%s", "Filter: %s · %.0f%% · %s\r\nFormat: %s\r\n%s"), phase, status.EffectiveIntensity*100, func() string {
		if status.Backend == "" {
			return locale.Text("поверхность скрыта", "surface hidden")
		}
		return status.Backend
	}(), format, detail))
	s.Enable(uiConfirm, !a.confirmationDeadline.IsZero())
	s.SetText(uiReport, a.settingsDiagnostic())
	full := s.Selected(uiBackend) > 0
	for _, id := range []int{4101, 4102, 4103, 4105} {
		s.Enable(id, full)
	}
	exact := s.Selected(uiInput) == 0
	for _, id := range []int{4107, 4108, 4110} {
		s.Enable(id, full && !exact)
	}
	s.Enable(uiScale, full && (!exact || s.Selected(uiScale) != 0))
	s.Enable(uiDAR, full && (!exact || s.Selected(uiDAR) != 0))
	s.SetText(uiProbeStatus, fmt.Sprintf(locale.Text("Windows: регистрация %t. Команды получены:\r\nПереключение %d · аварийное %d.\r\nДля проверки нажмите сочетания (действия настоящие).", "Windows: registered %t. Commands received:\r\nToggle %d · emergency %d.\r\nPress the shortcuts to test (actions are real)."), a.tray.HotkeysRegistered(), a.tray.LastToggle, a.tray.LastEmergency))
	c, _, err := a.readSettings()
	if err != nil {
		s.SetText(uiPreviewLabel, locale.Text("Предпросмотр: ", "Preview: ")+err.Error())
		return
	}
	// Preview intentionally runs while the source is unfocused; it never captures
	// another app. Its enabled switch is independent of the live overlay.
	c.Enabled = true
	side := locale.Text("после", "after")
	if a.settingsPreviewBefore {
		c.Effects.Intensity = 0
		side = locale.Text("до", "before")
	}
	width, height := s.PreviewSize()
	pixels, err := a.previewRGBA(c, width, height, time.Since(a.start).Seconds())
	if err != nil {
		s.SetText(uiPreviewLabel, locale.Text("Предпросмотр недоступен: ", "Preview unavailable: ")+err.Error())
		return
	}
	s.SetPreview(pixels, width, height)
	s.SetText(uiPreviewLabel, fmt.Sprintf(locale.Text("Предпросмотр · %s · %s · %.0f%%", "Preview · %s · %s · %.0f%%"), side, c.Preset, c.Effects.Intensity*100))
}
func (a *application) closeSettings() {
	if a.settings != nil {
		a.settings.Close()
		a.settings = nil
	}
}
