// Native, offscreen validation of the EXACT repository GLSL. This validates
// shader math on Apple's OpenGL driver, not WGL/DWM/capture behavior on Windows.
#define GL_SILENCE_DEPRECATION
#include <OpenGL/OpenGL.h>
#include <OpenGL/gl3.h>
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct { float scan,mask,bloom,soft,chroma,noise,jitter,tracking,vignette,curve,intensity,time,radius,glass; int freeze; } Effects;
typedef struct { int x,y,w,h; } Area;
static int failures=0, checks=0;
static GLuint framebuffer, outputTexture, sourceTexture, vao, fullProgram, overlayProgram;
static const Effects subtle={.scan=.12f,.mask=.04f,.bloom=.06f,.soft=.08f,.vignette=.10f,.intensity=1,.freeze=1};
static const Effects classic={.scan=.62f,.mask=.42f,.bloom=.18f,.soft=.10f,.vignette=.24f,.intensity=1,.freeze=1};
static const Effects softTV={.scan=.16f,.mask=.025f,.bloom=.44f,.soft=.76f,.chroma=.12f,.noise=.01f,.vignette=.18f,.intensity=1,.freeze=1};
static const Effects vhsLight={.scan=.14f,.mask=.02f,.bloom=.10f,.soft=.20f,.chroma=.50f,.noise=.24f,.vignette=.14f,.intensity=1,.freeze=1};
static const Effects vhsTape={.scan=.10f,.mask=.015f,.bloom=.08f,.soft=.28f,.chroma=.92f,.noise=.62f,.vignette=.16f,.intensity=1,.freeze=1};
// Explicit demonstration settings, NOT a shipping preset/default. Keep mouse
// positions unchanged: no curvature, jitter, tracking or coordinate remapping.
static const Effects vhsDemo={.scan=.28f,.mask=.08f,.bloom=.12f,.soft=.35f,.chroma=.80f,.noise=.18f,.vignette=.16f,.intensity=1,.freeze=1};

static void require(int ok,const char *name) { checks++;printf("%s %s\n",ok?"PASS":"FAIL",name);if(!ok) failures++; }
static void fatal(const char *message) { fprintf(stderr,"ERROR %s\n",message);exit(2); }
static void glOK(const char *where) { GLenum e=glGetError();if(e){fprintf(stderr,"OpenGL error 0x%x: %s\n",e,where);exit(2);} }
static char *readFile(const char *path) {
    FILE *f=fopen(path,"rb");if(!f)fatal(path);fseek(f,0,SEEK_END);long n=ftell(f);rewind(f);
    if(n<1)fatal("empty shader");char *s=malloc((size_t)n+1);if(!s)fatal("allocation");
    if(fread(s,1,(size_t)n,f)!=(size_t)n)fatal("read shader");s[n]=0;fclose(f);return s;
}
static GLuint compile(GLenum kind,const char *path) {
    char *text=readFile(path);GLuint id=glCreateShader(kind);const GLchar *source=text;
    glShaderSource(id,1,&source,NULL);glCompileShader(id);free(text);GLint ok=0;glGetShaderiv(id,GL_COMPILE_STATUS,&ok);
    if(!ok){char log[16384];glGetShaderInfoLog(id,sizeof(log),NULL,log);fprintf(stderr,"%s:\n%s\n",path,log);fatal("shader compilation");}return id;
}
static GLuint program(const char *fragment) {
    GLuint v=compile(GL_VERTEX_SHADER,"internal/render/shaders/fullscreen.vert"),f=compile(GL_FRAGMENT_SHADER,fragment),p=glCreateProgram();
    glAttachShader(p,v);glAttachShader(p,f);glLinkProgram(p);glDeleteShader(v);glDeleteShader(f);GLint ok=0;glGetProgramiv(p,GL_LINK_STATUS,&ok);
    if(!ok){char log[16384];glGetProgramInfoLog(p,sizeof(log),NULL,log);fprintf(stderr,"%s\n",log);fatal("shader linking");}return p;
}
static void uniform1(GLuint p,const char *n,float a){glUniform1f(glGetUniformLocation(p,n),a);}
static void uniform2(GLuint p,const char *n,float a,float b){glUniform2f(glGetUniformLocation(p,n),a,b);}
static void uniform4(GLuint p,const char *n,float a,float b,float c,float d){glUniform4f(glGetUniformLocation(p,n),a,b,c,d);}
static unsigned char *pixels(int w,int h){unsigned char *p=malloc((size_t)w*h*4);if(!p)fatal("pixel allocation");return p;}

