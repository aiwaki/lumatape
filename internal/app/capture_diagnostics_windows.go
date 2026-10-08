//go:build windows

package app

import (
	"time"

	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func (a *application) captureObservation(f capture.Frame, ready bool) map[string]any {
	d := map[string]any{
		"ready": ready, "acquire_call_ms": a.captureAcquireMS,
		"capture_transfer": a.resolvedTransfer, "requested_transfer": a.cfg.Capture.Transfer,
		"source_serial": f.Serial, "source_updated": f.Updated != 0, "dropped": f.Dropped,
		"capture_width": f.Width, "capture_height": f.Height,
		"capture_telemetry": nil, "transfer_stats_valid": a.transferMetricsValid,
	}
	if ready {
		d["capture_age_ms"] = float64(f.Age100ns) / 1e4
	}
	// These legacy statistics describe the last completed transfer. Keep them
	// even when the current Acquire is stale or not ready, with their identity.
	if a.transferMetricsValid {
		d["cpu_transfer_ms"] = float64(a.transferStats.CPUTransfer100ns) / 1e4
		d["readback_wait_ms"] = float64(a.transferStats.ReadbackWait100ns) / 1e4
		d["upload_call_ms"] = float64(a.transferStats.UploadCall100ns) / 1e4
		d["transfer_bytes"] = a.transferStats.BytesPerFrame
		d["transfer_stats_source_serial"] = a.transferStatsSerial
		d["transfer_stats_observed_at"] = a.transferStatsObservedAt
	}
	if a.capture != nil {
		s, err := a.capture.Telemetry()
		if err != nil {
			d["capture_telemetry_error"] = err.Error()
		} else {
			d["capture_telemetry"] = captureTelemetryFields(s)
		}
	}
	return d
}

func (a *application) recordAcquireDiagnostics(f capture.Frame, ready bool, elapsed time.Duration, acquireErr error) {
	now := time.Now()
	slow, slowSkipped, waiting, waitingSkipped := false, uint64(0), false, uint64(0)
	if elapsed > 150*time.Millisecond {
		slow, slowSkipped = a.captureSlowLogs.allow(now)
	}
	if !ready {
		waiting, waitingSkipped = a.captureWaitingLogs.allow(now)
	}
	if !slow && !waiting {
		return
	}
	d := a.captureObservation(f, ready)
	d["slow_events_suppressed"] = slowSkipped
	d["waiting_events_suppressed"] = waitingSkipped
	if acquireErr != nil {
		d["acquire_error"] = acquireErr.Error()
	}
	if slow {
		a.record("capture_acquire_slow", d)
	} else {
		a.record("capture_wait_metrics", d)
	}
}
