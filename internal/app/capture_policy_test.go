package app

import (
	"testing"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func TestCompatibilityPacingIsExplicitAndCapped(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	if compatibilityCapture(c) || captureTransfer(c) != capture.TransferGPU || presentationInterval(c, 144) != time.Second/144 {
		t.Fatal("default Full silently enabled CPU transfer or changed GPU pacing")
	}
	c.Capture.Transfer = config.TransferCompatibility
	for _, hz := range []uint32{0, 1, 30, 60, 144, 1000, 1001} {
		if !compatibilityCapture(c) || captureTransfer(c) != capture.TransferCompatibility || presentationInterval(c, hz) != time.Second/30 {
			t.Fatalf("compatibility not capped at 30 FPS for %d Hz", hz)
		}
	}
	if presentationInterval(c, 20) != time.Second/20 {
		t.Fatal("compatibility exceeded slower monitor refresh")
	}
	c.Mode = "overlay"
	if compatibilityCapture(c) || presentationInterval(c, 144) != time.Second/144 {
		t.Fatal("stored CPU preference changed Lightweight pacing")
	}
}

func TestSnapshotsCannotMasqueradeAsFull(t *testing.T) {
	c := config.Default()
	if validateSnapshotRequest(c, "snapshot.png", 60, 90) == nil {
		t.Fatal("Lightweight snapshot accepted as game image")
	}
	c.Mode = "full"
	if err := validateSnapshotRequest(c, "snapshot.png", 60, 90); err != nil {
		t.Fatal(err)
	}
	if validateSnapshotRequest(c, "snapshot.png", 0, 90) == nil || validateSnapshotRequest(c, "snapshot.png", 60, 30) == nil {
		t.Fatal("impossible snapshot threshold accepted")
	}
	if err := validateSnapshotRequest(c, "", 0, 0); err != nil {
		t.Fatal("snapshot constraints leaked into ordinary rendering")
	}
}
