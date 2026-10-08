# LumaTape documentation

[Русский](README.md) · **English**

Installation and first steps: [Русский](../README.md) · [English](../README.en.md).
The main application is a Windows app with a native Tauri tray. The retained
React prototypes and standalone Go/Win32 interface are not its current UI.

| Task | Document |
|---|---|
| Import an effect or create one with AI | [Русский](shaders/README.md), [English](shaders/README.en.md) |
| Write a shader using the v1 contract | [Technical specification, in Russian](SHADER_SPEC.md), [examples](../examples/shaders) |
| Build the tray application | [Desktop host](../desktop/src-tauri/README.en.md) |
| Prepare a signed update | [Updates](UPDATES.md) |
| Understand capture and rendering | [Architecture](ARCHITECTURE.md), [capture DLL](../native/capture/README.en.md) |
| Validate a game, geometry and recovery | [Windows protocol](WINDOWS_VALIDATION.md) |
| Understand cursor projection | [Pointer](../internal/pointer/README.en.md) |
| Understand display-mode changes | [Display](../internal/display/README.en.md) |
| Check dependency licenses | [Third-party notices](../third_party/README.en.md) |

## Image settings

**Processing.** New profiles use Auto: GPU first, then compatible CPU processing
only for a classified GPU-interop incompatibility. The presence of a capture DLL
does not prove support for `WGL_NV_DX_interop2`. Other capture errors do not cause
a hidden backend switch. Saved manual profiles stay manual. CPU processing is
capped at 30 FPS. Lightweight supports only overlay effects and cannot replace a
full image-processing shader.

**Screen shape.** Flat adds no frame. Rounded CRT adds antialiased opaque corners,
edge shading and glass while preserving pixel positions. Curved CRT warps the
image and requires Full. The radius is relative to the shorter image dimension.
Turning the effect off also removes its screen shape.

**Image distortion.** Both options keep keyboard and mouse input working.
“Accurate clicks” allows the built-in curvature: its known geometry is used to
project the system cursor's image. Windows coordinates, button events, the wheel,
Raw Input and mouse capture remain unchanged. This is not general game-input
remapping. Crop/Stretch, custom DAR and a custom `warp` shader require an explicit
choice of “Allow arbitrary distortion”. System-cursor projection is disabled for
a custom `warp`; an author's `preserve` declaration alone does not prove that
clicks align.

**Scaling and older games' aspect ratios.** Fit preserves the whole frame; Crop
fills the area by trimming edges; Stretch stretches the image. Usually keep the
source aspect ratio. DAR 4:3 is useful, for example, for a 320×200 framebuffer
designed for an older display's non-square pixels. 1280×1024 is 5:4, not 4:3.
Fitting 16:9 inside a 4:3 area still displays a 16:9 image.

**4:3 format.** For a normal game, set the format in the game itself. Masking and
client-window resizing were removed from the menu: masking only covered part of
the frame, and resizing caused recurring side-field flicker in Parallels. Older
JSON/CLI profiles remain readable. Their active state is shown as “from profile”;
“Original format” or “Turn off” restores LumaTape's own changes.

“Windows resolution…” uses only modes enumerated by Windows, tests the candidate
and applies it temporarily without writing to the registry. “Keep display mode”
is available for 15 seconds after application. A separate watchdog restores the
mode on timeout, exit or main-process crash. A later detected user change is not
overwritten. Win32 has no atomic compare-and-set: a short race or a change and
reversal between polls cannot be ruled out. The watchdog does not cover both
processes terminating together or an OS/driver failure. A GPU/display may stretch
4:3; there is no universal GPU-scaling control.

## Shortcuts, language and storage

In a new profile, Ctrl+Shift+9 toggles the effect; Ctrl+Shift+0 performs emergency
off and restores changes. Existing bindings are preserved. The alternative pair
is Ctrl+Alt+F9 / Ctrl+Alt+F10. A registration conflict leaves the working pair in
place. The menu's shortcut test distinguishes registration from actual receipt;
the commands continue to act while the test is open.

