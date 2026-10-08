# Projected system cursor

[Русский](README.md) · English

This package changes only where the system cursor is drawn. The actual Windows
cursor position, mouse messages, Raw Input, capture, button state, mouse speed and
clip rectangle remain untouched. The image map locates the native hotspot on the
same curved image that the renderer last presented.

## Lifecycle

The renderer calls `Start`, `Update(Frame)` after a successful swap, `Stop` before
hiding/changing the surface, and `Close` on exit. `Update` retains one latest frame
and never writes a pipe on the rendering thread. `Stop` is a generation barrier:
a restore acknowledgement cannot be followed by an older queued frame. `Poll`
latches native/transport failure; the application must hide the effect and close
the session. `RestorationConfirmed` separately records a successful Stop barrier
or verified post-exit fallback restore; the original error remains available.
The tray may keep running after the effect is safely disabled only when restoration is confirmed.
`Status` distinguishes an active projection from standby, which alone does not
prove that an unexpired worker lease cannot reactivate.

The companion `lumatape-watchdog.exe --pointer-stdio` owns a dedicated native
window and the Magnification runtime. It reads the real cursor independently
of the 30-FPS capture path, with an 8-ms timer. A 500-ms frame lease, pipe EOF or
parent exit restores the system cursor. Source/parent process creation identity,
foreground ownership, physical client bounds and overlay identity/visibility are
checked on each update. Expiry/failure requests hiding only the exact engine
surface through `ShowWindowAsync`; a completely suspended engine might not apply
that queued hide until it resumes, but cursor restoration does not wait for it.

## Visibility handoff and recovery

`MagShowSystemCursor` has global, non-reference-counted visibility. Our workers
hold one named ownership mutex through restoration and runtime teardown, and
recovery uses that same gate. Closing waits for confirmed child exit before a
fallback show call; it never kills a worker that might still owe restoration.
Coexistence with another application's cursor-hiding/magnification tool has not
been tested. Recovery is not guaranteed if both the engine and companion are killed.

Visibility handoff prepares the bitmap first, rechecks cursor eligibility, then
hides the system cursor before showing the projected window. Restoration hides
our HWND before showing the system cursor and retries incomplete operations.
A partial show failure cannot escape cleanup just because the cached visibility
flag is false. This orders Win32 operations; it does not promise atomic DWM
presentation or observe a host macOS cursor when a VM keeps guest coordinates.
There is no idle timeout or edge strip that hides a stationary game cursor.

## Rendering and limits

The cursor window is click-through, does not activate and is excluded from
capture. Current HCURSOR dimensions and hotspot are preserved; color alpha and
ordinary monochrome masks are reconstructed from native black/white renders.
XOR-inverting cursor pixels have no standard alpha representation and fail
explicitly. Animated cursor frame timing is not currently reproduced. Hidden or
touch-suppressed system cursors are not drawn; in-game software cursors remain
part of the captured image. There are no hooks, input injection or pointer warps.
When crop or the screen shape excludes the source pixel under the real cursor,
projection pauses and restores the ordinary cursor without hiding the valid
image. Source/focus/geometry/lease failures still invalidate the surface.

An already-hidden renderer surface restores the ordinary cursor without enqueuing another asynchronous hide. This avoids a hide/show feedback race during first presentation or capture recovery; invalid source identity, focus, geometry and lease expiry still invalidate the displayed mapping.

## Diagnostics

For the independent developer helper, class `LumaTape.PointerProjection`, title
`LumaTape pointer` carries read-only observations under `LumaTape.Pointer.*`:
`Active`, `Hidden`, `RealX/Y`, `ScreenX/Y`, `SourceHWND`, `SourcePID`, `Serial`,
`CursorFlags`, `CursorHandle`. Signed physical coordinates are two's-complement
pointer-sized values. Read `Observation` before/after: equal even values identify
a coherent native update. The HWND is hidden but retained during standby/Stop;
closing the worker destroys it. These properties are observation, not IPC or
proof of the system cursor's actual visibility in a VM host.
