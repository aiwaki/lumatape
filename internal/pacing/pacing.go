// Package pacing provides a deadline fallback for unavailable or ineffective
// VSync. It measures completed presentation intervals, not input latency or
// display refresh. The owner calls it from one render/event-loop thread.
package pacing

import "time"

const (
	defaultInterval = time.Second / 60
	fastLimit       = 8
)

type Pacer struct {
	interval      time.Duration
	timer         bool
	next          time.Time
	lastPresented time.Time
	fastCount     int
}

// New starts without a scheduled deadline. When swap control is unavailable,
// the first frame can be drawn immediately and later frames use the timer.
// Nonpositive intervals use a 60 Hz fallback; positive intervals are preserved.
func New(interval time.Duration, vsyncAvailable bool) *Pacer {
	p := &Pacer{timer: !vsyncAvailable}
	p.SetInterval(interval)
	return p
}

// UsingTimer reports whether the caller should use Delay rather than rely on
// swap control. Once enabled, timer mode remains enabled for this Pacer.
func (p *Pacer) UsingTimer() bool { return p.timer }

// Delay returns zero in the VSync path: it never adds a timer to working VSync.
// In timer mode the caller waits for this duration using an event-aware wait,
// pumps events and checks Delay again before drawing. It must not spin, and
// must round any conversion to a coarser wait unit upward. New events may wake
// the wait early; repeated calls do not move an existing deadline.
func (p *Pacer) Delay(now time.Time) time.Duration {
	if !p.timer {
		return 0
	}
	if p.next.IsZero() {
		p.next = now
	}
	if remaining := p.next.Sub(now); remaining > 0 {
		return remaining
	}
	return 0
}

// Presented is called after a successful complete frame, including SwapBuffers.
// Eight consecutive intervals below half the nominal refresh interval indicate
// that swap control is not pacing this loop. Only that transition returns true;
// the caller must then disable swap control before using the timer path.
//
// Timer deadlines advance from the prior target so rendering work is part of
// the frame budget. When late, missed deadlines are discarded instead of
// rendering a burst of old frames. Reset after hiding/resizing before resuming.
func (p *Pacer) Presented(now time.Time) (switched bool) {
	if p.timer {
		if p.next.IsZero() || (!p.lastPresented.IsZero() && now.Before(p.lastPresented)) {
			p.next = now.Add(p.interval)
		} else {
			p.next = p.next.Add(p.interval)
			if !p.next.After(now) {
				p.next = now.Add(p.interval)
			}
		}
		p.lastPresented = now
		return false
	}
	if !p.lastPresented.IsZero() {
		elapsed := now.Sub(p.lastPresented)
		if elapsed > 0 && elapsed < p.interval/2 {
			p.fastCount++
		} else {
			p.fastCount = 0
		}
	}
	p.lastPresented = now
	if p.fastCount >= fastLimit {
		p.timer = true
		p.next = now.Add(p.interval)
		return true
	}
	return false
}

// Reset forgets scheduling and observed intervals after visibility/geometry
// changes. The next frame is eligible immediately; timer mode is retained.
func (p *Pacer) Reset(now time.Time) {
	p.next = now
	p.lastPresented = time.Time{}
	p.fastCount = 0
}

// SetInterval updates the nominal monitor period and discards stale deadlines
// and history. It keeps a previous fallback decision, since the caller has
// already disabled swap control when that decision was made.
func (p *Pacer) SetInterval(interval time.Duration) {
	if interval <= 0 {
		interval = defaultInterval
	}
	p.interval = interval
	p.next = time.Time{}
	p.lastPresented = time.Time{}
	p.fastCount = 0
}
