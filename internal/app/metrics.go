package app

import (
	"math"
	"sort"
	"time"
)

const (
	FrameMetricCapacity = 2048
	FrameMetricWarmup   = 10 * time.Second
)

// FrameMetricSample describes one successful rendered frame. Durations are in
// milliseconds; negative/non-finite values mean unavailable. A GPU query or
// capture result must only be marked fresh once, never repeatedly sampled from
// a last-known value. Presented controls interval sampling, so explicit PNG
// readbacks and the first frame after a pause can be excluded from pacing data.
type FrameMetricSample struct {
	CPUSubmitMS, CPUWorkMS, GPUShaderMS float64
	TransferMS, FrameAgeMS              float64
	PresentIntervalMS                   float64
	GPUFresh, CaptureFresh, Presented   bool
}

// MetricPercentiles uses nearest-rank percentiles over the retained samples.
// Empty measurements are null rather than a misleading zero-millisecond value.
type MetricPercentiles struct {
	Count int      `json:"count"`
	P50   *float64 `json:"p50"`
	P95   *float64 `json:"p95"`
	P99   *float64 `json:"p99"`
	Max   *float64 `json:"max"`
}

// FrameMetricSummary is a bounded recent-window summary, not lifetime totals or
// input-to-photon latency. GPU results can arrive several frames after submission.
type FrameMetricSummary struct {
	Capacity               int               `json:"capacity"`
	WarmupSeconds          int               `json:"warmup_seconds"`
	Frames                 int               `json:"frames"`
	WindowSeconds          float64           `json:"window_seconds"`
	CPUSubmit              MetricPercentiles `json:"cpu_submit_ms"`
	CPUWork                MetricPercentiles `json:"cpu_work_ms"`
	GPUShader              MetricPercentiles `json:"gpu_shader_ms"`
	Transfer               MetricPercentiles `json:"transfer_ms"`
	FrameAge               MetricPercentiles `json:"frame_age_ms"`
	PresentInterval        MetricPercentiles `json:"present_interval_ms"`
	PresentStallsOver250MS int               `json:"present_stalls_over_250ms"`
}

type metricFrame struct {
	at     time.Time
	sample FrameMetricSample
}

// FrameMetrics belongs to the rendering thread. Observe never allocates, sorts,
// or waits. Recreate it when changing the measured backend/configuration; mixed
// configurations otherwise remain explicitly part of the same recent window.
type FrameMetrics struct {
	warmupUntil time.Time
	frames      [FrameMetricCapacity]metricFrame
	next, count int
}

func NewFrameMetrics(start time.Time) *FrameMetrics {
	return &FrameMetrics{warmupUntil: start.Add(FrameMetricWarmup)}
}

func (m *FrameMetrics) Observe(now time.Time, sample FrameMetricSample) {
	if m == nil || now.Before(m.warmupUntil) {
		return
	}
	m.frames[m.next] = metricFrame{at: now, sample: sample}
	m.next = (m.next + 1) % FrameMetricCapacity
	if m.count < FrameMetricCapacity {
		m.count++
	}
}

func validMetric(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func metricPercentiles(values []float64) MetricPercentiles {
	if len(values) == 0 {
		return MetricPercentiles{}
	}
	sort.Float64s(values)
	rank := func(percent int) *float64 {
		value := values[(len(values)*percent+99)/100-1]
		return &value
	}
	return MetricPercentiles{Count: len(values), P50: rank(50), P95: rank(95), P99: rank(99), Max: rank(100)}
}

// Snapshot performs all allocation and sorting. Call at a slow diagnostic/log
// interval (for example every ten seconds), never from the per-frame hot path.
func (m *FrameMetrics) Snapshot() FrameMetricSummary {
	summary := FrameMetricSummary{Capacity: FrameMetricCapacity, WarmupSeconds: int(FrameMetricWarmup / time.Second)}
	if m == nil || m.count == 0 {
		return summary
	}
	summary.Frames = m.count
	first := (m.next - m.count + FrameMetricCapacity) % FrameMetricCapacity
	last := (m.next + FrameMetricCapacity - 1) % FrameMetricCapacity
	summary.WindowSeconds = max(0, m.frames[last].at.Sub(m.frames[first].at).Seconds())
	var samples [6][]float64
	for i := range samples {
		samples[i] = make([]float64, 0, m.count)
	}
	appendValid := func(index int, value float64) {
		if validMetric(value) {
			samples[index] = append(samples[index], value)
		}
	}
	for i := 0; i < m.count; i++ {
		sample := m.frames[(first+i)%FrameMetricCapacity].sample
		appendValid(0, sample.CPUSubmitMS)
		appendValid(1, sample.CPUWorkMS)
		if sample.GPUFresh {
			appendValid(2, sample.GPUShaderMS)
		}
		if sample.CaptureFresh {
			appendValid(3, sample.TransferMS)
			appendValid(4, sample.FrameAgeMS)
		}
		if sample.Presented && validMetric(sample.PresentIntervalMS) && sample.PresentIntervalMS > 0 {
			appendValid(5, sample.PresentIntervalMS)
			if sample.PresentIntervalMS > 250 {
				summary.PresentStallsOver250MS++
			}
		}
	}
	summary.CPUSubmit = metricPercentiles(samples[0])
	summary.CPUWork = metricPercentiles(samples[1])
	summary.GPUShader = metricPercentiles(samples[2])
	summary.Transfer = metricPercentiles(samples[3])
	summary.FrameAge = metricPercentiles(samples[4])
	summary.PresentInterval = metricPercentiles(samples[5])
	return summary
}
