param(
 [Parameter(Mandatory=$true)][uint32]$EngineProcessId,
 [Parameter(Mandatory=$true)][string]$EngineExecutable,
 [Parameter(Mandatory=$true)][string]$EngineCreatedFileTime,
 [Parameter(Mandatory=$true)][uint32]$WorkerProcessId,
 [Parameter(Mandatory=$true)][string]$WorkerExecutable,
 [Parameter(Mandatory=$true)][string]$WorkerCreatedFileTime,
 [Parameter(Mandatory=$true)][uint32]$SourceProcessId,
 [Parameter(Mandatory=$true)][string]$SourceExecutable,
 [Parameter(Mandatory=$true)][string]$SourceCreatedFileTime,
 [Parameter(Mandatory=$true)][string]$Output
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# Explicit manual developer test. Run in the same interactive Windows session after
# pointer-input -action move has left an active projected cursor over this source.
# Only the identity-bound engine is suspended. The worker and source stay running.
# No input, WM_CLOSE, termination, profile mutation or restart is performed.
$report=[ordered]@{
 SchemaVersion=1; Action='engine_suspend_lease_expiry'; ObservedAt=(Get-Date).ToUniversalTime().ToString('o')
 ProbePID=$PID; ExpectedLeaseMs=500; MaximumSuspendMs=2000; WatchdogResumeMs=1500
 Engine=$null; Worker=$null; Source=$null; Result=$null; Cleanup=$null
 Passed=$false; Error=$null; CleanupError=$null
 Scope='Read native window/cursor state during an exact-engine stall. Passing means pointer restoration and recovery; overlay hiding is recorded separately because ShowWindowAsync may wait for the suspended owner. CURSOR_SHOWING alone does not prove compositor visibility; MagShowSystemCursor can leave that flag set.'
}
$probe=$null
try {
 $Output=[IO.Path]::GetFullPath($Output)
 if(-not [IO.Directory]::Exists([IO.Path]::GetDirectoryName($Output))){throw 'Output parent directory must already exist'}
 if(-not ('LumaPointerLeaseProbe' -as [type])){
  Add-Type @'
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;

public sealed class LumaPointerLeaseProbe : IDisposable {
 public delegate bool EnumProc(IntPtr window,IntPtr unused);
 [StructLayout(LayoutKind.Sequential)] public struct Rect {public int Left,Top,Right,Bottom;}
 [StructLayout(LayoutKind.Sequential)] public struct Point {public int X,Y;}
 [StructLayout(LayoutKind.Sequential)] struct FileTime {public uint Low,High;}
 [StructLayout(LayoutKind.Sequential)] struct CursorInfo {public uint Size,Flags;public IntPtr Cursor;public Point Position;}
 [StructLayout(LayoutKind.Sequential)] struct IconInfo {public int Icon;public uint HotX,HotY;public IntPtr Mask,Color;}
 [DllImport("kernel32.dll",SetLastError=true)] static extern IntPtr OpenProcess(uint access,bool inherit,uint pid);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool GetProcessTimes(IntPtr process,out FileTime created,out FileTime exited,out FileTime kernel,out FileTime user);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool QueryFullProcessImageNameW(IntPtr process,uint flags,StringBuilder path,ref uint length);
 [DllImport("kernel32.dll")] static extern uint WaitForSingleObject(IntPtr handle,uint timeout);
 [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool ProcessIdToSessionId(uint pid,out uint session);
 [DllImport("ntdll.dll")] static extern int NtSuspendProcess(IntPtr process);
 [DllImport("ntdll.dll")] static extern int NtResumeProcess(IntPtr process);
 [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc callback,IntPtr unused);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr window,StringBuilder text,int maximum);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr window,StringBuilder text,int maximum);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr window,out uint pid);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr window);
 [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr window,out Rect rect);
 [DllImport("user32.dll")] static extern bool GetClientRect(IntPtr window,out Rect rect);
 [DllImport("user32.dll")] static extern bool ClientToScreen(IntPtr window,ref Point point);
 [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] static extern short GetAsyncKeyState(int key);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern IntPtr GetPropW(IntPtr window,string name);
 [DllImport("user32.dll")] static extern bool GetCursorPos(out Point point);
 [DllImport("user32.dll")] static extern bool GetCursorInfo(ref CursorInfo info);
 [DllImport("user32.dll")] static extern bool GetIconInfo(IntPtr cursor,out IconInfo info);
 [DllImport("gdi32.dll")] static extern bool DeleteObject(IntPtr bitmap);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);

 public sealed class Identity {
  public uint PID,Thread;public string Executable,CreatedFileTime,HWND,Class,Title;
  internal IntPtr Process,Window;
 }
 public sealed class Sample {
  public string ObservedAt,Foreground,CursorHandle,SourceHWND;
  public double SinceSuspendMs;public bool WorkerVisible,OverlayVisible,Active,Hidden;
  public long Observation,Serial,SourcePID,WorkerCursorFlags;public uint NativeCursorFlags;
  public Point NativeCursor,WorkerReal,WorkerProjected,WindowPlusNativeHotspot;
  public Rect WorkerWindow,OverlayWindow,SourceClient;
 }
 public sealed class ResumeResult {
  public bool Suspended,ResumeAttempted,Resumed;public int SuspendStatus,ResumeStatus;
  public string ResumeReason,Error;public double ActualSuspendMs;
 }
 public sealed class Result {
  public Sample Before,Expired,Recovered;public List<Sample> During=new List<Sample>();
  public ResumeResult Suspension;public double ExpiryObservedMs;
  public bool PointerRestoredDuringSuspension,OverlayHiddenDuringSuspension,ResumeWithinBound,Passed;
 }
 public Identity Engine,Worker,Source;
 public readonly Result ObservationResult=new Result();
 readonly object suspendLock=new object();
 readonly Stopwatch elapsed=new Stopwatch();
 readonly ManualResetEvent startWatchdog=new ManualResetEvent(false),resumeDone=new ManualResetEvent(false);
 readonly ResumeResult resume=new ResumeResult();
 Thread watchdog;bool suspended,disposed;
 static Exception Error(string message){return new Exception(message+"; Win32="+Marshal.GetLastWin32Error());}
 static string Hex(IntPtr value){return "0x"+value.ToInt64().ToString("x");}
 static long Creation(IntPtr process){
  FileTime created,exited,kernel,user;
  if(!GetProcessTimes(process,out created,out exited,out kernel,out user))throw Error("GetProcessTimes");
  return ((long)created.High<<32)|created.Low;
 }
 static string Class(IntPtr hwnd){var s=new StringBuilder(256);GetClassNameW(hwnd,s,s.Capacity);return s.ToString();}
 static string Title(IntPtr hwnd){var s=new StringBuilder(256);GetWindowTextW(hwnd,s,s.Capacity);return s.ToString();}
 static void Validate(Identity target){
  if(target==null||WaitForSingleObject(target.Process,0)!=0x102)throw new Exception("Exact process is no longer alive");
  if(Creation(target.Process).ToString()!=target.CreatedFileTime)throw new Exception("Process creation identity changed");
  var path=new StringBuilder(32768);uint size=(uint)path.Capacity;
  if(!QueryFullProcessImageNameW(target.Process,0,path,ref size))throw Error("QueryFullProcessImageNameW");
  if(!String.Equals(Path.GetFullPath(path.ToString()),target.Executable,StringComparison.OrdinalIgnoreCase))throw new Exception("Exact executable mismatch");
  uint owner;uint thread=GetWindowThreadProcessId(target.Window,out owner);
  if(owner!=target.PID||thread!=target.Thread||Class(target.Window)!=target.Class||Title(target.Window)!=target.Title)throw new Exception("Exact window identity changed");
 }
 static Identity Bind(uint pid,string path,string created,string cls,string title,bool maySuspend){
  if(pid==0||String.IsNullOrWhiteSpace(path)||String.IsNullOrWhiteSpace(created))throw new Exception("PID, executable and creation FILETIME are mandatory");
  long expected;if(!Int64.TryParse(created,out expected)||expected<=0)throw new Exception("Invalid expected creation FILETIME");
  uint currentSession,targetSession;
  if(!ProcessIdToSessionId((uint)Process.GetCurrentProcess().Id,out currentSession)||!ProcessIdToSessionId(pid,out targetSession)||currentSession==0||targetSession!=currentSession)throw new Exception("Run in the same interactive Windows session");
  var identity=new Identity {PID=pid,Executable=Path.GetFullPath(path),CreatedFileTime=expected.ToString(),Class=cls,Title=title};
  identity.Process=OpenProcess(0x1000|0x100000|(maySuspend?0x800u:0u),false,pid);
  if(identity.Process==IntPtr.Zero)throw Error("OpenProcess exact identity");
  try {
   if(Creation(identity.Process)!=expected)throw new Exception("Expected creation FILETIME does not match PID");
   int matches=0;
   EnumProc callback=delegate(IntPtr hwnd,IntPtr unused){uint owner;uint thread=GetWindowThreadProcessId(hwnd,out owner);if(owner==pid&&Class(hwnd)==cls&&Title(hwnd)==title){identity.Window=hwnd;identity.Thread=thread;matches++;}return true;};
   if(!EnumWindows(callback,IntPtr.Zero)||matches!=1)throw new Exception("Expected exactly one "+cls+" / "+title+" window for PID "+pid+"; got "+matches);
   identity.HWND=Hex(identity.Window);Validate(identity);return identity;
  }catch{CloseHandle(identity.Process);throw;}
 }
 public LumaPointerLeaseProbe(uint enginePID,string engineExe,string engineCreated,uint workerPID,string workerExe,string workerCreated,uint sourcePID,string sourceExe,string sourceCreated){
  if(enginePID==workerPID||enginePID==sourcePID||workerPID==sourcePID)throw new Exception("Engine, worker and source PIDs must be distinct");
  try {
   Engine=Bind(enginePID,engineExe,engineCreated,"GLFW30","LumaTape overlay",true);
   Worker=Bind(workerPID,workerExe,workerCreated,"LumaTape.PointerProjection","LumaTape pointer",false);
   Source=Bind(sourcePID,sourceExe,sourceCreated,"LumaTape.Native.TestCard","LumaTape pointer test",false);
   ObservationResult.Suspension=resume;
  }catch{Dispose();throw;}
 }
 long Prop(string name){return GetPropW(Worker.Window,"LumaTape.Pointer."+name).ToInt64();}
 Rect Client(){
  Rect r;if(!GetClientRect(Source.Window,out r))throw Error("GetClientRect");
  Point a=new Point {X=r.Left,Y=r.Top},b=new Point {X=r.Right,Y=r.Bottom};
  if(!ClientToScreen(Source.Window,ref a)||!ClientToScreen(Source.Window,ref b))throw Error("ClientToScreen");
  return new Rect {Left=a.X,Top=a.Y,Right=b.X,Bottom=b.Y};
 }
 Sample Capture(){
  Validate(Engine);Validate(Worker);Validate(Source);
  for(int attempt=0;attempt<30;attempt++){
   long sequence=Prop("Observation");if(sequence<=0||(sequence&1)!=0){Thread.Sleep(1);continue;}
   var sample=new Sample {ObservedAt=DateTime.UtcNow.ToString("o"),SinceSuspendMs=elapsed.Elapsed.TotalMilliseconds,Observation=sequence};
   sample.Active=Prop("Active")!=0;sample.Hidden=Prop("Hidden")!=0;sample.Serial=Prop("Serial");
   sample.SourcePID=Prop("SourcePID");sample.SourceHWND=Hex(new IntPtr(Prop("SourceHWND")));sample.WorkerCursorFlags=Prop("CursorFlags");
   sample.WorkerReal=new Point {X=unchecked((int)Prop("RealX")),Y=unchecked((int)Prop("RealY"))};
   sample.WorkerProjected=new Point {X=unchecked((int)Prop("ScreenX")),Y=unchecked((int)Prop("ScreenY"))};
   sample.WorkerVisible=IsWindowVisible(Worker.Window);sample.OverlayVisible=IsWindowVisible(Engine.Window);
   if(!GetWindowRect(Worker.Window,out sample.WorkerWindow)||!GetWindowRect(Engine.Window,out sample.OverlayWindow))throw Error("GetWindowRect");
   sample.SourceClient=Client();sample.Foreground=Hex(GetForegroundWindow());
   if(!GetCursorPos(out sample.NativeCursor))throw Error("GetCursorPos");
   var cursor=new CursorInfo {Size=(uint)Marshal.SizeOf(typeof(CursorInfo))};if(!GetCursorInfo(ref cursor))throw Error("GetCursorInfo");
   sample.NativeCursorFlags=cursor.Flags;sample.CursorHandle=Hex(cursor.Cursor);
   if(cursor.Cursor!=IntPtr.Zero){IconInfo icon;if(!GetIconInfo(cursor.Cursor,out icon))throw Error("GetIconInfo");try{sample.WindowPlusNativeHotspot=new Point {X=sample.WorkerWindow.Left+(int)icon.HotX,Y=sample.WorkerWindow.Top+(int)icon.HotY};}finally{if(icon.Mask!=IntPtr.Zero)DeleteObject(icon.Mask);if(icon.Color!=IntPtr.Zero)DeleteObject(icon.Color);}}
   if(Prop("Observation")==sequence)return sample;
  }
  throw new Exception("Could not read a coherent pointer observation");
 }
 static bool Same(Point a,Point b){return a.X==b.X&&a.Y==b.Y;}
 static bool Same(Rect a,Rect b){return a.Left==b.Left&&a.Top==b.Top&&a.Right==b.Right&&a.Bottom==b.Bottom;}
 void CheckStable(Sample before,Sample sample){
  if(sample.Foreground!=Source.HWND)throw new Exception("Source lost foreground during the probe");
  if(!Same(before.NativeCursor,sample.NativeCursor))throw new Exception("Real cursor moved during engine stall/recovery");
  if(!Same(before.SourceClient,sample.SourceClient))throw new Exception("Source client bounds changed during probe");
  if(sample.SourcePID!=Source.PID||sample.SourceHWND!=Source.HWND)throw new Exception("Worker observation is bound to another source");
  if(sample.NativeCursorFlags!=1)throw new Exception("Native CURSOR_SHOWING flag disappeared");
 }
 void Suspend(){
  Validate(Engine);Validate(Worker);Validate(Source);
  // Start the resume thread before touching suspension. It waits for the exact
  // process handle, never a PID lookup, and does not depend on window observations.
  watchdog=new Thread(delegate(){startWatchdog.WaitOne();if(!resumeDone.WaitOne(1500)){try{ResumeEngine("1500ms watchdog");}catch(Exception error){lock(suspendLock){resume.Error=error.Message;}}}});
  watchdog.IsBackground=true;watchdog.Name="LumaTape exact-engine resume guard";watchdog.Start();
  lock(suspendLock){
   elapsed.Restart();suspended=true;resume.Suspended=true;
   try{resume.SuspendStatus=NtSuspendProcess(Engine.Process);if(resume.SuspendStatus<0)throw new Exception("NtSuspendProcess status 0x"+resume.SuspendStatus.ToString("x"));}
   finally{startWatchdog.Set();}
  }
 }
 public ResumeResult ResumeEngine(string reason){
  lock(suspendLock){
   if(!suspended)return resume;
   resume.ResumeAttempted=true;resume.ResumeReason=reason;
   resume.ResumeStatus=NtResumeProcess(Engine.Process);
   if(resume.ResumeStatus<0){resume.Error="NtResumeProcess status 0x"+resume.ResumeStatus.ToString("x");throw new Exception(resume.Error);}
   suspended=false;resume.Resumed=true;resume.ActualSuspendMs=elapsed.Elapsed.TotalMilliseconds;resumeDone.Set();return resume;
  }
 }
 public Result Run(){
  IntPtr oldDpi=SetThreadDpiAwarenessContext(new IntPtr(-4));if(oldDpi==IntPtr.Zero)throw Error("SetThreadDpiAwarenessContext");
  try {
   ObservationResult.Before=Capture();var before=ObservationResult.Before;
   if(!before.Active||!before.Hidden||!before.WorkerVisible||!before.OverlayVisible||before.Serial<=0)throw new Exception("Start with the source foreground and an active projected cursor over its client");
   CheckStable(before,before);
   if(!Same(before.WorkerReal,before.NativeCursor)||!Same(before.WindowPlusNativeHotspot,before.WorkerProjected))throw new Exception("Baseline native/worker cursor evidence does not agree");
   if((GetAsyncKeyState(1)&0x8000)!=0||(GetAsyncKeyState(2)&0x8000)!=0||(GetAsyncKeyState(4)&0x8000)!=0)throw new Exception("Release mouse buttons before the lease probe");
   try {
    Suspend();
    while(elapsed.ElapsedMilliseconds<1100){
     var sample=Capture();ObservationResult.During.Add(sample);CheckStable(before,sample);
     if(!sample.Active&&!sample.Hidden&&!sample.WorkerVisible){
      ObservationResult.Expired=sample;ObservationResult.ExpiryObservedMs=elapsed.Elapsed.TotalMilliseconds;
      ObservationResult.PointerRestoredDuringSuspension=true;
      // Keep this strict observation separate. A suspended GLFW owner may not
      // process ShowWindowAsync until resumed; do not label its surface hidden.
      ObservationResult.OverlayHiddenDuringSuspension=!sample.OverlayVisible;
      break;
     }
     Thread.Sleep(25);
    }
    if(ObservationResult.Expired==null)throw new Exception("Worker did not restore native cursor state and hide its pointer surface within 1100ms of engine stall");
   } finally {ResumeEngine("C# finally");}
   ObservationResult.ResumeWithinBound=resume.Resumed&&resume.ActualSuspendMs<=2000;
   if(!ObservationResult.ResumeWithinBound)throw new Exception("Engine was not resumed inside the 2000ms bound");
   var recovery=Stopwatch.StartNew();
   while(recovery.ElapsedMilliseconds<3000){
    var sample=Capture();CheckStable(before,sample);
    if(sample.Active&&sample.Hidden&&sample.WorkerVisible&&sample.OverlayVisible&&sample.Serial>before.Serial&&Same(sample.WorkerReal,sample.NativeCursor)&&Same(sample.WindowPlusNativeHotspot,sample.WorkerProjected)){ObservationResult.Recovered=sample;break;}
    Thread.Sleep(25);
   }
   if(ObservationResult.Recovered==null)throw new Exception("Projection did not recover with a fresh frame after resume");
   ObservationResult.Passed=true;return ObservationResult;
  } finally {SetThreadDpiAwarenessContext(oldDpi);}
 }
 public void Dispose(){
  if(disposed)return;
  ResumeEngine("Dispose finally");
  resumeDone.Set();startWatchdog.Set();if(watchdog!=null)watchdog.Join(250);
  foreach(var target in new[]{Source,Worker,Engine})if(target!=null&&target.Process!=IntPtr.Zero){CloseHandle(target.Process);target.Process=IntPtr.Zero;}
  disposed=true;
 }
}
'@
 }
 $probe=[LumaPointerLeaseProbe]::new($EngineProcessId,$EngineExecutable,$EngineCreatedFileTime,$WorkerProcessId,$WorkerExecutable,$WorkerCreatedFileTime,$SourceProcessId,$SourceExecutable,$SourceCreatedFileTime)
 $report.Engine=$probe.Engine;$report.Worker=$probe.Worker;$report.Source=$probe.Source
 $report.Result=$probe.Run()
 $report.Passed=$report.Result.Passed
} catch {
 $report.Error=$_.Exception.Message
 if($null -ne $probe){$report.Result=$probe.ObservationResult}
} finally {
 if($null -ne $probe){
  try {$report.Cleanup=$probe.ResumeEngine('PowerShell finally');$probe.Dispose()}
  catch {$report.CleanupError=$_.Exception.Message;$report.Passed=$false}
 }
 $json=$report|ConvertTo-Json -Depth 16
 if(-not [string]::IsNullOrWhiteSpace($Output)){[IO.File]::WriteAllText($Output,$json+[Environment]::NewLine,(New-Object Text.UTF8Encoding($false)))}
}
if(-not $report.Passed){throw ('Pointer lease probe failed: '+$report.Error+' '+$report.CleanupError)}
