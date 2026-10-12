package app

import (
	"testing"
	"time"

	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestCaptureRecoveryNeverPresentsStaleAndWaitsForSustainedFreshFrames(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	if !g.Observe(now, 1, true, captureMaxAge) || g.Observe(now, 2, true, captureMaxAge+1) {
		t.Fatal("500 ms safety boundary changed")
	}
	for i := 0; i < 12; i++ {
		if g.Observe(now.Add(time.Duration(i)*time.Millisecond), 3, i == 0, time.Millisecond) {
			t.Fatal("one new frame plus repeats resumed the opaque surface")
		}
	}
	if g.Observe(now.Add(50*time.Millisecond), 4, true, time.Millisecond) {
		t.Fatal("resumed before healthy interval")
	}
	if !g.Observe(now.Add(100*time.Millisecond), 5, true, time.Millisecond) || !g.Presented(now.Add(100*time.Millisecond), 5) || g.Status().State != "ready" {
		t.Fatal("sustained fresh frames did not recover")
	}
}

func TestCaptureRecoveryRelapseLatchesUntilExplicitReset(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	g.Observe(now, 1, false, time.Second)
	for i := uint64(1); i <= 3; i++ {
		if g.Observe(now.Add(time.Duration(i)*50*time.Millisecond), i+1, true, time.Millisecond) {
			g.Presented(now.Add(time.Duration(i)*50*time.Millisecond), i+1)
		}
	}
	if g.Status().State != "ready" {
		t.Fatal("fixture did not recover")
	}
	if g.Observe(now.Add(time.Second), 5, true, time.Second) || g.Status().State != "blocked" || g.Status().Failures != 2 {
		t.Fatal("repeated stall was allowed to flash")
	}
	g.Suspend() // capture close / focus change is not a user retry
	for i := uint64(1); i < 100; i++ {
		if g.Observe(now.Add(time.Duration(i)*time.Second), i, true, time.Millisecond) {
			t.Fatal("latched failure resumed without explicit retry")
		}
	}
	g.Reset()
	if !g.Observe(now.Add(time.Minute), 1, true, time.Millisecond) || g.Status().Failures != 0 {
		t.Fatal("explicit retry did not clear the latch")
	}
}

func TestCaptureRecoveryContinuousStallCountsOneEpisode(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	for i := 0; i < 100; i++ {
		if g.Observe(now.Add(time.Duration(i)*time.Second), 1, false, time.Hour) {
			t.Fatal("stale source became visible")
		}
	}
	if got := g.Status(); got.State != "recovering" || got.Failures != 1 {
		t.Fatalf("one prolonged stall became repeated failures: %+v", got)
	}
}

func TestCaptureRecoveryRejectsOldFramesGapsAndBackwardClock(t *testing.T) {
	for _, gap := range []time.Duration{300 * time.Millisecond, -time.Second} {
		var g captureRecoveryGate
		now := time.Unix(100, 0)
		g.Observe(now, 1, false, time.Second)
		g.Observe(now, 2, true, time.Millisecond)
		g.Observe(now.Add(50*time.Millisecond), 3, true, time.Millisecond)
		if g.Observe(now.Add(50*time.Millisecond+gap), 4, true, time.Millisecond) {
			t.Fatal("discontinuous recovery was presented")
		}
	}
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	g.Observe(now, 1, false, time.Second)
	for i := uint64(1); i <= 10; i++ {
		if g.Observe(now.Add(time.Duration(i)*50*time.Millisecond), i+1, true, captureRecoveryMaxAge+1) {
			t.Fatal("near-stale frames were accepted as recovery")
		}
	}
}

func TestCaptureRecoveryIsolatedFutureStallGetsOneNewRecovery(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	g.Observe(now, 1, false, time.Second)
	for i := uint64(1); i <= 3; i++ {
		if g.Observe(now.Add(time.Duration(i)*50*time.Millisecond), i+1, true, time.Millisecond) {
			g.Presented(now.Add(time.Duration(i)*50*time.Millisecond), i+1)
		}
	}
	g.Observe(now.Add(time.Minute), 5, false, time.Second)
	if got := g.Status(); got.State != "recovering" || got.Failures != 2 {
		t.Fatalf("isolated later stall was treated as rapid relapse: %+v", got)
	}
}

func TestCaptureRecoveryKeepsValidatedStaticContentVisible(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	if !g.ObserveFrame(now, 10, true, time.Millisecond, false) {
		t.Fatal("fresh frame was rejected")
	}
	for i := 1; i <= 600; i++ {
		elapsed := time.Duration(i) * time.Second
		if !g.ObserveFrame(now.Add(elapsed), 10, false, elapsed, true) {
			t.Fatal("validated unchanged source flickered solely because its content is old")
		}
	}
	if got := g.Status(); got.State != "ready" || got.Failures != 0 {
		t.Fatalf("static content counted as capture failure: %+v", got)
	}
}

func TestCaptureRecoveryIdleReuseRequiresAcceptedGenerationAndHealth(t *testing.T) {
	now := time.Unix(100, 0)
	for _, kind := range []string{"no health", "pending copy", "unknown serial", "new stale", "closed session"} {
		t.Run(kind, func(t *testing.T) {
			var g captureRecoveryGate
			g.ObserveFrame(now, 10, true, time.Millisecond, false)
			serial, updated, healthy := uint64(10), false, true
			switch kind {
			case "no health", "pending copy":
				healthy = false
			case "unknown serial":
				serial++
			case "new stale":
				serial++
				updated = true
			case "closed session":
				g.Suspend()
			}
			if g.ObserveFrame(now.Add(time.Second), serial, updated, time.Second, healthy) {
				t.Fatal("unvalidated stale generation was displayed")
			}
			if g.ObserveFrame(now.Add(2*time.Second), serial, false, 2*time.Second, true) {
				t.Fatal("a rejected generation became valid merely by repeating")
			}
		})
	}
}

func FuzzCaptureRecoverySafety(f *testing.F) {
	f.Add([]byte{7, 2, 3, 4, 5, 6, 7, 1, 2, 3, 0, 2})
	f.Add([]byte{255, 127, 63, 31, 15, 7, 3, 2, 1, 0})
	f.Fuzz(func(t *testing.T, actions []byte) {
		var g captureRecoveryGate
		now := time.Unix(100, 0)
		serial := uint64(1)
		for _, action := range actions {
			switch action {
			case 0:
				g.Reset()
				continue
			case 2, 3:
				before := g.Status()
				size := geometry.Size{W: 960, H: 720}
				if action == 3 {
					size = geometry.Size{W: 1920, H: 1080}
				}
				g.ObserveSourceSize(size)
				if before.State == "blocked" && g.Status().State != "blocked" {
					t.Fatal("size change cleared the capture failure latch")
				}
				continue
			case 1:
				before := g.Status()
				g.Suspend()
				if g.Status() != before {
					t.Fatal("focus/capture suspension changed the recovery latch")
				}
				continue
			}
			delta := time.Duration(action%16) * 50 * time.Millisecond
			if action&64 != 0 {
				delta = -delta
			}
			now = now.Add(delta)
			age := time.Duration(action%13) * 50 * time.Millisecond
			updated := action&1 == 0
			if updated {
				serial++
			}
			before := g.Status()
			idleValidated := action&128 != 0
			acceptedIdle := before.State == "ready" && !updated && idleValidated && serial == g.acceptedSerial
			candidateCheck := g.pendingSerial != 0 && serial == g.pendingSerial
			usable := g.ObserveFrame(now, serial, updated, age, idleValidated)
			if usable && ((age > captureMaxAge && !acceptedIdle) || before.State == "blocked" || (before.State == "recovering" && !updated && !candidateCheck)) {
				t.Fatalf("unsafe frame accepted: before=%+v age=%v updated=%v", before, age, updated)
			}
			if g.Status().Failures < before.Failures {
				t.Fatal("failure history changed without explicit reset")
			}
			if usable && action&32 != 0 {
				g.Presented(now, serial)
			}
		}
	})
}

// A resize can warm capture quickly while the first overlay allocation or Swap
// still blocks. A recovery that never reached the screen is not a relapse.
func TestCaptureRecoveryFailedFirstPresentationRemainsOneStall(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	g.ObserveFrame(now, 1, true, time.Second, false)
	for attempt := 0; attempt < 3; attempt++ {
		base := now.Add(time.Duration(attempt) * time.Second)
		for i := uint64(0); i < 3; i++ {
			serial := uint64(attempt*3) + i + 2
			usable := g.ObserveFrame(base.Add(time.Duration(i)*50*time.Millisecond), serial, true, time.Millisecond, false)
			if usable != (i == 2) {
				t.Fatalf("attempt %d sample %d usable=%v", attempt, i, usable)
			}
		}
		serial := uint64(attempt*3) + 4
		if g.ObserveFrame(base.Add(700*time.Millisecond), serial, false, 600*time.Millisecond, false) {
			t.Fatal("expired first Draw/Swap was accepted")
		}
		if got := g.Status(); got.State != "recovering" || got.Failures != 1 {
			t.Fatalf("unshown frame became a second visible failure: %+v", got)
		}
	}
}

func TestCaptureRecoveryRequiresMatchingPresentedCandidate(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	g.ObserveFrame(now, 1, true, time.Second, false)
	for i := uint64(0); i < 3; i++ {
		g.ObserveFrame(now.Add(time.Duration(i)*50*time.Millisecond), i+2, true, time.Millisecond, false)
	}
	if got := g.Status(); got.State != "recovering" || got.Failures != 1 {
		t.Fatalf("acquisition prematurely marked capture healthy: %+v", got)
	}
	for i := 0; i < 4; i++ {
		if !g.ObserveFrame(now.Add(110*time.Millisecond), 4, false, 10*time.Millisecond, false) {
			t.Fatal("candidate was rejected at repeated final validation")
		}
	}
	if g.Presented(now.Add(120*time.Millisecond), 3) || g.Presented(now.Add(120*time.Millisecond), 0) {
		t.Fatal("wrong serial committed recovery")
	}
	if !g.Presented(now.Add(120*time.Millisecond), 4) || g.Status().State != "ready" {
		t.Fatal("successfully shown candidate did not confirm recovery")
	}
	if g.Presented(now.Add(130*time.Millisecond), 4) {
		t.Fatal("same presentation confirmed recovery twice")
	}
	if g.ObserveFrame(now.Add(time.Second), 5, true, time.Second, false) || g.Status().State != "blocked" {
		t.Fatal("real relapse after a shown recovery was not blocked")
	}
}

func TestCaptureRecoverySuspensionDiscardsUnshownCandidate(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	g.ObserveFrame(now, 1, true, time.Second, false)
	for i := uint64(0); i < 3; i++ {
		g.ObserveFrame(now.Add(time.Duration(i)*50*time.Millisecond), i+2, true, time.Millisecond, false)
	}
	g.Suspend()
	if g.Presented(now.Add(150*time.Millisecond), 4) || g.Status().State != "recovering" {
		t.Fatal("focus/geometry pause committed an unshown frame")
	}
	if g.ObserveFrame(now.Add(200*time.Millisecond), 4, false, time.Millisecond, true) {
		t.Fatal("reopened capture inherited the previous candidate")
	}
}

func TestCaptureRecoveryResizeStartsNewEpochWithoutReusingOldGeneration(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	size := geometry.Size{W: 960, H: 720}
	g.ObserveSourceSize(size)
	g.ObserveFrame(now, 1, true, time.Second, false)
	for i := uint64(0); i < 3; i++ {
		g.ObserveFrame(now.Add(time.Duration(i)*50*time.Millisecond), i+2, true, time.Millisecond, false)
	}
	g.Presented(now.Add(120*time.Millisecond), 4)
	// A static image from this session can normally be reused indefinitely.
	if !g.ObserveFrame(now.Add(time.Second), 4, false, time.Second, true) {
		t.Fatal("fixture did not accept validated idle content")
	}
	g.ObserveSourceSize(geometry.Size{W: 1920, H: 1080})
	if g.ObserveFrame(now.Add(2*time.Second), 4, false, time.Second, true) {
		t.Fatal("new resolution reused the old accepted generation")
	}
	if got := g.Status(); got.State != "recovering" || got.Failures != 2 {
		t.Fatalf("new allocation inherited the previous resolution relapse window: %+v", got)
	}
	// Native reopened capture serials start again; three fresh frames are needed.
	for i := uint64(0); i < 3; i++ {
		usable := g.ObserveFrame(now.Add(3*time.Second+time.Duration(i)*50*time.Millisecond), i+1, true, time.Millisecond, false)
		if usable != (i == 2) {
			t.Fatalf("new epoch sample %d usable=%v", i, usable)
		}
	}
	if !g.Presented(now.Add(3200*time.Millisecond), 3) {
		t.Fatal("new resolution did not recover after a fresh complete presentation")
	}
}

func TestCaptureRecoverySameSizeAndBlockedResizeDoNotClearFailures(t *testing.T) {
	var g captureRecoveryGate
	now := time.Unix(100, 0)
	size := geometry.Size{W: 960, H: 720}
	g.ObserveSourceSize(size)
	g.ObserveFrame(now, 1, true, time.Second, false)
	for i := uint64(0); i < 3; i++ {
		g.ObserveFrame(now.Add(time.Duration(i)*50*time.Millisecond), i+2, true, time.Millisecond, false)
	}
	g.Presented(now.Add(120*time.Millisecond), 4)
	g.Suspend() // Alt-Tab, tray or position-only change
	g.ObserveSourceSize(size)
	if g.ObserveFrame(now.Add(time.Second), 1, true, time.Second, false) || g.Status().State != "blocked" {
		t.Fatal("same-size reopen bypassed rapid relapse protection")
	}
	for _, changed := range []geometry.Size{{W: 1280, H: 720}, {W: 1920, H: 1080}, {}} {
		g.ObserveSourceSize(changed)
		if g.Status().State != "blocked" || g.ObserveFrame(now.Add(2*time.Second), 1, true, time.Millisecond, false) {
			t.Fatal("resize or invalid bounds cleared an existing failure latch")
		}
	}
}