// Input and returned CPU pixels use a top-left origin, like the capture bridge.
// The chart has fine lines, color patches, dark details and a physical circle.
static unsigned char *chart(int w,int h,float displayAspect,int exact) {
    unsigned char *image=pixels(w,h);
    for(int y=0;y<h;y++)for(int x=0;x<w;x++){
        size_t o=((size_t)y*w+x)*4;unsigned char r,g,b;
        if(exact){r=(unsigned char)(x*37+y*3);g=(unsigned char)(y*29+x*5);b=(unsigned char)((x*13)^(y*17));}
        else {
            float nx=((float)x+.5f)/w,ny=((float)y+.5f)/h;
            r=(unsigned char)(20+nx*215);g=(unsigned char)(20+ny*215);b=(unsigned char)(20+(1-nx)*140);
            if(y<h/5){static const unsigned char colors[8][3]={{240,240,240},{230,210,30},{30,220,220},{30,210,35},{220,40,210},{230,35,30},{35,45,220},{16,16,16}};int i=x*8/w;r=colors[i][0];g=colors[i][1];b=colors[i][2];}
            else if(y>h*4/5){unsigned char v=(unsigned char)(nx*255);r=v;g=v;b=v;}
            else {
                float dx=(nx-.5f)*displayAspect,dy=ny-.52f,rad=sqrtf(dx*dx+dy*dy);
                if(fabsf(rad-.22f)<.007f){r=245;g=245;b=245;}
                if(x%20==0||y%20==0){r=25;g=25;b=25;}
                if(nx>.06f&&nx<.28f&&ny>.45f&&ny<.70f){int bit=((x/2)+(y/2))%2;r=bit?220:25;g=r;b=r;}
            }
        }
        image[o]=r;image[o+1]=g;image[o+2]=b;image[o+3]=255;
    }
    return image;
}
static void upload(const unsigned char *input,int w,int h) {
    glActiveTexture(GL_TEXTURE0);glBindTexture(GL_TEXTURE_2D,sourceTexture);
    glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA8,w,h,0,GL_RGBA,GL_UNSIGNED_BYTE,input);
    glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_S,GL_CLAMP_TO_EDGE);glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_T,GL_CLAMP_TO_EDGE);
}
static unsigned char *render(GLuint p,int w,int h,int sw,int sh,Area area,Effects e) {
    glBindTexture(GL_TEXTURE_2D,outputTexture);glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA8,w,h,0,GL_RGBA,GL_UNSIGNED_BYTE,NULL);
    glBindFramebuffer(GL_FRAMEBUFFER,framebuffer);glFramebufferTexture2D(GL_FRAMEBUFFER,GL_COLOR_ATTACHMENT0,GL_TEXTURE_2D,outputTexture,0);
    if(glCheckFramebufferStatus(GL_FRAMEBUFFER)!=GL_FRAMEBUFFER_COMPLETE)fatal("incomplete framebuffer");
    glViewport(0,0,w,h);glUseProgram(p);glBindVertexArray(vao);
    uniform2(p,"uOutput",w,h);uniform2(p,"uSourceSize",sw,sh);uniform4(p,"uArea",area.x,area.y,area.w,area.h);uniform4(p,"uSourceUV",0,0,1,1);
    uniform4(p,"uCRT",e.scan,e.mask,e.bloom,e.soft);uniform4(p,"uVHS",e.chroma,e.noise,e.jitter,e.tracking);
    uniform1(p,"uVignette",e.vignette);uniform1(p,"uCurvature",e.curve);uniform1(p,"uIntensity",e.intensity);uniform1(p,"uTime",e.time);
    uniform4(p,"uScreen",e.radius,e.glass,0,0);
    glUniform1ui(glGetUniformLocation(p,"uSeed"),173);glUniform1i(glGetUniformLocation(p,"uFreeze"),e.freeze);glUniform1i(glGetUniformLocation(p,"uSource"),0);
    glActiveTexture(GL_TEXTURE0);glBindTexture(GL_TEXTURE_2D,sourceTexture);glDrawArrays(GL_TRIANGLES,0,3);
    unsigned char *raw=pixels(w,h),*image=pixels(w,h);glReadPixels(0,0,w,h,GL_RGBA,GL_UNSIGNED_BYTE,raw);glOK("render/readback");
    for(int y=0;y<h;y++)memcpy(image+(size_t)y*w*4,raw+(size_t)(h-1-y)*w*4,(size_t)w*4);free(raw);return image;
}
static void save(const char *name,const unsigned char *image,int w,int h) {
    const char *directory=getenv("LUMATAPE_SHADER_OUTPUT");if(!directory||!*directory)directory="artifacts/shader-validation";
    char path[1024];snprintf(path,sizeof(path),"%s/%s.ppm",directory,name);FILE *f=fopen(path,"wb");if(!f)fatal(path);
    fprintf(f,"P6\n%d %d\n255\n",w,h);for(size_t i=0;i<(size_t)w*h;i++)fwrite(image+i*4,1,3,f);fclose(f);
}
static int opaque(const unsigned char *p,int w,int h){for(size_t i=0;i<(size_t)w*h;i++)if(p[i*4+3]!=255)return 0;return 1;}
static int maxDifference(const unsigned char *a,const unsigned char *b,int w,int h){int worst=0;for(size_t i=0;i<(size_t)w*h*4;i++){int d=abs((int)a[i]-(int)b[i]);if(d>worst)worst=d;}return worst;}
static double mean(const unsigned char *p,int w,Area a){double sum=0;for(int y=a.y;y<a.y+a.h;y++)for(int x=a.x;x<a.x+a.w;x++){size_t o=((size_t)y*w+x)*4;sum+=p[o]+p[o+1]+p[o+2];}return sum/((double)a.w*a.h*3);}
static Area fit43(int w,int h){int aw=(h*4)/3,ah=h;if(aw>w){aw=w;ah=w*3/4;}return(Area){(w-aw)/2,(h-ah)/2,aw,ah};}
static int blackOutside(const unsigned char *p,int w,int h,Area a){for(int y=0;y<h;y++)for(int x=0;x<w;x++)if(x<a.x||x>=a.x+a.w||y<a.y||y>=a.y+a.h){size_t o=((size_t)y*w+x)*4;if(p[o]||p[o+1]||p[o+2]||p[o+3]!=255)return 0;}return 1;}

