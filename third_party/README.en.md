# Third-party components

[Русский](README.md) · **English**

## Current application

LumaTape uses a native Tauri tray and local HTML/CSS/JavaScript settings.
A WebView is created only when settings open. The React panel is not included;
current settings import no npm packages.

* GLFW 3.4, zlib/libpng license: pinned commit `a74efa0d5628b74adc0426af4c5710e287fa7c2c`. CMake installs the upstream LICENSE.md beside distributed binaries.
* Go runtime/standard library, BSD: license retained for the Go executables.
* Microsoft C++/WinRT, MIT: Windows SDK projection/runtime headers used by the capture bridge.
* Microsoft STL, Apache-2.0 WITH LLVM-exception: C++ standard-library headers used by the native bridge.
* Rust/Tauri dependency notices: `desktop/scripts/licenses.py` collects full license texts from the locked Windows Cargo graph. The bundle also includes pinned supplemental provenance.

These notices apply to the listed dependencies; the root MIT license covers
LumaTape's own code. The Rust graph still includes Tauri's transitive dependencies,
including `wry` and `webview2-com`, which the settings window uses; their notices
are retained. Microsoft Edge WebView2 Runtime is installed separately from the
portable ZIP; NSIS includes a bootstrapper to install it if needed.

Windows SDK and MSVC build files remain external build-tool dependencies; their headers and import libraries are not vendored or included in the application bundle.

Packaging copies four explicit native notices and the full Rust license texts
listed in the manifest. It verifies the Cargo.lock and license-text hashes,
then regenerates the reader index. Unreferenced files and sync-conflict copies
are excluded.

## Archived React panel

React, Radix, Lucide, Tailwind, shadcn/ui and the Mona Sans font belong
to the [retained prototype](https://github.com/aiwaki/lumatape/blob/main/desktop/ui/README.en.md). Its sources, licenses
and [provenance](https://github.com/aiwaki/lumatape/blob/main/third_party/frontend-provenance.json) remain in the repository.
`desktop/scripts/npm-licenses.py` collects notices for that prototype;
the application build no longer includes them. Node.js/npm are still needed
to run the pinned Tauri CLI during builds, but not to run the application.

The published 0.3.4 package still contains a `licenses/npm` directory with notices
from the former panel. The React panel itself is not included in that package.
