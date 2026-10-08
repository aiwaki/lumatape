# Windows IPC lifecycle smoke

[Русский](README.md) · English

This developer tool checks the Go engine. Run it in an interactive Windows desktop,
with a separately started test card and no other LumaTape engine. The engine
executable must have its normal GLFW/capture DLLs beside it. The lifecycle/EOF
cases do not automate input. No case changes the system display mode or modifies
the main user profile.

## Startup and diagnostics

```powershell
.\windows-control-smoke.exe --engine C:\LumaTape\lumatape-controlled.exe --target-pid 1234 --case lifecycle --expect-gpu-unavailable --output C:\LumaTape\ipc-lifecycle
.\windows-control-smoke.exe --engine C:\LumaTape\lumatape-controlled.exe --target-pid 1234 --case eof --output C:\LumaTape\ipc-eof
.\windows-control-smoke.exe --engine C:\LumaTape\lumatape-controlled.exe --target-pid 1234 --case input --input-helper C:\LumaTape\lumatape-source-input.exe --target-exe C:\LumaTape\lumatape-testcard.exe --output C:\LumaTape\ipc-input
```

Use the actual test card PID. If that process has multiple eligible windows,
also pass `--target-title "LumaTape test card"`, using its exact current title
for the scene's language. Omit
`--expect-gpu-unavailable` on a GPU where the native interop path may work; this
flag explicitly requires GPU rejection and is intended for the Parallels fixture.
Without `--output`, artifacts go to a new private temporary directory.
Process creation has a separate 15-second deadline (`--startup-timeout 1s..60s`).
JSONL `process_start_begin`, `process_started` or `process_start_timeout` identify
whether failure preceded protocol readiness. On startup timeout only parent pipe
ends close: a late engine receives EOF, and the helper never calls process Kill.
`process-start-timeout-stacks.txt` captures the helper's Go goroutines to locate
the blocked startup call. A DPI setup refusal fails before geometry testing;
the helper never treats an unverified DPI context as physical-pixel evidence.

If startup fails again, first preserve its JSONL, stack dump, stderr and protocol
log. `process_start_begin` without `process_started` points to process creation;
`process_started` without `ready` points to engine initialization/protocol.
Record the exact engine/helper SHA-256 and, while the owned processes still live,
their PID, parent PID, session, executable path, command line, CPU and thread wait
states. Compare a direct launch of the same engine from the same directory only
after the prior helper has exited and its child cleanup is known. Do not change
the GPU backend or assume an ARM/driver failure from missing `ready` alone.

## Lifecycle and EOF

The lifecycle case checks correlated protocol replies, exact live source
identity, explicit Full CPU plus rounded screen and actual 4:3 client resize,
configuration preservation after rejected GPU/reload requests, real renderer
PNG previews before/after, exact original outer rectangle after emergency, and
cleanup acknowledgement followed by exit code 0. The EOF case closes host stdin
after applying the window change and requires exit code 0 plus exact restoration.
It never force-kills the engine on timeout.

Artifacts include a private `config.json`, engine diagnostic log/stderr, compact
protocol log, and preview PNGs. The output JSONL records assertions, not evidence
of live foreground rendering: the helper deliberately does not focus the target.
Full capture visibility, mouse alignment, native GPU support, and physical display
mode restoration require their separate Windows tests. These cases temporarily
resizes the chosen test card and restores it through the engine's cleanup path.

## Input and hotkeys

The `input` case invokes the separately built
`scripts/windows-ui-smoke/cmd/source-input` helper; the main module has no makc
dependency. Both helper and testcard paths must be absolute. That helper validates
the exact live PID/executable and `LumaTape.Native.TestCard` window class, checks
foreground ownership before SendInput, and releases its keys/buttons on failure.
The case selects Full CPU, rounded screen, mouse-exact input, window 4:3 and
`Ctrl+Shift+9` / `Ctrl+Shift+0` in its private profile. It explicitly focuses the
owned testcard, waits for actual `full-compatibility` presentation, checks one real
click count increment, then checks exact hotkey receipt deltas and filter state
after off/on. While window 4:3 remains enabled, filter-off must reach actual Full
`bypass` with effective intensity 0; filter-on must reach `active` at intensity 1.
Both toggles preserve the exact formatted client/outer rectangles and all saved
preferences except `enabled` (including the rounded shape to restore on enable).
The live renderer receives a flat effective shape when the filter is off; this
helper verifies status and geometry, not the framebuffer pixels of that bypass.
A real emergency combo must advance its receipt and emergency
sequence, disable both filter and format, clear recovery, and restore the exact
original outer rectangle. A stale apply must remain rejected; quit must acknowledge
cleanup and exit 0. Step reports and authoritative snapshots are saved beside the
JSONL log. Missing focus, duplicate/missing receipts or an inactive Full backend
fail the case. An input helper timeout is reported without force-killing it while
deferred key release may still be necessary. This proves Windows SendInput delivery
on that desktop, not physical Mac keyboard/Parallels key translation.

