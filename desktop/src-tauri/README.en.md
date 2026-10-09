# LumaTape desktop host

[Русский](README.md) · **English**

Windows Tauri 2 host. **The application uses only a native
tray menu.** The host creates no application window or WebView at startup.
Go/Win32 handles rendering, capture, hotkeys and geometry recovery. Rust manages
the menu, file selection, clipboard operations and updates.

`tauri.conf.json` has `app.windows: []`, embeds the minimal `../tray-assets`
directory, and has no frontend build/dev command. WebView2 is not required for
this windowless application; `bundle.windows.webviewInstallMode` is `skip`.
React/Vite, `desktop/ui` and former panel source remain in the
repository as history; separate design experiments are local backups.
`dev:ui`, `build:ui` and browser `?demo=1` refer to those retained prototypes,
not to the shipped tray. Their tests do not validate the native menu.
The retained CSP and old window capability files do not
create a WebView or expose a frontend command handler.

## Build and resources

From `desktop/`, install the locked Tauri CLI dependencies with
`npm ci --ignore-scripts --no-audit --no-fund`, then use `npm run dev` on Windows
after staging the engine resources below. No Vite server or React build is
needed. Node/npm are build tools only; players do not need Node, Rust or Go.

Build with the committed Cargo.lock and `--locked`. The lock contains Tauri
runtime 2.11.3, macros/build 2.6.3, utils 2.9.3 and tray-icon 0.24.2. Keep this
coherent set: an earlier unconstrained lock selected an incompatible runtime
2.12.1 during Windows compilation.

The parent build stages these files under `resources/engine/` before invoking
Tauri. Paths are resolved through Tauri's resource directory, including Unicode
installation paths:

- `lumatape-engine.exe`: Go `cmd/lumatape` with its stdio controller.
- `lumatape-watchdog.exe`, `lumatape-testcard.exe`.
- `glfw3.dll`, `lumatape_capture.dll`.

`lumatape.exe` is the Rust host. It launches only the fixed engine executable
with inherited anonymous pipes, the host PID, `--headless-settings`, and no
console. There is no localhost HTTP service. The engine directory is the child
working directory. Required files are checked before launch; provenance and
hashes are recorded in the package manifest. Profiles and logs remain in the normal
user-data directories, not beside the installed executable.

The commands below use `0.3.1` as an example. Replace it with the version in the
current `desktop/package.json`, `Cargo.toml`, and `tauri.conf.json`; all three must match.

From the repository root:

```sh
scripts/build-desktop.sh /absolute/current-native-install 0.3.1
```

Or on Windows:

```powershell
.\scripts\build-desktop.ps1 -Version 0.3.1
```

The scripts rebuild the Go binaries/resources and Rust host, assemble a package
from an explicit file list, and produce `dist/lumatape-0.3.1-windows-x64.zip`, checksums, version
metadata and licenses. They do not invoke `build:ui`. An existing ZIP with the
same version number can still be an older panel build; match the concrete
artifact and SHA256 against the release checksums and package manifest.

