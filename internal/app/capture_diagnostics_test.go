package app

import (
	"testing"
	"time"

	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func TestCaptureEventGateBoundsRepeatedLogsAndCountsSuppressed(t *testing.T) {
	var g captureEventGate
	now := time.Unix(100, 0)
	if ok, n := g.allow(now); !ok || n != 0 {
		t.Fatal("first observation was suppressed")
	}
	for i := 1; i < 100; i++ {
		if ok, _ := g.allow(now.Add(time.Duration(i) * time.Millisecond)); ok {
			t.Fatal("repeated observation escaped rate limit")
		}
	}
	if ok, n := g.allow(now.Add(time.Second)); !ok || n != 99 {
		t.Fatalf("suppressed events not accounted for: allowed=%v skipped=%d", ok, n)
	}
}

func TestCaptureTelemetryDistinguishesMissingSourceFromZeroDuration(t *testing.T) {
	if captureTelemetryFields(nil) != nil {
		t.Fatal("old DLL must report unavailable telemetry")
	}
	d := captureTelemetryFields(&capture.TelemetryV1{Version: 1, IncomingAge100ns: -1,
		SourceIdle100ns: -1, PendingReadback: 1, GLPrepare100ns: 25000}).(map[string]any)
	if d["incoming_age_ms"] != nil || d["source_idle_ms"] != nil || d["pending_readback"] != true ||
		d["gl_prepare_ms"] != 2.5 || d["upload_call_ms"] != float64(0) {
		t.Fatalf("lost telemetry availability or units: %#v", d)
	}
}
