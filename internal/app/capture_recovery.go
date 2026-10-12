package app

import (
	"time"

	"github.com/aiwaki/lumatape/internal/geometry"
)

const (
	captureMaxAge         = 500 * time.Millisecond
	captureRecoveryMaxAge = captureMaxAge / 2
	captureRecoveryWindow = 100 * time.Millisecond
	captureRecoveryFrames = 3
	captureRelapseWindow  = 10 * time.Second
)

// CaptureRecoveryStatus describes a pause caused by unreliable capture, not a
// loss of focus. "blocked" requires an explicit off/on or source/backend change.
type CaptureRecoveryStatus struct {
	State    string `json:"state"`
	Failures uint64 `json:"failures"`
}

// A stale image is hidden immediately. One young frame is insufficient to put
// an opaque surface back over the interactive source: collect distinct frames
// over a short healthy interval first. A quick relapse then latches the pause,
// instead of repeatedly flashing between the effect and the original game.
type captureRecoveryGate struct {
	recovering, blocked   bool
	failures              uint64
	lastResume            time.Time
	firstFresh, lastFresh time.Time
	lastSerial            uint64
	acceptedSerial        uint64
	freshFrames           int
	pendingSerial         uint64
	sourceSize            geometry.Size
}

func (g *captureRecoveryGate) Status() CaptureRecoveryStatus {
	state := "ready"
	if g.blocked {
		state = "blocked"
	} else if g.recovering {
		state = "recovering"
	}
	return CaptureRecoveryStatus{State: state, Failures: g.failures}
}

// Suspend discards observations from a closed capture/hidden source. It must
// not clear a failure latch merely because the user opened the tray or Alt-Tab.
func (g *captureRecoveryGate) Suspend() {
	g.acceptedSerial = 0
	g.resetProgress()
}

func (g *captureRecoveryGate) resetProgress() {
	g.firstFresh, g.lastFresh = time.Time{}, time.Time{}
	g.lastSerial, g.freshFrames, g.pendingSerial = 0, 0, 0
}

func (g *captureRecoveryGate) Reset() { *g = captureRecoveryGate{} }

// ObserveSourceSize separates a new capture/texture allocation from failures at
// the previous resolution. Moving or focusing the same-sized source is not a
// new epoch. An already latched failure still requires the explicit retry.
func (g *captureRecoveryGate) ObserveSourceSize(size geometry.Size) {
	if size.W <= 0 || size.H <= 0 || size == g.sourceSize {
		return
	}
	previous := g.sourceSize
	g.sourceSize = size
	if previous.W <= 0 || previous.H <= 0 {
		return
	}
	g.Suspend()
	g.lastResume = time.Time{}
}

// Presented confirms that a recovery candidate survived rendering, Swap and
// the final age/alignment checks. Acquire alone cannot prove recovery: the first
// allocation or swap after a resize can still stall before anything is shown.
func (g *captureRecoveryGate) Presented(now time.Time, serial uint64) bool {
	if g.blocked || !g.recovering || serial == 0 || serial != g.pendingSerial {
		return false
	}
	g.recovering, g.lastResume, g.acceptedSerial = false, now, serial
	g.resetProgress()
	return true
}

func (g *captureRecoveryGate) Observe(now time.Time, serial uint64, updated bool, age time.Duration) bool {
	return g.ObserveFrame(now, serial, updated, age, false)
}

func (g *captureRecoveryGate) ObserveFrame(now time.Time, serial uint64, updated bool, age time.Duration, idleValidated bool) bool {
	if g.blocked {
		return false
	}
	// WGC may skip static frames. Reuse only a generation accepted fresh in
	// this session, with a successful native health check and no pending copy.
	// A newly acquired stale generation never becomes safe simply by repeating.
	if !g.recovering && !updated && idleValidated && serial != 0 && serial == g.acceptedSerial && age >= 0 {
		return true
	}
	if age < 0 || age > captureMaxAge {
		if !g.recovering {
			g.failures++
			g.blocked = !g.lastResume.IsZero() && (now.Before(g.lastResume) || now.Sub(g.lastResume) < captureRelapseWindow)
		}
		g.recovering = true
		g.acceptedSerial = 0
		g.resetProgress()
		return false
	}
	if !g.recovering {
		if updated {
			g.acceptedSerial = serial
		}
		return true
	}
	// The same candidate is checked again after Draw, Swap and cursor update.
	// These checks must not count as new samples, nor declare recovery before
	// the application actually shows it.
	if g.pendingSerial != 0 && serial == g.pendingSerial && !updated {
		return true
	}
	if age > captureRecoveryMaxAge || (!g.lastFresh.IsZero() && (now.Before(g.lastFresh) || now.Sub(g.lastFresh) > captureRecoveryMaxAge)) {
		g.resetProgress()
	}
	if !updated || serial == 0 || serial <= g.lastSerial || age > captureRecoveryMaxAge {
		return false
	}
	if g.firstFresh.IsZero() {
		g.firstFresh = now
	}
	g.lastFresh, g.lastSerial = now, serial
	g.freshFrames++
	if g.freshFrames < captureRecoveryFrames || now.Sub(g.firstFresh) < captureRecoveryWindow {
		return false
	}
	g.pendingSerial = serial
	return true
}
