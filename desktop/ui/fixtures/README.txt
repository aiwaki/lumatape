Explicit browser demo fixtures only (?demo=1). Actual Mac CGL GLSL framebuffer readbacks; fixed intensity=100% and fixed scene, never live Windows capture or a simulation of arbitrary slider values. Production preview comes from the Go renderer. No images are synthesized by CSS.

Reproduce the source images with:
LUMATAPE_SHADER_OUTPUT=artifacts/macos-ux-validation/regression sh scripts/validate-shaders-macos.sh

polish-<preset>-<shape>.png maps to <preset>-<shape>.png here; original.png comes from vhs-tape-native-original.png. CRT Classic and Soft TV were refreshed for scale-aware Full rendering. Subtle CRT and both VHS presets/shapes remain byte-identical to the earlier fixtures. Provenance and A/B comparisons: artifacts/macos-ux-validation/summary.json and demo-fixture-updates.json. No preset control values changed.
