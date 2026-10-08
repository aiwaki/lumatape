package capture

import (
	"testing"
	"unsafe"
)

func TestNativeFrameAndStatisticsABI(t *testing.T) {
	var frame Frame
	var stats Stats
	if unsafe.Sizeof(frame) != 56 || unsafe.Offsetof(frame.Timestamp100ns) != 32 || unsafe.Offsetof(frame.Serial) != 48 {
		t.Fatal("capture Frame no longer matches native ABI v1")
	}
	if unsafe.Sizeof(stats) != 40 || unsafe.Offsetof(stats.ReadbackWait100ns) != 8 || unsafe.Offsetof(stats.CPUTransfer100ns) != 24 || unsafe.Offsetof(stats.BytesPerFrame) != 32 {
		t.Fatal("capture Stats no longer matches additive native export")
	}
	if TransferGPU != 0 || TransferCompatibility != 1 {
		t.Fatal("transfer choices no longer match native ABI")
	}
}

func TestNativeTelemetryV1ABI(t *testing.T) {
	var s TelemetryV1
	if unsafe.Sizeof(s) != 136 || unsafe.Offsetof(s.AcquireSequence) != 8 ||
		unsafe.Offsetof(s.UploadsThisAcquire) != 32 || unsafe.Offsetof(s.IncomingAge100ns) != 40 ||
		unsafe.Offsetof(s.AcquireCall100ns) != 112 || unsafe.Offsetof(s.SourceIdle100ns) != 128 {
		t.Fatal("capture telemetry no longer matches optional native v1 export")
	}
}