Run `npm run test:host` from `desktop/` for Rust tests. The pure menu model is in
[`src/tray_model.rs`](https://github.com/aiwaki/lumatape/blob/main/desktop/src-tauri/src/tray_model.rs), native menu lifetime and event handling are in
[`src/tray.rs`](https://github.com/aiwaki/lumatape/blob/main/desktop/src-tauri/src/tray.rs), and system dialogs and clipboard code are in
[`src/native_dialog.rs`](https://github.com/aiwaki/lumatape/blob/main/desktop/src-tauri/src/native_dialog.rs). Compilation and portable tests
are separate from Windows menu qualification. No new runtime result is implied
by these instructions.

### Icon

The vector source `assets/lumatape.svg` and 1024×1024 PNG `assets/lumatape.png`
are exported from Figma. Export both files after changing colors or geometry.
From `desktop/`, run `npm run tauri -- icon ../assets/lumatape.png --output ../build/icon-export`.
Copy the resulting `icon.ico` to `assets/lumatape.ico` and
`desktop/src-tauri/icons/icon.ico`, and `128x128@2x.png` to
`desktop/src-tauri/icons/icon.png`. The build validates these files and embeds
the ICO in the executables; it does not redraw the icon.

## Native menu and capture capability

Left and right click open the same Windows menu. It contains the actual status,
one **Turn on / Turn off** action, **Game**, **Effect**,
**Add effect from file…**, **Settings**,
**Tools**, and **Quit**. **Keep display mode** appears only while a display
change awaits confirmation. There is no panel, window that closes to the tray,
embedded preview, intensity slider, parameter editor or second Stop action.
The OS controls menu appearance.

**Game** lists concrete windows. IDs encode HWND, PID and process creation
identity instead of titles or positions. Duplicate titles are disambiguated;
menu labels escape ampersands and control characters. The shell never silently
replaces a closed source with another same-title window. Full-monitor capture
remains unsupported. Whole-monitor selection for Lightweight is available only
in the separate legacy/CLI application; the new tray selects game windows.

**Settings** provides screen shape, 4:3/display choice, image distortion, scale, source DAR,
backend and hotkey profiles. Selections apply immediately, without a form draft.
Invalid combinations are disabled. System display changes require
a native warning first, then explicit **Keep display mode** within the
engine/watchdog's 15-second timeout; otherwise the previous mode is restored.
The tray no longer offers a mask or automatic game-window resize. Legacy active
formats are shown read-only and can be restored; choose 4:3 inside the game first.

For a curved screen with a mouse, choose **Settings → Image distortion →
Accurate clicks** and **Settings → Screen shape → Curved CRT**, with Full Auto,
GPU or compatible CPU processing. The built-in screen shape projects the system
cursor onto the curved image while leaving Windows cursor coordinates, mouse
events, Raw Input and capture unchanged. Flat and rounded shapes keep their
original pixel positions. Crop/stretch and custom DAR require **Allow arbitrary distortion**;
keyboard and mouse work in both modes, but arbitrary distortion can misalign
clicks. A custom `coordinates: "warp"` shader has no cursor projection.
This is not general input remapping.

Projection does not reproduce animated system-cursor timing and does not support
XOR-inverting cursors. Software cursors drawn by a game remain in the captured
image. Real games, physical host input and Parallels host-cursor visibility need
separate qualification; see the
[Windows protocol](../../docs/WINDOWS_VALIDATION.en.md).

The model reads the authoritative config without migrating it. Explicit effect
selection or Power-on sets intensity to 1. Selecting a full effect for a
window from Lightweight selects Full Auto; source, shape, format, hotkeys and
filter enabled state otherwise survive effect changes. The existing saved
shader parameter values survive reselecting the same immutable asset.

New profiles use Auto and Ctrl+Shift+9 / Ctrl+Shift+0. Existing manual modes and
bindings are retained. **Settings → Hotkeys** offers that pair and
Ctrl+Alt+F9 / Ctrl+Alt+F10; an existing custom pair is shown and retained until
another profile is explicitly selected. There is no custom key recorder in the
tray. Go registers the replacement pair transactionally. **Tools → Test keyboard shortcuts…** reports registration separately from commands actually
received while the native test message is open. Those commands act for real.

Auto tries GPU first and permits CPU only for classified interop failures.
Missing prerequisites are detected in the current GL context. Even when all
prerequisites are present, support remains unconfirmed until a successful capture Open. Generic source,
device or HDR failures are not fallback reasons. Actual backend and requested
transfer are distinct. CPU submission is capped at 30 FPS; this is not a promise
of sustained frame rate or game latency.

### Import and the standalone test card

**Add effect from file…** opens the Windows file dialog for `.lumatape.glsl`
or `.glsl`, using its own STA thread. File reads are limited to 64 KiB and must
be UTF-8. `shader_import` parses the versioned contract, compiles on the real GL
thread and saves the shader only after successful compilation; that RPC alone never activates the program.
`shaders` and `shader_source` read the local library. Selection compiles before
capture, hotkey or geometry mutations and keeps the last working program on
failure. See [LumaTape Shader v1](../../docs/SHADER_SPEC.en.md); GLSL is not a GPU
sandbox.

The host may select a successfully imported shader when a source is already
selected and controls are compatible, preserving enabled state. Import before
game selection still saves the asset and explains the next step. A `warp`
shader is saved but remains unselected until **Settings → Image distortion →
Allow arbitrary distortion** is explicitly chosen. The host does not silently switch
input modes. An AI prompt example is in the [shader guide](../../docs/shaders/README.en.md),
not in the menu. The user sends it to an AI and saves the returned file. There is no code-paste editor or
automatic AI request.

**Tools → Test scene** launches the fixed bundled color-field test card as
an independent window. The user selects it under **Game** and operates the
normal effect. The host owns only that child, reuses its window, and closes it
with WM_CLOSE plus a bounded exit wait before host exit or update installation.
It does not kill an unrelated test card or accept arbitrary process arguments.
The engine's preview RPC remains for compatibility/developer checks; the tray
has no embedded A/B renderer.

## Lifetime, cancellation and protocol

Single-instance handling runs before engine creation. A second launch normally
shows a native message directing the user to the existing tray. Tauri owns the
one visible icon and native menu; the controlled Go engine retains a hidden
Win32 controller for hotkeys and recovery without registering another icon or
opening its legacy settings. Standalone Go CLI use retains the original Win32
interface.

The host polls the authoritative snapshot, source catalog and shader catalog,
with one refresh in flight and a three-second timeout per read. Native menu
updates run on the main thread. Config-changing actions are serialized; Power
remains available to cancel an in-flight action. Off always invokes `emergency`,
including format-only, pending confirmation, unsaved or recovery states. Unknown
state also offers Off rather than attempting an implicit start.

Each apply clones a fresh config and includes `expected_emergency_sequence` plus
`expected_config`. Go checks both before replacing state. The config comparison
is optional only for legacy clients; the new tray always sends it. Rejected
changes are not automatically retried against newer state. The host captures
intent before native dialogs and observes advancing engine emergency events,
including hotkeys, to cancel pending follow-up work. Responses from before the
emergency cannot restore stale menu state. If a refresh fails, stale
editable state is discarded and the emergency action remains reachable. Runtime
reasons and operation failures are available through **Tools → Last error…**.

On Windows, icon reachability is checked via `Shell_NotifyIconGetRect`.
tray-icon handles Explorer's TaskbarCreated notification for the same native
icon. A missing icon triggers emergency restoration; persistent loss or initial
creation failure triggers native messages and a safe shutdown or retry, without
a fallback WebView. This behavior still needs real Explorer-restart qualification.

Explicit Quit waits for the engine cleanup response and actual process exit.
If the engine has already terminated and cleanup failed, explicit Quit closes
the host with exit code 1; an unknown or still-running process continues to block
exit. Failed restoration is reported and can be retried. Update installation
requires successful cleanup. The stdout reader drains the
final cleanup response before process-exit handling finalizes the result. Host
failure closes stdin, which requests Go cleanup; forceful OS/session shutdown
requires separate runtime testing.

Protocol v1 is one JSON object per line. Requests are allowlisted and limited
to 120 KiB, leaving room under Go's 128 KiB envelope limit. There are at most
16 ordinary pending requests, plus reserved emergency/quit capacity; responses
are limited to 2 MiB. Normal mutation requests time out after 12 seconds. A
timeout means the outcome must be refreshed, not that the mutation was undone.
Errors retain the complete response, including an authoritative `result` after
an applied-but-unsaved change. Engine stderr is drained separately from stdout.
Diagnostics remain in the Go rotating log; clipboard diagnostics do not upload
anything.

## Updates and license packaging

Ordinary developer builds are **unconfigured** and send no update requests.
Official release builds embed the LumaTape endpoint and public key; no feed,
signing key or credentials from another product are used. There is no WebView2
bootstrap download; its installer mode is `skip`.

A release operator must supply a new LumaTape public key and HTTPS feed at build
time using `LUMATAPE_UPDATE_PUBLIC_KEY` and `LUMATAPE_UPDATE_ENDPOINT`, produce
signed Tauri updater artifacts, and qualify the installed-to-updated Windows
scenario. `createUpdaterArtifacts` is deliberately false in the unconfigured
build. Signing secrets never belong in this repository or packaged resources.

The configured tray checks once 20 seconds after startup, without a popup, and
on demand. Only a user-confirmed current-user NSIS installation can install an
update; portable/copy identity is rejected before download or engine shutdown.
Metadata is bounded at 64 KiB and the signed installer at 256 MiB. Version, target,
immutable URL, full minisign signature and PE version are checked before shutdown.
Progress appears in the native menu. The host launches the NSIS process in
Windows only after engine cleanup and checks the launch result. A successful
launch allows the host to exit; a failure keeps the tray with a restart
instruction. This replaces the unchecked Windows process launch in the official updater; it does not claim automatic
rollback or successful installation merely from process launch.

See [signed update preparation and qualification](../../docs/UPDATES.en.md) for the
local release scripts, dedicated key configuration, and remaining installed-update
runtime checks. Signature verification is separate from Windows Authenticode
publisher signing.

The native shell calls `GetUserDefaultUILanguage` at launch: a Russian primary
language selects RU; otherwise EN. It passes the normalized language to its Go
engine and testcard. A standalone Go process detects Windows UI language itself.
User content, protocol fields and profile values stay untranslated. For isolated
native smoke only, `LUMATAPE_NATIVE_UI_TEST=1` plus `LUMATAPE_TEST_LANGUAGE=ru/en`
overrides detection without changing Windows settings. No language preference is
stored in the profile. Technical OS/driver details may remain in their original language.

From the repository root,
`python3 desktop/scripts/licenses.py --output build/desktop-licenses/rust`
collects full available texts from the locked Windows dependency graph using
offline Cargo metadata. Run it after fetching/building dependencies. Missing
full texts are errors; an SPDX declaration alone is not a replacement.

Tray packaging does not collect npm notices: React, shadcn and the Mona Sans font
are used only by the archived panel and are not included in this package.
`npm ci` remains a build step for the pinned Tauri CLI; `node_modules` is not shipped.
Rust notices are retained for the entire locked Windows graph, including Wry
dependencies: creating no WebView window does not remove them from the Cargo graph.
Packaging verifies the Cargo.lock hash and each license text checksum,
copying only manifest-listed paths alongside native notices.
Sync-conflict `LICENSE 2` copies cannot enter the ZIP.

For separate builds of the archived panel, the collector remains available:
`python3 desktop/scripts/npm-licenses.py --output build/archive-ui-licenses/npm`.
It reads installed dependencies after `npm ci`, performs no network request,
records integrity/source, and rejects missing texts or version mismatches.
CI checks it separately from the tray application build.

Match concrete artifact hashes to the release checksum and package manifest.
The [Windows protocol](../../docs/WINDOWS_VALIDATION.en.md) separates Windows, GPU,
display and update qualification. A process/window inventory cannot establish
that every menu command works, shaders look correct, physical keyboard commands
arrive or recovery succeeds.
