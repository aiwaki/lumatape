English · Русский: README.txt

These fixtures are used only in the explicitly enabled browser demo of the
archived panel (?demo=1). They are actual Mac CGL GLSL framebuffer readbacks
with fixed intensity=100% and a fixed scene, not live Windows capture or a
simulation of arbitrary slider values. The archived panel preview comes from
the Go renderer. The current tray-only app has no embedded preview. CSS does
not generate these images.

Reproduce the source images with:
LUMATAPE_SHADER_OUTPUT=artifacts/macos-ux-validation/regression sh scripts/validate-shaders-macos.sh

polish-<preset>-<shape>.png maps to <preset>-<shape>.png here; original.png comes
from vhs-tape-native-original.png. CRT Classic and Soft TV were refreshed for
scale-aware Full rendering. Subtle CRT and both VHS presets/shapes remain
byte-identical to the earlier fixtures. Local provenance and A/B comparisons:
artifacts/macos-ux-validation/summary.json and demo-fixture-updates.json. No preset
control values changed. These local artifacts are not part of the public package.
