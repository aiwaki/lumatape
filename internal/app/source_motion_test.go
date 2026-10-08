package app

import (
	"testing"
	"time"

	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestSourceMotionWaitsForDragToEndEvenWithoutPixelMovement(t *testing.T) {
	var g sourceMotionGate
	t0 := time.Unix(100, 0)
	bounds := geometry.Rect{X: -960, Y: 60, W: 960, H: 720}
	if g.Observe(1, bounds, false, t0) || !g.Observe(1, bounds, false, t0.Add(sourceSettleTime)) {
		t.Fatal("stationary source did not settle")
	}
	for _, elapsed := range []time.Duration{time.Second, 5 * time.Second, 10 * time.Second} {
		if g.Observe(1, bounds, true, t0.Add(elapsed)) {
			t.Fatal("drag with unchanged bounds allowed an opaque capture")
		}
	}
	if g.Observe(1, bounds, false, t0.Add(10*time.Second+sourceSettleTime-1)) {
		t.Fatal("resumed before post-drag settle period")
	}
	if !g.Observe(1, bounds, false, t0.Add(10*time.Second+sourceSettleTime)) {
		t.Fatal("did not resume after drag ended and geometry settled")
	}
}

func TestSourceMotionDetectsProgrammaticMoveResizeAndTargetChange(t *testing.T) {
	var g sourceMotionGate
	now := time.Unix(100, 0)
	bounds := geometry.Rect{X: 20, Y: 30, W: 960, H: 720}
	g.Observe(1, bounds, false, now)
	now = now.Add(time.Second)
	if !g.Observe(1, bounds, false, now) {
		t.Fatal("source not settled")
	}
	for _, changed := range []geometry.Rect{
		{X: -1200, Y: 30, W: 960, H: 720},
		{X: -1200, Y: -100, W: 960, H: 720},
		{X: -1200, Y: -100, W: 1280, H: 1024},
	} {
		now = now.Add(time.Second)
		if g.Observe(1, changed, false, now) || g.Observe(1, changed, false, now.Add(100*time.Millisecond)) {
			t.Fatal("programmatic geometry change was presented immediately")
		}
		if !g.Observe(1, changed, false, now.Add(sourceSettleTime)) {
			t.Fatal("settled programmatic geometry never resumed")
		}
		bounds = changed
	}
	if g.Observe(2, bounds, false, now.Add(time.Second)) {
		t.Fatal("new target inherited previous source stability")
	}
	g.Reset()
	if g.Observe(2, bounds, false, now.Add(2*time.Second)) {
		t.Fatal("hidden source resumed without a fresh stability observation")
	}
}

func TestSourceMotionRejectsInvalidGeometryAndBackwardClock(t *testing.T) {
	for _, bounds := range []geometry.Rect{{}, {W: 0, H: 10}, {W: 10, H: -1}} {
		var g sourceMotionGate
		g.Observe(1, bounds, false, time.Unix(100, 0))
		if g.Observe(1, bounds, false, time.Unix(101, 0)) {
			t.Fatal("invalid bounds settled")
		}
	}
	var g sourceMotionGate
	bounds := geometry.Rect{W: 960, H: 720}
	g.Observe(1, bounds, false, time.Unix(100, 0))
	if g.Observe(1, bounds, false, time.Unix(99, 0)) {
		t.Fatal("backward clock retained a stale deadline")
	}
}

func TestSourceGeometryRejectsCoordinatesThatAgeDuringTransferOrDraw(t *testing.T) {
	original := geometry.Rect{X: -1200, Y: 40, W: 960, H: 720}
	moved := original
	moved.X += 12
	resized := original
	resized.H++
	for _, sample := range []struct{ before, captured, current geometry.Rect }{
		{original, original, moved}, // moved while readback/upload was running
		{moved, original, moved},    // stale native coordinates despite fresh host sample
		{original, moved, moved},    // moved between host and native samples
		{original, original, resized},
	} {
		if sourceGeometryMatches(sample.before, sample.captured, sample.current) {
			t.Fatal("accepted misaligned opaque presentation")
		}
	}
	if !sourceGeometryMatches(original, original, original) {
		t.Fatal("rejected matching physical coordinates on a negative-origin monitor")
	}
}