// Small procedural bitmap labels only. The image pixels below the header come
// exclusively from glReadPixels; no CPU code imitates the effect.
static void label(unsigned char *image,int w,int x,int y,const char *text) {
    static const char chars[]="ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 :.-/";
    static const unsigned char rows[][7]={
        {14,17,17,31,17,17,17},{30,17,17,30,17,17,30},{14,17,16,16,16,17,14},{30,17,17,17,17,17,30},
        {31,16,16,30,16,16,31},{31,16,16,30,16,16,16},{14,17,16,23,17,17,15},{17,17,17,31,17,17,17},
        {14,4,4,4,4,4,14},{7,2,2,2,18,18,12},{17,18,20,24,20,18,17},{16,16,16,16,16,16,31},
        {17,27,21,21,17,17,17},{17,25,21,19,17,17,17},{14,17,17,17,17,17,14},{30,17,17,30,16,16,16},
        {14,17,17,17,21,18,13},{30,17,17,30,20,18,17},{15,16,16,14,1,1,30},{31,4,4,4,4,4,4},
        {17,17,17,17,17,17,14},{17,17,17,17,17,10,4},{17,17,17,21,21,27,17},{17,17,10,4,10,17,17},
        {17,17,10,4,4,4,4},{31,1,2,4,8,16,31},{14,17,19,21,25,17,14},{4,12,4,4,4,4,14},
        {14,17,1,2,4,8,31},{30,1,1,14,1,1,30},{2,6,10,18,31,2,2},{31,16,16,30,1,1,30},
        {14,16,16,30,17,17,14},{31,1,2,4,8,8,8},{14,17,17,14,17,17,14},{14,17,17,15,1,1,14},
        {0,0,0,0,0,0,0},{0,4,4,0,4,4,0},{0,0,0,0,0,4,4},{0,0,0,31,0,0,0},{1,2,2,4,8,8,16}
    };
    for(;*text;text++,x+=18){const char *found=strchr(chars,*text);if(!found)continue;size_t index=(size_t)(found-chars);
        for(int ry=0;ry<7;ry++)for(int rx=0;rx<5;rx++)if(rows[index][ry]&(1<<(4-rx)))for(int dy=0;dy<3;dy++)for(int dx=0;dx<3;dx++){
            size_t o=((size_t)(y+ry*3+dy)*w+x+rx*3+dx)*4;image[o]=230;image[o+1]=235;image[o+2]=245;image[o+3]=255;
        }
    }
}
static void comparisonLabels(const char *name,const char *leftLabel,const char *rightLabel,const unsigned char *before,const unsigned char *after,int w,int h) {
    const int header=42,gap=8;int ow=w*2+gap,oh=h+header;unsigned char *image=pixels(ow,oh);
    for(size_t i=0;i<(size_t)ow*oh;i++){image[i*4]=14;image[i*4+1]=18;image[i*4+2]=27;image[i*4+3]=255;}
    label(image,ow,18,10,leftLabel);label(image,ow,w+gap+18,10,rightLabel);
    for(int y=0;y<h;y++){memcpy(image+((size_t)(y+header)*ow)*4,before+(size_t)y*w*4,(size_t)w*4);memcpy(image+((size_t)(y+header)*ow+w+gap)*4,after+(size_t)y*w*4,(size_t)w*4);}
    save(name,image,ow,oh);free(image);
}
static void comparison(const char *name,const char *rightLabel,const unsigned char *before,const unsigned char *after,int w,int h) {
    comparisonLabels(name,"ORIGINAL / INTENSITY 0",rightLabel,before,after,w,h);
}

static void demonstration(void) {
    const int sw=320,sh=240,w=960,h=720;Area area={0,0,w,h};
    unsigned char *input=chart(sw,sh,4.0f/3.0f,0);upload(input,sw,sh);
    unsigned char *original=render(fullProgram,w,h,sw,sh,area,(Effects){0});
    unsigned char *light=render(fullProgram,w,h,sw,sh,area,vhsLight);
    unsigned char *demo=render(fullProgram,w,h,sw,sh,area,vhsDemo);
    save("vhs-ab-original",original,w,h);save("vhs-ab-light",light,w,h);save("vhs-ab-custom-demo",demo,w,h);
    comparison("vhs-ab-default","VHS LIGHT / SHIPPING PRESET",original,light,w,h);
    comparison("vhs-ab-custom","CUSTOM DEMO / NOT DEFAULT",original,demo,w,h);
    double lightAbs=0,demoAbs=0;int lightMax=0,demoMax=0;
    for(size_t i=0;i<(size_t)w*h;i++)for(int c=0;c<3;c++){int a=abs((int)light[i*4+c]-original[i*4+c]),b=abs((int)demo[i*4+c]-original[i*4+c]);lightAbs+=a;demoAbs+=b;if(a>lightMax)lightMax=a;if(b>demoMax)demoMax=b;}
    printf("VHS A/B actual GL readback: preset mean-absolute RGB change %.4f/255, max %d; custom demo %.4f/255, max %d\n",lightAbs/(w*h*3),lightMax,demoAbs/(w*h*3),demoMax);
    require(lightAbs>0&&demoAbs>0,"VHS Light and explicit Custom both change source pixels");
    // Show a true, nearest-neighbor zoom of the rendered green/magenta edge.
    // Both panels receive the identical presentation zoom; no filter is added.
    const int cx=450,cy=18,cw=96,ch=96,scale=6,zw=cw*scale,zh=ch*scale;
    unsigned char *zoomA=pixels(zw,zh),*zoomB=pixels(zw,zh);
    for(int y=0;y<zh;y++)for(int x=0;x<zw;x++){size_t src=((size_t)(cy+y/scale)*w+cx+x/scale)*4,dst=((size_t)y*zw+x)*4;memcpy(zoomA+dst,original+src,4);memcpy(zoomB+dst,demo+src,4);}
    comparison("vhs-ab-edge-zoom","CUSTOM / PIXEL ZOOM X6",zoomA,zoomB,zw,zh);free(zoomA);free(zoomB);
    free(input);free(original);free(light);free(demo);
}

