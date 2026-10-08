package app

import (
	"time"

	"github.com/aiwaki/lumatape/internal/platform/capture"
)

// Bound repeated diagnostics independently of rendering and capture policy.
// Suppressed events are counted, rather than silently discarded.
type captureEventGate struct {
	last       time.Time
	suppressed uint64
}

func (g *captureEventGate) allow(now time.Time) (bool, uint64) {
	if !g.last.IsZero() && now.Sub(g.last) < time.Second {
		g.suppressed++
		return false, 0
	}
	n := g.suppressed
	g.last, g.suppressed = now, 0
	return true, n
}

func captureTelemetryFields(s *capture.TelemetryV1) any {
	if s == nil {
		return nil
	}
	optionalMS := func(n int64) any {
		if n < 0 {
			return nil
		}
		return float64(n) / 1e4
	}
	return map[string]any{
		"version": s.Version, "acquire_sequence": s.AcquireSequence, "source_serial": s.SourceSerial,
		"uploads_total": s.UploadsTotal, "uploads_this_acquire": s.UploadsThisAcquire,
		"pending_readback": s.PendingReadback != 0, "incoming_age_ms": optionalMS(s.IncomingAge100ns),
		"map_call_ms": float64(s.MapCall100ns) / 1e4, "gl_prepare_ms": float64(s.GLPrepare100ns) / 1e4,
		"upload_call_ms": float64(s.UploadCall100ns) / 1e4, "gl_restore_ms": float64(s.GLRestore100ns) / 1e4,
		"unmap_call_ms": float64(s.UnmapCall100ns) / 1e4, "frame_close_ms": float64(s.FrameClose100ns) / 1e4,
		"copy_submit_ms": float64(s.CopySubmit100ns) / 1e4, "flush_call_ms": float64(s.FlushCall100ns) / 1e4,
		"acquire_call_ms": float64(s.AcquireCall100ns) / 1e4, "pending_age_ms": float64(s.PendingAge100ns) / 1e4,
		"source_idle_ms": optionalMS(s.SourceIdle100ns),
	}
}
