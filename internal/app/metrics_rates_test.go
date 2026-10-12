package app

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func recordRateFrames(m *FrameMetrics, start time.Time, frames, fps, freshEvery int, capture bool) time.Time {
	var now time.Time
	for n := 0; n < frames; n++ {
		now = start.Add(time.Duration(n) * time.Second / time.Duration(fps))
		m.Observe(now, FrameMetricSample{Presented: n > 0, CaptureAvailable: capture, CaptureFresh: freshEvery > 0 && n%freshEvery == 0})
	}
	return now
}

func requireFPS(t *testing.T, actual *float64, want float64) {
	t.Helper()
	if actual == nil || math.Abs(*actual-want) > 1e-6 {
		t.Fatalf("FPS = %v, want %v", actual, want)
	}
}

func TestFrameRatesDistinguishRepeatedTexturesFromFreshFrames(t *testing.T) {
	start := time.Unix(1000, 0)
	m := NewFrameMetrics(start)
	now := recordRateFrames(m, start, 91, 30, 3, true)
	s := m.FrameRates(now, true)
	if s.State != "current" || s.SampleSeconds != 2 || s.ObservedAt == nil || !s.ObservedAt.Equal(now) {
		t.Fatalf("measurement window: %+v", s)
	}
	requireFPS(t, s.PresentFPS, 30)
	requireFPS(t, s.FreshFrameFPS, 10)
	if s.LastFreshFrameAt == nil || !s.LastFreshFrameAt.Equal(now) {
		t.Fatalf("fresh observation time missing: %+v", s)
	}
	if m.Snapshot().Frames != 0 {
		t.Fatal("short FPS window bypassed the percentile collector's warmup")
	}
}

func TestFrameRatesUnavailableWarmupAndStale(t *testing.T) {
	start := time.Unix(1000, 0)
	var absent *FrameMetrics
	if got := absent.FrameRates(start, true); got.State != "unavailable" || got.PresentFPS != nil || got.FreshFrameFPS != nil || got.ObservedAt != nil {
		t.Fatalf("absent collector reported a measurement: %+v", got)
	}
	m := NewFrameMetrics(start)
	if m.FrameRates(start, true).State != "unavailable" {
		t.Fatal("empty collector should be unavailable")
	}
	now := recordRateFrames(m, start, 16, 30, 1, true)
	if got := m.FrameRates(now, true); got.State != "warming-up" || got.PresentFPS != nil || got.FreshFrameFPS != nil {
		t.Fatalf("short interval reported an FPS estimate: %+v", got)
	}
	now = recordRateFrames(m, start.Add(time.Second), 61, 30, 1, true)
	for _, tc := range []struct {
		name       string
		now        time.Time
		presenting bool
	}{
		{"hidden", now, false},
		{"expired", now.Add(frameRateStaleAfter), true},
		{"backward-clock", now.Add(-time.Millisecond), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := m.FrameRates(tc.now, tc.presenting)
			if got.State != "stale" || got.PresentFPS != nil || got.FreshFrameFPS != nil || got.ObservedAt == nil || !got.ObservedAt.Equal(now) {
				t.Fatalf("stale observation presented as live FPS: %+v", got)
			}
		})
	}
}

func TestFrameRatesLightweightAndNoFreshFramesAreDifferent(t *testing.T) {
	start := time.Unix(1000, 0)
	for _, capture := range []bool{false, true} {
		m := NewFrameMetrics(start)
		now := recordRateFrames(m, start, 61, 30, 0, capture)
		s := m.FrameRates(now, true)
		requireFPS(t, s.PresentFPS, 30)
		if capture {
			requireFPS(t, s.FreshFrameFPS, 0)
		} else if s.FreshFrameFPS != nil {
			t.Fatal("Lightweight has no captured-frame measurement")
		}
		if s.LastFreshFrameAt != nil {
			t.Fatal("a fresh image was invented")
		}
	}
}

func TestFrameRatesPauseAndBackendChangeStartNewWindow(t *testing.T) {
	start := time.Unix(1000, 0)
	for _, tc := range []struct {
		name   string
		sample FrameMetricSample
		delay  time.Duration
	}{
		{"pause", FrameMetricSample{Presented: false, CaptureAvailable: true}, time.Millisecond},
		{"backend", FrameMetricSample{Presented: true, CaptureAvailable: false}, time.Millisecond},
		{"stalled", FrameMetricSample{Presented: true, CaptureAvailable: true}, frameRateStaleAfter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewFrameMetrics(start)
			now := recordRateFrames(m, start, 61, 30, 1, true).Add(tc.delay)
			m.Observe(now, tc.sample)
			got := m.FrameRates(now, true)
			if got.State != "warming-up" || got.PresentFPS != nil || got.FreshFrameFPS != nil || got.LastFreshFrameAt != nil {
				t.Fatalf("old rates survived a measurement boundary: %+v", got)
			}
		})
	}
}

