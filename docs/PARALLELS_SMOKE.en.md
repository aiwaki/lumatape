# Parallels smoke protocol

[Русский](PARALLELS_SMOKE.md) | [English](PARALLELS_SMOKE.en.md)

Parallels is useful for Windows menu, CPU capture and input regression checks.
It does not establish physical GPU interop, monitor behavior or compatibility
with arbitrary games. Use this alongside the [Windows protocol](WINDOWS_VALIDATION.en.md)
and record the exact release and hashes for each run.

## Setup

Use the Windows interactive desktop with a disposable LumaTape profile and the
bundled test scene. Keep existing profiles, games and unrelated processes intact.
Record Windows architecture, Parallels/Tools and display-driver versions, physical
resolution, DPI and refresh rate. The release is Windows x64; running it on
Windows ARM uses Windows' compatibility layer, not a native ARM64 build.

Do not restart the VM/Tools or change host preferences as an automatic workaround.
For physical mouse tests, record Parallels' Smart Mouse setting. In the development
VM, the user confirmed acceleration and sticky clicks disappeared with **Don't
optimize for games**. This is a VM-specific observation, not a reason for LumaTape
to modify that setting. A different game may require a different host policy.

If guest execution fails, distinguish a tool failure from an application failure.
A background Session 0 process, stale screenshot or completed scheduled task does
not establish GUI success. Helpers must validate the intended session, executable,
process creation time and HWND before interacting.

## Short regression run

1. Extract a new portable candidate to a path containing spaces and non-ASCII
   characters. Check that WebView2 Runtime is installed, match the checksum and
   launch normally. On first launch, confirm one native tray, one engine and no
   WebView/panel. Launch again: settings should open in the existing instance,
   without another engine or tray. Close settings and check that the window is
   destroyed while the tray stays available.
2. Inspect RU/EN menu text, version above Quit, shortcut descriptions, actual
   enabled/check states, and readable error dialogs. Open settings from the tray:
   game, effect and shape must match the menu. Test language inheritance in
   a newly launched testcard; an older already-running testcard is not translated.
3. Open Tools → Test scene, select it under Game and enable VHS Tape. Automatic
   processing may choose CPU only after a classified GPU-interop failure. Record
   the actual backend and reason. A present DLL does not establish GPU support.
4. Compare all five effects and three screen shapes at native scale. Check live
   frame freshness, HUD corners and readable text. Import a valid example shader,
   then reject an invalid one while keeping the working effect.
5. Move/resize the source, minimize/restore, switch focus and test fullscreen with
   F11, Alt+Enter and Escape. Require fresh recovery without a stale rectangle,
   unintended focus transfer or opaque error surface.
6. With Accurate clicks and Curved CRT, test target hover/click, drag and wheel.
   Disable while a button is held using the identity-bound input helper; verify
   release and ordinary cursor restoration. Check edges in both windowed and
   fullscreen presentations.
7. Test Ctrl+Shift+9 toggle and Ctrl+Shift+0 emergency off, or the profile's saved
   bindings. Registration and receipt are separate checks. Guest-injected input
   and physical keyboard delivery must be reported separately.
8. Choose Make window 4:3 in settings. Check actual client bounds, keeping borders
   and title bar inside the work area. Restore a wide size through the test scene:
   format should become inactive, without a mask or repeated resize. A preset
   change must not resize the window. Explicitly apply 4:3 again, then Restore
   original format; check that the effect's enabled state survives and only owned
   changes are restored. Test normal and maximized sources separately.
9. Quit normally, verify engine cleanup and preserved settings, then restart.
   Do not change the game's selected input mode or profile merely to simplify
   the test. No source window should be closed unless it belongs to this run.

Masking remains for compatibility only. Old Crop/window-format scripts do not
validate the new one-shot resize or its actual status. In a real game, also
check FOV and internal resolution; window size alone does not establish them.
Ultrawide layouts, negative monitor coordinates and different DPI values need
the appropriate runtime setup, not just geometry calculations.

System resolution restoration and installed N→N+1 updates use separate explicit
protocols. For a display-mode test, settings must open before switching, then
show Keep/Revert and a 15-second countdown. Check confirmation, Revert, timeout
and closure during preparation/confirmation separately; closure must turn off
the effect and restore owned changes. These are not part of the short run.

## Input and cursor evidence

The [Windows UI helpers](../scripts/windows-ui-smoke/README.en.md) check actual
Win32 events and own testcard counters. They do not change mouse sensitivity,
Parallels configuration or host keyboard mappings. Physical Mac/Parallels cursor
doubling may be invisible in a guest screenshot because the host cursor is not
part of captured Windows pixels. A clean screenshot cannot disprove that symptom.

For repeatable observation, use the native pointer-test scene and fullscreen
helper. Preserve the original focus, bounds and profile when a probe fails.
Distinguish projected-cursor geometry, cursor visibility ordering and capture
freshness; passing one does not prove the others or the host compositor.

Alt on a Mac keyboard corresponds to Option, and Control is not Command. F-key
delivery may also depend on Fn and Parallels. The default Ctrl+Shift+9/0 pair
avoids those keys, but only a physical test can confirm delivery from that host.

## Report boundaries

Save local JSON/log receipts and the real exit code for each owned helper. Keep
screenshots optional and scoped to the test scene. Do not publish unredacted
window titles, local paths, profiles or desktop captures with the release.

Classify the outcome separately for native menu/language, Full CPU rendering,
input, source lifecycle, display recovery and updates. Note interrupted focus,
VM stalls and untested items. Performance runs require warm-up and sustained
capture without PNG readback. The CPU ceiling of 30 FPS is not a measurement.
Record new-image rate separately from presentation rate, plus frame age,
transfer time and stalls. No new frames from a static scene alone does not mean
failure; an old frame may be reused only after source, device and geometry checks
with no pending CPU copy. Separately test an actual delayed or lost capture:
after a recurring failure, presentation should stay paused instead of flickering
between the effect and the original image.

A successful Parallels run remains **Windows CPU compatibility evidence**.
Hardware GPU interop, physical display/DPI transitions, host input and real-game
compatibility require their own qualification.
