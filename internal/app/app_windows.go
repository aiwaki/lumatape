//go:build windows

package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/control"
	"github.com/aiwaki/lumatape/internal/diagnostics"
	"github.com/aiwaki/lumatape/internal/display"
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/pacing"
	"github.com/aiwaki/lumatape/internal/platform/capture"
	"github.com/aiwaki/lumatape/internal/platform/glfw"
	"github.com/aiwaki/lumatape/internal/platform/win32"
	"github.com/aiwaki/lumatape/internal/pointer"
	"github.com/aiwaki/lumatape/internal/render"
)

type application struct {
	cfg                                 config.Config
	configPath, logPath, directory      string
	log                                 *diagnostics.Logger
	api                                 *glfw.API
	window                              *glfw.Window
	renderer                            *render.Renderer
	tray                                *win32.Tray
	library                             *capture.Library
	capture                             *capture.Capture
	captureError                        error
	monitors                            []win32.Monitor
	monitorDevice                       string
	target                              win32.Window
	bounds                              geometry.Rect
	visible, suspended, quit            bool
	terminalError                       error
	abandonGPU                          bool
	captureStale                        bool
	displaySession                      *display.Session
	confirmationDeadline                time.Time
	windowRestore                       *windowChange
	lastIssue                           string
	start, lastMetrics, lastTopology    time.Time
	cpuMS, ageMS                        float64
	frameCount, maxFrames               int
	pacer                               *pacing.Pacer
	pacingInterval                      time.Duration
	pacingDevice                        string
	pacingCompatibility                 bool
	compatibilityNotified               bool
	transferStats                       capture.Stats
	transferMetricsValid                bool
	transferStatsSerial                 uint64
	transferStatsObservedAt             time.Time
	captureAcquireMS                    float64
	captureSlowLogs, captureWaitingLogs captureEventGate
	snapshotPath                        string
	snapshotAfter                       int
	snapshotDone                        bool
	sourceMotion                        sourceMotionGate
	sourceMotionPaused                  bool
	settings                            *win32.SettingsWindow
	settingsLastRefresh                 time.Time
	settingsWindows                     []win32.Window
	settingsDraft                       config.Config
	settingsPreviewBefore               bool
	settingsDirty                       bool
	phase, pauseReason, lastError       string
	gpuCapability, cpuCapability        CapabilityStatus
	displayTerminalSeen                 bool
	unsaved                             bool
	windowRecoveryPath                  string
	windowRecoveryError                 error
	metrics                             *FrameMetrics
	metricSummary                       FrameMetricSummary
	metricSummaryAt, lastPresented      time.Time
	lastFrameAt, gpuObservedAt          time.Time
	metricSize                          geometry.Size
	lastGPUSequence                     uint64
	cpuWorkMS                           float64
	control                             *control.Server
	controllerPID                       uint32
	controllerHWND                      uintptr
	controlQuitID                       string
	previewDone                         chan struct{}
	previewBusy                         bool
	lastControlPreview                  time.Time
	emergencyBarrier                    emergencyBarrier
	shaderDirectory                     string
	shaderProgram, previewShader        *render.ShaderProgram
	resolvedTransfer                    string
	lastForeground                      uintptr
	suggestedSource                     *controlSource
	pointerSession                      *pointer.Session
	pointerEngaged                      bool
	pointerFailure                      error
	pointerTarget                       win32.Window
	pointerSourceCreated                uint64
}
type windowChange struct {
	target      win32.Window
	transaction *windowTransaction
}

var controlledProcess bool

func ReportError(err error) {
	if !controlledProcess {
		win32.Message("LumaTape", err.Error())
	}
}

