package app

import "testing"

func TestRequestedEffectDoesNotClaimPausedOrFailedPresentation(t *testing.T) {
	for _, phase := range []string{"starting", "ready", "disabled", "bypass", "paused-settings", "paused-stale", "waiting-source", "paused-focus", "paused-moving", "waiting-frame", "source-closed", "error", "recovery-error"} {
		t.Run(phase, func(t *testing.T) {
			s := RuntimeStatus{Enabled: true, EffectiveIntensity: 1, Phase: phase, Backend: "full-compatibility"}
			s.observePresentation(true, false)
			if s.EffectActive || !s.Enabled {
				t.Fatalf("pause/failure changed intent or claimed an active effect: %+v", s)
			}
		})
	}
}

func TestIndependentlyHiddenSurfaceInvalidatesCachedPresentation(t *testing.T) {
	for _, phase := range []string{"active", "bypass"} {
		s := RuntimeStatus{Enabled: true, EffectiveIntensity: 1, Phase: phase, Backend: "full-gpu", FormatMethod: "mask", FormatActive: true}
		s.observePresentation(false, false)
		if !s.Enabled || s.EffectActive || s.SurfaceVisible || s.Backend != "" || s.FormatActive || s.Phase != "waiting-frame" || s.Reason == "" {
			t.Fatalf("native hide retained cached output or erased user intent: %+v", s)
		}
	}
	// A real window/display resize remains applied while presentation is hidden.
	s := RuntimeStatus{Phase: "error", Reason: "capture failed", FormatMethod: "window", FormatActive: true}
	s.observePresentation(false, true)
	if !s.FormatActive || s.Phase != "error" || s.Reason != "capture failed" {
		t.Fatalf("visibility readback erased independent geometry/error: %+v", s)
	}
}

func TestOnlyPresentedEnabledEffectIsActive(t *testing.T) {
	for _, backend := range []string{"lightweight", "full-gpu", "full-compatibility"} {
		s := RuntimeStatus{Enabled: true, EffectiveIntensity: 1, Phase: "active", Backend: backend}
		s.observePresentation(true, false)
		if !s.EffectActive || !s.SurfaceVisible || s.Phase != "active" {
			t.Fatalf("working presentation not reported: %+v", s)
		}
		for _, change := range []func(*RuntimeStatus){
			func(s *RuntimeStatus) { s.Enabled = false },
			func(s *RuntimeStatus) { s.EffectiveIntensity = 0 },
			func(s *RuntimeStatus) { s.Backend = "" },
		} {
			paused := s
			change(&paused)
			paused.observePresentation(true, false)
			if paused.EffectActive {
				t.Fatalf("disabled/bypass/unresolved presentation reported active: %+v", paused)
			}
		}
		s.observePresentation(true, true)
		if s.EffectActive {
			t.Fatal("suspended renderer retained active effect")
		}
	}
}