func TestFrameRatesCountsOnlyRecentWindowAndRetainsBoundedHistory(t *testing.T) {
	start := time.Unix(1000, 0)
	m := NewFrameMetrics(start)
	now := recordRateFrames(m, start, FrameMetricCapacity+1000, 1000, 5, true)
	if m.rates.count != FrameMetricCapacity {
		t.Fatal("measurement storage exceeded its bound")
	}
	s := m.FrameRates(now, true)
	requireFPS(t, s.PresentFPS, 1000)
	requireFPS(t, s.FreshFrameFPS, 200)
	// A half-second without presentation is included in the denominator,
	// rather than silently continuing to show the prior rate unchanged.
	delayed := m.FrameRates(now.Add(500*time.Millisecond), true)
	if delayed.State != "current" || delayed.PresentFPS == nil || *delayed.PresentFPS >= *s.PresentFPS {
		t.Fatalf("unobserved time was excluded from throughput: %+v", delayed)
	}
}

func TestFrameRatesEmptyJSONUsesNullRates(t *testing.T) {
	var m *FrameMetrics
	data, err := json.Marshal(m.FrameRates(time.Now(), false))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"state":"unavailable","present_fps":null,"fresh_frame_fps":null,"sample_seconds":0,"observed_at":null,"last_fresh_frame_at":null}`
	if string(data) != want {
		t.Fatalf("unavailable FPS JSON = %s", data)
	}
}

func TestFrameRatesRetainsLastQualifiedSampleWhileSettingsPause(t *testing.T) {
	start := time.Unix(1000, 0)
	m := NewFrameMetrics(start)
	now := recordRateFrames(m, start, 61, 30, 3, true)
	// No live snapshot was requested. Opening settings must still preserve the
	// completed measurement instead of requiring a lucky poll before focus loss.
	paused := m.FrameRates(now.Add(10*time.Second), false)
	if paused.State != "stale" || paused.PresentFPS != nil || paused.FreshFrameFPS != nil || paused.LastSample == nil {
		t.Fatalf("pause lost historical measurement or invented a current rate: %+v", paused)
	}
	last := paused.LastSample
	if last.PresentFPS != 30 || last.SampleSeconds != 2 || !last.ObservedAt.Equal(now) {
		t.Fatalf("historical measurement changed during pause: %+v", last)
	}
	requireFPS(t, last.FreshFrameFPS, 10)
	// An isolated first frame after reopening capture starts a new current
	// window, retaining the old one with its original time until it qualifies.
	resume := now.Add(11 * time.Second)
	m.Observe(resume, FrameMetricSample{Presented: false, CaptureAvailable: true, CaptureFresh: true})
	warming := m.FrameRates(resume, true)
	if warming.State != "warming-up" || warming.PresentFPS != nil || warming.LastSample == nil || !warming.LastSample.ObservedAt.Equal(now) {
		t.Fatalf("resume lost historical sample or presented it as current: %+v", warming)
	}
	now = recordRateFrames(m, resume.Add(time.Second/20), 41, 20, 1, true)
	qualified := m.FrameRates(now, true)
	if qualified.LastSample == nil || !qualified.LastSample.ObservedAt.Equal(now) || qualified.LastSample.PresentFPS != 20 {
		t.Fatalf("new qualified interval did not replace the historical one: %+v", qualified)
	}
}

func TestFrameRatesHistoricalSampleRespectsMeasurementBoundaries(t *testing.T) {
	start := time.Unix(1000, 0)
	m := NewFrameMetrics(start)
	now := recordRateFrames(m, start, 61, 30, 1, true)
	m.Observe(now.Add(time.Second), FrameMetricSample{CaptureAvailable: true})
	if m.FrameRates(now.Add(time.Second), false).LastSample == nil {
		t.Fatal("pause failed to preserve measurement")
	}
	m.Observe(now.Add(2*time.Second), FrameMetricSample{CaptureAvailable: false})
	if got := m.FrameRates(now.Add(2*time.Second), true); got.LastSample != nil {
		t.Fatalf("Full rates leaked into a Lightweight measurement: %+v", got)
	}
	// The application recreates this collector on source/config/size changes.
	m = NewFrameMetrics(now)
	if m.FrameRates(now, false).LastSample != nil {
		t.Fatal("fresh collector retained another source's rates")
	}
}

func TestFrameRatesHistoricalSampleIsQualifiedAndDetached(t *testing.T) {
	start := time.Unix(1000, 0)
	m := NewFrameMetrics(start)
	now := recordRateFrames(m, start, 16, 30, 1, true)
	if got := m.FrameRates(now, false); got.LastSample != nil {
		t.Fatal("half-second warmup stored a historical rate")
	}
	now = recordRateFrames(m, start.Add(time.Second), 61, 30, 1, false)
	got := m.FrameRates(now, false)
	if got.LastSample == nil || got.LastSample.FreshFrameFPS != nil {
		t.Fatalf("Lightweight history must have no capture rate: %+v", got)
	}
	got.LastSample.PresentFPS = 999
	if next := m.FrameRates(now, false); next.LastSample.PresentFPS != 30 {
		t.Fatal("caller mutated measurement storage")
	}
	// The observation path retains a qualified rate at a boundary without
	// allocating; this matters when focus alternates frequently.
	if allocations := testing.AllocsPerRun(20, func() {
		m.rates.observe(now.Add(time.Second), FrameMetricSample{CaptureAvailable: false})
	}); allocations != 0 {
		t.Fatalf("observe allocated %v objects", allocations)
	}
}
