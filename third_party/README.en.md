# Third-party components

[Русский](README.md) · **English**

* GLFW 3.4, zlib/libpng license: pinned commit `a74efa0d5628b74adc0426af4c5710e287fa7c2c`. CMake installs the upstream LICENSE.md beside distributed binaries.
* Go runtime/standard library, BSD: license retained for the Go executables.
* Microsoft C++/WinRT, MIT: Windows SDK projection/runtime headers used by the optional capture bridge.
* Microsoft STL, Apache-2.0 WITH LLVM-exception: C++ standard-library headers used by the native bridge.
* Rust/Tauri dependency notices: `desktop/scripts/licenses.py` collects full license texts from the locked Windows Cargo graph. The bundle also includes pinned supplemental provenance.
* React, Radix, Lucide and other frontend runtime dependencies: `desktop/scripts/npm-licenses.py` collects their notices from the npm lock, including Tailwind/generated-CSS notices where applicable. Package version, registry source/integrity and text hashes are retained; build tools and node_modules are not shipped.
* Copied shadcn/ui components: the upstream MIT license and pinned provenance are retained in `frontend-provenance.json` and included in the generated frontend notices. The component list is in `desktop/ui/README.md`.

These notices apply to the listed dependencies; the root MIT license covers LumaTape's own code. Lightweight-only distributions do not include the optional capture bridge.

Windows SDK and MSVC build files remain external build-tool dependencies; their headers and import libraries are not vendored or included in the application bundle.

Release packaging copies four explicit native notices and only manifest-declared
Rust/npm full-text paths. It rechecks the hashes of lock files, provenance and
license texts, then regenerates the reader indexes; unreferenced files and sync-conflict copies are excluded.
