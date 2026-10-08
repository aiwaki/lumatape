//go:build windows

package app

import (
	"errors"
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/capture"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func TestKnownUnavailableGPURejectsBeforeSourceKeysCaptureOrGeometry(t *testing.T) {
	current := config.Default()
	current.Mode, current.Capture.Transfer = "full", config.TransferCompatibility
	current.Target = config.Target{Kind: "window", WindowTitle: "owned game"}
	current.Aspect.Enabled, current.Aspect.Method = true, "window"
	owned := &capture.Capture{}
	a := &application{cfg: current, capture: owned, target: win32.Window{Handle: 123, PID: 456},
		gpuCapability: CapabilityStatus{State: "unavailable", Code: "gpu_interop_unavailable"}}
	// No tray/API/library: touching source, hotkeys, capture or geometry before
	// this global preflight would enter Win32 with invalid fixture ownership.
	next := current
	next.Capture.Transfer = config.TransferGPU
	err := a.ApplyDraft(next)
	var backend *BackendUnavailableError
	if !errors.As(err, &backend) || backend.Code != "gpu_interop_unavailable" {
		t.Fatalf("expected typed preflight rejection: %v", err)
	}
	if a.cfg != current || a.capture != owned || a.target.Handle != 123 || a.target.PID != 456 || a.quit || a.unsaved {
		t.Fatal("unsupported GPU request changed the working config, capture, source or lifecycle")
	}
	for _, safe := range []config.Config{current, func() config.Config { c := current; c.Enabled = false; return c }()} {
		if err := a.capturePreflight(safe); err != nil {
			t.Fatalf("GPU probe blocked explicit CPU/disable: %v", err)
		}
	}
}

func TestGPUProbeAbsenceAndNativeOpenFailureRemainDistinct(t *testing.T) {
	a := &application{}
	a.setCaptureCapability(config.TransferGPU, gpuUnavailable(capture.ErrGPUInteropUnavailable, true))
	if a.gpuCapability.State != "unavailable" || a.cpuCapability.State != "" {
		t.Fatal("confirmed GPU prerequisite absence affected CPU capability")
	}
	// ABI v1's old diagnostic also covers a NULL extension query; its string
	// alone must not turn a failed live Open into a proven missing extension.
	a.setCaptureCapability(config.TransferGPU, gpuUnavailable(errors.New("legacy native prerequisite failure"), true))
	if a.gpuCapability.State != "failed" || a.gpuCapability.Code != "gpu_interop_unavailable" {
		t.Fatalf("ambiguous native failure became confirmed extension absence: %+v", a.gpuCapability)
	}
	a.setCaptureCapability(config.TransferGPU, nil)
	if a.gpuCapability.State != "available" || a.gpuCapability.Code != "" {
		t.Fatal("successful live open did not establish availability")
	}
}

func TestIdempotentDisableDoesNotRetrySuspendedUnavailableCapture(t *testing.T) {
	c := config.Default()
	c.Enabled, c.Mode, c.Capture.Transfer = false, "full", config.TransferGPU
	c.Target = config.Target{Kind: "window", WindowTitle: "closed game"}
	c.Aspect.Enabled, c.Aspect.Method = true, "window"
	a := &application{cfg: c, suspended: true, unsaved: true,
		gpuCapability: CapabilityStatus{State: "unavailable", Code: "gpu_interop_unavailable"}}
	for range 2 {
		if err := a.disableFilter(); err != nil {
			t.Fatalf("off retried unavailable capture: %v", err)
		}
		if a.cfg != c || !a.unsaved || a.quit || a.emergencyBarrier.sequence != 0 {
			t.Fatal("repeated off mutated independent format/preferences/recovery intent")
		}
	}
}

func TestUnavailableGPUActivationFromSavedProfileRejectsBeforeCommit(t *testing.T) {
	for _, activate := range []string{"filter", "window-format", "format-method"} {
		t.Run(activate, func(t *testing.T) {
			c := config.Default()
			c.Enabled, c.Mode, c.Capture.Transfer = false, "full", config.TransferGPU
			c.Target = config.Target{Kind: "window", WindowTitle: "saved game"}
			if activate == "format-method" {
				c.Aspect.Enabled = true
			}
			next := c
			if activate == "filter" {
				next.Enabled = true
			} else {
				next.Aspect.Enabled, next.Aspect.Method = true, "window"
			}
			plan, err := planTransition(c, next, true)
			if err != nil || plan.Capture {
				t.Fatalf("fixture must use same capture choice: %+v %v", plan, err)
			}
			a := &application{cfg: c, target: win32.Window{Handle: 123, PID: 456},
				gpuCapability: CapabilityStatus{State: "unavailable", Code: "gpu_interop_unavailable"}}
			var failure *BackendUnavailableError
			if err := a.ApplyDraft(next); !errors.As(err, &failure) {
				t.Fatalf("activation bypassed known-unavailable preflight: %v", err)
			}
			if a.cfg != c || a.capture != nil || a.windowRestore != nil || a.displaySession != nil || a.unsaved || a.quit {
				t.Fatal("failed same-profile activation changed preferences/native ownership")
			}
		})
	}
}
