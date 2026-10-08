// Package capture exposes the native WGC bridge with an explicit transfer
// choice. The GPU path never falls back to CPU implicitly.
package capture

type Transfer uint32

const (
	TransferGPU Transfer = iota
	TransferCompatibility
)

type Frame struct {
	StructSize, Texture, Width, Height uint32
	X, Y                               int32
	Updated, Dropped                   uint32
	Timestamp100ns, Age100ns           int64
	Serial                             uint64
}

// Stats describe the last newly transferred frame. ReadbackWait includes time
// between polls and scheduling, not CPU work. CPUTransfer is wall time spent in
// successful Map, upload and Unmap calls, not a CPU profiler or GPU completion.
type Stats struct {
	StructSize        uint32
	Transfer          Transfer
	ReadbackWait100ns int64
	UploadCall100ns   int64
	CPUTransfer100ns  int64
	BytesPerFrame     uint64
}

// TelemetryV1 describes the last Acquire call, including ready=false or an
// error. Durations are wall times in 100 ns units, not GPU execution time.
// IncomingAge is -1 without a dequeued frame; SourceIdle is -1 before the
// first dequeue. These counters never change capture or pacing decisions.
type TelemetryV1 struct {
	StructSize, Version                 uint32
	AcquireSequence, SourceSerial       uint64
	UploadsTotal                        uint64
	UploadsThisAcquire, PendingReadback uint32
	IncomingAge100ns, MapCall100ns      int64
	GLPrepare100ns, UploadCall100ns     int64
	GLRestore100ns, UnmapCall100ns      int64
	FrameClose100ns, CopySubmit100ns    int64
	FlushCall100ns, AcquireCall100ns    int64
	PendingAge100ns, SourceIdle100ns    int64
}