static void chromaIndependenceTest(void) {
    const int w=64,h=32;unsigned char *input=pixels(w,h);
    for(int y=0;y<h;y++)for(int x=0;x<w;x++){size_t i=((size_t)y*w+x)*4;input[i]=x<w/2?200:40;input[i+1]=x<w/2?60:120;input[i+2]=x<w/2?70:180;input[i+3]=255;}
    upload(input,w,h);Effects e={.chroma=1,.intensity=1,.freeze=1};unsigned char *out=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);
    size_t p=((size_t)h/2*w+w/2)*4;double inputY=.299*input[p]+.587*input[p+1]+.114*input[p+2],outputY=.299*out[p]+.587*out[p+1]+.114*out[p+2];
    char name[180];snprintf(name,sizeof(name),"Chroma-only effect changes RGB edge while preserving luma within 1 level (%.4f -> %.4f)",inputY,outputY);
    require(abs((int)out[p]-input[p])>10&&fabs(inputY-outputY)<1,name);free(input);free(out);
}

static void softSignalImpulseTests(void) {
    // A step edge alone misses separated copies of thin glyphs. An isolated
    // one-pixel line must have a single peak with continuously decaying sides,
    // including high-DPI source heights that enlarge the analog footprint.
    const int heights[]={720,1898},w=128;
    for(int r=0;r<2;r++)for(int kind=0;kind<2;kind++){
        int h=heights[r],cx=w/2;unsigned char *input=pixels(w,h);
        for(int y=0;y<h;y++)for(int x=0;x<w;x++){size_t p=((size_t)y*w+x)*4;input[p]=input[p+1]=input[p+2]=x==cx?224:0;input[p+3]=255;}
        upload(input,w,h);Effects e={.intensity=1,.freeze=1};if(kind==0)e.soft=.76f;else e.bloom=.44f;
        unsigned char *out=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);
        int monotonic=1,symmetric=1,peak=out[((size_t)h/2*w+cx)*4],previous=peak;
        printf("Impulse %s height%d:",kind?"bloom":"softness",h);
        for(int x=0;x<18;x++){
            int right=out[((size_t)h/2*w+cx+x)*4],left=out[((size_t)h/2*w+cx-x)*4];
            printf(" %d",right);if(right>previous+1)monotonic=0;if(abs(left-right)>1)symmetric=0;previous=right;
        }
        printf("\n");char name[220];snprintf(name,sizeof(name),"Soft TV %s height%d isolated glyph line has one symmetric peak without secondary ghost images",kind?"bloom":"softness",h);
        require(monotonic&&symmetric&&peak>0,name);free(input);free(out);
    }
}

