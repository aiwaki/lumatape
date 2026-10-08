#!/bin/sh
# Build the Windows Tauri shell on macOS/Linux using official MSVC libraries.
# Native input is an explicit CMake install directory, never discovered in artifacts/.
set -eu
# Keep the default build within an 8 GB host; callers can explicitly override.
export CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}"
cd "$(dirname "$0")/.."
native=${1:?Usage: scripts/build-desktop.sh /absolute/native-install [version] [output-directory]}
version=${2:-0.3.3}
output=${3:-dist/lumatape-$version-windows-x64}
revision=${LUMATAPE_COMMIT:-working-tree}
stamp=${LUMATAPE_BUILD_TIME:-unknown}
update_channel=unconfigured
if [ -n "${LUMATAPE_UPDATE_ENDPOINT:-}" ] || [ -n "${LUMATAPE_UPDATE_PUBLIC_KEY:-}" ]; then
    if [ -z "${LUMATAPE_UPDATE_ENDPOINT:-}" ] || [ -z "${LUMATAPE_UPDATE_PUBLIC_KEY:-}" ]; then
        echo 'Update channel requires both LUMATAPE_UPDATE_ENDPOINT and LUMATAPE_UPDATE_PUBLIC_KEY; no build started' >&2
        exit 1
    fi
    update_channel=configured
fi
case "$version:$revision:$stamp" in *[!A-Za-z0-9.:_+-]*) echo 'Invalid build metadata' >&2; exit 1;; esac
python3 - "$version" <<'PY'
import json,sys,tomllib
v=sys.argv[1]
for f in ['desktop/package.json','desktop/src-tauri/tauri.conf.json']:
    assert json.load(open(f))['version']==v, f+' version mismatch'
assert tomllib.load(open('desktop/src-tauri/Cargo.toml','rb'))['package']['version']==v
PY
python3 - "$native" <<'PY_INPUT'
from pathlib import Path
import sys
native = Path(sys.argv[1]).resolve()
for generated in [Path('desktop/src-tauri/resources/engine').resolve(), Path('build/desktop-licenses').resolve()]:
    if native == generated or generated in native.parents:
        raise SystemExit('Native input must be outside generated engine/license staging')
PY_INPUT
for name in glfw3.dll lumatape_capture.dll; do test -f "$native/$name"; done
test -d "$native/licenses"
stage=desktop/src-tauri/resources/engine
mkdir -p "$stage"
# Only this generated resource directory is cleaned.
find "$stage" -type f -delete
go run ./scripts/resources -version "$version"
metadata="-X github.com/aiwaki/lumatape/internal/diagnostics.Version=$version -X github.com/aiwaki/lumatape/internal/diagnostics.Commit=$revision -X github.com/aiwaki/lumatape/internal/diagnostics.BuildTime=$stamp"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui $metadata" -o "$stage/lumatape-engine.exe" ./cmd/lumatape
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$metadata" -o "$stage/lumatape-watchdog.exe" ./cmd/lumatape-watchdog
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui $metadata" -o "$stage/lumatape-testcard.exe" ./cmd/lumatape-testcard
cp "$native/glfw3.dll" "$native/lumatape_capture.dll" "$stage/"
python3 - <<'PY_CLEAN'
from pathlib import Path
import shutil
p=Path("build/desktop-licenses")
if p.exists(): shutil.rmtree(p)
PY_CLEAN
mkdir -p build/desktop-licenses/native
cp "$native"/licenses/* build/desktop-licenses/native/
(cd desktop && npm ci --ignore-scripts --no-audit --no-fund)
cargo fetch --manifest-path desktop/src-tauri/Cargo.toml --locked --target x86_64-pc-windows-msvc
python3 desktop/scripts/licenses.py --output build/desktop-licenses/rust
python3 desktop/scripts/npm-licenses.py --output build/desktop-licenses/npm
python3 scripts/stage-installer.py --licenses build/desktop-licenses
(cd desktop && npm run tauri -- build --runner cargo-xwin --target x86_64-pc-windows-msvc --no-bundle -- --locked)
python3 scripts/package-desktop.py --shell desktop/src-tauri/target/x86_64-pc-windows-msvc/release/lumatape.exe --engine "$stage" --licenses build/desktop-licenses --version "$version" --revision "$revision" --build-time "$stamp" --output "$output" --update-channel "$update_channel"
