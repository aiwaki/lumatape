#!/bin/sh
# Cross-build a signed Windows release on macOS/Linux. Never publishes or creates keys.
set -eu
cd "$(dirname "$0")/.."
native=${1:?Usage: scripts/build-update.sh /absolute/native-install [version]}
version=${2:-$(python3 -c 'import json; print(json.load(open("desktop/package.json"))["version"])')}
: "${TAURI_SIGNING_PRIVATE_KEY:?Supply the dedicated LumaTape key or its file path in the environment}"
: "${LUMATAPE_BUILD_TIME:?Supply an ISO UTC build timestamp}"
: "${LUMATAPE_COMMIT:?Supply the source revision}"
case "$version" in *[!0-9.]*|'') echo 'Use a stable X.Y.Z version' >&2; exit 1;; esac
output="build/update-$version"
test ! -e "$output" || { echo 'Update staging exists; preserve it before a new build' >&2; exit 1; }
export LUMATAPE_UPDATE_ENDPOINT='https://github.com/aiwaki/lumatape/releases/latest/download/latest.json'
export LUMATAPE_UPDATE_PUBLIC_KEY
LUMATAPE_UPDATE_PUBLIC_KEY=$(cat desktop/src-tauri/updater.pub)
sh scripts/build-desktop.sh "$native" "$version"
mkdir -p "$output"
python3 - "$output/tauri-update.json" <<'PY'
import json, os, sys
from pathlib import Path
Path(sys.argv[1]).write_text(json.dumps({
    'bundle': {'createUpdaterArtifacts': True},
    'plugins': {'updater': {'pubkey': os.environ['LUMATAPE_UPDATE_PUBLIC_KEY'],
                            'endpoints': [os.environ['LUMATAPE_UPDATE_ENDPOINT']]}}
}) + '\n')
PY
config="$(pwd)/$output/tauri-update.json"
(cd desktop && npm run tauri -- bundle --target x86_64-pc-windows-msvc --bundles nsis --config "$config" --ci)
name="LumaTape_${version}_x64-setup.exe"
bundle=desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis
cp "$bundle/$name" "$bundle/$name.sig" "$output/"
python3 scripts/prepare-update.py --installer "$output/$name" --signature "$output/$name.sig" \
    --public-key desktop/src-tauri/updater.pub --version "$version" \
    --published-at "$LUMATAPE_BUILD_TIME" --output "$output/index"
# Tauri's bundler patches the EXE with its bundle type. Package those exact bytes
# again so the portable manifest and installed host agree with the final binary.
python3 scripts/package-desktop.py --shell desktop/src-tauri/target/x86_64-pc-windows-msvc/release/lumatape.exe \
    --engine desktop/src-tauri/resources/engine --licenses build/desktop-licenses \
    --version "$version" --revision "$LUMATAPE_COMMIT" --build-time "$LUMATAPE_BUILD_TIME" --update-channel configured
echo "Signed candidate: $output. Publication and Windows runtime qualification are separate."
