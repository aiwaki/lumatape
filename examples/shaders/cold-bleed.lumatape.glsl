/* LumaTape
{
  "version": 1,
  "name": "Cold Bleed",
  "description": "Холодные тени и короткий цветовой шлейф.",
  "coordinates": "preserve",
  "parameters": [
    {"name": "Цветовой шлейф", "min": 0, "max": 1, "step": 0.01, "default": 0.65}
  ]
}
*/
vec3 lumatape(vec2 uv) {
    vec3 color = ltSample(uv);
    vec2 dx = vec2(3.0 / max(ltResolution.x, 1.0), 0.0);
    vec3 shifted = vec3(ltSample(uv - dx).r, color.g, ltSample(uv + dx).b);
    vec3 result = mix(color, shifted, ltParams[0]);
    float shadow = 1.0 - dot(color, vec3(0.299, 0.587, 0.114));
    return result + vec3(-0.01, 0.015, 0.035) * shadow * ltParams[0];
}
