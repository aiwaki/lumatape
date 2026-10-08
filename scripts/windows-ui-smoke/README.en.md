# Windows UI smoke tools

[Русский](README.md) · English

Developer-only tools in a separate Go module. LumaTape itself has no makc dependency or injected input. Build on any Go host; run only on an explicitly chosen Windows test desktop:

```sh
cd scripts/windows-ui-smoke
go test -race ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../artifacts/production-polish/lumatape-ui-smoke.exe .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../artifacts/production-polish/lumatape-source-input.exe ./cmd/source-input
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../artifacts/production-polish/lumatape-pointer-input.exe ./cmd/pointer-input
```

The tools require the exact PID and executable path. They hold a handle to that process, validate its live image path and the HWND owner/class, and never force-terminate attached processes. Settings `quit` is an explicit graceful close request to the validated same-PID controller. The harness that starts app/testcard processes must retain and clean up only its own process handles/PIDs. These tools do not launch or enumerate processes by filename for termination.

## Native Settings controls

Open Settings first, then inspect or execute a UTF-8 JSON array of steps:

```powershell
.\lumatape-ui-smoke.exe -pid $app.Id -exe $app.Path -output inspect.jsonl
.\lumatape-ui-smoke.exe -pid $app.Id -exe $app.Path -steps steps.json -config test-config.json -log lumatape.log -output result.jsonl
```

The output file avoids PowerShell 5 native-pipeline encoding conversion. One UTF-8 JSON record per step contains the actual controls and optional on-disk config/log tail. An assertion mismatch exits 1. The class must be `LumaTape.Settings`. `WM_SETTEXT`, `CB_SETCURSEL` plus `CBN_SELCHANGE`, and `BM_CLICK` are sent with a two-second timeout. This tests application control handling; it does not prove physical keyboard/mouse delivery.

```json
[
  {"op":"select","id":3007,"index":4},
  {"op":"wait","ms":200},
  {"op":"expect-select","id":3007,"index":4},
  {"op":"text","id":3008,"text":"50"},
  {"op":"click","id":3019},
  {"op":"wait","ms":700},
  {"op":"expect-config","field":"effects.intensity","value":0.5},
  {"op":"expect-text","id":3029,"text":"Настройки применены"}
]
```

Actions: `inspect`, `foreground`, `select` (ID/index), `text` (ID/text), `check` (ID/index 0 or 1), `click` (ID), `wait` (0..5000 ms), `quit` (last step only). `quit` sends `WM_CLOSE` to same-PID `LumaTape.Control`, waits up to five seconds for a real exit, requires exit code 0, and captures config/log after shutdown. It also works without an open Settings window. Expectations: `expect-select`, `expect-text` (substring), `expect-check`, `expect-visible`, `expect-enabled` (index 0 or 1), `expect-config` (dotted field/exact JSON value), `expect-log` (substring in the last 32 KiB). Inspect current IDs/options before mutation; the source list order is dynamic. `expect-log` can match an earlier event, so use a fresh log or compare the session/timestamp in collected records when proving a new transition.

The helper intentionally does not auto-answer system-mode confirmation or choose a display mode. Destructive display mutations must have an explicit test case and the app's watchdog/confirmation protections.

## Real testcard input through makc

`makc` is pinned to v0.2.0, commit `31d0078d4ad8f3c10423016974a698280c2939f2`. Its purpose here is actual Win32 `SendInput` for source click-through, drag/resize, and hotkey delivery inside Windows. The original makc and x/sys notices are preserved in `licenses/`; the Go runtime notice is in the repository's `third_party/Go-LICENSE.txt`.

```powershell
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action inspect -output source-before.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action click -output click.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action drag -dx 60 -dy 40 -output drag.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action resize -dx 80 -dy 30 -output resize.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action keys -keys ctrl+shift+9 -output keys.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action minimize -output minimized.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action restore -output restored.json
```

The source class must be `LumaTape.Native.TestCard`. The helper uses per-monitor DPI v2 and virtual-desktop physical coordinates. It requests foreground explicitly, checks ownership/foreground before injection and every drag step, refuses already held buttons/keys, and releases its injected holds on failure. `minimize`/`restore` use `ShowWindow` on the validated source and assert the actual `IsIconic` state; the report includes before/after rectangles. Drag/resize must change the actual window rectangle; click must increment the testcard's `LumaTape.TestCard.Clicks` property exactly once. The property stores the same counter drawn on the card, plus one; older cards without it fail the click oracle rather than passing silently.

Hotkey injection success alone is not a delivery assertion. Follow it with Settings `expect-text`/`expect-config`, inspect diagnostics command counts, and verify the visible effect. Windows `SendInput` does not qualify the physical Mac Option/Fn → Parallels key path. No script here changes host shortcuts, VM settings, system mouse speed, or installs hooks.

