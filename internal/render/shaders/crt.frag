#version 330 core
in vec2 uv;
out vec4 frag;
uniform sampler2D uSource;
uniform vec2 uSourceSize;
uniform vec2 uOutput;
uniform vec4 uArea;
uniform vec4 uSourceUV;
uniform vec4 uCRT; // scanlines, mask, bloom, softness (already intensity-scaled)
uniform vec4 uVHS; // chroma bleed, noise, jitter, tracking
uniform float uVignette;
uniform float uCurvature;
uniform vec4 uScreen; // corner radius / min side, glass, style enabled, reserved
uniform float uIntensity;
uniform float uTime;
uniform uint uSeed;
uniform int uFreeze;

vec3 decodeSRGB(vec3 c) {
    return mix(c/12.92,pow((c+0.055)/1.055,vec3(2.4)),step(vec3(0.04045),c));
}
vec3 encodeSRGB(vec3 c) {
    c=max(c,vec3(0));
    return mix(c*12.92,1.055*pow(c,vec3(1.0/2.4))-0.055,step(vec3(0.0031308),c));
}
vec3 sampleImage(vec2 p) {
    vec2 halfPixel=0.5/uSourceSize;
    return texture(uSource,clamp(p,halfPixel,vec2(1)-halfPixel)).rgb;
}
vec3 linearImage(vec2 p) { return decodeSRGB(sampleImage(p)); }
vec3 toYC(vec3 c) { return vec3(dot(c,vec3(.299,.587,.114)),dot(c,vec3(-.168736,-.331264,.5)),dot(c,vec3(.5,-.418688,-.081312))); }
vec3 fromYC(vec3 c) { return vec3(c.x+1.402*c.z,c.x-.344136*c.y-.714136*c.z,c.x+1.772*c.y); }
uint hash(uvec3 x) {
    uint h=x.x*1664525u+x.y*1013904223u+x.z*747796405u;
    h^=h>>16u;h*=2246822519u;h^=h>>13u;return h;
}
float screenCoverage(vec2 pixel) {
    float radius=uScreen.x*min(uArea.z,uArea.w);
    if(radius<=0.0) return 1.0;
    vec2 local=abs(pixel-uArea.xy-uArea.zw*.5)-uArea.zw*.5+radius;
    float distance=length(max(local,vec2(0)))+min(max(local.x,local.y),0.0)-radius;
    // Physical pixels keep a circular radius and a stable antialiased edge at
    // any output aspect ratio. Black is opaque; alpha cannot expose the source.
    return 1.0-smoothstep(-.75,.75,distance);
}
vec3 signalImage(vec2 tex) {
    vec2 dx=vec2(1.0/uSourceSize.x,0);
    vec3 base=sampleImage(tex);
    if(uVHS.x>0.0) {
        vec3 yc=toYC(base);
        vec2 bleed=(toYC(sampleImage(tex-dx*1.5)).yz+toYC(sampleImage(tex-dx*3.0)).yz)*.5;
        yc.yz=mix(yc.yz,bleed,uVHS.x*.65);
        float tape=smoothstep(.25,.95,uVHS.x);
        if(tape>0.0) {
            vec2 signalPixel=dx*(uSourceSize.y/480.0);
            vec2 c2=toYC(sampleImage(tex-signalPixel*2.0)).yz;
            vec2 c5=toYC(sampleImage(tex-signalPixel*5.0)).yz;
            vec2 c9=toYC(sampleImage(tex-signalPixel*9.0)).yz;
            vec2 c14=toYC(sampleImage(tex-signalPixel*14.0)).yz;
            vec2 early=toYC(sampleImage(tex+signalPixel*2.0)).yz;
            vec2 smear=c2*.32+c5*.28+c9*.24+c14*.16;
            smear.y=mix(smear.y,early.y,.24);
            yc.yz=mix(yc.yz,smear,tape*.94);
        }
        base=clamp(fromYC(yc),0.0,1.0);
    }
    return base;
}
void main() {
    vec2 p=vec2(gl_FragCoord.x,uOutput.y-gl_FragCoord.y);
    vec2 q=(p-uArea.xy)/uArea.zw;
    if(any(lessThan(q,vec2(0)))||any(greaterThanEqual(q,vec2(1)))){frag=vec4(0,0,0,1);return;}
    // Bypass all effects exactly; aspect-fit/crop/stretch remains intentional.
    if(uIntensity<=0.0){frag=vec4(sampleImage(uSourceUV.xy+q*uSourceUV.zw),1);return;}
    float coverage=screenCoverage(p);
    if(coverage<=0.0){frag=vec4(0,0,0,1);return;}
    vec2 centered=q*2.0-1.0;
    q=(centered*(1.0+uCurvature*0.09*dot(centered,centered))+1.0)*0.5;
    float clockTime=uFreeze!=0 ? 0.0 : uTime;
    q.x += uVHS.z*0.002*sin(clockTime*3.7+q.y*9.0);
    // A rare narrow tracking band, disabled in all default presets.
    float event=step(10.8,mod(clockTime,12.0));
    q.x += uVHS.w*event*0.012*exp(-pow((q.y-fract(clockTime*.3))*70.0,2.0));
    if(any(lessThan(q,vec2(0)))||any(greaterThan(q,vec2(1)))){frag=vec4(0,0,0,1);return;}
    vec2 tex=uSourceUV.xy+q*uSourceUV.zw;
    vec2 dx=vec2(1.0/uSourceSize.x,0),dy=vec2(0,1.0/uSourceSize.y);
    vec3 base=signalImage(tex);
    vec3 color=decodeSRGB(base);
    vec3 a=linearImage(tex-dx),b=linearImage(tex+dx),c=linearImage(tex-dy),d=linearImage(tex+dy);
    vec3 softened=(color*4.0+a+b+c+d)/8.0;
    color=mix(color,softened,uCRT.w);
    float television=smoothstep(.30,.90,uCRT.w);
    if(television>0.0) {
        // Signal bandwidth is tied to a 480-line television, not a fixed
        // three-pixel blur that vanishes on a high-DPI captured framebuffer.
        // Pair adjacent Gaussian taps using bilinear filtering: all 17 source
        // texels are represented, without detached sparse copies of glyphs.
        float sigma=clamp(uSourceSize.y/480.0,.9,3.5);
        vec4 lo=exp(-vec4(1.0,4.0,9.0,16.0)/(2.0*sigma*sigma));
        vec4 hi=exp(-vec4(25.0,36.0,49.0,64.0)/(2.0*sigma*sigma));
        vec4 weight=vec4(lo.x+lo.y,lo.z+lo.w,hi.x+hi.y,hi.z+hi.w);
        vec4 offset=vec4(1,3,5,7)+vec4(lo.y,lo.w,hi.y,hi.w)/weight;
        vec3 wide=decodeSRGB(signalImage(tex));
        wide+=(decodeSRGB(signalImage(tex-dx*offset.x))+decodeSRGB(signalImage(tex+dx*offset.x)))*weight.x;
        wide+=(decodeSRGB(signalImage(tex-dx*offset.y))+decodeSRGB(signalImage(tex+dx*offset.y)))*weight.y;
        wide+=(decodeSRGB(signalImage(tex-dx*offset.z))+decodeSRGB(signalImage(tex+dx*offset.z)))*weight.z;
        wide+=(decodeSRGB(signalImage(tex-dx*offset.w))+decodeSRGB(signalImage(tex+dx*offset.w)))*weight.w;
        wide/=1.0+2.0*dot(weight,vec4(1));
        color=mix(color,wide,television);
        // A softer analog picture also has raised dark tones and slightly
        // reduced saturation. This remains visible on smooth game regions,
        // where blur and scanlines alone cannot describe a television signal.
        float light=dot(color,vec3(.2126,.7152,.0722));
        color=mix(color,vec3(light),television*.18);
        color=pow(max(color,vec3(0)),vec3(1.0-television*.08));
    }
    if(uCRT.z>0.0) {
        // Threshold and spread in linear light, then encode once at the end.
        vec3 glow=max(a-0.65,0.0)+max(b-0.65,0.0)+max(c-0.65,0.0)+max(d-0.65,0.0);
        glow+=max(linearImage(tex-dx*2.0)-0.65,0.0)+max(linearImage(tex+dx*2.0)-0.65,0.0);
        color+=glow*(uCRT.z/6.0)*0.5;
        if(uCRT.z>.20) {
            // Include the center and adjacent samples: a pair of remote
            // bright-pass taps creates detached glow copies of white text.
            vec3 halo=max(linearImage(tex)-.50,0.0)*.40;
            halo+=(max(a-.50,0.0)+max(b-.50,0.0))*.22;
            halo+=(max(linearImage(tex-dx*2.0)-.50,0.0)+max(linearImage(tex+dx*2.0)-.50,0.0))*.08;
            color+=halo*(uCRT.z-.20)*.20;
        }
    }
    // Follow source rows when they are resolvable; high-resolution captures use
    // a 3-physical-pixel pitch to avoid Nyquist shimmer at 1:1. No animated phase.
    float classic=smoothstep(.35,.60,uCRT.x);
    float rows=min(uSourceSize.y*uSourceUV.w,uArea.w/3.0);
    rows=mix(rows,min(rows,240.0),classic);
    float pixelsPerRow=uArea.w/rows;
    float aa=smoothstep(1.1,2.5,pixelsPerRow);
    color*=1.0+uCRT.x*mix(.22,.50,classic)*aa*cos(q.y*rows*6.283185307);
    // The strong CRT phosphor pitch follows the same virtual signal scale.
    // Delicate CRT and VHS retain their original physical-pixel triads.
    float phosphorScale=mix(1.0,max(1.0,uArea.w/720.0),classic);
    int stripe=int(mod(floor(p.x/phosphorScale),3.0));
    vec3 mask=vec3(-.5);mask[stripe]=1.0;
    color*=vec3(1.0)+mask*uCRT.y*.24;
    float edge=smoothstep(.35,1.65,dot(centered,centered));
    color*=1.0-uVignette*.22*edge;
    // Low contrast glass lighting supplies depth without relocating any pixels.
    float rim=pow(max(abs(centered.x),abs(centered.y)),8.0);
    color*=1.0-uScreen.y*.20*rim;
    color+=vec3(.018,.023,.026)*uScreen.y*pow(max(0.0,1.0-length((q-vec2(.32,.08))*vec2(.75,2.5))),5.0);
    vec3 encoded=encodeSRGB(color);
    uint tick=uFreeze!=0 ? 0u : uint(floor(uTime*24.0));
    float n=float(hash(uvec3(uvec2(p),uSeed+tick))&65535u)/65535.0-.5;
    encoded+=n*uVHS.y*.10;
    float tapeNoise=smoothstep(.12,.75,uVHS.y);
    if(tapeNoise>0.0) {
        // Correlated signal noise is wider horizontally than vertically. Its
        // 24 Hz phase is shared with fine grain and honors deterministic freeze.
        // This changes brightness/color only, never mouse hit coordinates.
        uvec2 cell=uvec2(floor(q*vec2(320.0,480.0)));
        float luma=float(hash(uvec3(cell,uSeed+tick+101u))&65535u)/65535.0-.5;
        float cb=float(hash(uvec3(cell/ uvec2(2u,1u),uSeed+tick+211u))&65535u)/65535.0-.5;
        float cr=float(hash(uvec3(cell/ uvec2(2u,1u),uSeed+tick+307u))&65535u)/65535.0-.5;
        float row=float(hash(uvec3(0u,cell.y,uSeed+tick+401u))&65535u)/65535.0-.5;
        vec3 noisy=toYC(encoded);
        noisy.x+=tapeNoise*(luma*.065+row*.012);
        noisy.yz+=tapeNoise*vec2(cb,cr)*.055;
        encoded=fromYC(noisy);
    }
    frag=vec4(clamp(encoded,0.0,1.0)*coverage,1); // opaque: source cannot double underneath
}
