# LumaTape

<div align="center">

<img src="desktop/src-tauri/icons/icon.png" width="96" height="96" alt="LumaTape app icon">

[Download for Windows](https://github.com/aiwaki/lumatape/releases/latest) · [Русский](README.md)

</div>

CRT and VHS for games: TV scanlines, a soft glow, and videotape distortion.
LumaTape overlays effects on a game window and can give the screen rounded
corners or a curved shape, like an old TV.
No need to haul the TV down from the attic.

Five presets are included, and you can add your own effects from a file.
All controls live in the system tray, next to the Windows clock.
LumaTape is free and open source. It runs on Windows 10/11 x64 and uses
Russian or English to match the Windows display language.

## Switch it on

1. Download and run `LumaTape_<version>_x64-setup.exe` from the
   [latest release](https://github.com/aiwaki/lumatape/releases/latest).
   For a portable copy, extract the entire ZIP and open `lumatape.exe`;
   keep the `engine` folder beside it.
2. Start a game in windowed or borderless mode. Click the LumaTape tray icon
   and select the game's window under "Game".
3. Choose an effect under "Effect", select "Turn on", and return to the game.
   Try VHS Tape first. You can change effects while playing;
   they take effect immediately.
4. "Turn off" removes the effect and restores changes made by LumaTape.
   "Quit" closes the application.

You can try the test picture without launching a game:
open "Tools → Test scene" and select it under "Game".
F11 makes the test scene fullscreen; Escape returns it to a window.

## Shortcuts

| New-profile shortcut | Action |
|---|---|
| Ctrl+Shift+9 | Turn the effect on / off |
| Ctrl+Shift+0 | Emergency off and restore |

Existing shortcuts are preserved.
Find your current bindings under "Settings → Hotkeys".

## Your effect collection

Choose "Add effect from file…" and open a `.lumatape.glsl` file.
Once checked, the effect appears under "Effect".
The [guide](docs/shaders/README.en.md) has examples and a ready-to-use AI prompt.

Requires Windows 10 version 2004 or later, x64, OpenGL 3.3, and SDR.
HDR and exclusive fullscreen are unsupported.
This is a preview: so far, it has been tested in Windows through Parallels.
Physical GPU support and real-game compatibility still need validation.

[Documentation](docs/README.en.md) · [MIT](LICENSE) · [Component licenses](third_party/README.en.md)
