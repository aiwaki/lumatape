#include "capture.h"

#include <array>
#include <cstddef>
#include <cstdio>
#include <cstring>

static_assert(offsetof(VhsCaptureFrame, texture) == 4);
static_assert(offsetof(VhsCaptureFrame, source_x) == 16);
static_assert(offsetof(VhsCaptureFrame, timestamp_100ns) == 32);
static_assert(offsetof(VhsCaptureFrame, age_100ns) == 40);
static_assert(offsetof(VhsCaptureFrame, serial) == 48);
static_assert(offsetof(VhsCaptureStats, readback_wait_100ns) == 8);
static_assert(offsetof(VhsCaptureStats, cpu_transfer_100ns) == 24);
static_assert(offsetof(VhsCaptureStats, bytes_per_frame) == 32);
static_assert(offsetof(VhsCaptureTelemetryV1, acquire_sequence) == 8);
static_assert(offsetof(VhsCaptureTelemetryV1, uploads_this_acquire) == 32);
static_assert(offsetof(VhsCaptureTelemetryV1, incoming_age_100ns) == 40);
static_assert(offsetof(VhsCaptureTelemetryV1, acquire_call_100ns) == 112);
static_assert(offsetof(VhsCaptureTelemetryV1, source_idle_100ns) == 128);

int main() {
    auto check = [](bool condition, const char* description) {
        if (!condition) std::fprintf(stderr, "ABI smoke failed: %s\n", description);
        return condition;
    };
    if (!check(vhs_capture_abi_version() == 1, "ABI version")) return 1;
    VhsCapture* capture = nullptr;
    std::array<char, 256> error{};
    if (!check(vhs_capture_close(&capture, error.data(), static_cast<uint32_t>(error.size())) == 1,
        "close a null handle") || !check(capture == nullptr, "close preserves null")) return 1;
    if (!check(vhs_capture_close(&capture, nullptr, 0) == 1, "repeated close without error buffer")) return 1;

    // This test intentionally creates no GL context, window, capture session or
    // graphics device. Opening must fail safely before the app can be covered.
    error.fill('\x7f');
    if (!check(vhs_capture_open(0, &capture, error.data(), static_cast<uint32_t>(error.size())) == -1,
        "open without a current GL context fails") ||
        !check(capture == nullptr, "failed open leaves no capture handle") ||
        !check(error[0] != '\0' && std::memchr(error.data(), '\0', error.size()), "bounded error string")) return 1;
    std::array<char, 2> tiny{{'x', 'y'}};
    if (!check(vhs_capture_open(0, &capture, tiny.data(), 1) == -1, "one-byte error buffer") ||
        !check(tiny[0] == '\0' && tiny[1] == 'y', "error terminator without overflow")) return 1;
    if (!check(vhs_capture_open_ex(0, VHS_CAPTURE_TRANSFER_COMPATIBILITY, &capture, error.data(),
        static_cast<uint32_t>(error.size())) == -1, "compatibility still requires a GL context") ||
        !check(capture == nullptr, "failed compatibility open leaves no capture handle")) return 1;
    if (!check(vhs_capture_open_ex(0, 99, &capture, error.data(), static_cast<uint32_t>(error.size())) == -1,
        "unknown transfer mode rejected") || !check(capture == nullptr, "invalid transfer leaves no handle")) return 1;
    VhsCaptureStats stats{};
    stats.struct_size = sizeof(stats);
    if (!check(vhs_capture_get_stats(nullptr, &stats, error.data(), static_cast<uint32_t>(error.size())) == -1,
        "statistics rejects a null capture handle")) return 1;
    VhsCaptureTelemetryV1 telemetry{};
    telemetry.struct_size = sizeof(telemetry);
    telemetry.version = 99;
    if (!check(vhs_capture_get_telemetry_v1(nullptr, &telemetry, error.data(), static_cast<uint32_t>(error.size())) == -1,
        "telemetry rejects a null capture handle") ||
        !check(telemetry.version == 99, "failed telemetry read leaves output untouched") ||
        !check(vhs_capture_get_telemetry_v1(nullptr, nullptr, nullptr, 0) == -1,
        "telemetry rejects null output without error buffer")) return 1;
    VhsCaptureFrame frame{};
    frame.struct_size = sizeof(frame);
    if (!check(vhs_capture_acquire(nullptr, &frame, error.data(), static_cast<uint32_t>(error.size())) == -1,
        "acquire rejects a null handle") ||
        !check(vhs_capture_release(nullptr, nullptr, 0) == -1, "release rejects a null handle") ||
        !check(vhs_capture_close(nullptr, nullptr, 0) == -1, "close rejects a null handle pointer")) return 1;
    std::puts("Capture DLL ABI, exports and failure boundaries passed (no GPU/runtime qualification).");
    return 0;
}
