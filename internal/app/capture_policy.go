package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func validateSnapshotRequest(c config.Config, path string, after, maxFrames int) error {
	if path == "" {
		return nil
	}
	if c.Mode != "full" {
		return fmt.Errorf("--snapshot requires --mode full; Lightweight cannot provide an image of the game")
	}
	if after < 1 || (maxFrames > 0 && after > maxFrames) {
		return fmt.Errorf("--snapshot-after-frames must be positive and no larger than --frames")
	}
	return nil
}

func compatibilityCapture(c config.Config) bool {
	return c.Mode == "full" && c.Capture.Transfer == config.TransferCompatibility
}

func captureTransfer(c config.Config) capture.Transfer {
	if c.Capture.Transfer == config.TransferCompatibility {
		return capture.TransferCompatibility
	}
	return capture.TransferGPU
}

func presentationInterval(c config.Config, refreshHz uint32) time.Duration {
	if refreshHz < 2 || refreshHz > 1000 {
		refreshHz = 60
	}
	if compatibilityCapture(c) && refreshHz > 30 {
		refreshHz = 30
	}
	return time.Second / time.Duration(refreshHz)
}

// Fallback never changes the saved preference and never hides unrelated WGC,
// window, HDR or resource cleanup failures behind a different capture path.
func shouldFallbackCapture(preference, attempted string, err error) bool {
	if preference != config.TransferAuto || attempted != config.TransferGPU || err == nil {
		return false
	}
	var backend *BackendUnavailableError
	return isMissingGPUInterop(err) || (errors.As(err, &backend) && backend.Code == "gpu_interop_unavailable")
}
