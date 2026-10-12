# LumaTape architecture

[Русский](ARCHITECTURE.md) | [English](ARCHITECTURE.en.md)

LumaTape processes the image of a Windows SDR game running in a window or
borderless mode. The game remains the source of native keyboard and mouse input.
The target is Windows x64, Windows 10 build 19041 or later and OpenGL 3.3.
Development on macOS does not imply a macOS product.

## Tray, settings and engine

At startup, **Tauri 2** creates the native tray. Settings are created on demand
and load bundled HTML/CSS/JavaScript from `desktop/tray-assets/`. Closing the
window destroys its WebView; there is no localhost server. Rust owns the window,
menu, file dialogs, clipboard access and update coordination. Go/Win32 owns
capture, rendering, configuration, hotkeys and recovery. Retained React/Vite
sources are development history, not the shipped interface.

Settings call only `settings_snapshot` and `settings_action`. Allowed actions
share the tray's handler, lock and state checks; the page has no generic engine
RPC, process or filesystem access. Navigation is restricted to the local Tauri
origin. Closing cancels pending actions; if a display change is being prepared
or awaits confirmation, it performs an emergency off and restores changes. A
failed restoration keeps the window open with an error. Normal closure does not
turn off a running effect.

The host launches only the bundled engine at its fixed resource path and uses
private stdin/stdout JSONL pipes. The engine's hidden `LumaTape.Control` window
handles native messages without creating another tray icon. Requests carry
configuration revision and emergency sequence checks; stale applies cannot
re-enable an effect after emergency off. Requested settings, applied state and
backend capabilities remain separate. A timeout requires reading actual state,
without assuming a rollback.

A second launch opens settings in the existing instance. Quit waits for acknowledged
engine cleanup and process exit. Losing the host pipe also initiates engine
cleanup. Errors remain accessible from the tray. Russian Windows UI language
selects RU; other UI languages select EN. That choice is passed to child engine
and testcard processes without changing stored user values or source titles.

Dependencies are locked with Cargo/npm lockfiles. Tauri embeds the local settings
assets; page code and styles are not downloaded at runtime. The window needs
WebView2 Runtime. NSIS uses `embedBootstrapper`: the bundled bootstrapper downloads
the runtime if needed; a portable ZIP expects it to be installed already. The package
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
sustain that rate. Timer pacing includes frame work in its interval rather than
adding a full interval after rendering. Working VSync is not followed by a second
wait, and a late frame does not create a burst of catch-up frames.

The desktop application's Automatic setting tries GPU and allows CPU fallback only for
classified interop failures. An explicit GPU selection does not silently fall
back. Source, device and HDR failures retain their specific cause. Failed
transitions preserve the previous working state where possible; without one, the
tray remains available with the presentation surface hidden.

Freshness and geometry are checked before presentation. An empty WGC pool for a
static window is distinct from a slow transfer: only a frame previously accepted
fresh in that session can be reused, after native checks of the source, device,
geometry and absence of a pending CPU copy. Its age is not reset. Source closure,
capture loss and device errors do not permit continued display of an old texture.

A stale frame is hidden immediately. Resuming after that failure requires at
least three distinct fresh frames over at least 100 ms. A relapse within 10
seconds leaves presentation paused until an explicit retry; opening the tray
does not reset it. The reason is reported separately from focus loss. Full
pixels do not accumulate across frames.

## Effects, shape and clicks

Five presets are included: Subtle CRT, CRT Classic, Soft TV, VHS Light and VHS
Tape. Custom GLSL effects use the versioned [shader contract](SHADER_SPEC.en.md).
Import compiles before activation and stores an immutable local asset only after
validation. Selecting a preset does not change the source, format or enabled
state. Selecting a full effect from Lightweight chooses the compatible Full Auto
path. The tray and settings use full shader strength and have no manual parameter editor.

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

The primary 4:3 action resizes the client area once. Geometry uses physical pixels
and the monitor's work area, accounting for borders, menus, DPI, negative
coordinates and maximized windows. Borderless windows do not gain a border.
Recovery state is recorded before mutation, and actual geometry is checked
afterward. Partial failure rolls back; rollback errors are retained.

`format_active` is checked against the current client area, and `source_client`
contains its physical coordinates and size. If the game restores a wide window,
the format no longer counts as applied. Retrying requires explicit
`reapply_window_format` with both CAS preconditions; changing presets or the
same HWND/PID's title does not replay geometry changes. Restoration checks process
identity, a window nonce, outer bounds and client bounds to avoid overwriting
later changes.

Window and system methods do not add a 4:3 mask over the image. The legacy mask
method remains readable but is not offered in the interface. Neither window
resizing nor display-mode switching guarantees FOV, the game's internal render
size or monitor scaling; selecting 4:3 inside the game may still be necessary.

Windows modes must come from enumeration and pass candidate validation. Settings
open before the switch; afterward, a Keep/Revert prompt shows the time remaining.
The engine and independent watchdog enforce the 15-second deadline, not a
JavaScript timer. Confirmation belongs to a specific change; a stale response
or window closure cannot confirm a later change. Revert retains the effect's
enabled state after successful restoration; closing during confirmation performs
an emergency off. Restoration checks ownership; subsequent user display changes
are not overwritten. An unresolved rollback remains in the recovery journal.

## Updates and diagnostics

Updates use a dedicated LumaTape trust root, bounded HTTPS metadata/downloads,
immutable version identity and signature verification before cleanup and
replacement. Only a supported current-user NSIS installation can update in
place; a portable ZIP can check but must be replaced manually. The detailed
transaction and release protocol is in [UPDATES.md](UPDATES.en.md).

Structured logs use a bounded queue and continuous rotation: current file plus
three backups, each up to 4 MiB. Dropped events and write errors are counted.
Clipboard diagnostics omit full paths, foreign window titles and screenshots by
default and are never uploaded automatically. CPU submission, CPU work, GPU shader
time, CPU transfer and frame age are separate measurements. `present_fps` counts
completed presentations; `fresh_frame_fps` counts newly captured images that were
actually presented. Validity state, sample interval and observation time accompany
the rates; historical `last_sample` is separate from current values. These are
neither the game's FPS nor monitor refresh rate. Shader time is not labeled total
input-to-photon latency.

Portable rules/geometry tests, Mac shader readback, Windows CPU compatibility,
hardware GPU interop, physical input and installer updates are distinct evidence
layers. Use the [Windows protocol](WINDOWS_VALIDATION.en.md) and
[Parallels protocol](PARALLELS_SMOKE.en.md); release checksums identify the tested
artifact. Passing unit tests does not qualify all games, drivers or displays.