static void tapeTestsAndDemonstration(void) {
    // A native-size source catches an effect that looks strong only when a
    // small source texture is enlarged. No presentation zoom in this A/B.
    const int w=960,h=720;Area area={0,0,w,h};
    unsigned char *input=chart(w,h,4.0f/3.0f,0);upload(input,w,h);
    unsigned char *original=render(fullProgram,w,h,w,h,area,(Effects){0});
    unsigned char *light=render(fullProgram,w,h,w,h,area,vhsLight);
    unsigned char *tape=render(fullProgram,w,h,w,h,area,vhsTape);
    save("vhs-tape-native-original",original,w,h);save("vhs-tape-native",tape,w,h);
    comparison("vhs-tape-native-ab","VHS TAPE / NEW PRESET",original,tape,w,h);
    comparisonLabels("vhs-tape-vs-light","VHS LIGHT / SAME SOURCE","VHS TAPE / NEW PRESET",light,tape,w,h);
    double lightAbs=0,tapeAbs=0;
    for(size_t i=0;i<(size_t)w*h;i++)for(int c=0;c<3;c++){lightAbs+=abs((int)light[i*4+c]-original[i*4+c]);tapeAbs+=abs((int)tape[i*4+c]-original[i*4+c]);}
    char name[220];snprintf(name,sizeof(name),"VHS Tape native 960x720 mean RGB change %.4f/255 exceeds retuned Light %.4f/255 by >1.5x",tapeAbs/(w*h*3),lightAbs/(w*h*3));
    require(tapeAbs>lightAbs*1.5&&tapeAbs/(w*h*3)>4,name);
    Effects moving=vhsTape;moving.freeze=0;moving.time=.125f;
    unsigned char *next=render(fullProgram,w,h,w,h,area,moving);
    save("vhs-tape-native-next",next,w,h);
    require(maxDifference(tape,next,w,h)>10,"VHS Tape animated luma/chroma noise changes real rendered pixels");
    moving.freeze=1;moving.time=17;unsigned char *frozen=render(fullProgram,w,h,w,h,area,moving);
    require(maxDifference(tape,frozen,w,h)==0,"VHS Tape complete high-strength signal noise freezes deterministically");
    require(opaque(tape,w,h),"VHS Tape full framebuffer remains opaque");
    free(original);free(light);free(tape);free(next);free(frozen);
    // The chroma tail must remain visible well beyond the old 3-pixel radius.
    for(int y=0;y<h;y++)for(int x=0;x<w;x++){size_t p=((size_t)y*w+x)*4;input[p]=x<w/2?200:40;input[p+1]=x<w/2?60:120;input[p+2]=x<w/2?70:180;input[p+3]=255;}
    upload(input,w,h);Effects chroma={.chroma=.92f,.intensity=1,.freeze=1};
    unsigned char *tail=render(fullProgram,w,h,w,h,area,chroma);size_t p=((size_t)h/2*w+w/2+10)*4;
    double inputY=.299*input[p]+.587*input[p+1]+.114*input[p+2],outputY=.299*tail[p]+.587*tail[p+1]+.114*tail[p+2];
    snprintf(name,sizeof(name),"VHS Tape color tail at +10 pixels is visible and preserves luma (%.3f -> %.3f)",inputY,outputY);
    require(abs((int)tail[p]-input[p])>10&&fabs(inputY-outputY)<1,name);free(tail);
    // Chroma processing must not move any monochrome edge/hit target.
    for(int y=0;y<h;y++)for(int x=0;x<w;x++){size_t p=((size_t)y*w+x)*4;unsigned char v=(x%71<3||y%59<3)?230:35;input[p]=input[p+1]=input[p+2]=v;}
    upload(input,w,h);unsigned char *grid=render(fullProgram,w,h,w,h,area,chroma);
    require(maxDifference(grid,input,w,h)<=1,"VHS Tape chroma preserves every monochrome grid coordinate within 1 level");free(grid);
    for(size_t i=0;i<(size_t)w*h;i++)input[i*4]=input[i*4+1]=input[i*4+2]=128;
    upload(input,w,h);Effects noise={.noise=.62f,.intensity=1,.freeze=1};unsigned char *grain=render(fullProgram,w,h,w,h,area,noise);
    double sumY=0,sumY2=0,sumColor2=0;
    for(size_t i=0;i<(size_t)w*h;i++){double y=.299*grain[i*4]+.587*grain[i*4+1]+.114*grain[i*4+2];double cb=(double)grain[i*4+2]-y;sumY+=y;sumY2+=y*y;sumColor2+=cb*cb;}
    double avg=sumY/(w*h),sd=sqrt(sumY2/(w*h)-avg*avg),colorSD=sqrt(sumColor2/(w*h));
    snprintf(name,sizeof(name),"VHS Tape noise has luma/chroma variation without global wash (mean %.3f, luma SD %.3f, B-Y RMS %.3f)",avg,sd,colorSD);
    require(fabs(avg-128)<1&&sd>5&&colorSD>3,name);free(grain);
    for(int v=0;v<=255;v+=255){
        for(size_t i=0;i<(size_t)w*h;i++)input[i*4]=input[i*4+1]=input[i*4+2]=(unsigned char)v;
        upload(input,w,h);unsigned char *out=render(fullProgram,w,h,w,h,area,vhsTape);double m=mean(out,w,area);
        snprintf(name,sizeof(name),"VHS Tape neutral endpoint %d retains useful range (mean %.3f)",v,m);
        require(v==0?m<8:m>240,name);free(out);
    }
    free(input);
}

static void identityTests(int w,int h) {
    char name[160];unsigned char *input=chart(w,h,(float)w/h,1);upload(input,w,h);
    Effects e=vhsLight;e.intensity=0;e.curve=.5f;e.jitter=.5f;e.tracking=.5f;e.time=11.5f;
    unsigned char *out=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);
    int difference=maxDifference(input,out,w,h);snprintf(name,sizeof(name),"Full intensity=0 exact 1:1 identity %dx%d (max byte difference %d)",w,h,difference);require(difference==0,name);
    require(opaque(out,w,h),"Full bypass alpha is 255");free(out);free(input);
    Effects zero={0};out=render(overlayProgram,w,h,w,h,(Area){0,0,w,h},zero);int allZero=1;
    for(size_t i=0;i<(size_t)w*h*4;i++)if(out[i]){allZero=0;break;}
    snprintf(name,sizeof(name),"Overlay effective intensity=0 has zero RGBA %dx%d",w,h);require(allZero,name);free(out);
}

