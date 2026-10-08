# LumaTape — CRT and VHS effects for windowed games

<div align="center">

<img src="desktop/src-tauri/icons/icon.png" width="96" height="96" alt="LumaTape app icon">

**TV scanlines, a soft glow, and the imperfections of a videotape.**

Choose a game and an effect in the tray. LumaTape processes the image over its window.

**[Download for Windows](https://github.com/aiwaki/lumatape/releases/latest)** · [Installation](#installation) · [Русский](README.md)

**Windows 10/11 · x64 · SDR · Free · MIT · Preview**

</div>

LumaTape works with windowed and borderless games, without injecting code into
their processes. Everything lives in the system tray: select a game, switch
effects, import a shader, or turn processing off from one menu.

## What it does

- **Five effects:** Subtle CRT, CRT Classic, Soft TV, VHS Light, and VHS Tape.
- **Screen shape:** flat, rounded, or curved. The built-in curved screen supports
  system-cursor projection for accurate clicks.
- **Custom shaders:** import `.lumatape.glsl` files, including ones created with AI.
  The shader defines the look; there is no intensity slider.
- **Keyboard shortcuts:** toggle the effect or immediately turn it off and restore
  LumaTape's changes.
- **Automatic RU/EN:** menus and messages follow the Windows display language.
  Russian is used for RU and English for other languages.

> [!NOTE]
> This is a preview. Full CPU has been tested in Windows through Parallels;
> physical GPU support, real games, and other display configurations still need
> validation. HDR, exclusive fullscreen, and protected windows are unsupported.

## Installation

Requires **Windows 10 version 2004 (build 19041) or later**, x64, and a driver
with OpenGL 3.3 support. WebView2, Node.js, Rust, and Go are not required to run it.

1. Open the [latest release](https://github.com/aiwaki/lumatape/releases/latest).
   Download **`LumaTape_<version>_x64-setup.exe`** to install. For a portable copy,
   extract the entire ZIP and run the root `lumatape.exe`; keep the `engine`
   folder and its DLLs beside it.
2. Click the LumaTape icon in the Windows notification area. Both left and right
   clicks open the menu. There is no separate application window.
3. Start a game in windowed or borderless mode and select it under **Game**.
4. Choose **Effect → VHS Tape** or another preset, select **Turn on**, and return
   to the game. You can switch effects while processing is active.
5. **Turn off** removes the effect and restores window/display-mode changes made
   by LumaTape. **Quit** closes the application.

For a first look, choose **Tools → Test scene**, then select its window under
**Game**. F11 or Alt+Enter makes the scene borderless fullscreen; Escape returns
it to a window. Toggle the effect to compare before and after.

## Your own effects

Already have a file? Choose **Add effect from file…** and open `.lumatape.glsl`.
Once checked and compiled, it appears under **Effect**. If you have not selected
a game yet, the file is still saved to your library for later. A failed import
leaves the working effect intact.

Need a file? Use the prompt below with an AI assistant of your choice, replacing
the effect description. Save its answer as `.lumatape.glsl` and import it through
the menu. LumaTape itself makes no AI requests.

<details>
<summary>Example AI prompt</summary>

```text
Create a complete LumaTape v1 shader: a warm CRT with visible scanlines,
soft glow, and light noise. Return only one .lumatape.glsl file.
This is single-pass GLSL 330 core, SDR sRGB; not Shadertoy or ReShade.
Start with this JSON comment:
/* LumaTape
{"version":1,"name":"Warm CRT","description":"Scanlines, glow, and noise","coordinates":"preserve","parameters":[]}
*/
Then provide vec3 lumatape(vec2 uv), returning RGB sRGB.
The application provides:
- uv: 0..1, origin at top left;
- vec3 ltSample(vec2 uv): game color, with crop/clamp already handled;
- vec2 ltResolution: image-area size in physical pixels;
- float ltTime: time in seconds.
Define the look using constants, without user parameters.
The host handles alpha, CRT shape, aspect ratio, and intensity.
Do not add #version, main, uniforms, in/out/inout, layout, discard,
preprocessor directives, gl_*, frag, uSource*, external textures, or frame history.
for/while/do loops are forbidden: manually unroll a small number of samples.
Helper functions and ordinary GLSL math are allowed. Maximum file size: 64 KiB.
For this color effect, keep coordinates:"preserve" and do not move the image.
```

</details>

[Example shaders](examples/shaders) · [LumaTape Shader v1 specification, in Russian](docs/SHADER_SPEC.md)

Shadertoy/ReShade files need adaptation. Arbitrary geometric distortion
(`coordinates: "warp"`) must be explicitly allowed in Settings; accurate clicks
are not guaranteed for it. Try new shaders on the test scene first.

## Mouse, screen shape, and 4:3

**Settings → Image distortion → Accurate clicks** is the usual choice for mouse
input. It also works with the built-in **Curved CRT**: LumaTape projects the
system-cursor image onto the curved screen while preserving real Windows input.

**Allow arbitrary distortion** enables Crop/Stretch, custom aspect ratios,
and warp shaders. Visible buttons may no longer line up with click positions.
**Keyboard and mouse work in both modes** — this setting controls the image,
not which input devices you can use.

For 4:3, choose the resolution and aspect ratio **inside the game** first.
The additional **4:3 format → Windows resolution…** option temporarily changes
the display mode. Confirm it within 15 seconds or the previous mode is restored.
The final scaling depends on the graphics driver and monitor.

## Shortcuts and updates

| New-profile shortcut | Action |
|---|---|
| **Ctrl+Shift+9** | Turn the effect on / off |
| **Ctrl+Shift+0** | Emergency off and restore |

Existing shortcuts are preserved. Their actions and the alternative F9/F10
profile appear under **Settings → Hotkeys**.

**Tools → Check for updates…** checks for a signed release. A quiet check also
runs after startup; installation requires confirmation. LumaTape first turns
off the effect and restores its changes, then starts the installer. In-place
updates work for the NSIS-installed version. Update a portable ZIP copy by
extracting the new package into a separate folder.

## If something goes wrong

Start with the test scene and check the top menu line for the actual state.
**Full CPU** is the compatibility mode, capped at 30 FPS. **Lightweight** cannot
perform full pixel processing. For color shaders, leave
**Settings → Processing → Automatic** selected.

Under **Tools**, you can view the last error, check shortcut delivery, and copy
diagnostics. The copied text excludes screenshots, full paths, and other windows'
titles; nothing is sent automatically. When reporting a problem, include the
game, Windows version, GPU, effect, and steps to reproduce it.

[Settings and limitations, in Russian](docs/README.md) · [Report an issue](https://github.com/aiwaki/lumatape/issues)

## Development

Tauri 2 owns the tray, Go/Win32 handles capture and restoration, and GLSL/OpenGL
3.3 processes the effects. Game images are processed locally. Settings and logs
live in `%APPDATA%\LumaTape`; shaders live in `%LOCALAPPDATA%\LumaTape\Shaders`.
The application does not need write access beside its executable.

[Build and tests](docs/README.md#сборка-и-проверки) · [Architecture](docs/ARCHITECTURE.md) ·
[Updates](docs/UPDATES.md) · [Documentation](docs/README.md)

LumaTape is licensed under [MIT](LICENSE). Bundled component licenses are listed
in [third_party](third_party/README.md) and included with the builds.
