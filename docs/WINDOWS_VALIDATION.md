# Windows validation

This protocol covers the current **native Tauri tray** and Go rendering engine.
It is a checklist, not a claim that every combination has passed. Record results
against the exact release, binary hashes and environment under test. Browser demos
and the retained React prototypes do not qualify the shipped interface.

## Candidate and environment

Use Windows 10 version 2004 (build 19041) or later, x64 binaries, SDR and an
OpenGL 3.3 driver. Start with a disposable test profile and the bundled test scene.
Keep the normal profile and recovery records intact. Run GUI tests in an
interactive Windows session; Session 0 cannot qualify capture, focus or input.

Record the release/tag, ZIP or installer SHA256, EXE/DLL hashes, Windows build and
architecture, GPU/driver, monitor size/refresh/DPI and selected backend. Record
requested and actual processing separately. A present DLL or OpenGL extension is
not proof of working GPU interop. For installed updates, use the additional
[update qualification protocol](UPDATES.md).

For a portable candidate, extract to a new path containing spaces and non-ASCII
characters, such as `C:\LumaTape tests\Проверка`. Verify the checksum and archive
contents; repeat with writes beside the executable denied. Configuration, shader
library and logs must remain in the user profile. Do not infer exit code 0 from
`Finished=true`, a screenshot or a null `ExitCode`.

Before each mutation, identify only the test's own PID, executable path, process
creation time and HWND. Keep those identities in local evidence. Do not use a
blanket process-name kill or terminate the independent recovery watchdog together
with its owner. Local evidence can contain window titles and full paths; review
and redact it before sharing. It is not included in releases.

## Build and portable checks

A Windows build needs Go, Python, Node/npm, Rust MSVC, Visual Studio C++ tools and
Windows SDK 10.0.26100 or newer. That build SDK does not raise the runtime minimum.
Build steps are in the [desktop host guide](../desktop/src-tauri/README.md).

Run checks for changed components, preserving unrelated green results:

```sh
sh scripts/check.sh
(cd desktop && npm run test:host)
python3 -m unittest discover -s scripts -p 'test_package_desktop.py'
python3 -m unittest discover -s scripts -p 'test_prepare_update.py'
python3 -m unittest discover -s desktop/scripts -p 'test_*licenses.py'
```

The Cargo build uses `Cargo.lock` and `--locked`. Windows-only Go test executables
must also run on Windows with a separately recorded exit code. Native
`lumatape_capture_abi_smoke.exe` checks the capture ABI and error lifecycle; it
does not establish successful capture. The package validates required exports
including `open_ex` and `get_stats`, rather than accepting an old GPU-only DLL.

Mac shader readback tests are separate from Windows driver, DWM and WGC tests.
The executable capability matrix and deterministic geometry/transition tests
cover rules, not thousands of real display or game sessions.

## Native tray and normal use

| Scenario | Required observation |
|---|---|
| First launch | One Tauri icon and one engine; no application panel, Go tray icon or WebView2 descendant. A fresh profile starts disabled. |
| Second launch | The second instance exits without another engine, overlay or hotkey registration. |
| Menu | Left and right click open the same native menu. Version appears immediately above Quit; About opens the project GitHub URL. |
| Language | Russian Windows UI language selects RU; other UI languages select EN. Regions/date formats do not decide the language. Restart to apply a Windows UI language change. |
| Source | Game lists actual windows and distinguishes duplicate titles. A closed source is not replaced by a different same-title process. |
| Enable | Open Tools → Test scene, select it under Game, choose an effect and enable it. Return focus yourself; require fresh captured frames and working native input. |
| Settings | Changes apply directly, checked choices match applied state, and invalid combinations explain why they are unavailable. No intensity slider or manual shader-parameter editor is expected. |
| Failure | Missing capture DLL, device/source failure or hotkey conflict leaves no opaque covering surface. An error must not claim that a previous working configuration was replaced successfully. |
| Disable/quit | Surface and projected cursor disappear; owned format changes are restored. Cleanup errors remain visible/recoverable. Restart must not silently repeat an emergency-disabled format change. |

Useful identity-bound native helpers, run from the repository root:

```powershell
# Set these to the current test bundle, retained process handle and evidence folder.
./scripts/windows-tray-only-smoke.ps1 -Bundle $Bundle -HostProcessId $App.Id -Output "$Evidence/inventory.json"
./scripts/windows-tray-menu-smoke.ps1 -Bundle $Bundle -HostProcessId $App.Id -Output "$Evidence/menu.json"
./scripts/windows-testcard-fullscreen-smoke.ps1 -Exe "$Bundle/engine/lumatape-testcard.exe" -Output "$Evidence/fullscreen.json"
```

Read each helper's parameters and scope first. Inventory is read-only; menu
inspection opens its own native menu; `-MenuPath` dispatches an actual command.
The fullscreen helper owns the testcard processes it creates and checks F11,
Alt+Enter, Escape, the native button and exact window restoration through Win32
messages. It does not prove delivery from a physical keyboard.

