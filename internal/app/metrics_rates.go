package app

import "time"

const (
	frameRateWindow     = 2 * time.Second
	frameRateMinimum    = time.Second
	frameRateStaleAfter = time.Second
)

// FrameRateSummary measures completed presentations and newly captured images
// that were actually presented. Neither rate is the game's internal FPS or the
// monitor's refresh rate. A paused/expired measurement has null rates.
type FrameRateSummary struct {
	State            string                `json:"state"` // unavailable, warming-up, current, stale
	PresentFPS       *float64              `json:"present_fps"`
	FreshFrameFPS    *float64              `json:"fresh_frame_fps"`
	SampleSeconds    float64               `json:"sample_seconds"`
	ObservedAt       *time.Time            `json:"observed_at"`
	LastFreshFrameAt *time.Time            `json:"last_fresh_frame_at"`
	LastSample       *FrameRateMeasurement `json:"last_sample,omitempty"`
}

// FrameRateMeasurement is explicitly historical. Opening settings pauses the
// overlay, but the last complete measurement can still help diagnose its speed.
// It must not be displayed as the current rate or mixed across config resets.
type FrameRateMeasurement struct {
	PresentFPS    float64   `json:"present_fps"`
	FreshFrameFPS *float64  `json:"fresh_frame_fps"`
	SampleSeconds float64   `json:"sample_seconds"`
	ObservedAt    time.Time `json:"observed_at"`
}

type frameRateMeasurement struct {
	presentFPS, freshFPS, sampleSeconds float64
	observedAt                          time.Time
	capture, qualified                  bool
}

func (m frameRateMeasurement) snapshot() *FrameRateMeasurement {
	if !m.qualified {
		return nil
	}
	s := &FrameRateMeasurement{PresentFPS: m.presentFPS, SampleSeconds: m.sampleSeconds, ObservedAt: m.observedAt}
	if m.capture {
		fresh := m.freshFPS
		s.FreshFrameFPS = &fresh
	}
	return s
}

type frameRateSample struct {
	at    time.Time
	fresh bool
}

type frameRates struct {
	samples        [FrameMetricCapacity]frameRateSample
	next, count    int
	capture        bool
	lastFreshFrame time.Time
	lastSample     frameRateMeasurement
}

// observe is called only after a successful Swap, excluding explicit PNG
// snapshots. It allocates nothing and stores no unbounded event history.
func (r *frameRates) observe(now time.Time, sample FrameMetricSample) {
	if r.count > 0 {
		last := r.samples[(r.next+FrameMetricCapacity-1)%FrameMetricCapacity].at
		if !sample.Presented || sample.CaptureAvailable != r.capture || !now.After(last) || now.Sub(last) >= frameRateStaleAfter {
			if sample.CaptureAvailable != r.capture {
				r.lastSample = frameRateMeasurement{}
			} else if measurement := r.measure(last); measurement.qualified {
				r.lastSample = measurement
			}
			r.next, r.count, r.lastFreshFrame = 0, 0, time.Time{}
		}
	}
	r.capture = sample.CaptureAvailable
	fresh := r.capture && sample.CaptureFresh
	r.samples[r.next] = frameRateSample{at: now, fresh: fresh}
	r.next = (r.next + 1) % FrameMetricCapacity
	r.count = min(r.count+1, FrameMetricCapacity)
	if fresh {
		r.lastFreshFrame = now
	}
}

// FrameRates reads the short recent window without sorting. Paused current rates
// are null; a qualified historical rate has a separate timestamped field. The
// separate percentile collector still uses
// its ten-second warmup; this user-facing measurement becomes ready after one.
func (m *FrameMetrics) FrameRates(now time.Time, presenting bool) FrameRateSummary {
	result := FrameRateSummary{State: "unavailable"}
	if m == nil || m.rates.count == 0 {
		return result
	}
	r := &m.rates
	last := r.samples[(r.next+FrameMetricCapacity-1)%FrameMetricCapacity].at
	previous := r.lastSample
	if latest := r.measure(last); latest.qualified {
		previous = latest
	}
	result.LastSample = previous.snapshot()
	result.ObservedAt = &last
	if !r.lastFreshFrame.IsZero() {
		fresh := r.lastFreshFrame
		result.LastFreshFrameAt = &fresh
	}
	if !presenting || now.Before(last) || now.Sub(last) >= frameRateStaleAfter {
		result.State = "stale"
		return result
	}
	measurement := r.measure(now)
	result.State = "warming-up"
	result.SampleSeconds = measurement.sampleSeconds
	if !measurement.qualified {
		return result
	}
	result.State = "current"
	result.PresentFPS = &measurement.presentFPS
	if r.capture {
		result.FreshFrameFPS = &measurement.freshFPS
	}
	return result
}

// measure uses value storage so an observation-boundary reset can retain the
// previous qualified interval without allocating on the rendering thread.
func (r *frameRates) measure(now time.Time) frameRateMeasurement {
	result := frameRateMeasurement{observedAt: now, capture: r.capture}
	first := (r.next - r.count + FrameMetricCapacity) % FrameMetricCapacity
	cutoff := now.Add(-frameRateWindow)
	var start time.Time
	presented, fresh := 0, 0
	for n := 0; n < r.count; n++ {
		sample := r.samples[(first+n)%FrameMetricCapacity]
		if sample.at.Before(cutoff) {
			continue
		}
		if start.IsZero() {
			// The first sample starts the observation interval. Counting its
			// presentation would inflate FPS by one frame per sample window.
			start = sample.at
			continue
		}
		presented++
		if sample.fresh {
			fresh++
		}
	}
	if start.IsZero() {
		return result
	}
	elapsed := now.Sub(start)
	result.sampleSeconds = elapsed.Seconds()
	if elapsed < frameRateMinimum || presented == 0 {
		return result
	}
	result.qualified = true
	result.presentFPS = float64(presented) / elapsed.Seconds()
	result.freshFPS = float64(fresh) / elapsed.Seconds()
	return result
}
