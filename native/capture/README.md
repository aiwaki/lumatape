# LumaTape Windows capture bridge

`lumatape_capture.dll` is an optional x64 C++/WinRT bridge. The host remains Go/OpenGL
3.3. Build it through the root CMake project with Visual Studio 2022 C++ desktop
tools and Windows SDK **10.0.26100 or newer**, using C++20. Windows 10 version 2004, build
19041, is the minimum runtime because captured-cursor suppression first appears
there. The static MSVC runtime avoids a separate redistributable DLL dependency.

The implementation supports **SDR top-level window capture only**. Monitor
capture is absent. The default GPU transfer requires WGC and the exact `WGL_NV_DX_interop` plus
`WGL_NV_DX_interop2` extension tokens and entry points must be present. Hardware
adapters are probed by creating the same BGRA D3D11 texture used at runtime,
registering it with the current GL context, locking it, checking its dimensions,
and unlocking it. A matching vendor name or an extension string alone does not
qualify the adapter. A driver failure leaves Lightweight mode available in the
host; there is no hidden CPU screenshot fallback.

In GPU mode there is one GPU copy per updated frame: WGC surface → private client-area BGRA
texture → GL sampling under an interop lock. This is GPU-only, **not zero-copy**.
WGL lock/unlock transfers ownership and synchronizes GPU access; therefore a
lock can wait for the GPU even though `TryGetNextFrame` never waits for a frame.
The frame pool has two buffers and each host acquire drains at most two frames,
discarding the older one. No additional frame queue, rendering thread or
DispatcherQueue is created by this library. The WGC internal worker uses the
multithread-protected D3D immediate context.

`vhs_capture_open_ex` adds an explicit transfer selection without changing ABI
version 1 or the 56-byte frame. Transfer `0` is the default GPU path (also used
by the original `vhs_capture_open`); transfer `1` is **Compatibility (CPU)**.
There is no automatic fallback. Compatibility still captures the selected HWND
through WGC, but copies its client crop into a `D3D11_USAGE_STAGING` texture,
maps it for CPU reading and uploads BGRA bytes into an ordinary GL RGBA8 texture.
It needs no NV interop extension. The same SDR, target, cursor and geometry
checks remain enforced. This is a slower transfer path, not proof of GPU interop.

Compatibility retains at most one WGC frame while a staging copy is pending.
`Map(READ, DO_NOT_WAIT)` is tried once per acquire stage and does not wait for a
busy GPU; a still-busy copy returns no frame or reuses the prior uploaded frame.
The WGC frame stays checked out until the copy is ready. Row pitch is respected
with `GL_UNPACK_ROW_LENGTH`; unpack/PBO and texture bindings are restored after
the upload. The upload call itself can wait inside the driver. The host should
cap this mode's FPS and display its transfer cost explicitly.

Each acquire uploads at most one compatibility image. When its entry Map
completes a previous copy, the next copy may be submitted, but its Map/upload
waits for the next acquire. This avoids uploading two full images into the same
GL texture before either has been presented. It is a transfer-work bound, not a
claim that the virtual driver always meets the host's frame-age deadline.

The additive `vhs_capture_get_stats` export takes a 40-byte `VhsCaptureStats`.
For the most recent uploaded compatibility frame it reports readback wait
(copy submission to successful Map, including polling/scheduling), upload-call
wall time, successful Map-through-Unmap wall time, and client pixel bytes.
These are not GPU timings or CPU profiler samples. GPU mode and startup report
zero transfer metrics. The acquire/release pairing remains required in both
modes; compatibility uses a logical ownership guard instead of an NV lock.

The optional `vhs_capture_get_telemetry_v1` export accepts the separate 136-byte
`VhsCaptureTelemetryV1`; the existing ABI version, frame and stats layouts do
not change. It describes the most recent **Acquire call**, including a call
that returned no usable frame or an error. CPU phases distinguish the Map
call, GL state preparation, upload, GL state restoration/error check, Unmap,
source-frame close, copy submission and Flush. Phase wall times are summed
within an acquire and reset on the next call. `uploads_this_acquire` and the
cumulative `uploads_total` make redundant transfers observable. The acquire
sequence is cumulative; source serial counts successfully uploaded frames.
`incoming_age_100ns` is the dequeued WGC frame's age before readback (-1 when
none was dequeued). `source_idle_100ns` measures time since the latest WGC
dequeue (-1 before any frame), while `pending_age_100ns` measures the currently
pending readback's age. These distinguish upstream inactivity from a pending
copy, but do not prove why a source stopped producing content. GPU mode has
only acquire duration/sequence/serial; CPU phases remain zero/sentinels. Hosts
must treat a missing optional export as unavailable diagnostics for old DLLs.

The host must use per-monitor DPI awareness and keep every bridge call on its
initial rendering thread with the original WGL context current. Close capture
before destroying that context. See [capture.h](capture.h) for the C ABI. It is
designed for Go's Windows DLL calls without cgo. The internal `vhs_capture_*`
export names remain stable across the LumaTape rename. The frame struct is 56
bytes; the host sets `struct_size` before acquiring. Successful acquire returns an
owned GL lock, including when reusing the previous frame. Release after drawing.
On an error, stop presenting and close the bridge. Closing a null handle is safe.
Close attempts to stop acquisition and clean every safely releasable resource,
and reports its first error. If GPU unlock/unregister/device-close fails, it
retains those resources and the capture handle; the host must report the error
and exit without unloading the active DLL or destroying its GL context first.
The same rule applies if Open reports an error but returns a non-null handle:
that means partial-initialization cleanup failed. The handle stays alive for a
Close retry; it must not be silently discarded by the language binding.

