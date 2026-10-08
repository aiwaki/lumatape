/* LumaTape
{
  "version": 1,
  "name": "Amber CRT",
  "description": "Тёплый монохромный монитор с мягкими строками.",
  "coordinates": "preserve",
  "parameters": [
    {"name": "Теплота", "min": 0, "max": 1, "step": 0.01, "default": 0.85},
    {"name": "Строки", "min": 0, "max": 1, "step": 0.01, "default": 0.45}
  ]
}
*/
vec3 lumatape(vec2 uv) {
    vec3 color = ltSample(uv);
    float light = dot(color, vec3(0.299, 0.587, 0.114));
    vec3 amber = light * vec3(1.12, 0.80, 0.34);
    vec3 result = mix(color, amber, ltParams[0]);
    float rows = min(ltResolution.y / 3.0, 480.0);
    result *= 1.0 + 0.12 * ltParams[1] * cos(uv.y * rows * 6.2831853);
    return result;
}
