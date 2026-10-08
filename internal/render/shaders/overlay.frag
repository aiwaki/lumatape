#version 330 core
in vec2 uv;
out vec4 frag;
uniform vec2 uOutput;
uniform vec4 uArea; // x,y,width,height in physical top-left framebuffer pixels
uniform vec4 uCRT; // scanlines,mask(unused),bloom(unused),softness(unused)
uniform vec4 uVHS; // chroma(unused),noise,jitter(unused),tracking(unused)
uniform float uVignette;
uniform float uTime;
uniform uint uSeed;
uniform int uFreeze;
uniform vec4 uScreen; // corner radius / min side, glass, style enabled, reserved

uint hash(uvec3 x) {
    uint h = x.x * 1664525u + x.y * 1013904223u + x.z * 747796405u;
    h ^= h >> 16u; h *= 2246822519u; h ^= h >> 13u;
    return h;
}
void main() {
    vec2 p = vec2(gl_FragCoord.x, uOutput.y - gl_FragCoord.y);
    vec2 q = (p-uArea.xy)/uArea.zw;
    if (any(lessThan(q,vec2(0))) || any(greaterThanEqual(q,vec2(1)))) {
        frag = vec4(0,0,0,1); return; // explicit format mask, independent of filter
    }
    // Stable 3-physical-pixel rows, never animated in the vertical direction.
    float line = (mod(floor(p.y-uArea.y),3.0)==2.0 ? 1.0 : 0.0);
    vec2 v=q*2.0-1.0;
    float edge=smoothstep(0.35,1.65,dot(v,v));
    float dark=clamp(uCRT.x*0.28*line + uVignette*0.22*edge,0.0,0.4);
    uint tick=uFreeze!=0 ? 0u : uint(floor(uTime*24.0));
    float n=float(hash(uvec3(uvec2(p),uSeed+tick)) & 65535u)/65535.0-0.5;
    float grain=abs(n)*uVHS.y*0.35;
    float alpha=dark+grain*(1.0-dark);
    // GLFW's Win32 transparent framebuffer is premultiplied by DWM. Blending
    // in the GL framebuffer is disabled; output RGB is already premultiplied.
    float white=n>0.0 ? grain*(1.0-dark) : 0.0;
    float radius=uScreen.x*min(uArea.z,uArea.w);
    float coverage=1.0;
    if(radius>0.0) {
        vec2 local=abs(p-uArea.xy-uArea.zw*.5)-uArea.zw*.5+radius;
        float distance=length(max(local,vec2(0)))+min(max(local.x,local.y),0.0)-radius;
        coverage=1.0-smoothstep(-.75,.75,distance);
    }
    float rim=pow(max(abs(v.x),abs(v.y)),8.0)*uScreen.y*.20;
    float reflection=pow(max(0.0,1.0-length((q-vec2(.32,.08))*vec2(.75,2.5))),5.0)*uScreen.y*.05;
    white=white*(1.0-rim)+reflection*(1.0-alpha);
    alpha=alpha+rim*(1.0-alpha)+reflection*(1.0-alpha)*(1.0-rim);
    white*=coverage;
    alpha=1.0-coverage*(1.0-alpha);
    frag=vec4(vec3(white),alpha);
}