Microsoft API contracts: [SendMessageTimeoutW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendmessagetimeoutw), [CB_SETCURSEL](https://learn.microsoft.com/en-us/windows/win32/controls/cb-setcursel), [BM_CLICK](https://learn.microsoft.com/en-us/windows/win32/controls/bm-click).

## Projected cursor and source lifecycle

Build the separate source scene from the repository root with `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o artifacts/production-polish/lumatape-testcard.exe ./cmd/lumatape-testcard`, then launch it in Windows with `-pointer-test`. Its title is `LumaTape pointer test`; the ordinary testcard remains unchanged. Select this exact scene in LumaTape and enable the static convex configuration before testing. The independent mapping oracle assumes the source and overlay have identical physical bounds, no crop/DAR change, no jitter/tracking, and the shader curvature supplied by `-curvature`.

```powershell
.\lumatape-testcard.exe -pointer-test -width 960 -height 720
.\lumatape-pointer-input.exe -pid $sourcePID -exe $sourceExe -worker-pid $workerPID -worker-exe $workerExe -action targets -curvature 0.7 -output targets.json
.\lumatape-pointer-input.exe -pid $sourcePID -exe $sourceExe -worker-pid $workerPID -worker-exe $workerExe -action drag -target 5 -dx 80 -dy 40 -curvature 0.7 -output drag.json
```

Actions are `inspect`, `move`, `targets` (all five hover/click targets), `drag`, `wheel` (+120), and `held-disable` (hold left mouse, send `ctrl+shift+0`, verify restoration, release). `-target` selects 1–5; `-disable-keys` changes the explicit hotkey. `-projection visible` is the default; `hidden` requires the worker to have restored the native cursor, and `ignore` is only a baseline without projection assertions. Worker PID/exe are required except with `ignore`. The helper verifies native source events, real cursor coordinates, source capture ownership, worker identity, and actual cursor-window position plus the native hotspot. Hover is awaited as an event result, independently of cursor-position convergence. JSON is written directly as UTF-8; exit 1 means an assertion failed.

Two explicit manual protocols live one directory above this module. Both scripts retain UTF-8 BOM for Windows PowerShell 5, require an existing output directory, and write JSON on failure:

```powershell
..\windows-pointer-lifecycle-smoke.ps1 -RuntimeJson candidate-runtime.json -HelperPath .\lumatape-pointer-input.exe -Output lifecycle.json -Curvature 0.7
```

Lifecycle metadata must contain `Bundle` (absolute candidate bundle directory), `HostPID`, and `SourcePID`. The script resolves only that bundle's host → engine → pointer-worker chain, pins native process creation times and HWND identities, and checks minimize → restore → resize → source close. It uses the pointer helper after restore/resize to verify mapping at the new bounds. **Closing the exact pointer-test scene is the final action.** It does not relaunch it. Failure before closure restores its original outer rectangle even if engine/worker failed. After its own `WM_CLOSE`, source exit is confirmed from the retained process handle; it does not read image metadata from the dying process.

```powershell
..\windows-pointer-lease-smoke.ps1 `
  -EngineProcessId $enginePID -EngineExecutable $engineExe -EngineCreatedFileTime $engineCreated `
  -WorkerProcessId $workerPID -WorkerExecutable $workerExe -WorkerCreatedFileTime $workerCreated `
  -SourceProcessId $sourcePID -SourceExecutable $sourceExe -SourceCreatedFileTime $sourceCreated `
  -Output lease.json
```

Lease inputs are exact PIDs, absolute executable paths and decimal native `GetProcessTimes` creation FILETIMEs for all three processes; use identity fields from the probes, not rounded CIM timestamps. Start with an active projection over the foreground source and no mouse buttons held. Only the engine is briefly suspended. A 1500 ms resume watchdog and C#/PowerShell `finally` blocks protect resume; the result requires the measured suspension to stay within 2000 ms. The probe checks cursor restoration after the 500 ms lease and fresh-frame recovery. `OverlayHiddenDuringSuspension` is separate: `ShowWindowAsync` may wait for the suspended engine, so cursor restoration alone does not prove the stale overlay disappeared.

For sustained capture checks, `pointer-input -action motion -duration 90s -projection ignore` moves through a deterministic path inside the pinned testcard. Duration is bounded to 1–180 seconds; foreground, geometry and button state are checked throughout. Each second it verifies the real cursor and delivered native mouse coordinates. It never presses a button. Run an independent overlay-visibility observer alongside it; this action does not claim to validate the cropped/warped cursor position. A hidden console launch must use `ProcessStartInfo.CreateNoWindow=true` and `UseShellExecute=false`, **not** `Start-Process -WindowStyle Hidden`: the latter supplies `STARTF_USESHOWWINDOW/SW_HIDE`, which overrides the helper's first `ShowWindow` and can hide the test source itself.

`windows-overlay-visibility-smoke.ps1 -Runtime candidate-runtime.json -OutputDirectory <existing directory> -Label moving -Seconds 95` observes the identity-bound overlay independently every ~10 ms (Windows scheduling may produce a longer interval). The runtime record must contain the exact `Bundle`, `HostPID` and `SourcePID`. It reports source focus, visibility gaps, cursor coordinates and elapsed time, with raw CSV samples. It performs no input or screenshot capture. A zero-hidden result is meaningful only while the intended effect/format is actually enabled and the source stays foreground; confirm the menu/config and correlate the same UTC interval with engine logs. Benchmark host builds and unrelated GUI actions separately.

These protocols qualify guest Win32 events and owned-window lifecycle. They do not qualify physical Mac/Parallels input, arbitrary games, hardware GPU paths, or compositor visibility solely from `CURSOR_SHOWING`. No product input remapping or injected game input is introduced by these developer tools.
