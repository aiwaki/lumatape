package main

import (
	"math"
	"testing"
	"time"
)

func TestCaptureHealthRatesDistinguishIdleFromUnavailable(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	freshAt := now.Add(-2 * time.Second)
	present, fresh := 30.0, 0.0
	r := captureRates{State: "current", PresentFPS: &present, FreshFrameFPS: &fresh, SampleSeconds: 2, ObservedAt: &now, LastFreshFrameAt: &freshAt}
	if err := verifyCaptureRates(r, now); err != nil || !captureIdleReuseObserved(r, now) {
		t.Fatalf("valid presentation of idle source rejected: %+v %v", r, err)
	}
	fresh = 10 // WGC can emit duplicate pictures; valid but not evidence of idle reuse.
	r.LastFreshFrameAt = &now
	if err := verifyCaptureRates(r, now); err != nil || captureIdleReuseObserved(r, now) {
		t.Fatalf("valid repeated WGC output misclassified: %v", err)
	}
	for name, change := range map[string]func(*captureRates){
		"paused":       func(r *captureRates) { r.State = "stale" },
		"null capture": func(r *captureRates) { r.FreshFrameFPS = nil },
		"null present": func(r *captureRates) { r.PresentFPS = nil },
		"old":          func(r *captureRates) { r.ObservedAt = &freshAt },
		"warming":      func(r *captureRates) { r.SampleSeconds = 0.5 },
		"inverted":     func(r *captureRates) { x := 31.0; r.FreshFrameFPS = &x },
		"nan":          func(r *captureRates) { x := math.NaN(); r.PresentFPS = &x },
	} {
		t.Run(name, func(t *testing.T) {
			bad := r
			change(&bad)
			if verifyCaptureRates(bad, now) == nil || captureIdleReuseObserved(bad, now) {
				t.Fatal("invalid sample accepted")
			}
		})
	}
}