func Run(args []string) (err error) {
	settingsDir, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	fs := flag.NewFlagSet("lumatape", flag.ContinueOnError)
	path := fs.String("config", filepath.Join(settingsDir, "LumaTape", "config.json"), "settings JSON")
	windowTitle := fs.String("window", "", "exact game window title")
	monitor := fs.Int("monitor", -1, "whole-monitor Lightweight target index")
	mode := fs.String("mode", "", "overlay or full")
	transfer := fs.String("capture-transfer", "", "Full transfer: auto, gpu (CLI default), compatibility (CPU, at most 30 FPS)")
	snapshot := fs.String("snapshot", "", "save one actual Full backbuffer PNG (explicit GPU readback for qualification)")
	snapshotAfter := fs.Int("snapshot-after-frames", 60, "earliest completed frame count for a fresh Full snapshot")
	list := fs.Bool("list", false, "list current physical monitors and windows")
	disabled := fs.Bool("disabled", false, "start suspended (tray only)")
	frames := fs.Int("frames", 0, "exit after N rendered frames; runtime smoke testing")
	controlStdio := fs.Bool("control-stdio", false, "local JSONL controller over inherited stdin/stdout")
	headlessSettings := fs.Bool("headless-settings", false, "route settings requests to the stdio controller")
	controllerPID := fs.Uint("controller-pid", 0, "desktop host PID, excluded from capture targets")
	if e = fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	controlledProcess = *controlStdio
	var controller *control.Server
	var running *application
	if *controlStdio {
		controller = control.New(os.Stdin, os.Stdout)
		defer func() {
			if err != nil {
				failure := &control.Failure{Code: "engine_failed", Message: err.Error()}
				var typed *control.Failure
				if errors.As(err, &typed) {
					failure = typed
				}
				controller.Emit("fatal", failure)
			}
			if running != nil && running.controlQuitID != "" {
				var failure *control.Failure
				if err != nil {
					failure = &control.Failure{Code: "cleanup_failed", Message: err.Error()}
				}
				controller.Reply(running.controlQuitID, map[string]any{"clean_shutdown": err == nil}, failure)
			}
			controller.Emit("stopped", map[string]any{"clean_shutdown": err == nil})
			err = errors.Join(err, controller.Close())
		}()
		if *list || *frames != 0 || *snapshot != "" {
			return errors.New("control-stdio cannot be combined with list/frames/snapshot")
		}
		if *controllerPID == 0 || uint64(*controllerPID) > uint64(^uint32(0)) || *controllerPID == uint(os.Getpid()) {
			return errors.New("control-stdio requires a distinct live controller-pid")
		}
		if _, alive, err := win32.ProcessIdentity(uint32(*controllerPID)); err != nil || !alive {
			return errors.Join(errors.New(locale.Text("процесс управления недоступен", "controller process is unavailable")), err)
		}
	} else if *headlessSettings || *controllerPID != 0 {
		return errors.New("headless-settings/controller-pid require control-stdio")
	}
	var logger *diagnostics.Logger
	if !*list {
		instance, already, err := win32.AcquireInstance()
		if err != nil {
			if controller != nil && already {
				return &control.Failure{Code: "engine_already_running", Message: err.Error()}
			}
			return err
		}
		if already {
			if controller != nil {
				return &control.Failure{Code: "engine_already_running", Message: "another LumaTape engine is already running"}
			}
			return nil
		}
		defer instance.Close()
		logger, e = diagnostics.Open(filepath.Join(filepath.Dir(*path), "lumatape.log"))
		if e != nil {
			return e
		}
		defer func() {
			if err != nil {
				logger.Record(diagnostics.Error, "fatal", diagnostics.ErrorData("startup_or_runtime", err))
			}
			err = errors.Join(err, logger.Close())
		}()
		logger.Record(diagnostics.Info, "launch", map[string]any{"build": diagnostics.BuildInfo()})
	}
	if e = win32.InitializeDPI(); e != nil {
		return e
	}
	var recoveryPath string
	var recoveryErr error
	if !*list {
		local, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		recoveryPath = filepath.Join(local, "LumaTape", "window-recovery.json")
		recoveryErr = recoverInterruptedWindow(recoveryPath)
		if recoveryErr != nil {
			logger.Record(diagnostics.Error, "window_recovery_error", recoveryErr.Error())
		}
	}
	monitors, e := win32.Monitors()
	if e != nil {
		return e
	}
	if len(monitors) == 0 {
		return errors.New(locale.Text("нет активных мониторов", "no active monitors"))
	}
	if *list {
		for i, m := range monitors {
			fmt.Printf("Monitor %d: %s %+v\n", i, m.Device, m.Bounds)
		}
		for _, w := range win32.Windows() {
			fmt.Printf("Window 0x%x: %s\n", w.Handle, w.Title)
		}
		return nil
	}
	c, e := loadStartupConfig(*path, *controlStdio)
	if e != nil {
		return e
	}
	if *windowTitle != "" {
		c.Target.Kind = "window"
		c.Target.WindowTitle = *windowTitle
	}
	if *monitor >= 0 {
		c.Target.Kind = "monitor"
		c.Target.Monitor = *monitor
	}
	if *mode != "" {
		c.Mode = *mode
	}
	if *transfer != "" {
		c.Capture.Transfer = *transfer
	}
	if e = c.Validate(); e != nil {
		return e
	}
	if e = validateSnapshotRequest(c, *snapshot, *snapshotAfter, *frames); e != nil {
		return e
	}
	if c.Target.Kind == "monitor" && c.Target.Monitor >= len(monitors) {
		return fmt.Errorf(locale.Text("монитор %d недоступен; используйте --list", "monitor %d unavailable; use --list"), c.Target.Monitor)
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	a := &application{cfg: c, configPath: *path, directory: filepath.Dir(exe), monitors: monitors, suspended: *disabled, maxFrames: *frames, start: time.Now()}
	running = a
	a.control, a.controllerPID, a.previewDone = controller, uint32(*controllerPID), make(chan struct{}, 1)
	a.windowRecoveryPath, a.windowRecoveryError = recoveryPath, recoveryErr
	localAssets, assetErr := os.UserCacheDir()
	if assetErr != nil {
		return assetErr
	}
	a.shaderDirectory = filepath.Join(localAssets, "LumaTape", "Shaders")
	a.snapshotPath, a.snapshotAfter = *snapshot, *snapshotAfter
	a.monitorDevice = monitors[0].Device
	if c.Target.Kind == "monitor" {
		a.monitorDevice = monitors[c.Target.Monitor].Device
	}
	a.log, a.logPath = logger, logger.Status().Path
	a.setPhase("starting", locale.Text("Подготовка приложения", "Preparing application"))
	a.record("startup", map[string]any{"mode": c.Mode, "config": *path, "capture_transfer": c.Capture.Transfer, "build": diagnostics.BuildInfo()})
	if _, e = os.Stat(*path); errors.Is(e, os.ErrNotExist) {
		if e = config.Save(*path, c); e != nil {
			return e
		}
	}
	if a.api, e = glfw.Open(a.directory); e != nil {
		return e
	}
	defer func() {
		if !a.abandonGPU {
			a.api.Close()
		}
	}()
	if a.window, e = a.api.NewOverlay(); e != nil {
		return e
	}
	defer func() {
		if !a.abandonGPU {
			a.window.Close()
		}
	}()
	a.pacingInterval = presentationInterval(c, 60)
	a.pacingCompatibility = compatibilityCapture(c)
	vsync := false
	if a.pacingCompatibility {
		a.api.DisableVSync()
	} else {
		vsync = a.api.ConfigureVSync()
	}
	a.pacer = pacing.New(a.pacingInterval, vsync)
	a.record("pacing", map[string]any{"timer_fallback": a.pacer.UsingTimer(), "compatibility_cap": a.pacingCompatibility})
	if e = win32.OverlayStyles(a.window.HWND); e != nil {
		return e
	}
	if e = win32.ExcludeCapture(a.window.HWND); e != nil {
		a.record("affinity_error", e.Error())
		a.captureError = fmt.Errorf(locale.Text("не удалось исключить оверлей из захвата: %w", "presentation capture exclusion failed: %w"), e)
	}
	if a.renderer, e = render.New(a.api.GLProc); e != nil {
		return e
	}
	defer func() {
		if !a.abandonGPU {
			a.shaderProgram.Close()
			a.previewShader.Close()
			a.renderer.Close()
		}
	}()
	if a.tray, e = win32.NewTray(controller == nil); e != nil {
		return e
	}
	defer a.tray.Close()
	defer a.closeSettings()
	if controller != nil {
		hwnd := a.tray.HWND
		controller.SetWake(func() { win32.WakeController(hwnd) })
	}
	if recoveryErr != nil {
		a.lastError = recoveryErr.Error()
		a.issue(locale.Text("Восстановление окна требует внимания", "Window recovery needs attention"), recoveryErr.Error())
	}
	if e = a.registerHotkeys(c); e != nil {
		a.suspended = true
		a.lastError = e.Error()
		a.setPhase("error", locale.Text("Горячие клавиши заняты; выберите другие в настройках", "Hotkeys are in use; choose different shortcuts in settings"))
		a.issue(locale.Text("Горячие клавиши не зарегистрированы", "Hotkeys could not be registered"), e.Error()+locale.Text(" Оверлей останется скрытым до успешной регистрации.", " The overlay will remain hidden until registration succeeds."))
	}
	if a.captureError == nil {
		a.library, a.captureError = capture.Load(a.directory)
	}
	a.probeGPUCapability()
	if a.shaderProgram, _, e = a.prepareLiveShader(a.cfg); e != nil {
		a.suspended = true
		a.lastError = e.Error()
		a.setPhase("error", locale.Text("Пользовательский шейдер недоступен; выберите другой эффект", "Custom shader is unavailable; choose another effect"))
		a.issue(locale.Text("Шейдер не загружен", "Shader not loaded"), e.Error())
	}
	// One cleanup boundary for exit, emergency and fatal frame failures. Stop
	// WGC before restoring a display/window geometry that it was capturing.
	defer func() {
		err = errors.Join(err, a.closeProjectedPointer())
		if e := a.stopAndRestore(); e != nil {
			err = errors.Join(err, e)
		}
		err = errors.Join(err, a.terminalError)
		if a.capture != nil {
			a.abandonGPU = true
		} else if a.library != nil && !a.abandonGPU {
			a.library.Close()
		}
		a.record("shutdown", nil)
	}()
	if c.Target.Kind == "window" {
		a.resolveTarget()
	}
	if c.Mode == "full" && (c.Target.Kind != "window" || a.library == nil) {
		a.suspended = true
		reason := locale.Text("Для Full выберите окно игры.", "Select a game window for Full.")
		if a.captureError != nil {
			reason += " " + a.captureError.Error()
		}
		if a.snapshotPath != "" {
			return fmt.Errorf(locale.Text("снимок Full недоступен: %s", "Full snapshot unavailable: %s"), reason)
		}
		a.lastError = reason
		a.setPhase("error", locale.Text("Full недоступен; выберите другой режим явно", "Full is unavailable; explicitly choose another mode"))
		a.issue(locale.Text("Full недоступен", "Full unavailable"), reason)
	}
	if c.Aspect.Enabled && c.Aspect.Method != "mask" {
		// Persist a preference, never replay display/window mutations on startup.
		a.cfg.Aspect.Enabled = false
		if c.Aspect.Method == "window" {
			a.issue(locale.Text("Формат 4:3", "4:3 format"), locale.Text("Изменение размера окна 4:3 убрано из меню. Выберите пропорции в настройках самой игры.", "4:3 window resizing was removed from the menu. Choose proportions in the game settings."))
		} else {
			a.issue(locale.Text("Формат 4:3", "4:3 format"), locale.Text("Изменение видеорежима применяется только вручную через меню трея.", "Display mode changes require a manual action in the tray menu."))
		}
	}
	a.tray.Notify(locale.Text("LumaTape готов", "LumaTape is ready"), locale.Text("Правый клик по значку: окно игры, пресет, 4:3. Фильтр: ", "Right-click the icon: game window, preset, 4:3. Filter: ")+c.Hotkeys.Toggle+locale.Text(". Аварийное отключение: ", ". Emergency disable: ")+c.Hotkeys.Emergency)
	if warning := logger.Status().Warning; warning != "" {
		a.issue(locale.Text("Диагностика", "Diagnostics"), warning)
	}
	if controller != nil {
		controller.Emit("ready", a.controlSnapshot())
	}
	if *frames == 0 && *snapshot == "" {
		a.openSettings()
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	for !a.quit && !a.window.ShouldClose() {
		win32.PumpSettingsMessages(a.settings)
		a.api.Poll()
		select {
		case <-signals:
			a.quit = true
		default:
		}
		// One-shot commands run FIFO, once, outside all native event callbacks.
		for _, event := range a.tray.Events() {
			switch event {
			case win32.EventSettings:
				a.openSettings()
			case win32.EventMenu:
				a.hide()
				a.command(a.tray.Menu(a.menu()))
			case win32.EventToggle:
				a.command(cmdToggle)
				if a.settings != nil {
					a.settings.Check(uiEnabled, a.cfg.Enabled)
				}
			case win32.EventEmergency:
				a.emergency()
			case win32.EventQuit:
				a.quit = true
			}
		}
		if err := a.recoverProjectedPointerAtBoundary(); err != nil {
			return fmt.Errorf(locale.Text("ошибка процесса проекции курсора: %w", "cursor projection worker failed: %w"), err)
		}
		a.observeForegroundSource()
		a.updateSettings()
		if a.control != nil {
			a.processControl()
		}
		if a.quit {
			break
		}
		if a.displaySession != nil && !a.displayTerminalSeen {
			if done, e := a.displaySession.Poll(); done {
				a.hide()
				if e == nil {
					a.displaySession = nil
				} else {
					a.displayTerminalSeen = true
				}
				a.confirmationDeadline = time.Time{}
				if a.cfg.Aspect.Method == "system" {
					a.cfg.Aspect.Enabled = false
				}
				if e != nil {
					a.lastError = e.Error()
					a.setPhase("recovery-error", locale.Text("Восстановление видеорежима не подтверждено", "Display mode recovery is unconfirmed"))
					a.issue(locale.Text("Ошибка восстановления", "Recovery error"), e.Error())
				} else {
					a.issue(locale.Text("Видеорежим", "Display mode"), locale.Text("Watchdog завершил контроль: выполнен откат или обнаружено внешнее изменение.", "The watchdog has finished: restoration completed or an external change was detected."))
				}
			}
		}
		if time.Since(a.lastTopology) > 500*time.Millisecond {
			a.refreshMonitors()
			a.lastTopology = time.Now()
		}
		if !a.displayTerminalSeen && !a.confirmationDeadline.IsZero() && time.Now().After(a.confirmationDeadline) {
			a.hide()
			if e := a.restoreDisplay(); e != nil {
				a.issue(locale.Text("Откат видеорежима", "Display mode rollback"), e.Error())
			}
			a.cfg.Aspect.Enabled = false
			a.issue(locale.Text("Видеорежим", "Display mode"), locale.Text("Подтверждение не получено за 15 секунд; watchdog запросил восстановление.", "No confirmation within 15 seconds; the watchdog requested restoration."))
		}
		if a.visible {
			if delay := a.pacer.Delay(time.Now()); delay > 0 {
				win32.WaitMessages(uint32((delay + time.Millisecond - 1) / time.Millisecond))
				continue // pump commands after any early message wakeup
			}
		}
		if e := a.frame(); e != nil {
			a.hide()
			a.lastError = e.Error()
			a.setPhase("error", locale.Text("Рендер остановлен; выбранные настройки сохранены", "Rendering stopped; selected settings are preserved"))
			a.issue(locale.Text("Рендер остановлен", "Rendering stopped"), e.Error())
			if cleanup := a.stopAndRestore(); cleanup != nil {
				a.lastError = errors.Join(e, cleanup).Error()
				a.setPhase("recovery-error", locale.Text("Рендер остановлен; восстановление не подтверждено", "Rendering stopped; recovery is unconfirmed"))
				if a.capture != nil {
					return errors.Join(e, cleanup)
				}
			}
			if a.snapshotPath != "" {
				return e // qualification must not silently succeed with no Full image
			}
			a.suspended = true
		}
		// A hide during frame processing may latch a cursor failure while a WGC
		// texture is acquired. The frame has now returned ownership, so emergency
		// cleanup may close capture and restore source geometry safely.
		if err := a.recoverProjectedPointerAtBoundary(); err != nil {
			return fmt.Errorf(locale.Text("ошибка процесса проекции курсора: %w", "cursor projection worker failed: %w"), err)
		}
		if !a.visible {
			win32.WaitMessages(50)
		}
		if a.maxFrames > 0 && a.frameCount >= a.maxFrames {
			break
		}
	}
	if a.snapshotPath != "" && !a.snapshotDone {
		return errors.New("requested Full snapshot was not created before shutdown; no fresh Full frame met the requested threshold")
	}
	if controller != nil && controller.Err() != nil {
		return controller.Err()
	}
	return nil
}

func (a *application) registerHotkeys(c config.Config) error {
	t, e := config.ParseHotkey(c.Hotkeys.Toggle)
	if e != nil {
		return e
	}
	x, e := config.ParseHotkey(c.Hotkeys.Emergency)
	if e != nil {
		return e
	}
	return a.tray.Hotkeys(t.Modifiers, t.Key, x.Modifiers, x.Key)
}
func (a *application) record(event string, v any) {
	if a.log == nil {
		return
	}
	level := diagnostics.Info
	if strings.Contains(event, "error") || strings.Contains(event, "rejected") {
		level = diagnostics.Error
	}
	a.log.Record(level, event, v)
}
func (a *application) issue(title, body string) {
	a.lastIssue = title + ": " + body
	a.record("notice", a.lastIssue)
	a.tray.Notify(title, body)
}
func (a *application) setPhase(phase, reason string) {
	if a.phase != phase || a.pauseReason != reason {
		a.record("state_changed", map[string]any{"phase": phase, "reason": reason, "requested_mode": a.cfg.Mode})
		if a.control != nil {
			a.control.Emit("state_changed", map[string]any{"phase": phase, "reason": reason})
		}
	}
	a.phase, a.pauseReason = phase, reason
}

func (a *application) RuntimeStatus() RuntimeStatus {
	s := RuntimeStatus{RequestedMode: a.cfg.Mode, RequestedTransfer: a.cfg.Capture.Transfer,
		Phase: a.phase, Reason: a.pauseReason, LastError: a.lastError, Preset: a.cfg.Preset,
		Enabled: a.cfg.Enabled, EffectiveIntensity: a.cfg.Effective().Intensity,
		GPU: a.gpuCapability, Compatibility: a.cpuCapability, FormatMethod: a.cfg.Aspect.Method,
		FormatRequested: a.cfg.Aspect.Enabled, RecoveryPending: a.windowRecoveryError != nil || a.displayTerminalSeen || (a.windowRestore != nil && a.windowRestore.transaction.recoveryOnly)}
	if a.cfg.Aspect.Enabled {
		switch a.cfg.Aspect.Method {
		case "mask":
			s.FormatActive = a.visible
		case "window":
			s.FormatActive = a.windowRestore != nil && a.windowRestore.transaction.active && !a.windowRestore.transaction.recoveryOnly
			if s.FormatActive {
				r := a.windowRestore.target
				client, err := win32.ClientBounds(r.Handle)
				s.FormatActive = err == nil && win32.SameProcess(r.Handle, r.PID) && client.W > 0 && client.H > 0 && client.W*3 == client.H*4
			}
		case "system":
			s.FormatActive = a.displaySession != nil && !a.displayTerminalSeen
		}
	}
	s.PointerProjection = "off"
	if a.pointerSession != nil && a.pointerEngaged {
		s.PointerProjection = "waiting-cursor"
		if a.pointerSession.Status().Active {
			s.PointerProjection = "projected"
		}
	}
	s.Unsaved = a.unsaved
	s.Metrics, s.MetricsUpdatedAt = a.metricSummary, a.metricSummaryAt
	s.LastFrameAt, s.CPUWorkMS = a.lastFrameAt, a.cpuWorkMS
	if !a.gpuObservedAt.IsZero() {
		gpu := a.renderer.GPUTimeMS
		s.GPUShaderMS, s.GPUObservedAt = &gpu, a.gpuObservedAt
	}
	if a.shaderProgram != nil {
		s.ShaderID, s.ShaderName = a.shaderProgram.ID, a.shaderProgram.Metadata.Name
	}
	if s.GPU.State == "" {
		s.GPU.State = "unknown"
	}
	if s.Compatibility.State == "" {
		s.Compatibility.State = "unknown"
	}
	if a.library == nil {
		reason := locale.Text("Capture DLL недоступна", "Capture DLL unavailable")
		if a.captureError != nil {
			reason = a.captureError.Error()
		}
		s.GPU, s.Compatibility = CapabilityStatus{State: "unavailable", Reason: reason, Code: "capture_bridge_unavailable"}, CapabilityStatus{State: "unavailable", Reason: reason, Code: "capture_bridge_unavailable"}
	} else if !a.library.SupportsCompatibility() {
		s.Compatibility = CapabilityStatus{State: "unavailable", Reason: locale.Text("В этом комплекте недоступен Full CPU. Обновите LumaTape.", "Full CPU is unavailable in this package. Update LumaTape."), Code: "compatibility_bridge_unavailable"}
	}
	if a.visible {
		s.Backend = "lightweight"
		if a.cfg.Mode == "full" && a.capture != nil {
			s.Backend = "full-" + transferName(a.capture.Transfer())
		}
	}
	s.observePresentation(a.visible && a.window != nil && win32.Visible(a.window.HWND), a.suspended)
	return s
}
func (a *application) save() error {
	if e := config.Save(a.configPath, a.cfg); e != nil {
		a.unsaved = true
		a.lastError = locale.Text("Настройки не сохранены: ", "Settings not saved: ") + e.Error()
		a.record("save_error", diagnostics.ErrorData("config_save", e))
		a.issue(locale.Text("Настройки не сохранены", "Settings not saved"), e.Error())
		return e
	} else {
		a.unsaved = false
	}
	return nil
}
func (a *application) hide() {
	if err := a.stopProjectedPointer(); err != nil {
		a.record("pointer_restore_error", err.Error())
		// Stop already closes/restores the cursor worker on failure. Do not run
		// emergency here: frame() may still own a captured texture that it must
		// release before capture can be closed or source geometry restored.
		a.pointerFailure = errors.Join(a.pointerFailure, err)
	}
	a.lastPresented = time.Time{}
	if a.visible {
		a.window.Hide()
		a.visible = false
		if a.pacer != nil {
			a.pacer.Reset(time.Now())
		}
	}
	if a.phase == "active" || a.phase == "bypass" {
		a.setPhase("waiting-frame", locale.Text("Ожидается показ нового кадра", "Waiting for a new frame to be shown"))
	}
}
func (a *application) closeCapture() error {
	if a.capture == nil {
		return nil
	}
	if e := a.capture.Close(); e != nil {
		a.failTerminal(e, false) // ownership failure: never render another GL frame
		return e
	}
	a.capture = nil
	a.transferMetricsValid = false
	a.transferStats = capture.Stats{}
	a.transferStatsSerial = 0
	a.transferStatsObservedAt = time.Time{}
	a.captureAcquireMS = 0
	a.captureStale = false
	return nil
}

// Preserve a fatal transition error until Run's final acknowledgement. A
// successful later cleanup does not make that forced shutdown a clean exit.
func (a *application) failTerminal(err error, abandonGPU bool) {
	if err == nil {
		return
	}
	a.quit = true
	a.abandonGPU = a.abandonGPU || abandonGPU
	a.terminalError = errors.Join(a.terminalError, err)
}
func (a *application) resolveTarget() {
	var found []win32.Window
	for _, w := range a.availableWindows() {
		if w.Title == a.cfg.Target.WindowTitle {
			found = append(found, w)
		}
	}
	if len(found) == 1 {
		a.target = found[0]
	} else {
		a.issue(locale.Text("Окно игры", "Game window"), locale.Text("Выберите окно в трее: сохранённый заголовок не найден или совпал с несколькими окнами.", "Select a window in the tray: the saved title was not found or matches multiple windows."))
	}
}
func (a *application) refreshMonitors() error {
	m, e := win32.Monitors()
	if e != nil {
		a.hide()
		a.suspended = true
		a.issue(locale.Text("Мониторы", "Monitors"), e.Error())
		return e
	}
	a.monitors = m
	if a.pacer != nil {
		if monitor, e := a.selectedMonitor(); e == nil {
			a.updatePacing(monitor)
		}
	}
	return nil
}
func (a *application) updatePacing(monitor win32.Monitor) {
	refreshHz := uint32(60)
	if mode, e := display.CurrentMode(monitor.Device); e == nil {
		refreshHz = mode.RefreshHz
	}
	interval := presentationInterval(a.resolvedConfig(), refreshHz)
	compatibility := compatibilityCapture(a.resolvedConfig())
	if compatibility != a.pacingCompatibility {
		vsync := false
		if compatibility {
			a.api.DisableVSync()
		} else {
			vsync = a.api.ConfigureVSync()
		}
		a.pacer = pacing.New(interval, vsync)
		a.pacingCompatibility = compatibility
		a.record("pacing", map[string]any{"timer_fallback": a.pacer.UsingTimer(), "compatibility_cap": compatibility})
	} else if interval != a.pacingInterval || monitor.Device != a.pacingDevice {
		a.pacer.SetInterval(interval)
	}
	a.pacingInterval, a.pacingDevice = interval, monitor.Device
}
func (a *application) selectedMonitor() (win32.Monitor, error) {
	if a.cfg.Target.Kind == "window" && a.target.Handle != 0 {
		h := win32.MonitorFromWindow(a.target.Handle)
		for _, m := range a.monitors {
			if m.Handle == h {
				return m, nil
			}
		}
	}
	for _, m := range a.monitors {
		if m.Device == a.monitorDevice {
			return m, nil
		}
	}
	return win32.Monitor{}, errors.New(locale.Text("выбранный монитор отключён; выберите монитор в трее", "selected monitor was disconnected; choose a monitor in the tray"))
}

func (a *application) frame() error {
	if a.projectedPointerFailed() {
		return nil
	}
	if a.suspended || (!a.cfg.Enabled && !a.cfg.Aspect.Enabled) {
		a.hide()
		if !a.cfg.Enabled {
			a.setPhase("disabled", locale.Text("Эффект выключен", "Effect disabled"))
		}
		return a.closeCapture()
	}
	// A missing source takes precedence over tray focus: there is no game
	// to return to. Close any stale capture before presenting a pause status.
	if a.cfg.Target.Kind == "window" {
		if a.target.Handle == 0 {
			a.hide()
			a.setPhase("waiting-source", locale.Text("Выберите открытое окно источника", "Select an open source window"))
			return a.closeCapture()
		}
		if !win32.SameProcess(a.target.Handle, a.target.PID) {
			a.hide()
			if e := a.closeCapture(); e != nil {
				return e
			}
			a.target = win32.Window{}
			a.setPhase("waiting-source", locale.Text("Источник закрыт; выберите новое окно", "Source closed; select a new window"))
			a.issue(locale.Text("Источник закрыт", "Source closed"), locale.Text("Выберите новое окно в трее.", "Select a new window in the tray."))
			return nil
		}
	}
	if a.controllerPID != 0 && win32.SameProcess(win32.Foreground(), a.controllerPID) {
		a.hide()
		a.setPhase("paused-settings", locale.Text("Открыты настройки LumaTape; вернитесь в игру, чтобы увидеть эффект", "LumaTape settings are open; return to the game to see the effect"))
		return a.closeCapture()
	}
	if a.settings != nil && a.settings.Visible() && win32.Foreground() == a.settings.HWND {
		a.hide()
		a.setPhase("paused-settings", locale.Text("Открыты настройки LumaTape; вернитесь в игру, чтобы увидеть эффект", "LumaTape settings are open; return to the game to see the effect"))
		return a.closeCapture()
	}
	m, e := a.selectedMonitor()
	if e != nil {
		return e
	}
	if m.Device != a.pacingDevice || compatibilityCapture(a.resolvedConfig()) != a.pacingCompatibility {
		a.updatePacing(m)
	}
	source := m.Bounds
	if a.cfg.Target.Kind == "window" {
		if !win32.Visible(a.target.Handle) || win32.Foreground() != a.target.Handle {
			a.hide()
			a.setPhase("paused-focus", locale.Text("Вернитесь в окно игры, чтобы увидеть эффект", "Return to the game window to see the effect"))
			a.sourceMotion.Reset()
			return a.closeCapture()
		}
		source, e = win32.ClientBounds(a.target.Handle)
		if e != nil {
			return e
		}
		if source.W <= 0 || source.H <= 0 {
			a.hide()
			a.setPhase("waiting-frame", locale.Text("Окно источника не имеет видимой области", "The source window has no visible area"))
			return nil
		}
		if a.cfg.Mode == "full" {
			moving, err := win32.InMoveSize(a.target.Handle)
			if err != nil {
				return err
			}
			if !a.sourceMotion.Observe(a.target.Handle, source, moving, time.Now()) {
				return a.pauseForSourceMotion()
			}
		}
	}
	full := a.cfg.Mode == "full"
	if full && a.cfg.Effective().Intensity > 0 && a.cfg.Shader.ID != "" && (a.shaderProgram == nil || a.shaderProgram.ID != a.cfg.Shader.ID) {
		return fmt.Errorf(locale.Text("пользовательский шейдер не загружен", "custom shader is not loaded"))
	}
	var captured capture.Frame
	begin := time.Now()
	if full {
		if a.library == nil || a.cfg.Target.Kind != "window" {
			return errors.New(locale.Text("Full требует доступную capture DLL и выбранное окно игры", "Full requires an available capture DLL and a selected game window"))
		}
		if a.capture == nil {
			a.hide()
			if a.pointerFailure != nil {
				return nil
			}
			a.capture, e = a.openCandidate(a.cfg, a.target)
			if e != nil {
				return fmt.Errorf(locale.Text("выбранный Full backend недоступен: %w", "the selected Full backend is unavailable: %w"), e)
			}
			a.resolvedTransfer = transferName(a.capture.Transfer())
			a.updatePacing(m)
			a.record("capture_opened", map[string]any{"requested_transfer": a.cfg.Capture.Transfer, "transfer": a.resolvedTransfer})
			if compatibilityCapture(a.resolvedConfig()) && !a.compatibilityNotified {
				a.compatibilityNotified = true
				a.issue(locale.Text("Full · совместимость", "Full · compatibility"), locale.Text("Выбран перенос кадров через CPU, до 30 кадров/с. В статусе и журнале отдельно измеряются перенос и возраст кадра.", "CPU frame transfer selected, up to 30 FPS. Transfer time and frame age are measured separately in status and logs."))
			}
		}
		var ready bool
		acquireStarted := time.Now()
		captured, ready, e = a.capture.Acquire()
		acquireElapsed := time.Since(acquireStarted)
		a.captureAcquireMS = float64(acquireElapsed.Nanoseconds()) / 1e6
		// Snapshot completed-transfer stats before the stale branch can reject
		// this image. Otherwise the most useful slow transfer disappears from logs.
		if e == nil && ready && captured.Updated != 0 {
			stats, err := a.capture.Stats()
			if err != nil {
				a.recordAcquireDiagnostics(captured, ready, acquireElapsed, err)
				return errors.Join(err, a.capture.Release())
			}
			a.transferStats, a.transferMetricsValid = stats, true
			a.transferStatsSerial, a.transferStatsObservedAt = captured.Serial, time.Now()
		}
		a.recordAcquireDiagnostics(captured, ready, acquireElapsed, e)
		if e != nil {
			return e
		}
		if !ready {
			a.hide()
			a.setPhase("waiting-frame", locale.Text("Ожидается новый кадр Windows Graphics Capture", "Waiting for a new Windows Graphics Capture frame"))
			return nil
		}
		capturedBounds := geometry.Rect{X: int(captured.X), Y: int(captured.Y), W: int(captured.Width), H: int(captured.Height)}
		aligned, err := a.fullSourceAligned(source, capturedBounds)
		if err != nil {
			return errors.Join(err, a.capture.Release())
		}
		if !aligned {
			// Acquire transferred texture ownership even when its coordinates
			// became obsolete during a CPU readback or GPU interop wait.
			if err := a.capture.Release(); err != nil {
				return err
			}
			return a.pauseForSourceMotion()
		}
		// Do not leave an opaque frozen picture above an interactive game if
		// WGC stalls. Static sources may also stop producing frames; showing the
		// original until a fresh frame is safer than concealing a changed source.
		if captured.Age100ns > 5000000 {
			a.hide()
			a.setPhase("paused-stale", locale.Text("Захват не обновляется; показано исходное окно", "Capture is not updating; showing the original window"))
			if !a.captureStale {
				a.captureStale = true
				data := a.captureObservation(captured, true)
				data["age_ms"] = float64(captured.Age100ns) / 1e4
				a.record("stale_capture_hidden", data)
			}
			return a.capture.Release()
		}
		a.captureStale = false
		// Keep the independently observed source bounds. The captured bounds
		// have now been checked, never used to move the overlay backwards.
		a.ageMS = float64(captured.Age100ns) / 1e4
	}
	release := func() error {
		if full {
			return a.capture.Release()
		}
		return nil
	}
	p, e := calculatePresentation(a.cfg, source, m.Bounds)
	if e != nil {
		return errors.Join(e, release())
	}
	if p.Bounds != a.bounds {
		a.hide()
		if a.pointerFailure != nil {
			return release()
		}
		if e = win32.Position(a.window.HWND, p.Bounds); e != nil {
			return errors.Join(e, release())
		}
		a.bounds = p.Bounds
	}
	w, h := a.window.Framebuffer()
	if w != p.Bounds.W || h != p.Bounds.H {
		a.hide()
		a.setPhase("waiting-frame", locale.Text("Ожидается новый размер поверхности", "Waiting for the presentation surface to resize"))
		return release()
	} // wait for DPI/resize acknowledgement
	if a.metrics == nil || a.metricSize != (geometry.Size{W: w, H: h}) {
		a.resetFrameMetrics()
		a.metricSize = geometry.Size{W: w, H: h}
	}
	if a.projectedPointerFailed() {
		return release()
	}
	submitBegin := time.Now()
	frameTime := time.Since(a.start).Seconds()
	a.renderer.Draw(render.Frame{Width: w, Height: h, Area: p.Area, SourceUV: p.SourceUV, Texture: captured.Texture, SourceSize: source.Size(), Time: frameTime, Effects: a.cfg.Effective(), Screen: a.cfg.EffectiveScreen(), Shader: a.shaderProgram, ShaderParams: a.cfg.Shader.Params}, full)
	if e = release(); e != nil {
		return e
	}
	a.cpuMS = float64(time.Since(submitBegin).Nanoseconds()) / 1e6
	if e = a.renderer.Error(); e != nil {
		return e
	}
	if full {
		aligned, err := a.fullSourceAligned(source, source)
		if err != nil {
			return err
		}
		if !aligned {
			return a.pauseForSourceMotion()
		}
	}
	a.cpuWorkMS = float64(time.Since(begin).Nanoseconds()) / 1e6
	wroteSnapshot := false
	if a.snapshotPath != "" && !a.snapshotDone && full && captured.Updated != 0 && a.frameCount+1 >= a.snapshotAfter {
		if e = a.renderer.WriteSnapshot(a.snapshotPath); e != nil {
			return fmt.Errorf(locale.Text("запись снимка Full: %w", "write Full snapshot: %w"), e)
		}
		a.snapshotDone = true
		wroteSnapshot = true
		a.record("snapshot", map[string]any{"path": a.snapshotPath, "mode": "full", "capture_transfer": a.resolvedConfig().Capture.Transfer, "requested_transfer": a.cfg.Capture.Transfer, "shader_id": a.cfg.Shader.ID, "source_serial": captured.Serial, "width": w, "height": h, "preset": a.cfg.Preset, "intensity": a.cfg.Effects.Intensity, "frame": a.frameCount + 1, "capture_age_ms": a.ageMS})
		// Explicit PNG encoding can take much longer than an ordinary frame.
		// The source may have moved while that diagnostic was being written.
		aligned, err := a.fullSourceAligned(source, source)
		if err != nil {
			return err
		}
		if !aligned {
			return a.pauseForSourceMotion()
		}
	}
	// The first complete valid buffer is prepared before the surface is shown.
	if e = a.window.Swap(); e != nil {
		return e
	}
	presentedAt := time.Now()
	a.observeFrameMetrics(presentedAt, captured, full, wroteSnapshot)
	if a.pacer.Presented(presentedAt) {
		a.api.DisableVSync()
		a.record("pacing_fallback", "VSync did not pace eight consecutive frame intervals; using message-aware deadlines with swap interval 0")
	}
	// Publish only the successfully swapped image transform. The independent
	// worker handles the cursor at its own cadence and restores it on lease loss.
	if a.projectedPointerFailed() {
		return nil
	}
	if e = a.updateProjectedPointer(p, source.Size(), frameTime); e != nil {
		return e
	}
	if a.projectedPointerFailed() {
		return nil
	}
	if !a.visible || !win32.Visible(a.window.HWND) {
		a.window.Show()
		a.visible = true
	}
	if full && a.sourceMotionPaused {
		a.sourceMotionPaused = false
		a.record("source_alignment_resumed", map[string]any{"bounds": source, "source_serial": captured.Serial})
	}
	if a.cfg.Effective().Intensity == 0 {
		a.setPhase("bypass", locale.Text("Исходное изображение: эффект выключен или интенсивность 0%", "Original image: effect disabled or intensity at 0%"))
	} else if full {
		a.setPhase("active", locale.Text("Full обрабатывает изображение источника", "Full is processing the source image"))
	} else {
		a.setPhase("active", locale.Text("Lightweight: scanlines, vignette и шум; обработка цветов источника недоступна", "Lightweight: scanlines, vignette and noise; source color processing is unavailable"))
	}
	a.frameCount++
	if time.Since(a.lastMetrics) >= time.Second {
		var age, gpu, cpuTransfer, readbackWait, uploadCall, transferBytes any
		if full {
			age = a.ageMS
		}
		if a.renderer.GPUTimeMS >= 0 {
			gpu = a.renderer.GPUTimeMS
		}
		if compatibilityCapture(a.resolvedConfig()) && a.transferMetricsValid {
			cpuTransfer = float64(a.transferStats.CPUTransfer100ns) / 1e4
			readbackWait = float64(a.transferStats.ReadbackWait100ns) / 1e4
			uploadCall = float64(a.transferStats.UploadCall100ns) / 1e4
			transferBytes = a.transferStats.BytesPerFrame
		}
		data := map[string]any{"cpu_submit_ms": a.cpuMS, "frame_cpu_work_ms": a.cpuWorkMS, "gpu_shader_ms": gpu, "gpu_observed_at": a.gpuObservedAt, "capture_age_ms": age, "mode": a.cfg.Mode, "capture_transfer": a.resolvedConfig().Capture.Transfer, "requested_transfer": a.cfg.Capture.Transfer, "cpu_transfer_ms": cpuTransfer, "readback_wait_ms": readbackWait, "upload_call_ms": uploadCall, "transfer_bytes": transferBytes, "dropped": captured.Dropped, "source_serial": captured.Serial, "source_updated": captured.Updated != 0, "output_w": w, "output_h": h}
		if full {
			for key, value := range a.captureObservation(captured, true) {
				data[key] = value
			}
		}
		a.record("frame_metrics", data)
		a.lastMetrics = time.Now()
	}
	return nil
}

// Recheck after slow capture/upload and again after drawing. A frame can only
// be presented over the same client rectangle that was observed before acquire.
func (a *application) fullSourceAligned(before, captured geometry.Rect) (bool, error) {
	if !win32.Visible(a.target.Handle) || win32.Foreground() != a.target.Handle {
		a.sourceMotion.Reset()
		return false, nil
	}
	moving, err := win32.InMoveSize(a.target.Handle)
	if err != nil {
		return false, err
	}
	current, err := win32.ClientBounds(a.target.Handle)
	if err != nil {
		return false, err
	}
	stable := a.sourceMotion.Observe(a.target.Handle, current, moving, time.Now())
	return stable && sourceGeometryMatches(before, captured, current), nil
}

func (a *application) pauseForSourceMotion() error {
	a.hide()
	a.setPhase("paused-moving", locale.Text("Источник перемещается или меняет размер; показано исходное окно", "Source is moving or resizing; showing the original window"))
	if !a.sourceMotionPaused {
		a.sourceMotionPaused = true
		a.record("source_alignment_paused", "Source is moving, resizing, unfocused, or has not settled; showing the original window until a fresh aligned capture is available")
	}
	// Reopening discards both the old capture image and any pending CPU copy.
	return a.closeCapture()
}

func (a *application) restoreDisplay() error {
	if a.displaySession == nil {
		return nil
	}
	if err := a.displaySession.Restore(); err != nil {
		a.displayTerminalSeen = true
		return err
	}
	a.displaySession = nil
	a.displayTerminalSeen = false
	a.confirmationDeadline = time.Time{}
	return nil
}
func (a *application) restoreWindow() error {
	if a.windowRestore == nil {
		// A failed startup recovery has no live transaction. Retry its journal
		// through the same identity checks so Off can finish recovery as well.
		if a.windowRecoveryError != nil {
			a.windowRecoveryError = recoverInterruptedWindow(a.windowRecoveryPath)
			return a.windowRecoveryError
		}
		return nil
	}
	r := a.windowRestore
	if !win32.SameProcess(r.target.Handle, r.target.PID) {
		if err := r.transaction.finish(); err != nil {
			return err
		}
		a.windowRestore = nil
		return nil
	}
	if err := r.transaction.Restore(); err != nil {
		r.transaction.recoveryOnly = true
		return err
	}
	a.windowRestore = nil
	return nil
}
func (a *application) restore() error {
	a.hide()
	err := errors.Join(a.restoreDisplay(), a.restoreWindow())
	a.cfg.Aspect.Enabled = false
	if err != nil {
		a.lastError = locale.Text("Восстановление не подтверждено: ", "Recovery is unconfirmed: ") + err.Error()
		a.setPhase("recovery-error", a.lastError)
	}
	return err
}

func (a *application) stopAndRestore() error {
	a.hide()
	captureErr := a.closeCapture()
	return errors.Join(captureErr, a.restore())
}
func (a *application) emergency() error {
	a.emergencyBarrier.advance() // cancel stale UI intent even if restore/save fails
	if a.control != nil {
		// Hotkeys, the native menu and IPC share this boundary. Otherwise a
		// queued legacy toggle/reload could undo a keyboard emergency immediately.
		a.control.CancelQueued()
		a.control.Emit("state_changed", map[string]any{"emergency_sequence": a.emergencyBarrier.sequence})
	}
	a.hide()
	a.suspended = true
	a.cfg.Enabled = false
	a.cfg.Aspect.Enabled = false
	err := a.stopAndRestore()
	a.setPhase("disabled", locale.Text("Аварийное отключение", "Emergency disable"))
	if err != nil {
		a.lastError = err.Error()
		a.setPhase("recovery-error", locale.Text("Оверлей скрыт; восстановление требует внимания", "Overlay hidden; recovery needs attention"))
		a.issue(locale.Text("LumaTape скрыт — есть ошибка", "LumaTape hidden — error"), err.Error())
	} else {
		a.issue(locale.Text("LumaTape отключён", "LumaTape disabled"), locale.Text("Оверлей скрыт; собственные изменения восстановлены либо оставлены без вмешательства после внешней смены настроек.", "Overlay hidden; owned changes were restored or left untouched following an external settings change."))
	}
	saveErr := a.save()
	if a.settings != nil {
		a.populateSettings()
	}
	return errors.Join(err, saveErr)
}

func (a *application) applyWindow43() error {
	if a.windowRecoveryError != nil {
		a.windowRecoveryError = recoverInterruptedWindow(a.windowRecoveryPath)
		if a.windowRecoveryError != nil {
			return a.windowRecoveryError
		}
	}
	if a.target.Handle == 0 || a.cfg.Target.Kind != "window" || !win32.SameProcess(a.target.Handle, a.target.PID) {
		return errors.New(locale.Text("сначала выберите существующее окно игры", "select an existing game window first"))
	}
	m, err := a.selectedMonitor()
	if err != nil {
		return err
	}
	left, top, right, bottom, err := win32.ClientInsets(a.target.Handle)
	if err != nil {
		return err
	}
	area, err := fitClient43(m.Work, clientInsets{left, top, right, bottom})
	if err != nil {
		return err
	}
	if err = a.restore(); err != nil {
		return err
	}
	if err = a.closeCapture(); err != nil {
		return err
	}
	tx, err := beginWindowTransaction(&nativeWindowMutation{target: a.target, recoveryPath: a.windowRecoveryPath}, area)
	if tx != nil {
		a.windowRestore = &windowChange{target: a.target, transaction: tx}
	}
	if err != nil {
		a.cfg.Aspect.Enabled = false
		return err
	}
	a.sourceMotion.Reset()
	a.cfg.Aspect.Enabled = true
	a.cfg.Aspect.Method = "window"
	a.issue(locale.Text("Клиентская область 4:3", "4:3 client area"), locale.Text("Окно размещено в рабочей области вместе с заголовком. В игре выберите разрешение 4:3: изменение окна не гарантирует правильный FOV.", "The window and title bar fit within the work area. Select a 4:3 resolution in the game: window resizing does not guarantee correct FOV."))
	return nil
}
func (a *application) applySystem43() error {
	m, err := a.selectedMonitor()
	if err != nil {
		return err
	}
	modes, err := display.ListModes(m.Device)
	if err != nil {
		return err
	}
	current, err := display.CurrentMode(m.Device)
	if err != nil {
		return err
	}
	chosen, err := display.Choose43(modes, current)
	if err != nil {
		return err
	}
	if err = a.restore(); err != nil {
		return err
	}
	if err = a.closeCapture(); err != nil {
		return err
	}
	a.displaySession, err = display.StartSession("", m.Device, chosen, 15*time.Second)
	if err != nil {
		return err
	}
	a.displayTerminalSeen = false
	a.confirmationDeadline = a.displaySession.Deadline()
	a.cfg.Aspect.Enabled = true
	a.cfg.Aspect.Method = "system"
	a.sourceMotion.Reset()
	if err = a.refreshMonitors(); err != nil {
		return errors.Join(err, a.restoreDisplay())
	}
	a.issue(locale.Text("Подтвердите видеорежим", "Confirm display mode"), fmt.Sprintf(locale.Text("%d×%d @ %d Hz. В течение 15 секунд выберите «Подтвердить». Проверьте круг: GPU/монитор могут растянуть 4:3.", "%d×%d @ %d Hz. Choose “Confirm” within 15 seconds. Check the circle: the GPU/monitor may stretch 4:3."), chosen.Width, chosen.Height, chosen.RefreshHz))
	return nil
}

func truncate(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "&", "&&"))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}
