param(
 [Parameter(Mandatory=$true)][string]$Runtime,
 [Parameter(Mandatory=$true)][string]$OutputDirectory,
 [ValidateRange(1,300)][int]$Seconds=90
)
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
# Read-only observer for our identity-bound testcard, engine and pointer worker.
# Only diagnostic files are written. No screenshots, input, hooks or application changes.
$out=[IO.Path]::GetFullPath($OutputDirectory)
if(-not [IO.Directory]::Exists($out)){throw 'Output directory must already exist'}
$x=Get-Content $Runtime -Raw -Encoding UTF8|ConvertFrom-Json
$sourceExe=if($x.SourceExe){$x.SourceExe}else{Join-Path $x.Bundle 'engine\lumatape-testcard.exe'}
$hostExe=Join-Path $x.Bundle 'lumatape.exe'
$engineExe=Join-Path $x.Bundle 'engine\lumatape-engine.exe'
$workerExe=Join-Path $x.Bundle 'engine\lumatape-watchdog.exe'
$all=@(Get-CimInstance Win32_Process)
$engine=@($all|Where-Object{$_.ParentProcessId -eq $x.HostPID -and $_.ExecutablePath -eq $engineExe})
if($engine.Count -ne 1){throw 'Exact child engine missing'}
$worker=@($all|Where-Object{$_.ParentProcessId -eq $engine[0].ProcessId -and $_.ExecutablePath -eq $workerExe -and $_.CommandLine -like '*--pointer-stdio*'})
if($worker.Count -ne 1){throw 'Exact child pointer worker missing'}
$processes=@();$priorDpi=[IntPtr]::Zero;$failure=$null;$result=$null;$metadata=$null
Add-Type @'
using System;using System.IO;using System.Text;using System.Collections.Generic;using System.Runtime.InteropServices;using System.Threading;using System.Diagnostics;using System.Globalization;
public static class CursorBoundaryProbe {
 public delegate bool EP(IntPtr h,IntPtr p);
 [StructLayout(LayoutKind.Sequential)] public struct Pt {public int X,Y;}
 [StructLayout(LayoutKind.Sequential)] public struct Rect {public int L,T,R,B;}
 [StructLayout(LayoutKind.Sequential)] public struct CI {public uint Size,Flags;public IntPtr Handle;public Pt Position;}
 [StructLayout(LayoutKind.Sequential)] public struct GTI {public uint Size,Flags;public IntPtr Active,Focus,Capture,MenuOwner,MoveSize,Caret;public Rect CaretRect;}
 [DllImport("user32.dll")] static extern bool EnumWindows(EP cb,IntPtr p);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] static extern IntPtr GetAncestor(IntPtr h,uint flags);
 [DllImport("user32.dll")] static extern IntPtr WindowFromPoint(Pt p);
 [DllImport("user32.dll")] static extern bool GetCursorInfo(ref CI p);
 [DllImport("user32.dll")] static extern bool GetGUIThreadInfo(uint thread,ref GTI info);
 [DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr c);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern IntPtr GetPropW(IntPtr h,string n);
 [DllImport("kernel32.dll")] static extern uint WaitForSingleObject(IntPtr h,uint ms);
 [DllImport("kernel32.dll")] static extern bool GetProcessTimes(IntPtr h,out long created,out long exit,out long kernel,out long user);
 public sealed class Bound {public uint PID;public IntPtr Handle,Window;public long Created;public string Class,Title;}
 static bool WindowMatches(IntPtr hwnd,uint pid,string cls,string title){uint owner;GetWindowThreadProcessId(hwnd,out owner);if(owner!=pid)return false;var c=new StringBuilder(200);var t=new StringBuilder(200);GetClassNameW(hwnd,c,200);GetWindowTextW(hwnd,t,200);return c.ToString()==cls&&t.ToString()==title;}
 public static Bound Bind(uint pid,IntPtr handle,long created,string cls,string title){
  var b=new Bound{PID=pid,Handle=handle,Created=created,Class=cls,Title=title};
  if(cls.Length!=0){int n=0;EP cb=(h,p)=>{if(WindowMatches(h,pid,cls,title)){b.Window=h;n++;}return true;};EnumWindows(cb,IntPtr.Zero);GC.KeepAlive(cb);if(n!=1)throw new Exception("Expected one owned window: "+cls);}
  if(!Valid(b))throw new Exception("Identity changed while binding process");return b;
 }
 public static bool Valid(Bound b){long c,e,k,u;return WaitForSingleObject(b.Handle,0)==258&&GetProcessTimes(b.Handle,out c,out e,out k,out u)&&c==b.Created&&(b.Class.Length==0||WindowMatches(b.Window,b.PID,b.Class,b.Title));}
 static long Prop(IntPtr h,string key){return GetPropW(h,"LumaTape.Pointer."+key).ToInt64();}
 static object CardProp(IntPtr h,string key){long raw=GetPropW(h,"LumaTape.TestCard.Pointer."+key).ToInt64();return raw==0?null:(object)(raw-2147483649L);}
 public static Dictionary<string,object> Read(Bound[] b){
  var d=new Dictionary<string,object>();d["utc"]=DateTime.UtcNow.ToString("o");bool valid=true;foreach(var v in b)valid&=Valid(v);d["identity_valid"]=valid;
  if(!valid)return d;var source=b[1].Window;var overlay=b[2].Window;var cursor=b[3].Window;
  var ci=new CI();ci.Size=(uint)Marshal.SizeOf(typeof(CI));bool ciOK=GetCursorInfo(ref ci);d["cursor_info_ok"]=ciOK;d["cursor_flags"]=ci.Flags;d["cursor_handle"]=ci.Handle.ToInt64();d["cursor_x"]=ci.Position.X;d["cursor_y"]=ci.Position.Y;
  var fg=GetForegroundWindow();d["foreground_hwnd"]=fg.ToInt64();d["source_foreground"]=GetAncestor(fg,2)==source;
  var g=new GTI();g.Size=(uint)Marshal.SizeOf(typeof(GTI));d["gui_info_ok"]=GetGUIThreadInfo(0,ref g);d["gui_active_hwnd"]=g.Active.ToInt64();d["gui_focus_hwnd"]=g.Focus.ToInt64();d["gui_capture_hwnd"]=g.Capture.ToInt64();
  var under=ciOK?WindowFromPoint(ci.Position):IntPtr.Zero;d["point_hwnd"]=under.ToInt64();d["point_root_hwnd"]=GetAncestor(under,2).ToInt64();
  long seq=Prop(cursor,"Observation");d["observation"]=seq;d["projected_visible"]=IsWindowVisible(cursor);d["overlay_visible"]=IsWindowVisible(overlay);
  foreach(string key in new[]{"Active","Hidden","RealX","RealY","ScreenX","ScreenY","Serial","SourcePID","SourceHWND"})d["projected_"+key]=Prop(cursor,key);
  long end=Prop(cursor,"Observation");d["observation_end"]=end;d["projection_consistent"]=seq!=0&&seq==end&&(seq%2)==0;d["runtime_consistent"]=(long)d["projected_Active"]==0||((long)d["projected_SourcePID"]==b[1].PID&&(long)d["projected_SourceHWND"]==source.ToInt64());
  object cs=CardProp(source,"Sequence");d["card_sequence"]=cs;
  foreach(string key in new[]{"Version","ClientX","ClientY","Hover","Down","Up","Held","Captured"})d["card_"+key]=CardProp(source,key);
  object ce=CardProp(source,"Sequence");d["card_sequence_end"]=ce;d["card_consistent"]=cs!=null&&cs.Equals(ce)&&((long)cs%2)==0;
  return d;
 }
 static string Cell(object value){if(value==null)return "";if(value is bool)return (bool)value?"1":"0";return Convert.ToString(value,CultureInfo.InvariantCulture);}
 public static object Run(Bound[] b,int seconds,string csv){
  string[] columns={"utc","elapsed_ms","identity_valid","cursor_info_ok","cursor_flags","cursor_handle","cursor_x","cursor_y","foreground_hwnd","source_foreground","gui_info_ok","gui_active_hwnd","gui_focus_hwnd","gui_capture_hwnd","point_hwnd","point_root_hwnd","observation","observation_end","projection_consistent","runtime_consistent","projected_visible","overlay_visible","projected_Active","projected_Hidden","projected_RealX","projected_RealY","projected_ScreenX","projected_ScreenY","projected_Serial","projected_SourcePID","projected_SourceHWND","card_sequence","card_sequence_end","card_consistent","card_Version","card_ClientX","card_ClientY","card_Hover","card_Down","card_Up","card_Held","card_Captured"};
  var watch=Stopwatch.StartNew();int samples=0,inconsistent=0;bool valid=true;Dictionary<string,object> first=null,last=null;
  using(var file=new FileStream(csv,FileMode.Create,FileAccess.Write,FileShare.ReadWrite))using(var writer=new StreamWriter(file,new UTF8Encoding(true))){
   writer.AutoFlush=true;writer.WriteLine(string.Join(",",columns));
   while(watch.Elapsed.TotalSeconds<seconds){long started=watch.ElapsedMilliseconds;var d=Read(b);d["elapsed_ms"]=started;valid=(bool)d["identity_valid"];if(first==null)first=d;last=d;
    var cells=new string[columns.Length];for(int i=0;i<columns.Length;i++){object v;d.TryGetValue(columns[i],out v);cells[i]=Cell(v);}writer.WriteLine(string.Join(",",cells));samples++;
    if(!valid)break;if(!(bool)d["projection_consistent"]||!(bool)d["card_consistent"]||!(bool)d["runtime_consistent"])inconsistent++;
    int pause=(int)Math.Max(0,20-(watch.ElapsedMilliseconds-started));if(pause>0)Thread.Sleep(pause);
   }
  }
  return new{Samples=samples,InconsistentSamples=inconsistent,IdentityValid=valid,DurationMS=watch.ElapsedMilliseconds,Initial=first,Final=last};
 }
}
'@
try {
 $specs=@(
  @{PID=$x.HostPID;Exe=$hostExe;Class='';Title=''},
  @{PID=$x.SourcePID;Exe=$sourceExe;Class='LumaTape.Native.TestCard';Title='LumaTape pointer test'},
  @{PID=$engine[0].ProcessId;Exe=$engineExe;Class='GLFW30';Title='LumaTape overlay'},
  @{PID=$worker[0].ProcessId;Exe=$workerExe;Class='LumaTape.PointerProjection';Title='LumaTape pointer'}
 )
 $bound=@();$identities=@()
 foreach($spec in $specs){
  $p=Get-Process -Id $spec.PID;$processes+=,$p;$handle=$p.Handle
  if($p.Path -ne $spec.Exe){throw 'Identity-bound process path mismatch'}
  $created=$p.StartTime.ToUniversalTime().ToFileTimeUtc()
  $b=[CursorBoundaryProbe]::Bind($p.Id,$handle,$created,$spec.Class,$spec.Title);$bound+=,$b
  $identities+=,[pscustomobject]@{PID=$p.Id;Created=$created;Window=$b.Window.ToInt64();Class=$spec.Class;Executable=[IO.Path]::GetFileName($spec.Exe)}
 }
 $priorDpi=[CursorBoundaryProbe]::SetThreadDpiAwarenessContext([IntPtr]::new(-4))
 if($priorDpi -eq [IntPtr]::Zero){throw 'Unable to establish physical-pixel observer context'}
 $metadata=[pscustomobject]@{StartedAt=[DateTime]::UtcNow.ToString('o');RequestedSeconds=$Seconds;IntervalMS=20;Processes=$identities;Initial=[CursorBoundaryProbe]::Read($bound);Note='Read-only; no screenshots or injected input. Hover=0 alone does not prove WM_MOUSELEAVE.'}
 $metadata|ConvertTo-Json -Depth 8|Set-Content (Join-Path $out 'boundary-ready.json') -Encoding UTF8
 $result=[CursorBoundaryProbe]::Run($bound,$Seconds,(Join-Path $out 'boundary-samples.csv'))
 if(-not $result.IdentityValid){throw 'An identity-bound process/window changed; observation stopped'}
 foreach($b in $bound){if(-not [CursorBoundaryProbe]::Valid($b)){throw 'Identity changed at completion'}}
} catch {$failure=$_.Exception.Message} finally {
 if($priorDpi -ne [IntPtr]::Zero){[void][CursorBoundaryProbe]::SetThreadDpiAwarenessContext($priorDpi)}
 foreach($p in $processes){$p.Dispose()}
 [pscustomobject]@{Error=$failure;Metadata=$metadata;Measurement=$result;CompletedAt=[DateTime]::UtcNow.ToString('o')}|ConvertTo-Json -Depth 10|Set-Content (Join-Path $out 'boundary-result.json') -Encoding UTF8
}
if($failure){throw $failure}
