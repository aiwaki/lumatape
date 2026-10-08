# LumaTape custom shaders v1

[Русский](SHADER_SPEC.md) | [English](SHADER_SPEC.en.md)

Choose **Add effect from file…** in the tray and open a `.lumatape.glsl` file. The
engine validates its format and compiles it with the real driver. After successful
validation, the file is saved to the library and appears in the **Effect** menu.
Import and selection are separate operations: if no game is selected or the
image-distortion setting is incompatible, the file is saved but not activated.
An error leaves the previous effect in place; its explanation is available in a
system message and **Tools → Last error…**.

[The guide includes a ready-to-use AI prompt](shaders/README.en.md). Save the
response as `.lumatape.glsl` and open it from the menu. The application has no AI
button, editor or code-paste control. It sends nothing; asking only "make a
LumaTape shader" without the contract may not be enough. Shadertoy, ReShade, Slang
and multipass chains require adaptation.

A shader defines a finished look. The desktop application has no effect-parameter
or overall-intensity sliders; explicit effect selection and enabling use 100%.
New files must contain `parameters: []`, with artistic values defined as GLSL
constants. Support for v1 parameters and intensity remains in JSON/CLI for
compatibility.

## File

UTF-8, up to 64 KiB. A BOM, CRLF and a single Markdown `glsl` block are accepted
when pasting. A JSON comment comes first, followed by the GLSL function. The
application supplies the preprocessor and `main()`.

```glsl
/* LumaTape
{
  "version": 1,
  "name": "Тёплый свет",
  "description": "Готовый тёплый оттенок",
  "coordinates": "preserve",
  "parameters": []
}
*/
vec3 lumatape(vec2 uv) {
    vec3 color = ltSample(uv);
    return mix(color, color * vec3(1.08, 0.97, 0.84), 0.6);
}
```

| Name | Meaning |
|---|---|
| `uv` | Normalized image-area coordinates with the origin at the top left. The wrapper has already applied the CRT shape. |
| `ltSample(uv)` | Game color in SDR sRGB; the application applies crop/source UV and clamps sampling to the edges. |
| `ltResolution` | Physical size of the image area as `vec2`, not the entire monitor. |
| `ltTime` | Seconds; 0 when frozen. |
| `ltParams[8]` | v1 compatibility: parameters in `parameters` order; selecting an older file uses its `default` values. Unused slots are 0; with `parameters: []`, all eight are 0. |
| Returned `vec3` | Processed RGB sRGB. The application calculates alpha, bypass, overall intensity and shape. |

ABI v1 still accepts up to 8 parameters with unique readable names. Each needs
finite `min < max`, a positive `step <= max-min` and a `default` within the range;
bounds have an absolute-value limit of 1 000 000. Existing files continue to work,
but the desktop application does not create sliders from this description. For
a new finished effect, use an empty array and constants as in the example.

Do not multiply the result by overall intensity: the render wrapper blends the
original and processed frames. Saved JSON/CLI `effects.intensity` settings remain
compatible; at 0, the custom function is bypassed entirely, with no filter or
shape, while independent 4:3 remains. Loading an existing profile does not rewrite
its values. An explicit select, enable or apply action in the desktop application
sets intensity to 1.

`coordinates: "preserve"` is the author's declaration that the shader preserves
geometry, not a mathematical proof of click alignment. Actual curvature or
movement requires `"warp"`, which cannot be enabled in **Accurate clicks** mode.
Choose **Settings → Image distortion → Allow arbitrary distortion** for it.
Keyboard and mouse work in both modes; the latter allows image changes that can
make a visible target differ from its clickable position. Check circles, text
and edges in the test scene. Color changes, grain, scanlines and short local
filters usually use preserve.

The wrapper applies the built-in **Curved CRT** shape separately from custom
GLSL. In Full it is available with **Accurate clicks** and
`coordinates: "preserve"`: the application projects the system cursor drawing
onto the known screen geometry while preserving Windows input. Projection is
disabled for `"warp"` because ABI v1 does not describe the inverse transform of
an arbitrary shader. The `preserve` label must not conceal a shader's own
distortion and does not enable general mouse remapping.

v1 provides ordinary GLSL 330 mathematics and helper functions. Custom uniforms,
`in/out/inout`, `layout`, `discard`, `main`, `gl_*`, internal `uSource*`/`frag`,
`#` directives, external files/textures and frame history are unavailable.
`for/while/do` loops are unsupported; write small blur kernels as unrolled
samples. This is an intentionally small single-pass interface. A compilation
error fails the import; it does not replace the working image with a black frame.

## Storage and limits

The identifier is the SHA256 of the normalized file. Shaders are stored in
`%LOCALAPPDATA%\LumaTape\Shaders`; importing a new version creates a new ID and
leaves the old one available. Configuration stores the ID and eight numbers;
source code is not included in diagnostics. Do not change a file's contents
under an existing ID. Import the updated file instead. The library is limited to
128 shaders. Examples: [Amber CRT](../examples/shaders/amber-crt.lumatape.glsl)
and [Cold Bleed](../examples/shaders/cold-bleed.lumatape.glsl); these also
demonstrate the compatible v1 parameter ABI.

Custom filters require Full. Automatic processing selects an available frame
transfer path; Lightweight cannot reproduce a full color shader. The tray
application has no built-in preview. For testing, open the separate testcard
through **Tools → Test scene**, select it as the game, and toggle the effect.

The size and language limits do not isolate the GPU driver. Complex code may
compile or run slowly; the application does not promise to forcibly interrupt a
stalled driver call. Custom-code support does not mean every generated result
is visually correct or performs well. Import, transaction and actual-rendering
checks are listed in the [Windows protocol](WINDOWS_VALIDATION.en.md).