static void geometryTests(int w,int h) {
    const int sw=320,sh=200;Area area=fit43(w,h);char name[180];
    unsigned char *input=chart(sw,sh,4.0f/3.0f,0);upload(input,sw,sh);
    Effects e={0};unsigned char *base=render(fullProgram,w,h,sw,sh,area,e);
    e=subtle;unsigned char *crt=render(fullProgram,w,h,sw,sh,area,e);
    snprintf(name,sizeof(name),"Full opaque output/mask %dx%d source320x200 DAR4:3 area=%d,%d,%d,%d",w,h,area.x,area.y,area.w,area.h);require(opaque(crt,w,h)&&blackOutside(crt,w,h,area),name);
    double before=mean(base,w,area),after=mean(crt,w,area),ratio=after/before;
    snprintf(name,sizeof(name),"Subtle CRT brightness sanity %dx%d mean %.3f -> %.3f (ratio %.5f)",w,h,before,after,ratio);require(ratio>.95&&ratio<1.05,name);
    // Frozen time covers noise and optional warp/tracking, not merely a preset
    // whose time-dependent controls might happen to be zero.
    Effects animated=vhsLight;animated.jitter=.3f;animated.tracking=.4f;animated.curve=.15f;animated.time=1;
    unsigned char *frozen1=render(fullProgram,w,h,sw,sh,area,animated);animated.time=11.7f;
    unsigned char *frozen2=render(fullProgram,w,h,sw,sh,area,animated);
    snprintf(name,sizeof(name),"Full frozen noise/optional motion deterministic across time %dx%d",w,h);require(maxDifference(frozen1,frozen2,w,h)==0,name);free(frozen1);free(frozen2);
    Effects overlay=vhsLight;overlay.time=1;unsigned char *over=render(overlayProgram,w,h,sw,sh,area,overlay);overlay.time=23;
    unsigned char *over2=render(overlayProgram,w,h,sw,sh,area,overlay);
    snprintf(name,sizeof(name),"Overlay freeze deterministic %dx%d",w,h);require(maxDifference(over,over2,w,h)==0,name);free(over2);
    int premultiplied=1;for(size_t i=0;i<(size_t)w*h;i++)for(int c=0;c<3;c++)if(over[i*4+c]>over[i*4+3])premultiplied=0;
    snprintf(name,sizeof(name),"Overlay premultiplied RGB<=alpha and opaque black format mask %dx%d",w,h);require(premultiplied&&blackOutside(over,w,h,area),name);
    unsigned char *zeroMask=render(overlayProgram,w,h,sw,sh,area,(Effects){0});int transparentInside=1;
    for(int y=area.y;y<area.y+area.h;y++)for(int x=area.x;x<area.x+area.w;x++){size_t i=((size_t)y*w+x)*4;if(zeroMask[i]||zeroMask[i+1]||zeroMask[i+2]||zeroMask[i+3])transparentInside=0;}
    require(transparentInside&&blackOutside(zeroMask,w,h,area),"Overlay zero filter retains only independent format mask");free(zeroMask);
    if(w==1280){
        save("source-320x200-dar43",input,sw,sh);save("full-bypass-1280x720",base,w,h);save("full-subtle-crt-1280x720",crt,w,h);
        unsigned char *vhs=render(fullProgram,w,h,sw,sh,area,vhsLight);save("full-vhs-light-1280x720",vhs,w,h);free(vhs);
        unsigned char *composite=pixels(w,h);for(size_t i=0;i<(size_t)w*h;i++){float alpha=over[i*4+3]/255.f;for(int c=0;c<3;c++)composite[i*4+c]=(unsigned char)fminf(255,over[i*4+c]+base[i*4+c]*(1-alpha)+.5f);composite[i*4+3]=255;}save("overlay-composited-1280x720",composite,w,h);free(composite);
    }
    free(over);free(base);free(crt);free(input);
}

static void neutralTests(void) {
    const int w=256,h=256;unsigned char *input=pixels(w,h);
    const int values[]={0,16,128,240,255};
    for(size_t v=0;v<sizeof(values)/sizeof(values[0]);v++){
        for(int i=0;i<w*h;i++){input[i*4]=input[i*4+1]=input[i*4+2]=(unsigned char)values[v];input[i*4+3]=255;}
        upload(input,w,h);Effects e=subtle;unsigned char *out=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);double actual=mean(out,w,(Area){0,0,w,h});
        char name[180];snprintf(name,sizeof(name),"Subtle CRT neutral %d -> mean %.4f (no washed black/global gamma shift)",values[v],actual);require(fabs(actual-values[v])<4.0,name);free(out);
        e=(Effects){.intensity=1};out=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);snprintf(name,sizeof(name),"Linear-light decode/encode neutral %d stays within 1 byte",values[v]);require(maxDifference(input,out,w,h)<=1,name);free(out);
    }
    // Non-frozen noise must actually animate; deterministic tests cannot pass
    // just because uTime was optimized away or omitted by the harness.
    upload(input,w,h);Effects e=vhsLight;e.freeze=0;e.noise=.8f;e.time=0;unsigned char *a=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);e.time=1;
    unsigned char *b=render(fullProgram,w,h,w,h,(Area){0,0,w,h},e);require(maxDifference(a,b,w,h)>0,"Full unfrozen noise changes with time");free(a);free(b);free(input);
}

static Effects scaled(Effects e,float intensity) {
    e.intensity=intensity;e.scan*=intensity;e.mask*=intensity;e.bloom*=intensity;e.soft*=intensity;e.chroma*=intensity;e.noise*=intensity;
    e.jitter*=intensity;e.tracking*=intensity;e.vignette*=intensity;e.curve*=intensity;e.radius*=intensity;e.glass*=intensity;return e;
}
static int zero(const unsigned char *p,int w,int h){for(size_t i=0;i<(size_t)w*h*4;i++)if(p[i])return 0;return 1;}
static int premult(const unsigned char *p,int w,int h){for(size_t i=0;i<(size_t)w*h;i++)for(int c=0;c<3;c++)if(p[i*4+c]>p[i*4+3])return 0;return 1;}
static double differenceMean(const unsigned char *a,const unsigned char *b,int w,Area area){double sum=0;for(int y=area.y;y<area.y+area.h;y++)for(int x=area.x;x<area.x+area.w;x++)for(int c=0;c<3;c++){size_t i=((size_t)y*w+x)*4+c;sum+=abs((int)a[i]-b[i]);}return sum/(area.w*area.h*3);}

