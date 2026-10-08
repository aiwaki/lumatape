# Initial architecture research

[Русский](PRIOR_ART_AUDIT.md) | [English](PRIOR_ART_AUDIT.en.md)

This is a historical record of the research and comparison completed on
2026-10-03. The file counts and findings below describe the repositories as they
stood on that date.

The original task included reviewing the user's [old windowing library](https://github.com/NeuralTeam/exwin/tree/efe4bef5c86bbc3017a2372a0fce77407c757cb7),
including `examples/widget/widget.go`, the windowing and rendering layers, Win32
helpers and go.mod. That checkout had no AGENTS.md at the time. The source
repository was not modified. The link records the history of technical decisions;
it is neither LumaTape's name nor a dependency.

| Area | Observation | Decision |
|---|---|---|
| internal/window/init.go | GLFW.Init runs in init; Terminate is called from a signal goroutine | Explicit startup and shutdown on the initial thread |
| internal/window/window.go | Creation in a goroutine, a window/err race, reflect/busy-wait; a startup error can leave an infinite wait | Synchronous constructor returning error; a single resource owner |
| internal/backend/backend.go | sync.Map mixes the render callback with commands; Load busy-waits; pass ordering is unspecified | Sequential rendering and a separate command FIFO |
| internal/window/window_windows.go | Repeated BringToTop, timer goroutine; Stop does not end range over ticker.C | Floating + WS_EX_NOACTIVATE; no window-raising loops |
| pkg/window/affinity_windows.go | SetWindowDisplayAffinity result is ignored | Check the result and read back the applied value, without guaranteeing monitor capture behavior |
| examples/widget/widget.go | Canvas and robotgo are used for text and screen size | Direct GLSL and physical Win32 geometry |
| go.mod | Old GLFW/OpenGL versions, robotgo, Canvas, OCR and transitive dependencies | Pinned GLFW 3.4; system APIs; a small optional WGC DLL |

## Code provenance at the time of comparison

The comparison was repeated on 2026-10-03. The review covered 16 Go files from the
original library, including the example. They were compared with the 39
Go/C++/GLSL files in LumaTape at that time; no matching sequence of five or more
nonempty lines was found. Lifecycle, backend, GLFW hints, Win32 styles and capture
affinity were also compared manually. The sequence check is supporting evidence;
the conclusion also rests on a function-level review.

No substantial transferred fragments, old types/queues or original example were
found in LumaTape. The window lifecycle, GLFW loader, renderer, Win32 helpers and
capture bridge were written anew. The transparent-window idea and standard
Windows/GLFW APIs and numeric flags remain shared. The old library is not
imported, vendored or shipped with the application.

The former root copyright line and a separate copy of the reviewed repository's
license were removed from LumaTape because they misrepresented the new
implementation's provenance. The root MIT LICENSE covers LumaTape code,
Copyright (c) 2026 aiwaki. Notices for the dependencies actually used, GLFW, Go,
C++/WinRT and Microsoft STL, remain in `third_party` and the packaging rules.

Documentation reviewed when selecting APIs:

- https://www.glfw.org/docs/3.4/intro_guide.html#thread_safety
- https://www.glfw.org/docs/3.4/window_guide.html#window_transparency
- https://learn.microsoft.com/en-us/windows/apps/develop/media-authoring-processing/screen-capture
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowdisplayaffinity
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumdisplaysettingsw
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-changedisplaysettingsexw
- https://registry.khronos.org/OpenGL/extensions/NV/WGL_NV_DX_interop2.txt
