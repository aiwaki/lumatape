//go:build windows

package app

import (
	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/platform/capture"
	"time"
)

func (a *application) resetFrameMetrics() {
	a.metrics = NewFrameMetrics(time.Now())
	a.metricSummary = a.metrics.Snapshot()
	a.metricSummaryAt, a.lastPresented, a.lastFrameAt, a.gpuObservedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	a.metricSize = geometry.Size{}
	a.lastGPUSequence = a.renderer.GPUTimeSequence
}

func (a *application) observeFrameMetrics(now time.Time, frame capture.Frame, full, snapshot bool) {
	a.lastFrameAt = now
	freshGPU := a.renderer.GPUTimeSequence != a.lastGPUSequence
	if freshGPU {
		a.gpuObservedAt = now
		a.lastGPUSequence = a.renderer.GPUTimeSequence
	}
	sample := FrameMetricSample{CPUSubmitMS: a.cpuMS, CPUWorkMS: a.cpuWorkMS, GPUShaderMS: a.renderer.GPUTimeMS, GPUFresh: freshGPU, TransferMS: -1, FrameAgeMS: -1, PresentIntervalMS: -1}
	if full && frame.Updated != 0 {
		sample.CaptureFresh, sample.FrameAgeMS = true, a.ageMS
		if compatibilityCapture(a.resolvedConfig()) && a.transferMetricsValid {
			sample.TransferMS = float64(a.transferStats.CPUTransfer100ns) / 1e4
		}
	}
	if !snapshot {
		if !a.lastPresented.IsZero() {
			sample.Presented = true
			sample.PresentIntervalMS = float64(now.Sub(a.lastPresented).Nanoseconds()) / 1e6
		}
		a.metrics.Observe(now, sample)
		a.lastPresented = now
	} else {
		a.lastPresented = time.Time{}
	}
	if a.metricSummaryAt.IsZero() || now.Sub(a.metricSummaryAt) >= 10*time.Second {
		a.metricSummary, a.metricSummaryAt = a.metrics.Snapshot(), now
		a.record("performance_summary", map[string]any{"mode": a.cfg.Mode, "transfer": a.resolvedConfig().Capture.Transfer, "requested_transfer": a.cfg.Capture.Transfer, "shader_id": a.cfg.Shader.ID, "preset": a.cfg.Preset, "size": a.metricSize, "summary": a.metricSummary, "snapshot_frame_excluded": snapshot})
	}
}
