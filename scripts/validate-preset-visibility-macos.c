// Full preset visibility at actual 1:1 framebuffer sizes, with before/after
// regression comparisons. Reuses only real CGL rendering/chart helpers.
#define main baseline_main
#include "validate-shaders-macos.c"
#undef main

static const char *outputDirectory;
static const Effects allEffects[]={subtle,classic,softTV,vhsLight,vhsTape};
static const char *presetNames[]={"subtle","crt-classic","soft-tv","vhs-light","vhs-tape"};
static void saveVisibility(const char *name,const unsigned char *image,int w,int h) {
 char path[1024];snprintf(path,sizeof(path),"%s/%s.ppm",outputDirectory,name);FILE *file=fopen(path,"wb");if(!file)fatal(path);
 fprintf(file,"P6\n%d %d\n255\n",w,h);for(int i=0;i<w*h;i++)if(fwrite(image+i*4,1,3,file)!=3)fatal("write image");if(fclose(file))fatal("close image");
}
static void visibilityAB(const char *name,const unsigned char *before,const unsigned char *after,int w,int h) {
 int ow=w*2+8,oh=h+42;unsigned char *image=pixels(ow,oh);
 for(int i=0;i<ow*oh;i++){image[i*4]=14;image[i*4+1]=18;image[i*4+2]=27;image[i*4+3]=255;}
 label(image,ow,18,10,"ORIGINAL");label(image,ow,w+26,10,"FULL EFFECT / 100");
 for(int y=0;y<h;y++){memcpy(image+((size_t)(y+42)*ow)*4,before+(size_t)y*w*4,(size_t)w*4);memcpy(image+((size_t)(y+42)*ow+w+8)*4,after+(size_t)y*w*4,(size_t)w*4);}
 saveVisibility(name,image,ow,oh);free(image);
}
static void visibilityMetric(GLuint program,int preset,int w,int h,const char *revision) {
 unsigned char *gray=pixels(w,h);for(int i=0;i<w*h;i++){gray[i*4]=gray[i*4+1]=gray[i*4+2]=160;gray[i*4+3]=255;}upload(gray,w,h);
 unsigned char *out=render(program,w,h,w,h,(Area){0,0,w,h},allEffects[preset]);
 double minRow=255,maxRow=0;int crossings=0;double previous=0;
 for(int y=h/4;y<3*h/4;y++){double row=0;for(int x=w/3;x<2*w/3;x++)row+=(out[((size_t)y*w+x)*4]+out[((size_t)y*w+x)*4+1]+out[((size_t)y*w+x)*4+2])/3.0;row/=w/3;
 if(row<minRow)minRow=row;if(row>maxRow)maxRow=row;if(y>h/4&&((row-160)*(previous-160)<0))crossings++;previous=row;}
 printf("METRIC {\"revision\":\"%s\",\"preset\":\"%s\",\"width\":%d,\"height\":%d,\"gray_row_min\":%.4f,\"gray_row_max\":%.4f,\"gray_row_range\":%.4f,\"crossings\":%d}\n",revision,presetNames[preset],w,h,minRow,maxRow,maxRow-minRow,crossings);
 if(preset==1&&!strcmp(revision,"after")) {
  char message[160];snprintf(message,sizeof(message),"CRT Classic %dp readable scan contrast and stable 240-line signal",h);
  require(maxRow-minRow>30&&crossings>=235&&crossings<=245,message);
 }
 free(gray);free(out);
}
int main(int argc,char **argv) {
 if(argc!=4)fatal("usage: visibility before.frag current.frag outputDirectory");outputDirectory=argv[3];
 CGLPixelFormatAttribute attrs[]={kCGLPFAOpenGLProfile,(CGLPixelFormatAttribute)kCGLOGLPVersion_GL4_Core,kCGLPFAAccelerated,kCGLPFAColorSize,(CGLPixelFormatAttribute)24,kCGLPFAAlphaSize,(CGLPixelFormatAttribute)8,0};
 CGLPixelFormatObj format=NULL;GLint count=0;if(CGLChoosePixelFormat(attrs,&format,&count)||!format)fatal("format");CGLContextObj context=NULL;if(CGLCreateContext(format,NULL,&context))fatal("context");CGLDestroyPixelFormat(format);CGLSetCurrentContext(context);
 printf("Driver %s / %s\n",glGetString(GL_RENDERER),glGetString(GL_VERSION));GLuint old=program(argv[1]),current=program(argv[2]);
 glGenFramebuffers(1,&framebuffer);glGenTextures(1,&outputTexture);glGenTextures(1,&sourceTexture);glGenVertexArrays(1,&vao);glBindVertexArray(vao);glDisable(GL_BLEND);glDisable(GL_DEPTH_TEST);glDisable(GL_FRAMEBUFFER_SRGB);glPixelStorei(GL_PACK_ALIGNMENT,1);glPixelStorei(GL_UNPACK_ALIGNMENT,1);
 const int dimensions[][2]={{960,720},{2308,1731}};
 for(int size=0;size<2;size++){
  int w=dimensions[size][0],h=dimensions[size][1];Area area={0,0,w,h};unsigned char *input=chart(w,h,4.0f/3.0f,0);upload(input,w,h);unsigned char *base=render(current,w,h,w,h,area,(Effects){0});char name[200];snprintf(name,sizeof(name),"original-%d",h);saveVisibility(name,base,w,h);
  for(int preset=0;preset<5;preset++){
   unsigned char *before=render(old,w,h,w,h,area,allEffects[preset]);unsigned char *after=render(current,w,h,w,h,area,allEffects[preset]);
   printf("METRIC {\"preset\":\"%s\",\"width\":%d,\"height\":%d,\"before_rgb_mae\":%.4f,\"after_rgb_mae\":%.4f,\"change_max\":%d}\n",presetNames[preset],w,h,differenceMean(input,before,w,area),differenceMean(input,after,w,area),maxDifference(before,after,w,h));
   snprintf(name,sizeof(name),"%s-%d alpha remains opaque",presetNames[preset],h);require(opaque(after,w,h),name);
   if(preset==1||preset==2){snprintf(name,sizeof(name),"%s-%d appearance remains visible over source",presetNames[preset],h);require(differenceMean(input,after,w,area)>10,name);}
   if(preset==0||preset>=3){snprintf(name,sizeof(name),"%s-%d appearance is byte-identical",presetNames[preset],h);require(maxDifference(before,after,w,h)==0,name);}
   snprintf(name,sizeof(name),"before-%s-%d",presetNames[preset],h);saveVisibility(name,before,w,h);snprintf(name,sizeof(name),"after-%s-%d",presetNames[preset],h);saveVisibility(name,after,w,h);
   if(size==0){snprintf(name,sizeof(name),"ab-%s",presetNames[preset]);visibilityAB(name,base,after,w,h);}
   free(before);free(after);
  }
  free(base);free(input);
  for(int preset=0;preset<5;preset++){visibilityMetric(old,preset,w,h,"before");visibilityMetric(current,preset,w,h,"after");}
 }
 glOK("visibility final");printf("RESULT: %d checks, %d failures\n",checks,failures);glDeleteProgram(old);glDeleteProgram(current);glDeleteTextures(1,&sourceTexture);glDeleteTextures(1,&outputTexture);glDeleteFramebuffers(1,&framebuffer);glDeleteVertexArrays(1,&vao);CGLSetCurrentContext(NULL);CGLDestroyContext(context);return failures?1:0;
}
