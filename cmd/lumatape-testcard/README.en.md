# LumaTape capture test card

[Русский](README.md) · English

`lumatape-testcard` opens an independent, interactive Win32 window titled **LumaTape test card**. It is a manual test source for LumaTape; passing this test does not establish compatibility with a game or anti-cheat.

## Build and launch

Build on Windows, or cross-compile with no C toolchain:

```powershell
go build -o lumatape-testcard.exe ./cmd/lumatape-testcard
.\lumatape-testcard.exe
.\lumatape-testcard.exe -width 1280 -height 1024
.\lumatape-testcard.exe --color-field
```

```sh
GOOS=windows GOARCH=amd64 go build -o lumatape-testcard.exe ./cmd/lumatape-testcard
```

The default client area is **960×720 physical pixels (4:3)**, in a bordered, resizable window. The 1280×1024 example is deliberately **5:4**. The source requires Windows 10 1703+ for DPI v2; the full LumaTape capture application requires a newer Windows version. It uses standard Win32/GDI only, with cached fonts and a backbuffer recreated on resize. A `WM_TIMER` message requests updates approximately every 16 ms; object motion uses elapsed monotonic time, not frame count. This is not a frame-rate or latency benchmark.

`--color-field` adds static colored midtones for inspecting VHS noise and chroma. The normal live timer and moving square remain available; compare the static regions when evaluating separately captured images. The root `Try-VHS.cmd` launcher selects this view and the stronger VHS Tape preset in explicit Full CPU compatibility, using a separate demo profile.

## Manual checks

1. Start this source, then select its exact window title in LumaTape. Keep `input_mode` set to `mouse-exact`; source and output client rectangles must coincide. Capture should exclude the cursor.
2. At intensity **0**, compare the source with full-mode output. Colors, circle, client edges, grid and button location should match. There should be no dimmed double image, captured title bar, recursion or duplicate pointer.
3. Click **CLICK HERE** at its center and close to every edge. The visible count must increase only inside the visible button. Repeat after dragging to a monitor with negative coordinates, changing DPI, and resizing. An overlay that passes clicks through does not by itself prove coordinate alignment.
4. Try all five builtin presets. VHS Tape should make noise and color bleed more noticeable than VHS Light, especially with `--color-field`. Return focus to the source after choosing a preset in the tray; source-bound effects hide while the tray has focus. Inspect 10/12-pixel text, bright color bars, one-pixel stripes and the moving orange square for blur, clipping, moire, flicker and excessive darkening. The circle must remain circular in preserve/fit mode with square pixels. Explicit DAR changes intentionally reinterpret the source.
5. Alt+Tab, minimize, restore, close the source, then restart it. Verify source-bound effects hide as intended and emergency disable works. Repeat on a real windowed/borderless game afterward.

Do not label crop/stretch or a shifted/scaled presentation mouse-compatible unless input mapping is implemented and verified. This source uses the normal system cursor without painting a second cursor and makes no changes to system display modes.

API references: [DPI v2 initialization](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setprocessdpiawarenesscontext), [physical client sizing](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-adjustwindowrectexfordpi), [painting lifecycle](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-beginpaint), [WM_TIMER scheduling](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-settimer).

Rendering and input must be checked on Windows. Cross-compilation alone does not verify runtime behavior.
