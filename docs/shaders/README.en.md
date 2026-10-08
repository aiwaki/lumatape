# Your own LumaTape effects

[Home](../../README.en.md) · [Русский](README.md)

Already have a file? Choose **Add effect from file…** and open `.lumatape.glsl`.
Once checked and compiled, it appears under **Effect**. If you have not selected
a game yet, the file is still saved to your library for later. A failed import
leaves the working effect intact.

Need a file? Use the prompt below with an AI assistant of your choice, replacing
the effect description. Save its answer as `.lumatape.glsl` and import it through
the menu. LumaTape itself makes no AI requests.

<details>
<summary>Example AI prompt</summary>

```text
Create a complete LumaTape v1 shader: a warm CRT with visible scanlines,
soft glow, and light noise. Return only one .lumatape.glsl file.
This is single-pass GLSL 330 core, SDR sRGB; not Shadertoy or ReShade.
Start with this JSON comment:
/* LumaTape
{"version":1,"name":"Warm CRT","description":"Scanlines, glow, and noise","coordinates":"preserve","parameters":[]}
*/
Then provide vec3 lumatape(vec2 uv), returning RGB sRGB.
The application provides:
- uv: 0..1, origin at top left;
- vec3 ltSample(vec2 uv): game color, with crop/clamp already handled;
- vec2 ltResolution: image-area size in physical pixels;
- float ltTime: time in seconds.
Define the look using constants, without user parameters.
The host handles alpha, CRT shape, aspect ratio, and intensity.
Do not add #version, main, uniforms, in/out/inout, layout, discard,
preprocessor directives, gl_*, frag, uSource*, external textures, or frame history.
for/while/do loops are forbidden: manually unroll a small number of samples.
Helper functions and ordinary GLSL math are allowed. Maximum file size: 64 KiB.
For this color effect, keep coordinates:"preserve" and do not move the image.
```

</details>

[Example shaders](../../examples/shaders) · [LumaTape Shader v1 specification, in Russian](../SHADER_SPEC.md)

Shadertoy/ReShade files need adaptation. Arbitrary geometric distortion
(`coordinates: "warp"`) must be explicitly allowed in Settings; accurate clicks
are not guaranteed for it. Try new shaders on the test scene first.