static void presetAndScreenMatrix(void) {
    const Effects presets[]={subtle,classic,softTV,vhsLight,vhsTape};
    const char *names[]={"subtle","crt-classic","soft-tv","vhs-light","vhs-tape"};
    const char *shapes[]={"flat","rounded","convex"};
    const int w=960,h=720;Area area={0,0,w,h};char name[240],file[160];
    unsigned char *input=chart(w,h,4.0f/3.0f,0);upload(input,w,h);
    unsigned char *base=render(fullProgram,w,h,w,h,area,(Effects){0});
    unsigned char *flat[5]={0};
    for(int preset=0;preset<5;preset++)for(int shape=0;shape<3;shape++)for(int level=0;level<3;level++) {
        Effects e=presets[preset];if(shape){e.radius=.04f;e.glass=.15f;}if(shape==2)e.curve=.18f;e=scaled(e,level*.5f);
        unsigned char *out=render(fullProgram,w,h,w,h,area,e);
        int ok=opaque(out,w,h);if(!level)ok=ok&&maxDifference(base,out,w,h)==0;
        if(shape&&level)ok=ok&&out[0]==0&&out[1]==0&&out[2]==0;
        snprintf(name,sizeof(name),"Full matrix %s / %s / %d%%: opaque, zero bypass, covered corners",names[preset],shapes[shape],level*50);require(ok,name);
        if(level==2){snprintf(file,sizeof(file),"polish-%s-%s",names[preset],shapes[shape]);save(file,out,w,h);if(shape==0){flat[preset]=pixels(w,h);memcpy(flat[preset],out,(size_t)w*h*4);}}
        if(preset==1&&shape==1&&level==2)comparison("polish-crt-classic-rounded-ab","CRT CLASSIC / ROUNDED",base,out,w,h);
        if(preset==2&&shape==2&&level==2)comparison("polish-soft-tv-convex-ab","SOFT TV / CONVEX",base,out,w,h);
        free(out);
        if(shape<2){
            unsigned char *over=render(overlayProgram,w,h,w,h,area,e);ok=premult(over,w,h);
            if(!level)ok=ok&&zero(over,w,h);if(shape&&level)ok=ok&&over[0]==0&&over[1]==0&&over[2]==0&&over[3]==255;
            snprintf(name,sizeof(name),"Lightweight matrix %s / %s / %d%%: premultiplied, zero bypass, black corners",names[preset],shapes[shape],level*50);require(ok,name);free(over);
        }
    }
    for(int a=0;a<5;a++)for(int b=a+1;b<5;b++){
        double difference=differenceMean(flat[a],flat[b],w,area);
        snprintf(name,sizeof(name),"Distinct flat Full presets %s vs %s: RGB MAE %.4f/255",names[a],names[b],difference);require(difference>.65,name);
    }
    comparisonLabels("polish-presets-crt-vs-soft","CRT CLASSIC","SOFT TV",flat[1],flat[2],w,h);
    comparisonLabels("polish-presets-light-vs-tape","VHS LIGHT","VHS TAPE",flat[3],flat[4],w,h);
    for(int i=0;i<5;i++)free(flat[i]);free(base);free(input);
    // Geometry-only comparison: zero glass and zero signal effects must retain
    // all interior source coordinates while masking only the rounded corners.
    const int sizes[][2]={{640,480},{1280,720},{1920,1080}};
    for(int s=0;s<3;s++){
        int rw=sizes[s][0],rh=sizes[s][1];Area inner={rw/10,rh/10,rw*8/10,rh*8/10};
        input=chart(rw,rh,(float)rw/rh,1);upload(input,rw,rh);Effects rounded={.intensity=1,.radius=.04f,.freeze=1};
        unsigned char *out=render(fullProgram,rw,rh,rw,rh,(Area){0,0,rw,rh},rounded);
        snprintf(name,sizeof(name),"Rounded %dx%d preserves exact interior coordinates (MAE %.6f)",rw,rh,differenceMean(input,out,rw,inner));require(differenceMean(input,out,rw,inner)<.01,name);free(out);
        for(size_t i=0;i<(size_t)rw*rh;i++)input[i*4]=input[i*4+1]=input[i*4+2]=255;
        upload(input,rw,rh);out=render(fullProgram,rw,rh,rw,rh,(Area){0,0,rw,rh},rounded);
        int radius=(int)(rh*.04f),symmetric=1,antialiased=0;
        for(int y=0;y<radius;y++)for(int x=0;x<radius;x++){
            size_t a=((size_t)y*rw+x)*4,b=((size_t)x*rw+y)*4;
            if(abs((int)out[a]-out[b])>1)symmetric=0;if(out[a]>0&&out[a]<255)antialiased=1;
        }
        snprintf(name,sizeof(name),"Rounded %dx%d circular physical radius and antialiased boundary",rw,rh);require(symmetric&&antialiased&&out[0]==0&&opaque(out,rw,rh),name);free(out);free(input);
    }
    // TV softness must reduce a fine monochrome edge without adding noise; a
    // flat gray checks the classic preset's stable periodic phosphor pattern.
    input=pixels(w,h);for(int y=0;y<h;y++)for(int x=0;x<w;x++){size_t i=((size_t)y*w+x)*4;unsigned char v=x<w/2?30:220;input[i]=input[i+1]=input[i+2]=v;input[i+3]=255;}upload(input,w,h);
    unsigned char *soft=render(fullProgram,w,h,w,h,area,softTV),*crisp=render(fullProgram,w,h,w,h,area,classic);
    size_t left=((size_t)h/2*w+w/2-2)*4,right=((size_t)h/2*w+w/2+2)*4;
    require((int)soft[right]-(int)soft[left]<(int)crisp[right]-(int)crisp[left]-15,"Soft TV visibly spreads a monochrome edge more than CRT Classic");free(soft);free(crisp);
    for(size_t i=0;i<(size_t)w*h;i++)input[i*4]=input[i*4+1]=input[i*4+2]=128;upload(input,w,h);
    Effects e=classic;e.freeze=0;e.time=0;crisp=render(fullProgram,w,h,w,h,area,e);e.time=2;unsigned char *later=render(fullProgram,w,h,w,h,area,e);
    require(maxDifference(crisp,later,w,h)==0,"CRT Classic signal remains static without VHS noise");free(later);free(crisp);free(input);
    // Selected interactions use nonmatching source/output sizes and a DAR 4:3
    // viewport. This catches mask/UV mistakes hidden by the native-size matrix.
    input=chart(320,200,4.0f/3.0f,0);upload(input,320,200);
    for(int variant=0;variant<3;variant++){
        Effects mixed=vhsTape;mixed.soft=.76f;mixed.bloom=.44f;mixed.radius=.04f;mixed.glass=.15f;
        if(variant==1)mixed.curve=.18f;if(variant==2)mixed=scaled(mixed,.5f);
        Area fit=fit43(1280,720);unsigned char *a=render(fullProgram,1280,720,320,200,fit,mixed);mixed.time=17;
        unsigned char *b=render(fullProgram,1280,720,320,200,fit,mixed);
        snprintf(name,sizeof(name),"Combined softness/chroma/glass variant%d source320x200 DAR4:3: opaque mask and frozen output",variant);
        require(opaque(a,1280,720)&&blackOutside(a,1280,720,fit)&&maxDifference(a,b,1280,720)==0,name);free(a);free(b);
    }
    free(input);
    // Retuning must not achieve visible differences through a uniform wash.
    input=pixels(w,h);
    for(int preset=0;preset<5;preset++){
        int sane=1;double observed[3];const int values[]={0,128,255};
        for(int v=0;v<3;v++){
            for(size_t i=0;i<(size_t)w*h;i++){input[i*4]=input[i*4+1]=input[i*4+2]=(unsigned char)values[v];input[i*4+3]=255;}
            upload(input,w,h);unsigned char *out=render(fullProgram,w,h,w,h,area,presets[preset]);observed[v]=mean(out,w,area);free(out);
        }
        sane=observed[0]<8&&fabs(observed[1]-128)<8&&observed[2]>240;
        snprintf(name,sizeof(name),"Preset %s retains black/midtone/white range (%.2f/%.2f/%.2f)",names[preset],observed[0],observed[1],observed[2]);require(sane,name);
    }
    free(input);
}

