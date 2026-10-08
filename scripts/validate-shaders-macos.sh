#!/bin/sh
# Requires the macOS SDK already shipped with Xcode / Command Line Tools.
# Does not install dependencies, modify shaders, or claim Windows validation.
set -eu
cd "$(dirname "$0")/.."
if [ "$(uname -s)" != Darwin ]; then
    echo "This optional CGL validation harness requires macOS." >&2
    exit 2
fi
out=${LUMATAPE_SHADER_OUTPUT:-artifacts/shader-validation}
export LUMATAPE_SHADER_OUTPUT="$out"
mkdir -p "$out"
xcrun clang -std=c11 -O2 -Wall -Wextra -Werror scripts/validate-shaders-macos.c \
    -framework OpenGL -lm -o "$out/validate-shaders"
shasum -a 256 internal/render/shaders/fullscreen.vert internal/render/shaders/crt.frag internal/render/shaders/overlay.frag \
    > "$out/shader-sha256.txt"
"$out/validate-shaders" > "$out/results.txt" 2>&1 || {
    cat "$out/results.txt"
    exit 1
}
cat "$out/results.txt"
for image in "$out"/*.ppm; do
    sips -s format png "$image" --out "${image%.ppm}.png" >/dev/null
done
echo "Native shader evidence and previews: $out/"
