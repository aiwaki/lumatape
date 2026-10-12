package app

import (
	"fmt"
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/locale"
	"time"
)

// transitionPlan distinguishes preferences from owned OS mutations. In
// particular, changing a preset or backend must not resize a window again.
type transitionPlan struct {
	Source, Capture, Hotkeys, RestoreFormat, ApplyWindow, ApplySystem bool
}

func planTransition(current, next config.Config, explicitGeometry bool) (transitionPlan, error) {
	if err := next.Validate(); err != nil {
		return transitionPlan{}, err
	}
	p := transitionPlan{}
	p.Source = current.Target != next.Target
	p.Capture = p.Source || current.Mode != next.Mode || current.Capture != next.Capture
	p.Hotkeys = current.Hotkeys != next.Hotkeys
	p.RestoreFormat = p.Source || current.Aspect.Enabled != next.Aspect.Enabled || (next.Aspect.Enabled && current.Aspect.Method != next.Aspect.Method)
	if explicitGeometry && next.Aspect.Enabled && p.RestoreFormat {
		p.ApplyWindow = next.Aspect.Method == "window"
		p.ApplySystem = next.Aspect.Method == "system"
	}
	return p, nil
}

// A source caption is lookup metadata, not window identity. The caller may set
// sameWindow only after resolving and checking the live HWND/PID. Replanning
// here keeps real source changes while a game's FPS/level title update cannot
// restart capture or replay an owned window/display format.
func planResolvedSourceTransition(current, next config.Config, explicitGeometry, sameWindow bool) (transitionPlan, error) {
	if sameWindow && current.Target.Kind == "window" && next.Target.Kind == "window" {
		current.Target.WindowTitle = next.Target.WindowTitle
	}
	return planTransition(current, next, explicitGeometry)
}

// explicitWindowReapply is a narrow opt-in for the window-format button. A
// still-applied format is idempotent; an externally changed window is resized
// only after another explicit request, never on a preset change or render tick.
func explicitWindowReapply(p transitionPlan, next config.Config, active bool) (transitionPlan, error) {
	if next.Target.Kind != "window" || !next.Aspect.Enabled || next.Aspect.Method != "window" {
		return transitionPlan{}, fmt.Errorf(locale.Text("повторное изменение окна требует выбранного окна и включённого оконного формата 4:3", "retrying a window resize requires a selected window and enabled window 4:3 format"))
	}
	if !active {
		p.RestoreFormat, p.ApplyWindow = true, true
	}
	return p, nil
}

// SourceClientBounds is an observed client rectangle, never a requested size.
type SourceClientBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type CapabilityStatus struct {
	State  string `json:"state"` // unknown, available, failed, unavailable
	Reason string `json:"reason"`
	Code   string `json:"code,omitempty"`
}

// RuntimeStatus describes what is actually presented, separately from saved
// user intent. It deliberately contains no native resource ownership.
type RuntimeStatus struct {
	CaptureRecovery    CaptureRecoveryStatus `json:"capture_recovery"`
	PointerProjection  string                `json:"pointer_projection"`
	RequestedMode      string                `json:"requested_mode"`
	RequestedTransfer  string                `json:"requested_transfer"`
	Backend            string                `json:"backend"`
	Phase              string                `json:"phase"`
	Reason             string                `json:"reason"`
	LastError          string                `json:"last_error"`
	ShaderID           string                `json:"shader_id"`
	ShaderName         string                `json:"shader_name"`
	Preset             string                `json:"preset"`
	Enabled            bool                  `json:"enabled"` // requested preference, retained for controller compatibility
	EffectActive       bool                  `json:"effect_active"`
	SurfaceVisible     bool                  `json:"surface_visible"`
	EffectiveIntensity float64               `json:"effective_intensity"`
	GPU                CapabilityStatus      `json:"gpu"`
	Compatibility      CapabilityStatus      `json:"compatibility"`
	FormatMethod       string                `json:"format_method"`
	FormatRequested    bool                  `json:"format_requested"`
	FormatActive       bool                  `json:"format_active"`
	FormatReason       string                `json:"format_reason,omitempty"`
	SourceClient       *SourceClientBounds   `json:"source_client,omitempty"`
	RecoveryPending    bool                  `json:"recovery_pending"`
	Unsaved            bool                  `json:"unsaved"`
	Metrics            FrameMetricSummary    `json:"metrics"`
	FrameRates         FrameRateSummary      `json:"frame_rates"`
	MetricsUpdatedAt   time.Time             `json:"metrics_updated_at"`
	LastFrameAt        time.Time             `json:"last_frame_at"`
	CPUWorkMS          float64               `json:"cpu_work_ms"`
	GPUShaderMS        *float64              `json:"gpu_shader_ms"`
	GPUObservedAt      time.Time             `json:"gpu_observed_at"`
}

// observePresentation combines the render-thread state with a fresh native
// visibility readback. The cursor worker may hide the HWND independently after
// focus/geometry/lease loss, so a cached active phase alone is not proof of output.
func (s *RuntimeStatus) observePresentation(surfaceVisible, suspended bool) {
	s.SurfaceVisible = surfaceVisible
	if !surfaceVisible {
		s.Backend = ""
		if s.FormatMethod == "mask" {
			s.FormatActive = false
		}
		if s.Phase == "active" || s.Phase == "bypass" {
			s.Phase = "waiting-frame"
			s.Reason = locale.Text("Ожидается показ нового кадра", "Waiting for a new frame to be shown")
		}
	}
	s.EffectActive = surfaceVisible && !suspended && s.Enabled && s.EffectiveIntensity > 0 && s.Phase == "active" && s.Backend != ""
}

type AppliedSettingsError struct{ Err error }

func (e *AppliedSettingsError) Error() string {
	return locale.Text("настройки применены, но не сохранены: ", "settings applied but not saved: ") + e.Err.Error()
}
func (e *AppliedSettingsError) Unwrap() error { return e.Err }