Child processes must use `CREATE_NO_WINDOW` without Go's
`SysProcAttr.HideWindow`. `HideWindow` adds `STARTF_USESHOWWINDOW/SW_HIDE`, which
overrides the child's first `ShowWindow` call. For source-input, that call targets
the testcard: launching with this flag makes its HWND foreground but invisible,
leaving the engine paused with no Full frames. Do not use that flag to
suppress consoles; the separate creation flag already does that. The source
visibility/backend assertion remains required.

## Shaders

The `shaders` case uses the real renderer and examples in LumaTape format:

```powershell
.\windows-control-smoke.exe --engine C:\LumaTape\lumatape-engine.exe --target-pid 1234 --case shaders --expect-gpu-unavailable --shader-file C:\LumaTape\examples\shaders\amber-crt.lumatape.glsl --secondary-shader-file C:\LumaTape\examples\shaders\cold-bleed.lumatape.glsl --output C:\LumaTape\shader-smoke
```

This case requires the Parallels test environment with no interop support. It
keeps `capture.transfer=auto` in the saved config and requires a successful CPU
Open, exact shader defaults and actual window 4:3. Import runs real GLSL compile;
a parser-valid body with an undefined function must return `shader_compile_failed`
without writing an asset or changing config, source geometry or the owned live
program ID. Duplicate import must retain one immutable ID; library/source roundtrip
must match. Decoded PNG pixels must show exact 0% bypass, a visible 100% difference
and opaque alpha. Previewing Cold Bleed must leave the live Amber program/config
unchanged. Out-of-range parameters and a warp shader under mouse-exact input must
be rejected atomically. Emergency restores the exact original rectangle, rejects
a stale Apply, and Quit requires cleanup acknowledgement followed by exit 0.

Only this child receives `LOCALAPPDATA=<output>\local-app-data`: its imported
shader library and diagnostics stay separate from the user's real library.
The source files are read from the two explicit absolute paths. The case does
not focus or inject input unless both `--input-helper` and `--target-exe` are
provided; those flags add only a validated foreground request for the owned
testcard and an actual Full-active assertion. The PNGs remain synthetic renderer
previews, not captured game screenshots. `shader-summary.json` distinguishes
that optional foreground evidence; final STOP/Quit assertions are in JSONL.

## Built-in presets

The `presets` case checks all five built-in effects in the Parallels test
environment with no GPU interop, without changing the testcard size or display mode:

```powershell
.\windows-control-smoke.exe --engine C:\LumaTape\lumatape-engine.exe --target-pid 1234 --case presets --expect-gpu-unavailable --input-helper C:\LumaTape\lumatape-source-input.exe --target-exe C:\LumaTape\lumatape-testcard.exe --output C:\LumaTape\presets-smoke
```

Each preset applies Full Auto, intensity 1, flat shape and format off, foregrounds
only the exact testcard PID/executable through the existing input helper, and
requires an `active`/`full-compatibility` snapshot. Both outer and client RECTs
must remain unchanged. Original 4:3 is recorded, never achieved by resizing.
The six `*-synthetic-preview.png` files use the renderer's test scene: they are
**not WGC screenshots or captured-game A/B evidence**. The summary is written
only after emergency restoration, stale-Apply rejection and clean Quit/exit 0.
Failed cases also attempt the same cleanup before the outer EOF fallback.

## Build

Run from the repository root:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o artifacts/polish-validation/windows-control-smoke.exe ./scripts/windows-control-smoke
```
