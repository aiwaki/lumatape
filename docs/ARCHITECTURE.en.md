# LumaTape architecture

[Русский](ARCHITECTURE.md) | [English](ARCHITECTURE.en.md)

LumaTape processes the image of a Windows SDR game running in a window or
borderless mode. The game remains the source of native keyboard and mouse input.
The target is Windows x64, Windows 10 build 19041 or later and OpenGL 3.3.
Development on macOS does not imply a macOS product.

## Native tray and engine

The user interface is a native **Tauri 2 tray menu**. No application panel,
WebView or localhost server is started. Rust owns the menu, file dialogs,
clipboard access and update coordination. Go/Win32 owns capture, rendering,
configuration, hotkeys and recovery. Retained React/Vite sources are development
history, not the shipped interface.

The host launches only the bundled engine at its fixed resource path and uses
private stdin/stdout JSONL pipes. The engine's hidden `LumaTape.Control` window
handles native messages without creating another tray icon. Requests carry
configuration revision and emergency sequence checks; stale applies cannot
re-enable an effect after emergency off. Requested settings, applied state and
backend capabilities remain separate. A timeout requires reading actual state,
without assuming a rollback.

A second application launch uses the existing instance. Quit waits for acknowledged
engine cleanup and process exit. Losing the host pipe also initiates engine
cleanup. Errors remain accessible from the tray. Russian Windows UI language
selects RU; other UI languages select EN. That choice is passed to child engine
and testcard processes without changing stored user values or source titles.

Dependencies are locked with Cargo/npm lockfiles. The minimal tray asset directory
is embedded by Tauri; no browser resources are downloaded at runtime. The package
includes license texts selected by verified manifests, public documentation and
example shaders. Local checkpoints, screenshots, logs and archived design
experiments are excluded.

## Window and rendering ownership

The initial Go goroutine is pinned to its OS thread. That thread owns GLFW,
OpenGL, rendering and native window messages. Configuration commands are bounded
and processed between frames; emergency commands take priority. No background
loop repeatedly raises or focuses the game.

An overlay remains hidden until shader, transparency, click-through and hotkey
setup succeeds. It follows the selected source's client bounds and focus. Menu
interaction can pause presentation; returning focus to the game is a user action.
Moving or resizing the source invalidates unstable geometry. A new frame must
match the current geometry before the surface returns.

Lightweight draws transparent scanlines/noise over the source without reading its
pixels. Full captures the chosen HWND with Windows Graphics Capture, then renders
one opaque GLSL pass. Full monitor capture, HDR and protected-capture bypass are
not supported.

## Capture paths

GPU processing copies a fresh frame to an owned texture and uses
`WGL_NV_DX_interop2` for OpenGL access. The driver must successfully register and
lock a real compatible resource; extension names and DLL presence alone are not
sufficient. This path includes a GPU copy and is not described as zero-copy.

Full CPU compatibility uses WGC → D3D11 staging → nonblocking Map → BGRA upload to
OpenGL. There is at most one pending staging copy and a bounded pool of capture
frames. Row pitch and GL unpack state are handled explicitly. Upload can still
wait inside the driver. CPU submission is capped at 30 FPS, not guaranteed to
sustain that rate.

The desktop application's Automatic setting tries GPU and allows CPU fallback only for
classified interop failures. An explicit GPU selection does not silently fall
back. Source, device and HDR failures retain their specific cause. Failed
transitions preserve the previous working state where possible; without one, the
tray remains available with the presentation surface hidden.

Freshness and geometry are checked before presentation. Closing a source, losing
capture, stale frames and device errors must hide unsuitable output and release
ownership. Full pixels do not accumulate across frames. GPU shader time,
submission, transfer and captured-frame age are separate measurements.

## Effects, shape and clicks

Five presets are included: Subtle CRT, CRT Classic, Soft TV, VHS Light and VHS
Tape. Custom GLSL effects use the versioned [shader contract](SHADER_SPEC.en.md).
Import compiles before activation and stores an immutable local asset only after
validation. Selecting a preset does not change the source, format or enabled
state. Selecting a full effect from Lightweight chooses the compatible Full Auto
path. The tray uses full shader strength and has no manual parameter editor.

Screen shape is independent: Flat, Rounded CRT or Curved CRT. Rounded edges do
not displace pixels. The built-in curved shape can project the system cursor
appearance onto the image while leaving Windows coordinates, native mouse
messages, Raw Input and mouse capture unchanged. A separate worker restores the
ordinary cursor if its owner stops or the lease expires.

**Accurate clicks** permits the known built-in curvature mapping. **Allow
arbitrary distortion** also permits Crop, Stretch, custom aspect ratio and custom
`warp` shaders. Both modes support keyboard and mouse; arbitrary transformations
may misalign visible click targets. There is no general inverse mapping for an
arbitrary shader. The [pointer module](../internal/pointer/README.en.md) describes
cursor limitations and recovery.

## Format and recovery

The tray offers the original format or a temporary Windows resolution. Masks and
automatic game-window resizing are retired controls. Their legacy configuration
and recovery records remain readable; they are not silently discarded. For
ordinary 4:3 play, prefer the game's own resolution setting. A display mode alone
does not guarantee the game's FOV, internal render size or monitor scaling.

Windows modes must come from enumeration and pass candidate validation. A change
requires confirmation within 15 seconds and is guarded by an independent
watchdog. Restoration checks ownership; subsequent user display changes are not
overwritten. Legacy window recovery uses process identity and a per-window nonce,
records state before mutation and checks actual geometry after application. An
unresolved rollback remains in the recovery journal.

## Updates and diagnostics

Updates use a dedicated LumaTape trust root, bounded HTTPS metadata/downloads,
immutable version identity and signature verification before cleanup and
replacement. Only a supported current-user NSIS installation can update in
place; a portable ZIP can check but must be replaced manually. The detailed
transaction and release protocol is in [UPDATES.md](UPDATES.en.md).

Structured logs use a bounded queue and continuous rotation: current file plus
three backups, each up to 4 MiB. Dropped events and write errors are counted.
Clipboard diagnostics omit full paths, foreign window titles and screenshots by
default and are never uploaded automatically. Shader time is not labeled total
input-to-photon latency.

Portable rules/geometry tests, Mac shader readback, Windows CPU compatibility,
hardware GPU interop, physical input and installer updates are distinct evidence
layers. Use the [Windows protocol](WINDOWS_VALIDATION.en.md) and
[Parallels protocol](PARALLELS_SMOKE.en.md); release checksums identify the tested
artifact. Passing unit tests does not qualify all games, drivers or displays.
