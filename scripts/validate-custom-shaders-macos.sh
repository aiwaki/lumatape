#!/bin/sh
# Real driver math checks. Does not qualify WGL, WGC or Windows lifecycle.
set -eu
cd "$(dirname "$0")/.."
[ "$(uname -s)" = Darwin ] || { echo 'Requires macOS CGL' >&2; exit 2; }
out=artifacts/customization-validation
mkdir -p "$out"
cat > "$out/identity.lumatape.glsl" <<'SHADER'
/* LumaTape
{"version":1,"name":"Identity fixture","description":"validation","coordinates":"preserve","parameters":[]}
*/
vec3 lumatape(vec2 uv){return ltSample(uv);}
SHADER
cat > "$out/invert.lumatape.glsl" <<'SHADER'
/* LumaTape
{"version":1,"name":"Invert fixture","description":"validation","coordinates":"preserve","parameters":[]}
*/
vec3 lumatape(vec2 uv){return vec3(1)-ltSample(uv);}
SHADER
go run ./scripts/shader-wrap --input "$out/identity.lumatape.glsl" --output "$out/identity.frag"
go run ./scripts/shader-wrap --input "$out/invert.lumatape.glsl" --output "$out/invert.frag"
go run ./scripts/custom-shader-fixtures --out "$out" > "$out/shader-fixtures.txt"
xcrun clang -std=c11 -O2 -Wall -Wextra -Werror scripts/validate-custom-shaders-macos.c -framework OpenGL -lm -o "$out/validate-custom"
"$out/validate-custom" "$out/identity.frag" "$out/invert.frag" "$out/amber-crt.frag" "$out/cold-bleed.frag" "$out" > "$out/shader-results.txt" 2>&1 || { cat "$out/shader-results.txt"; exit 1; }
shasum -a 256 internal/render/custom.go internal/render/shaders/fullscreen.vert examples/shaders/amber-crt.lumatape.glsl examples/shaders/cold-bleed.lumatape.glsl "$out"/*.frag "$out"/*.params > "$out/shader-sha256.txt"
cat "$out/shader-results.txt"

for name in amber-crt cold-bleed; do
    sips -s format png "$out/custom-$name.ppm" --out "$out/custom-$name.png" >/dev/null
done
echo "A/B readback images: $out/custom-amber-crt.png and $out/custom-cold-bleed.png"
