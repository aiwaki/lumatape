# LumaTape — CRT and VHS effects for games

<div align="center">

<img src="desktop/src-tauri/icons/icon.png" width="96" height="96" alt="LumaTape app icon">

**TV scanlines, a soft glow, and the imperfections of a videotape.**

**[Download for Windows](https://github.com/aiwaki/lumatape/releases/latest)** · [Русский](README.md)

**Windows 10/11 · x64 · Free · Open source · Preview**

</div>

LumaTape adds CRT/VHS effects over a game window. Choose one of five presets,
try a rounded or curved screen, or add your own effect.
Everything is controlled from the system tray, with no separate settings window.
The UI follows the Windows display language: Russian or English.

## Install and start

1. Download and run **`LumaTape_<version>_x64-setup.exe`** from the
   [latest release](https://github.com/aiwaki/lumatape/releases/latest).
   For a portable copy, extract the entire ZIP and open `lumatape.exe`;
   keep the `engine` folder beside it.
2. Start a game in windowed or borderless mode. Click the LumaTape icon
   in the Windows notification area and select its window under **Game**.
3. Choose an **Effect**, select **Turn on**, and return to the game.
   Try **VHS Tape** first; you can switch effects while processing is active.
4. **Turn off** removes the effect and restores changes made by LumaTape.
   **Quit** closes the application.

To try it without a game, open **Tools → Test scene** and select it under **Game**.
F11 makes the test scene fullscreen; Escape returns it to a window.

## Shortcuts

| New-profile shortcut | Action |
|---|---|
| **Ctrl+Shift+9** | Turn the effect on / off |
| **Ctrl+Shift+0** | Emergency off and restore |

Existing shortcuts are preserved. See them under **Settings → Hotkeys**.

## Your own effects

Choose **Add effect from file…** and open `.lumatape.glsl`. Once checked, it appears
under **Effect**. If you need a file, the [guide](docs/shaders/README.en.md) includes
examples and a ready-to-use AI prompt. LumaTape itself makes no AI requests.

## Updates and help

**Tools → Check for updates…** checks for a new version; installation requires
confirmation. Update a portable ZIP copy by extracting the new package separately.

If something goes wrong, open **Tools → Last error…** or **Copy diagnostics**.
Diagnostics are never sent automatically.
[Report an issue](https://github.com/aiwaki/lumatape/issues) · [Settings and documentation](docs/README.en.md)

Requires Windows 10 version 2004 or later, x64, OpenGL 3.3, and SDR; games must run
in windowed or borderless mode — HDR and exclusive fullscreen are unsupported.
This is a preview: it has been tested in Windows through Parallels; physical GPU
support and real-game compatibility still need validation.

[MIT](LICENSE) · [Component licenses](third_party/README.en.md)