int main(void) {
    // Apple's GL4 selector requests a 4.1 core context. The actual runtime
    // version is printed; shader sources remain exactly GLSL330 from the repo.
    CGLPixelFormatAttribute attrs[]={kCGLPFAOpenGLProfile,(CGLPixelFormatAttribute)kCGLOGLPVersion_GL4_Core,kCGLPFAAccelerated,kCGLPFAColorSize,(CGLPixelFormatAttribute)24,kCGLPFAAlphaSize,(CGLPixelFormatAttribute)8,(CGLPixelFormatAttribute)0};
    CGLPixelFormatObj pixelFormat=NULL;GLint count=0;CGLError error=CGLChoosePixelFormat(attrs,&pixelFormat,&count);if(error!=kCGLNoError||!pixelFormat)fatal(CGLErrorString(error));
    CGLContextObj context=NULL;error=CGLCreateContext(pixelFormat,NULL,&context);CGLDestroyPixelFormat(pixelFormat);if(error!=kCGLNoError)fatal(CGLErrorString(error));CGLSetCurrentContext(context);
    printf("Vendor: %s\nRenderer: %s\nOpenGL: %s\nGLSL: %s\n",glGetString(GL_VENDOR),glGetString(GL_RENDERER),glGetString(GL_VERSION),glGetString(GL_SHADING_LANGUAGE_VERSION));
    fullProgram=program("internal/render/shaders/crt.frag");overlayProgram=program("internal/render/shaders/overlay.frag");require(1,"exact GLSL330 sources compile and link on OpenGL 4.1 core");
    glGenFramebuffers(1,&framebuffer);glGenTextures(1,&outputTexture);glGenTextures(1,&sourceTexture);glGenVertexArrays(1,&vao);glBindVertexArray(vao);
    glDisable(GL_BLEND);glDisable(GL_DEPTH_TEST);glDisable(GL_FRAMEBUFFER_SRGB);glPixelStorei(GL_UNPACK_ALIGNMENT,1);glPixelStorei(GL_PACK_ALIGNMENT,1);
    const int resolutions[][2]={{640,480},{1280,720},{1920,1080}};
    for(size_t i=0;i<3;i++){identityTests(resolutions[i][0],resolutions[i][1]);geometryTests(resolutions[i][0],resolutions[i][1]);}
    neutralTests();chromaIndependenceTest();softSignalImpulseTests();demonstration();tapeTestsAndDemonstration();presetAndScreenMatrix();glOK("final checks");printf("RESULT: %d checks, %d failures\n",checks,failures);
    glDeleteProgram(fullProgram);glDeleteProgram(overlayProgram);glDeleteTextures(1,&outputTexture);glDeleteTextures(1,&sourceTexture);glDeleteFramebuffers(1,&framebuffer);glDeleteVertexArrays(1,&vao);CGLSetCurrentContext(NULL);CGLDestroyContext(context);
    return failures?1:0;
}