For isolated RU/EN menu checks, `windows-localization-smoke.ps1` expects an
**unconfigured portable** candidate and no running normal instance. It creates
its own temporary profiles and never enables an effect. Its test-only language
override does not change Windows preferences. Qualify ordinary system detection
separately. Do not run that helper unchanged against a configured update channel.

## Effects and custom shaders

Check Subtle CRT, CRT Classic, Soft TV, VHS Light and VHS Tape at native image
scale. Use color boundaries, fine text, gray ramps, a circle, a grid, black/white,
a moving object and corner HUD targets. Subtle CRT is intentionally restrained;
the other presets should have distinct visible characteristics. A numeric PNG
difference alone cannot assess readability, flicker or the intended effect.

Compare Flat, Rounded CRT and Curved CRT. Corners must not reveal an unprocessed
rectangle. Disabling removes the frame and curvature. Source identity, chosen
format, shortcuts and enabled state survive effect selection; the tray uses full
shader strength. Legacy zero-intensity bypass belongs to engine tests, not a
control in the tray. Lightweight cannot provide full pixel processing.

Import both [example shaders](../examples/shaders/) using Add effect from file.
Check genuine GLSL compilation, activation, reselect/restart and preserved custom
values. Invalid UTF-8, metadata, GLSL, excessive file size and a read-only library
must report a useful error without replacing the last working program. The
[shader contract](SHADER_SPEC.md) defines supported inputs and limitations.

## Input, focus and geometry

**Settings → Image distortion → Accurate clicks** and **Allow arbitrary
distortion** both support keyboard and mouse. Accurate clicks keeps the image and
native interaction aligned; the built-in curved shape projects the cursor drawing
without moving Windows coordinates or injecting game input. Arbitrary distortion
permits Crop, Stretch, custom display aspect ratio and custom `warp` shaders,
whose visible targets may differ from their native clickable positions.

Check hover/click at all five testcard targets, held drag, wheel, loss of focus,
minimize/restore, close, resize, fullscreen and source movement. Verify no duplicate
click, stuck button or retained projected cursor after disable/quit/failure.
Changing focus must not steal it back. Test both sides of every window edge and
repeat on a physical Windows system; a VM screenshot may omit the host cursor.

Use the [dedicated input helpers](../scripts/windows-ui-smoke/README.md), which
pin the target identity, check actual testcard counters and release injected
holds on failure. Guest SendInput confirms Windows command handling, not physical
Mac keyboard delivery through Parallels. RegisterHotKey success and receipt of a
shortcut are separate assertions. Defaults are Ctrl+Shift+9 (toggle) and
Ctrl+Shift+0 (emergency off); existing bindings remain unchanged. Test a registration
conflict without losing the working emergency shortcut.

## Format and recovery

The tray offers Original aspect ratio and Windows resolution. Masking and
resizing the game window are retired choices; an active legacy profile remains
readable and can be restored. For ordinary 4:3 play, first choose a 4:3 resolution
inside the game. 1280×1024 is 5:4. A Windows mode switch does not guarantee a game's
internal resolution/FOV or the monitor's scaling policy.

Display changes use only Windows-listed modes and require preflight plus a
15-second confirmation. Keep display mode appears only while confirmation is
pending. Check confirm, timeout, late confirmation, emergency off, normal quit,
main-process termination and a subsequent external display change. Restore only
changes still owned by LumaTape. Test failure at preparation, application,
readback, rollback and recovery-journal writes. The watchdog must remain alive
when testing failure of the main process.

Run display-mutating cases only on a test display with a recovery path. The
`windows-display-smoke` helper is read-only without explicit case/device/watchdog
arguments. A fake driver or virtual display does not qualify physical restoration.

Geometry coverage includes 320×200 with display aspect 4:3, 640×480, 960×720,
1280×1024 and 1920×1080; framed/borderless windows; 100/125/150/200% DPI; negative
coordinates and monitor/adapter changes. Check game-enforced minimum sizes and
stale frames after every geometry transition.

## Sustained runs and evidence

Measure after warm-up with PNG capture disabled. Save duration, fresh-frame count,
frame age distribution, transfer/submission timings, stalls, focus interruptions
and memory growth. GPU shader time alone is not total latency. CPU transfer is
capped at 30 FPS; the cap does not promise sustained 30 FPS.

Use a stationary source and deterministic pointer motion, then qualify real games
in windowed/borderless mode on physical hardware. Separate portable tests, Mac
shaders, Windows CPU compatibility, hardware GPU interop and installer updates in
the report. Mark unavailable equipment and unrun cases explicitly. Exclusive
fullscreen, HDR, Full monitor capture and anti-cheat/protected-capture bypass are
outside the supported scope.
