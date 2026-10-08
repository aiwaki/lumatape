package app

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestFrameMetricsWarmupAndPercentiles(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	m := NewFrameMetrics(start)
	m.Observe(start.Add(FrameMetricWarmup-time.Nanosecond), FrameMetricSample{CPUSubmitMS: 9000})
	if got := m.Snapshot(); got.Frames != 0 || got.CPUSubmit.Count != 0 {
		t.Fatal("warmup measurement leaked into summary")
	}
	for i := 1; i <= 100; i++ {
		m.Observe(start.Add(FrameMetricWarmup+time.Duration(i-1)*time.Millisecond), FrameMetricSample{
			CPUSubmitMS: float64(i), CPUWorkMS: float64(i * 2), GPUShaderMS: float64(i * 3),
			TransferMS: float64(i * 4), FrameAgeMS: float64(i * 5), PresentIntervalMS: float64(i * 6),
			GPUFresh: true, CaptureFresh: true, Presented: true,
		})
	}
	s := m.Snapshot()
	if s.Frames != 100 || s.Capacity != 2048 || s.WarmupSeconds != 10 || math.Abs(s.WindowSeconds-.099) > 1e-9 {
		t.Fatalf("unexpected summary metadata: %+v", s)
	}
	for i, got := range []MetricPercentiles{s.CPUSubmit, s.CPUWork, s.GPUShader, s.Transfer, s.FrameAge, s.PresentInterval} {
		factor := float64(i + 1)
		if got.Count != 100 || *got.P50 != 50*factor || *got.P95 != 95*factor || *got.P99 != 99*factor || *got.Max != 100*factor {
			t.Fatalf("metric %d does not use nearest-rank percentiles: %+v", i, got)
		}
	}
}

func TestFrameMetricsFreshnessAndUnavailable(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	m := NewFrameMetrics(start)
	now := start.Add(FrameMetricWarmup)
	for i := 0; i < 100; i++ {
		m.Observe(now, FrameMetricSample{GPUShaderMS: 9, TransferMS: 8, FrameAgeMS: 7, PresentIntervalMS: 600,
			GPUFresh: i == 0, CaptureFresh: i == 0})
	}
	s := m.Snapshot()
	if s.GPUShader.Count != 1 || s.Transfer.Count != 1 || s.FrameAge.Count != 1 || s.PresentInterval.Count != 0 || s.PresentStallsOver250MS != 0 {
		t.Fatalf("stale data or excluded present interval was sampled: %+v", s)
	}
	m = NewFrameMetrics(start)
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		m.Observe(now, FrameMetricSample{CPUSubmitMS: v, CPUWorkMS: v, GPUShaderMS: v, TransferMS: v,
			FrameAgeMS: v, PresentIntervalMS: v, GPUFresh: true, CaptureFresh: true, Presented: true})
	}
	s = m.Snapshot()
	for _, got := range []MetricPercentiles{s.CPUSubmit, s.CPUWork, s.GPUShader, s.Transfer, s.FrameAge, s.PresentInterval} {
		if got.Count != 0 || got.P50 != nil || got.P95 != nil || got.P99 != nil || got.Max != nil {
			t.Fatal("invalid measurements were included")
		}
	}
	// Zero is a valid measured duration/age, but never a real present interval.
	m.Observe(now, FrameMetricSample{GPUFresh: true, CaptureFresh: true, Presented: true})
	s = m.Snapshot()
	if s.CPUSubmit.Count != 1 || s.GPUShader.Count != 1 || s.Transfer.Count != 1 || s.FrameAge.Count != 1 || s.PresentInterval.Count != 0 {
		t.Fatal("zero-duration availability is incorrect")
	}
}

func TestFrameMetricsBoundedWindowAndStalls(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	m := NewFrameMetrics(start)
	for i := 0; i < FrameMetricCapacity+20; i++ {
		interval := 16.0
		if i < 20 {
			interval = 900
		}
		if i == FrameMetricCapacity+18 {
			interval = 250
		}
		if i == FrameMetricCapacity+19 {
			interval = 250.1
		}
		m.Observe(start.Add(FrameMetricWarmup+time.Duration(i)*time.Millisecond), FrameMetricSample{
			CPUSubmitMS: float64(i), GPUShaderMS: 20, TransferMS: -1,
			GPUFresh: i < 20, CaptureFresh: i%2 == 0, Presented: true, PresentIntervalMS: interval,
		})
	}
	s := m.Snapshot()
	if s.Frames != FrameMetricCapacity || s.CPUSubmit.Count != FrameMetricCapacity || *s.CPUSubmit.Max != 2067 || *s.CPUSubmit.P50 != 1043 {
		t.Fatalf("wrong ring retention: %+v", s)
	}
	if s.GPUShader.Count != 0 || s.Transfer.Count != 0 || s.FrameAge.Count != FrameMetricCapacity/2 {
		t.Fatal("fresh-sample counts did not follow frame eviction")
	}
	if s.PresentStallsOver250MS != 1 || *s.PresentInterval.Max != 250.1 {
		t.Fatal("stalls must be strictly over 250ms and limited to the retained window")
	}
}

func TestFrameMetricsObserveAllocationsAndSnapshotIsolation(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	m := NewFrameMetrics(start)
	now := start.Add(FrameMetricWarmup)
	sample := FrameMetricSample{CPUSubmitMS: 10}
	if allocations := testing.AllocsPerRun(1000, func() { m.Observe(now, sample) }); allocations != 0 {
		t.Fatalf("Observe allocated %.1f times per frame", allocations)
	}
	previous := m.Snapshot()
	for i := 0; i < FrameMetricCapacity; i++ {
		m.Observe(now, FrameMetricSample{CPUSubmitMS: 2})
	}
	current := m.Snapshot()
	if *previous.CPUSubmit.Max != 10 || *current.CPUSubmit.Max != 2 {
		t.Fatal("snapshots alias the mutable sample history")
	}
}

func TestFrameMetricsEmptyJSON(t *testing.T) {
	var m *FrameMetrics
	m.Observe(time.Now(), FrameMetricSample{}) // optional collector is safe
	data, err := json.Marshal(m.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"gpu_shader_ms":{"count":0,"p50":null,"p95":null,"p99":null,"max":null}`) {
		t.Fatalf("empty metric is not explicitly unavailable: %s", data)
	}
}
