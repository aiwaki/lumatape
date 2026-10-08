package render

// CustomFragment wraps the validated v1 body. Its public ABI deliberately has
// no output/alpha entry point: format, shape and exact zero bypass stay owned by
// LumaTape. The parser is a limited contract validator, not a GPU sandbox.
func CustomFragment(body string) string {
	return customPrefix + "\n#line 1\n" + body + "\n#line 10000\n" + customMain
}

const customPrefix = `#version 330 core
out vec4 frag;
uniform sampler2D uSource;
uniform vec2 uSourceSize, uOutput;
uniform vec4 uArea, uSourceUV, uScreen;
uniform float uIntensity, uCurvature;
uniform vec2 ltResolution;
uniform float ltTime;
uniform float ltParams[8];
vec3 ltSample(vec2 p) {
 vec2 halfPixel=0.5/uSourceSize;
 return texture(uSource,clamp(uSourceUV.xy+clamp(p,vec2(0),vec2(1))*uSourceUV.zw,halfPixel,vec2(1)-halfPixel)).rgb;
}
float ltCoverage(vec2 pixel) {
 float radius=uScreen.x*min(uArea.z,uArea.w);
 if(radius<=0.0) return 1.0;
 vec2 local=abs(pixel-uArea.xy-uArea.zw*.5)-uArea.zw*.5+radius;
 float distance=length(max(local,vec2(0)))+min(max(local.x,local.y),0.0)-radius;
 return 1.0-smoothstep(-.75,.75,distance);
}
`
const customMain = `
void main() {
 vec2 p=vec2(gl_FragCoord.x,uOutput.y-gl_FragCoord.y);
 vec2 q=(p-uArea.xy)/uArea.zw;
 if(any(lessThan(q,vec2(0)))||any(greaterThanEqual(q,vec2(1)))) {frag=vec4(0,0,0,1);return;}
 vec3 original=ltSample(q);
 if(uIntensity<=0.0) {frag=vec4(original,1);return;}
 float coverage=ltCoverage(p);
 if(coverage<=0.0) {frag=vec4(0,0,0,1);return;}
 vec2 centered=q*2.0-1.0;
 q=(centered*(1.0+uCurvature*.09*dot(centered,centered))+1.0)*.5;
 if(any(lessThan(q,vec2(0)))||any(greaterThan(q,vec2(1)))) {frag=vec4(0,0,0,1);return;}
 vec3 processed=lumatape(q);
 if(any(isnan(processed))||any(isinf(processed))) processed=ltSample(q);
 vec3 color=mix(ltSample(q),clamp(processed,vec3(0),vec3(1)),uIntensity);
 float glass=1.0-uScreen.y*.12*dot(centered,centered);
 color*=glass;
 frag=vec4(clamp(color,vec3(0),vec3(1))*coverage,1);
}
`
