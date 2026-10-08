package pacing

import (
	"testing"
	"time"
)

var epoch = time.Unix(1_000, 0)

func TestWorkingVSyncNeverAddsTimer(t *testing.T) {
	interval := 16 * time.Millisecond
	p := New(interval, true)
	for i := 0; i < 100; i++ {
		now := epoch.Add(time.Duration(i) * interval)
		if p.Delay(now) != 0 || p.Presented(now) || p.UsingTimer() {
			t.Fatal("working VSync was slowed by the fallback")
		}
	}
}

func TestConsistentFastFramesSwitchOnce(t *testing.T) {
	p := New(16*time.Millisecond, true)
	for i := 0; i <= fastLimit; i++ {
		now := epoch.Add(time.Duration(i) * time.Millisecond)
		if switched := p.Presented(now); switched != (i == fastLimit) {
			t.Fatalf("presentation %d: switched=%v", i, switched)
		}
	}
	if !p.UsingTimer() {
		t.Fatal("fallback was not retained")
	}
	now := epoch.Add(fastLimit * time.Millisecond)
	if d := p.Delay(now); d != 16*time.Millisecond {
		t.Fatalf("first fallback deadline = %v", d)
	}
	if p.Presented(now.Add(16 * time.Millisecond)) {
		t.Fatal("transition was signalled again")
	}
}

func TestFastEvidenceMustBeConsecutiveAndPositive(t *testing.T) {
	for _, delta := range []time.Duration{0, -time.Millisecond, 8 * time.Millisecond, 16 * time.Millisecond, time.Second} {
		t.Run(delta.String(), func(t *testing.T) {
			p := New(16*time.Millisecond, true)
			now := epoch
			p.Presented(now)
			for i := 0; i < fastLimit-1; i++ {
				now = now.Add(time.Millisecond)
				if p.Presented(now) {
					t.Fatal("switched before threshold")
				}
			}
			now = now.Add(delta)
			if p.Presented(now) {
				t.Fatal("non-fast interval should discard earlier evidence")
			}
			for i := 1; i <= fastLimit; i++ {
				now = now.Add(time.Millisecond)
				if switched := p.Presented(now); switched != (i == fastLimit) {
					t.Fatalf("after reset, fast interval %d switched=%v", i, switched)
				}
			}
		})
	}
}

func TestFallbackDeadlinesIncludeRenderTime(t *testing.T) {
	p := New(16*time.Millisecond, false)
	if !p.UsingTimer() || p.Delay(epoch) != 0 {
		t.Fatal("first fallback frame must be immediately eligible")
	}
	p.Presented(epoch.Add(2 * time.Millisecond))
	if d := p.Delay(epoch.Add(2 * time.Millisecond)); d != 14*time.Millisecond {
		t.Fatalf("render cost was added to the frame period: %v", d)
	}
	if d := p.Delay(epoch.Add(7 * time.Millisecond)); d != 9*time.Millisecond {
		t.Fatalf("an early event moved the deadline: %v", d)
	}
	if d := p.Delay(epoch.Add(16 * time.Millisecond)); d != 0 {
		t.Fatalf("deadline is not due: %v", d)
	}
	p.Presented(epoch.Add(18 * time.Millisecond))
	if d := p.Delay(epoch.Add(18 * time.Millisecond)); d != 14*time.Millisecond {
		t.Fatalf("deadline cadence drifted: %v", d)
	}
}

func TestLateFrameDoesNotCatchUp(t *testing.T) {
	p := New(10*time.Millisecond, false)
	p.Delay(epoch)
	p.Presented(epoch.Add(43 * time.Millisecond))
	if d := p.Delay(epoch.Add(43 * time.Millisecond)); d != 10*time.Millisecond {
		t.Fatalf("late frame caused immediate catch-up: %v", d)
	}
	if d := p.Delay(epoch.Add(54 * time.Millisecond)); d != 0 {
		t.Fatalf("overdue frame returned negative/nonzero delay: %v", d)
	}
	p.Presented(epoch.Add(55 * time.Millisecond))
	if d := p.Delay(epoch.Add(55 * time.Millisecond)); d != 8*time.Millisecond {
		t.Fatalf("new cadence incorrect: %v", d)
	}
}

func TestResetDropsDeadlineAndVSyncEvidence(t *testing.T) {
	p := New(16*time.Millisecond, false)
	p.Presented(epoch)
	p.Reset(epoch.Add(time.Millisecond))
	if !p.UsingTimer() || p.Delay(epoch.Add(time.Millisecond)) != 0 {
		t.Fatal("reset retained a stale deadline or re-enabled VSync")
	}
	v := New(16*time.Millisecond, true)
	for i := 0; i < fastLimit; i++ {
		v.Presented(epoch.Add(time.Duration(i) * time.Millisecond))
	}
	v.Reset(epoch.Add(time.Second))
	for i := 0; i < fastLimit; i++ {
		if v.Presented(epoch.Add(time.Second + time.Duration(i)*time.Millisecond)) {
			t.Fatal("hidden interval retained prior fast evidence")
		}
	}
}

func TestSetIntervalDropsOldSchedule(t *testing.T) {
	p := New(16*time.Millisecond, false)
	p.Presented(epoch)
	p.SetInterval(8 * time.Millisecond)
	now := epoch.Add(time.Millisecond)
	if !p.UsingTimer() || p.Delay(now) != 0 {
		t.Fatal("monitor change retained old deadline or re-enabled VSync")
	}
	p.Presented(now.Add(time.Millisecond))
	if d := p.Delay(now.Add(time.Millisecond)); d != 7*time.Millisecond {
		t.Fatalf("new refresh interval not applied: %v", d)
	}
}

func TestMissingDelayAndBackwardTimeAreSafe(t *testing.T) {
	p := New(10*time.Millisecond, false)
	p.Presented(epoch)
	if d := p.Delay(epoch); d != 10*time.Millisecond {
		t.Fatalf("Presented without initial Delay: %v", d)
	}
	backward := epoch.Add(-time.Second)
	p.Presented(backward)
	if d := p.Delay(backward); d != 10*time.Millisecond {
		t.Fatalf("backward timestamp kept a stale future deadline: %v", d)
	}
}

func TestInvalidIntervalUsesDefault(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		p := New(interval, false)
		p.Presented(epoch)
		if d := p.Delay(epoch); d != defaultInterval {
			t.Fatalf("interval %v produced %v", interval, d)
		}
	}
}
