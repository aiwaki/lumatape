# LumaTape

<div align="center">

<img src="desktop/src-tauri/icons/icon.png" width="96" height="96" alt="LumaTape app icon">

[Download for Windows](https://github.com/aiwaki/lumatape/releases/latest) · [Русский](README.md)

</div>

LumaTape adds CRT/VHS effects over a game window: TV scanlines, a soft glow,
and videotape distortion. It has five presets and supports your own effects.
You can also give the screen rounded corners or a curved shape.

LumaTape is free and open source, for Windows 10/11 x64.
You control it from the system tray; there is no separate settings window.
The UI follows the Windows display language: Russian or English.

## Install and start

1. Download and run `LumaTape_<version>_x64-setup.exe` from the
   [latest release](https://github.com/aiwaki/lumatape/releases/latest).
   For a portable copy, extract the entire ZIP and open `lumatape.exe`;
   keep the `engine` folder beside it.
2. Start a game in windowed or borderless mode. Click the LumaTape icon
   in the Windows notification area and select its window under "Game".
3. Choose an "Effect", select "Turn on", and return to the game.
   Try VHS Tape first. You can switch effects while playing;
   your choice takes effect immediately.
4. "Turn off" removes the effect and restores changes made by LumaTape.
   "Quit" closes the application.

To try it without a game, open "Tools → Test scene" and select it under "Game".
F11 makes the test scene fullscreen; Escape returns it to a window.

## Shortcuts

| New-profile shortcut | Action |
|---|---|
| Ctrl+Shift+9 | Turn the effect on / off |
| Ctrl+Shift+0 | Emergency off and restore |

Existing shortcuts are preserved. See them under "Settings → Hotkeys".

## Your own effects

Choose "Add effect from file…" and open `.lumatape.glsl`. Once checked, it appears
under "Effect". The [guide](docs/shaders/README.en.md) has examples and an AI prompt
if you want to create your own file. LumaTape itself makes no AI requests.

## Updates and help

Check for a new version through "Tools → Check for updates…". Installation requires
confirmation. Update a portable ZIP copy by extracting the new package separately.

If something goes wrong, open "Tools → Last error…" or "Copy diagnostics".
Diagnostics are never sent automatically.
[Report an issue](https://github.com/aiwaki/lumatape/issues) · [Settings and documentation](docs/README.en.md)

Requires Windows 10 version 2004 or later, x64, OpenGL 3.3, and SDR.
Run games in windowed or borderless mode; HDR and exclusive fullscreen
are unsupported.
This is a preview. It has been tested in Windows through Parallels; physical GPU
support and real-game compatibility still need validation.

[MIT](LICENSE) · [Component licenses](third_party/README.en.md)