On a Mac, Alt is Option ⌥ and Control is not Command. Function keys may need Fn;
delivery depends on Parallels. LumaTape does not change host settings.
[Windows keys on a Mac](https://support.apple.com/guide/mac-help/windows-keys-on-a-mac-keyboard-cpmh0152/mac),
[Parallels keyboard settings](https://kb.parallels.com/en/114309).
If changing focus in Parallels changes mouse speed or button state, check Smart
Mouse: **Don't optimize for games** resolved this in the tested configuration.
That is a VM setting, not a LumaTape setting.

Language is selected at startup from the Windows UI language: RU → Russian;
otherwise → English. Regional formats do not affect it; restart the application
after changing the Windows language. Game, preset and custom-shader names are not
translated. Technical OS/driver messages may remain in their original language.

Settings and logs: `%APPDATA%\LumaTape`. Shader library:
`%LOCALAPPDATA%\LumaTape\Shaders`. Saved JSON/CLI profiles continue to work:
intensity 0 bypasses the filter and shape while retaining independent 4:3.
The tray uses 100% when an effect is explicitly selected or enabled; simply
reading an older profile does not overwrite its values.

## Diagnostics and limitations

“Copy diagnostics” excludes screenshots, full paths, shader code and other
windows' titles; nothing is sent automatically. The local JSON log uses a bounded
queue and continuous rotation: the current file and three backups, each up to
4 MiB. Dropped events and write failures are counted.

Measurements distinguish CPU submission, GPU shader time, CPU transfer and
captured-frame age. These are not total input-to-display latency; GPU shader time
must not be presented as the whole application's latency. `null` means a
measurement is unavailable. Saving a diagnostic PNG synchronizes the GPU and is
not suitable for performance measurement.

Full works in SDR and rejects active HDR/Advanced Color. Exclusive fullscreen,
protected windows/DRM, anti-cheat, whole-monitor Full capture and compatibility
with every game are not claimed. Windows may display a capture-indicator border.
During movement, resizing, focus loss or stale frames, the effect may temporarily
hide and reveal the original; returning requires a fresh frame. WGC may stop
sending frames for a completely static window.

The system cursor is projected only for the known built-in geometry. XOR-inverting
cursors are unsupported, and animated system-cursor timing is not reproduced.
A cursor drawn by a game stays part of its frame. A separate watchdog restores
the normal cursor after failure or more than 500 ms without frames. If the engine
fully hangs, the frozen image itself may remain until the engine resumes or
terminates. The fix for a doubled cursor at Parallels window boundaries has not
yet been confirmed by physical observation.

## Building and validation

Build Windows x64 with locked dependencies. Requirements: Go ≥1.23, Rust,
Node/npm for the Tauri CLI, Python ≥3.11, Git, CMake ≥3.24 and Visual Studio 2022
Build Tools with **Desktop development with C++**, C++20 and Windows SDK ≥10.0.26100.
The build SDK is newer than the minimum runtime OS; the APIs used are limited to
build 19041.

```powershell
.\scripts\build-desktop.ps1 -Version 0.3.1
```

The script builds the native DLLs, Go engine/testcard/watchdog, resources and
Tauri host, then creates a clean ZIP, SHA256, metadata and licenses. React/Vite
is not built. Signed NSIS packages are built separately using the
[update instructions](UPDATES.md).

Cross-building on macOS/Linux needs the `x86_64-pc-windows-msvc` target,
`cargo-xwin`, LLVM and an explicitly prepared native CMake install directory:

```sh
scripts/build-desktop.sh /absolute/native-install 0.3.1
```

The version argument must match `desktop/package.json`, `Cargo.toml` and
`tauri.conf.json`. Resource preparation and local startup are described in
[Desktop host](../desktop/src-tauri/README.en.md).

```sh
sh scripts/check.sh                        # Go unit/race/vet + Windows cross-build
sh scripts/validate-shaders-macos.sh        # встроенные шейдеры, Mac CGL/OpenGL
sh scripts/validate-custom-shaders-macos.sh # пользовательские шейдеры, Mac CGL/OpenGL
cd desktop && npm run test:host            # Rust host/menu/updater
```

These are separate validation layers: portable logic, Mac CGL, Windows CPU
compatibility and physical Windows GPU. Passing unit tests or a cross-build does
not prove image quality, real-click alignment or a particular game's compatibility.
For Windows checks, use the [protocol](WINDOWS_VALIDATION.md) and the bundled test
scene. `lumatape-testcard.exe --fullscreen --pointer-test` starts the cursor-test
scene without a border; `--color-field` helps assess noise and chroma.

The separate `scripts/build-windows.ps1` builds the legacy/CLI Go application with
Win32 UI, not the main Tauri host. Its `--snapshot` is for diagnostics, not gaming.
Tests of the retained `desktop/ui` and prototypes do not qualify the current tray.

Portable settings and geometry are in `internal/config`, `internal/geometry` and
`internal/display`; shaders are in `internal/render/shaders`, and the platform
layer is in `internal/platform`. The tray model is
`desktop/src-tauri/src/tray_model.rs`, dispatch is in `tray.rs`, and native dialogs
are in `native_dialog.rs`.
