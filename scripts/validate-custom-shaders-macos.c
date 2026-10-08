// Uses the existing real CGL readback harness without changing its baseline.
#define main baseline_main
#include "validate-shaders-macos.c"
#undef main

static void customPixels(GLuint p,int w,int h,Effects e,const unsigned char *input,int expectInvert,float fraction) {
 unsigned char *out=render(p,w,h,w,h,(Area){0,0,w,h},e);
 int maxDelta=0,opaque=1;
 for(int i=0;i<w*h;i++) {
  if(out[i*4+3]!=255)opaque=0;
  for(int c=0;c<3;c++) { int original=input[i*4+c]; int expected=expectInvert ? (int)roundf(original*(1-fraction)+(255-original)*fraction) : original;
   int delta=abs(out[i*4+c]-expected);if(delta>maxDelta)maxDelta=delta; }
 }
 require(maxDelta<=1,"custom output obeys source identity / intensity mix within 1 code value");
 require(opaque,"custom wrapper output stays opaque");free(out);
}

static void namedRequire(int ok,const char *name,const char *detail) {
 char message[256];snprintf(message,sizeof(message),"%s %s",name,detail);require(ok,message);
}
static void exampleParameters(GLuint p,const char *directory,const char *name,int width,int height,int zero) {
 char path[1024];snprintf(path,sizeof(path),"%s/%s.params",directory,name);FILE *file=fopen(path,"r");if(!file)fatal(path);
 float values[8];for(int i=0;i<8;i++){if(fscanf(file,"%f",&values[i])!=1)fatal("example defaults need exactly 8 floats");if(zero)values[i]=0;}
 char extra;if(fscanf(file," %c",&extra)!=EOF)fatal("unexpected default values");fclose(file);
 glUseProgram(p);uniform2(p,"ltResolution",width,height);uniform1(p,"ltTime",2.5);
 glUniform1fv(glGetUniformLocation(p,"ltParams[0]"),8,values);
}
static void exampleComparison(const char *directory,const char *name,const char *title,const unsigned char *before,const unsigned char *after,int w,int h) {
 const int header=42,gap=8,ow=w*2+gap,oh=h+header;unsigned char *image=pixels(ow,oh);
 for(int i=0;i<ow*oh;i++){image[i*4]=14;image[i*4+1]=18;image[i*4+2]=27;image[i*4+3]=255;}
 label(image,ow,18,10,"ORIGINAL / 0");label(image,ow,w+gap+18,10,title);
 for(int y=0;y<h;y++){memcpy(image+((size_t)(y+header)*ow)*4,before+(size_t)y*w*4,(size_t)w*4);memcpy(image+((size_t)(y+header)*ow+w+gap)*4,after+(size_t)y*w*4,(size_t)w*4);}
 char path[1024];snprintf(path,sizeof(path),"%s/custom-%s.ppm",directory,name);FILE *file=fopen(path,"wb");if(!file)fatal(path);
 fprintf(file,"P6\n%d %d\n255\n",ow,oh);for(int i=0;i<ow*oh;i++)if(fwrite(image+i*4,1,3,file)!=3)fatal("comparison write failed");if(fclose(file))fatal("comparison close failed");free(image);
}
static void exampleChecks(GLuint p,const char *directory,const char *name,const char *title) {
 const int w=640,h=480;const Area area={0,0,w,h};unsigned char *input=chart(w,h,4.0f/3.0f,0);upload(input,w,h);
 exampleParameters(p,directory,name,w,h,0);
 unsigned char *before=render(p,w,h,w,h,area,(Effects){.intensity=0});
 namedRequire(maxDifference(input,before,w,h)<=1&&opaque(before,w,h),name,"zero bypass exact within 1 code value and opaque");
 unsigned char *full=render(p,w,h,w,h,area,(Effects){.intensity=1});
 unsigned char *half=render(p,w,h,w,h,area,(Effects){.intensity=.5});
 int worst=0;for(int i=0;i<w*h;i++)for(int c=0;c<3;c++){int expected=(int)roundf((before[i*4+c]+full[i*4+c])*.5f);int delta=abs(half[i*4+c]-expected);if(delta>worst)worst=delta;}
 namedRequire(worst<=1&&opaque(half,w,h),name,"intensity50 is original/effect blend");
 namedRequire(maxDifference(input,full,w,h)>8&&opaque(full,w,h),name,"default parameters visibly alter pixels with opaque alpha");
 exampleComparison(directory,name,title,before,full,w,h);
 exampleParameters(p,directory,name,w,h,1);
 unsigned char *neutral=render(p,w,h,w,h,area,(Effects){.intensity=1});
 namedRequire(maxDifference(input,neutral,w,h)<=1&&opaque(neutral,w,h),name,"zero parameters retain original pixels");
 exampleParameters(p,directory,name,w,h,0);
 unsigned char *rounded=render(p,w,h,w,h,area,(Effects){.intensity=1,.radius=.12});
 namedRequire(!rounded[0]&&!rounded[1]&&!rounded[2]&&opaque(rounded,w,h),name,"rounded corners are opaque black");
 unsigned char *bypassShape=render(p,w,h,w,h,area,(Effects){.intensity=0,.radius=.15,.curve=.8,.glass=.8});
 namedRequire(maxDifference(input,bypassShape,w,h)<=1&&opaque(bypassShape,w,h),name,"zero bypass ignores shape and curvature");
 const int ow=848;const Area mask=fit43(ow,h);unsigned char *masked=render(p,ow,h,w,h,mask,(Effects){.intensity=1});
 namedRequire(blackOutside(masked,ow,h,mask)&&opaque(masked,ow,h),name,"4:3 format retains opaque black outside");
 free(input);free(before);free(full);free(half);free(neutral);free(rounded);free(bypassShape);free(masked);
}
int main(int argc,char **argv) {
 if(argc!=6)fatal("usage: custom-validator identity.frag invert.frag amber.frag cold.frag output-directory");
 CGLPixelFormatAttribute attrs[]={kCGLPFAOpenGLProfile,(CGLPixelFormatAttribute)kCGLOGLPVersion_GL4_Core,kCGLPFAAccelerated,kCGLPFAColorSize,(CGLPixelFormatAttribute)24,kCGLPFAAlphaSize,(CGLPixelFormatAttribute)8,0};
 CGLPixelFormatObj format=NULL;GLint count=0;CGLError err=CGLChoosePixelFormat(attrs,&format,&count);if(err||!format)fatal("CGL format");
 CGLContextObj context=NULL;err=CGLCreateContext(format,NULL,&context);CGLDestroyPixelFormat(format);if(err)fatal("CGL context");CGLSetCurrentContext(context);
 printf("Driver %s / %s\n",glGetString(GL_RENDERER),glGetString(GL_VERSION));
 GLuint identity=program(argv[1]),invert=program(argv[2]),amber=program(argv[3]),cold=program(argv[4]);require(identity&&invert&&amber&&cold,"four exact generated GLSL wrappers compile/link");
 glGenFramebuffers(1,&framebuffer);glGenTextures(1,&outputTexture);glGenTextures(1,&sourceTexture);glGenVertexArrays(1,&vao);glBindVertexArray(vao);
 glDisable(GL_BLEND);glDisable(GL_DEPTH_TEST);glDisable(GL_FRAMEBUFFER_SRGB);glPixelStorei(GL_UNPACK_ALIGNMENT,1);glPixelStorei(GL_PACK_ALIGNMENT,1);
 int w=320,h=240;unsigned char *input=chart(w,h,4.0f/3.0f,1);upload(input,w,h);
 customPixels(identity,w,h,(Effects){.intensity=1},input,0,0);
 customPixels(invert,w,h,(Effects){.intensity=0,.curve=.4,.radius=.15,.glass=.7},input,0,0);
 customPixels(invert,w,h,(Effects){.intensity=.5},input,1,.5);
 customPixels(invert,w,h,(Effects){.intensity=1},input,1,1);
 unsigned char *rounded=render(identity,w,h,w,h,(Area){0,0,w,h},(Effects){.intensity=1,.radius=.15});
 require(rounded[0]==0&&rounded[1]==0&&rounded[2]==0&&rounded[3]==255,"rounded corner is opaque black");free(rounded);
 unsigned char *bars=render(invert,w,h,w,h,(Area){40,0,240,240},(Effects){.intensity=1});
 require(bars[(h/2*w)*4]==0&&bars[(h/2*w)*4+3]==255,"wrapper format bars remain opaque black");free(bars);
 // Driver compilation failure must not replace or invalidate an existing program.
 const char *bad="#version 330 core\nout vec4 frag;void main(){frag=unknownFunction();}";GLuint shader=glCreateShader(GL_FRAGMENT_SHADER);glShaderSource(shader,1,&bad,NULL);glCompileShader(shader);GLint ok=1;glGetShaderiv(shader,GL_COMPILE_STATUS,&ok);require(!ok,"real driver rejects invalid shader");glDeleteShader(shader);
 customPixels(identity,w,h,(Effects){.intensity=1},input,0,0);
 exampleChecks(amber,argv[5],"amber-crt","AMBER CRT / 100");
 exampleChecks(cold,argv[5],"cold-bleed","COLD BLEED / 100");
 glOK("custom final");printf("RESULT: %d checks, %d failures\n",checks,failures);
 free(input);glDeleteProgram(identity);glDeleteProgram(invert);glDeleteProgram(amber);glDeleteProgram(cold);glDeleteTextures(1,&sourceTexture);glDeleteTextures(1,&outputTexture);glDeleteFramebuffers(1,&framebuffer);glDeleteVertexArrays(1,&vao);CGLSetCurrentContext(NULL);CGLDestroyContext(context);return failures?1:0;
}
