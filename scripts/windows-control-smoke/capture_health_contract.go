package main

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type captureRates struct {
	State            string     `json:"state"`
	PresentFPS       *float64   `json:"present_fps"`
	FreshFrameFPS    *float64   `json:"fresh_frame_fps"`
	SampleSeconds    float64    `json:"sample_seconds"`
	ObservedAt       *time.Time `json:"observed_at"`
	LastFreshFrameAt *time.Time `json:"last_fresh_frame_at"`
}

// A static source may still produce identical WGC frames on some Windows
// versions. Zero fresh FPS is evidence, never a cross-version prerequisite.
func verifyCaptureRates(r captureRates, now time.Time) error {
	if r.State != "current" || r.PresentFPS == nil || r.FreshFrameFPS == nil || r.ObservedAt == nil {
		return errors.New("no current, independently measured presentation and fresh-frame rates")
	}
	present, fresh := *r.PresentFPS, *r.FreshFrameFPS
	if math.IsNaN(present) || math.IsNaN(fresh) || math.IsInf(present, 0) || math.IsInf(fresh, 0) || present <= 0 || fresh < 0 || fresh > present+0.001 || r.SampleSeconds < 1 {
		return fmt.Errorf("invalid measured rates: %+v", r)
	}
	if age := now.Sub(*r.ObservedAt); age < 0 || age >= time.Second {
		return fmt.Errorf("rate sample is not current: age=%s", age)
	}
	return nil
}

func captureIdleReuseObserved(r captureRates, now time.Time) bool {
	return verifyCaptureRates(r, now) == nil && r.LastFreshFrameAt != nil && now.Sub(*r.LastFreshFrameAt) > 500*time.Millisecond
}
