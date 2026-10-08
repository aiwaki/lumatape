#pragma once

#include <stdint.h>

#if defined(_WIN32)
#if defined(VHS_CAPTURE_BUILD)
#define VHS_API __declspec(dllexport)
#else
#define VHS_API __declspec(dllimport)
#endif
#define VHS_CALL __cdecl
#else
#define VHS_API
#define VHS_CALL
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef struct VhsCapture VhsCapture;

// ABI v1, exactly 56 bytes on Windows x64. No native pointers in frame data.
typedef struct VhsCaptureFrame {
    uint32_t struct_size;       // Caller sets sizeof(VhsCaptureFrame).
    uint32_t texture;           // GL_TEXTURE_2D; valid only until release/close.
    uint32_t width;             // Physical pixels of the cropped client area.
    uint32_t height;
    int32_t source_x;           // Physical desktop client origin; may be negative.
    int32_t source_y;
    uint32_t updated;           // 1 = a new captured frame, 0 = previous texture.
    uint32_t dropped;           // Older queued frames discarded during this call.
    int64_t timestamp_100ns;    // Compositor QPC timestamp, in 100 ns units.
    int64_t age_100ns;          // QPC now - timestamp; not input-to-photon latency.
    uint64_t serial;            // Increases only when a new frame is copied.
} VhsCaptureFrame;

enum VhsCaptureTransfer {
    VHS_CAPTURE_TRANSFER_GPU = 0,
    VHS_CAPTURE_TRANSFER_COMPATIBILITY = 1
};

// Additive ABI v1 extension, exactly 40 bytes. Metrics describe the most recent
// updated compatibility frame; zero before the first upload or in GPU mode.
typedef struct VhsCaptureStats {
    uint32_t struct_size;
    uint32_t transfer;
    int64_t readback_wait_100ns; // Copy submission -> Map ready; includes polling.
    int64_t upload_call_100ns;   // Wall duration of glTexSubImage2D, not GPU time.
    int64_t cpu_transfer_100ns;  // Successful Map -> upload -> Unmap wall duration.
    uint64_t bytes_per_frame;   // Client width * height * 4, excludes row padding.
} VhsCaptureStats;

// Optional, versioned diagnostic extension. Existing frame/stats ABI is unchanged.
// All durations are QPC wall times in 100 ns units, NOT GPU execution time.
// Phase durations describe the last Acquire call (including ready=0), not the
// last displayed frame. A phase not reached is zero. incoming_age is -1 when
// nothing was dequeued; source_idle is -1 before the first dequeued WGC frame.
typedef struct VhsCaptureTelemetryV1 {
    uint32_t struct_size;
    uint32_t version;                 // Always 1.
    uint64_t acquire_sequence;
    uint64_t source_serial;
    uint64_t uploads_total;
    uint32_t uploads_this_acquire;
    uint32_t pending_readback;
    int64_t incoming_age_100ns;
    int64_t map_call_100ns;
    int64_t gl_prepare_100ns;
    int64_t upload_call_100ns;
    int64_t gl_restore_100ns;
    int64_t unmap_call_100ns;
    int64_t frame_close_100ns;
    int64_t copy_submit_100ns;
    int64_t flush_call_100ns;
    int64_t acquire_call_100ns;
    int64_t pending_age_100ns;
    int64_t source_idle_100ns;
} VhsCaptureTelemetryV1;

// All functions except abi_version must run on the creating thread with its
// original OpenGL context current. The host must be per-monitor-DPI-aware.
// UTF-8 errors are bounded and always NUL-terminated when capacity > 0.
VHS_API uint32_t VHS_CALL vhs_capture_abi_version(void);
// Window-only WGC. Rejects current-process windows, child windows, HDR/Advanced
// Color, missing cursor-disable API, and drivers without a working GPU interop.
// On failure normally *out_capture remains NULL. If partial initialization
// cleanup also fails, it retains a non-NULL handle: Close it and treat a failed
// Close as fatal instead of unloading the DLL or destroying its GL context.
VHS_API int32_t VHS_CALL vhs_capture_open(uintptr_t hwnd, VhsCapture** out_capture,
    char* error, uint32_t error_capacity);
// Explicit opt-in only. GPU=0 is identical to open. Compatibility=1 uses one
// D3D11 staging readback and a CPU->GL upload; slower, never an automatic fallback.
// The same SDR/window/cursor/context and cleanup requirements apply to both.
VHS_API int32_t VHS_CALL vhs_capture_open_ex(uintptr_t hwnd, uint32_t transfer,
    VhsCapture** out_capture, char* error, uint32_t error_capacity);
VHS_API int32_t VHS_CALL vhs_capture_get_stats(VhsCapture* capture,
    VhsCaptureStats* stats, char* error, uint32_t error_capacity);
VHS_API int32_t VHS_CALL vhs_capture_get_telemetry_v1(VhsCapture* capture,
    VhsCaptureTelemetryV1* telemetry, char* error, uint32_t error_capacity);
// 1: GL owns the texture, release REQUIRED after drawing; 0: nothing suitable
// yet (startup/resize/minimized); -1: stop presenting and report error.
// The cropped texture's top row is v=0 (D3D convention). It is BGRA UNORM SDR,
// sampled as normal RGBA by GL, containing sRGB-encoded values, NOT GL_SRGB8.
// Frame retrieval/Map probe do not wait; GPU ownership transfer or CPU upload
// may synchronize the driver. Compatibility is intentionally a slower path.
VHS_API int32_t VHS_CALL vhs_capture_acquire(VhsCapture* capture,
    VhsCaptureFrame* frame, char* error, uint32_t error_capacity);
VHS_API int32_t VHS_CALL vhs_capture_release(VhsCapture* capture,
    char* error, uint32_t error_capacity);
// Idempotent for *capture == NULL. On successful close, writes NULL. Close
// before destroying the host GL context. Returns -1 on wrong thread/context.
// A failed close retains the handle; do not unload this DLL or destroy its GL
// context while resources remain owned. Report the error and terminate safely.
VHS_API int32_t VHS_CALL vhs_capture_close(VhsCapture** capture,
    char* error, uint32_t error_capacity);

#ifdef __cplusplus
}
static_assert(sizeof(VhsCaptureFrame) == 56, "Capture ABI layout changed");
static_assert(sizeof(VhsCaptureStats) == 40, "Capture statistics ABI layout changed");
static_assert(sizeof(VhsCaptureTelemetryV1) == 136, "Capture telemetry v1 ABI layout changed");
#endif
