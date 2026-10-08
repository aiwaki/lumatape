# Temporary monitor modes

[Русский](README.md) · English

Use this package only when the user explicitly selects a display mode. Keep the desktop at its native resolution by default.
`ListModes(device)` uses only `EnumDisplaySettingsW`; `Choose43` excludes 5:4,
interlaced and lower-color modes, preserving the current reported refresh rate
when one is available. Every requested mode is re-enumerated and tested with
`CDS_TEST` by an independent watchdog before application.

## Starting a session

Build and ship `cmd/lumatape-watchdog` as `lumatape-watchdog.exe` next to the app. The main
process calls `StartSession`, which waits for a ready handshake over inherited
anonymous pipes. The child captures the complete current `DEVMODEW` including
returned private driver bytes **before** changing anything. The child applies
and restores the mode. There is no gap between arming protection and changing
the display in which a main-process crash would leave the display unprotected.
Dynamic `ChangeDisplaySettingsExW` uses flags zero:
no registry update, custom resolution, unsafe mode or HDR setting is requested.

After a successful `StartSession`, immediately defer `Restore`. Show a user
confirmation with a 15-second countdown using `Session.Deadline()`. The watchdog
starts that window after successful apply/readback and sends the exact deadline
to the parent. Only call `Confirm` on explicit user
confirmation. Unconfirmed timeout, main-process pipe loss, a malformed request,
output pipe failure, and normal exit all trigger conditional restoration. A
confirmed session remains guarded until it ends. Restoration errors receive
bounded retries for transient driver failures.

Poll `Session.Poll()` from the application's event loop. It returns immediately;
`done` means restoration completed, ownership was relinquished after an external
change, or the watchdog ended with an error. Clear the application's active
session and confirmation UI when done. A terminal error persists across repeated
`Poll`/`Restore` calls. The watchdog sends its terminal result only after recovery
attempts finish; bare process EOF is never treated as successful restoration.
`Restore` accepts a terminal reply already delivered by automatic rollback and
also handles the race between such a reply and a broken-pipe write.

## Independent changes and recovery limits

Restoration compares the current resolution, reported frequency, color depth,
mode flags, rotation, position, scaling policy and panning size against the last
applied state. Polling every 200 ms permanently relinquishes ownership as soon
as a different state is observed. The same check occurs immediately before
restore, including after the restoration `CDS_TEST`. This preserves independent
user or application changes that are observed. Windows offers no atomic
compare-and-set for display modes: an external change between the final query
and the API call, or a change away and back within the polling interval, cannot
be distinguished. Killing both application and watchdog, OS failure, and a
failed graphics driver cannot be recovered by this process pair.

A 4:3 mode does not prove correct GPU/panel aspect scaling. The app preserves
the current scaling policy and cannot promise universal GPU scaling control.
Verify the panel is not stretching the mode; prefer native desktop resolution
plus a game's own 4:3 framebuffer when possible. This does not change any game's
internal rendering resolution or FOV by itself.

## Validation

Portable tests exercise timeout, confirmation, pipe EOF before/after
confirmation, idempotent restoration, rejected modes, full-state retention and
external mode/layout changes. Windows cross-compilation is not a runtime
display-switching qualification. Before release, test real multi-monitor
hardware with negative coordinates, refresh-rate changes, confirmation expiry,
`taskkill /PID <main pid> /F` (without `/T`, which also kills the watchdog),
disconnect/reconnect, independent changes and restart.

## API references

- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumdisplaysettingsw
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-changedisplaysettingsexw
- https://learn.microsoft.com/en-us/windows/win32/api/wingdi/ns-wingdi-devmodew