Textures are client-area crops in physical pixels. WGC's captured extent must
match either the current client area or DWM's extended frame bounds; arbitrary
offsets are never guessed. During resize/DPI transitions mismatched frames are
dropped and the pool is recreated. Persistent unresolvable geometry becomes an
explicit error. Texture row `v=0` is the image's **top** row. Sampling returns
normal RGBA channels from BGRA storage. Values are sRGB-encoded UNORM, not an
sRGB GL texture; the shader must explicitly decode for linear-light processing
and encode on output. Alpha should not mix Full output with the original game.

`timestamp_100ns` is the compositor's QPC timestamp; `age_100ns` compares it with
QPC at acquire completion. It is the age of captured content, not total
input-to-photon latency. `updated` and `serial` distinguish repeated textures.
No performance or compatibility measurements are claimed by this source build.

Captured cursors are explicitly disabled and read back. The application does not
hide the system cursor. The OS capture border is left under system control.
Current-process windows are rejected. Closing/reusing a source HWND is detected
with the WGC Closed event and original PID/thread identity. Minimized/hidden
sources do not produce a usable texture. All intersected source displays must
report Advanced Color disabled; unknown color state is rejected conservatively.
Color state is rechecked when source geometry changes and once per second, so a
system color toggle can take up to one second to stop Full output. There is no
HDR conversion or automatic change to Windows HDR settings.

## Validation status

The complete root CMake Release project was cross-compiled and linked on the
macOS development machine with clang-cl/LLD 22.1.8, target
`x86_64-pc-windows-msvc`, Microsoft's SDK 10.0.26100.4654 and MSVC 14.44
CRT/STL, using C++20, `/EHsc`, `/MT`, `/permissive-` and `/W4` for the bridge.
All 27 build steps completed with zero compiler/linker diagnostics, producing
AMD64 PE `lumatape_capture.dll`, pinned GLFW 3.4 `glfw3.dll` and
`lumatape_capture_abi_smoke.exe`.
PE inspection confirms the original five C ABI exports and no separate MSVC runtime DLL
imports. Logs, exact commands, COFF objects, PE inspection and SHA256 hashes are
in [artifacts/native-cross-build](../../artifacts/native-cross-build/README.md).
This is **not** a Windows execution, native MSVC build or GPU runtime test.
The older SDK19041's bundled C++/WinRT headers failed inside their
coroutine/strict-lookup support; the modern **build SDK** requirement is separate
from the 19041 runtime floor.
With `BUILD_TESTING=ON`, `ctest --test-dir build/native -C Release` runs
`capture_abi_smoke` on Windows: DLL loading/exports, frame field offsets, null
close idempotence, failed-open cleanup and bounded error buffers. It creates no
window or GPU device and cannot establish capture, color or gameplay support.
This cross-build compiled and linked the EXE without executing it. Separate
Windows/VM results belong in [WINDOWS_VALIDATION.md](../../docs/WINDOWS_VALIDATION.md).
The C++ implementation needs the Windows CI/build gate and real hardware tests:
BGRA/color/orientation, hybrid adapters, bounded frame age, resize/DPI, capture
failure, Alt+Tab, source close, cursor, and repeated open/close. Devices without
the required interop must produce the explicit availability error. Verify the
driver behavior before treating Full mode as qualified for gameplay.

The compatibility extension and its expanded ABI smoke were separately
cross-compiled and linked with the same toolchain, four incremental steps with
zero diagnostics. Its DLL has seven exports (the five original functions plus
`open_ex` and `get_stats`). Logs, source hashes and PE inspection for this
extension are in `artifacts/full-validation/native`; runtime evidence is kept
separately in the validation document.

## API references

- [Windows Graphics Capture and frame ownership](https://learn.microsoft.com/en-us/windows/apps/develop/media-authoring-processing/screen-capture)
- [CreateForWindow](https://learn.microsoft.com/en-us/windows/win32/api/windows.graphics.capture.interop/nf-windows-graphics-capture-interop-igraphicscaptureiteminterop-createforwindow)
- [CreateFreeThreaded](https://learn.microsoft.com/en-us/uwp/api/windows.graphics.capture.direct3d11captureframepool.createfreethreaded)
- [IsCursorCaptureEnabled and minimum Windows build](https://learn.microsoft.com/en-us/uwp/api/windows.graphics.capture.graphicscapturesession.iscursorcaptureenabled)
- [Compositor QPC timestamp](https://learn.microsoft.com/en-us/uwp/api/windows.graphics.capture.direct3d11captureframe.systemrelativetime)
- [D3D11 immediate-context thread protection](https://learn.microsoft.com/en-us/windows/win32/api/d3d11_4/nn-d3d11_4-id3d11multithread)
- [WGL_NV_DX_interop2 resource rules](https://registry.khronos.org/OpenGL/extensions/NV/WGL_NV_DX_interop2.txt)
- [WGL_NV_DX_interop ownership and synchronization](https://registry.khronos.org/OpenGL/extensions/NV/WGL_NV_DX_interop.txt)
- [Advanced Color and HDR considerations](https://learn.microsoft.com/en-us/windows/win32/direct3darticles/high-dynamic-range)
- [D3D11 staging resource usage](https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_usage)
- [Map and DXGI_ERROR_WAS_STILL_DRAWING](https://learn.microsoft.com/en-us/windows/win32/api/d3d11/nf-d3d11-id3d11devicecontext-map)
- [Nonblocking Map flag](https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_map_flag)
- [OpenGL upload and PBO pointer semantics](https://registry.khronos.org/OpenGL-Refpages/gl4/html/glTexSubImage2D.xhtml)
- [OpenGL unpack row length and alignment](https://registry.khronos.org/OpenGL-Refpages/gl4/html/glPixelStore.xhtml)
