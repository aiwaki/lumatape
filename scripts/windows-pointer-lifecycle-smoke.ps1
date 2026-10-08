param(
 [Parameter(Mandatory=$true)][string]$RuntimeJson,
 [Parameter(Mandatory=$true)][string]$HelperPath,
 [Parameter(Mandatory=$true)][string]$Output,
 [ValidateRange(0.0,1.0)][double]$Curvature=0.7
)
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
# Explicit manual developer test; never invoked by staging or build scripts.
# Last action closes only the exact candidate's LumaTape pointer test HWND. The
# original baseline testcard has another title and is never selected or changed.
$report=[ordered]@{
 SchemaVersion=1;Action='own_pointer_testcard_lifecycle';ObservedAt=(Get-Date).ToUniversalTime().ToString('o')
 RuntimeJson=$RuntimeJson;Helper=$null;Host=$null;Engine=$null;Worker=$null;Source=$null
 Before=$null;Minimized=$null;Restored=$null;Resized=$null;Closed=$null;LastSnapshot=$null;HelperResults=@()
 CloseQueued=$false;SourceExited=$false;EngineAliveAfterClose=$false;WorkerAliveAfterClose=$false
 Cleanup=$null;Passed=$false;Error=$null;CleanupError=$null
 Scope='Native source lifecycle plus guest makc input from the existing exact-PID helper. Cursor flags and worker restoration are recorded; CURSOR_SHOWING alone does not prove compositor visibility.'
}
$probe=$null;$closing=$false;$helpers=@()
try {
 $Output=[IO.Path]::GetFullPath($Output);$RuntimeJson=[IO.Path]::GetFullPath($RuntimeJson);$HelperPath=[IO.Path]::GetFullPath($HelperPath)
 $evidence=[IO.Path]::GetDirectoryName($Output)
 if(-not [IO.Directory]::Exists($evidence)){throw 'Output parent directory must already exist'}
 if(-not [IO.File]::Exists($HelperPath)){throw 'Exact pointer-input helper does not exist'}
 $report.Helper=[ordered]@{Path=$HelperPath;SHA256=(Get-FileHash -LiteralPath $HelperPath -Algorithm SHA256).Hash}
 $candidate=Get-Content -LiteralPath $RuntimeJson -Raw -Encoding UTF8|ConvertFrom-Json
 $bundle=[IO.Path]::GetFullPath([string]$candidate.Bundle)
 $all=@(Get-CimInstance Win32_Process)
 $hostProcess=@($all|Where-Object{$_.ProcessId -eq [uint32]$candidate.HostPID -and $_.ExecutablePath -eq (Join-Path $bundle 'lumatape.exe')})
 if($hostProcess.Count -ne 1){throw 'Runtime metadata does not identify the exact live bundle host'}
 $engine=@($all|Where-Object{$_.ParentProcessId -eq $hostProcess[0].ProcessId -and $_.ExecutablePath -eq (Join-Path $bundle 'engine\lumatape-engine.exe')})
 if($engine.Count -ne 1){throw 'Expected one exact engine child of the bound bundle host'}
 $worker=@($all|Where-Object{$_.ParentProcessId -eq $engine[0].ProcessId -and $_.ExecutablePath -eq (Join-Path $bundle 'engine\lumatape-watchdog.exe') -and $_.CommandLine -like '*--pointer-stdio*'})
 if($worker.Count -ne 1){throw 'Expected one exact pointer worker child of the bound engine'}
 $source=@($all|Where-Object{$_.ProcessId -eq [uint32]$candidate.SourcePID -and $_.ExecutablePath -eq (Join-Path $bundle 'engine\lumatape-testcard.exe')})
 if($source.Count -ne 1){throw 'Runtime metadata does not identify the exact candidate testcard'}
 $hostProcess=$hostProcess[0];$engine=$engine[0];$worker=$worker[0];$source=$source[0]
 $report.Host=[ordered]@{PID=$hostProcess.ProcessId;Executable=$hostProcess.ExecutablePath;CreatedFileTime=$hostProcess.CreationDate.ToFileTimeUtc().ToString()}
 if(-not ('LumaPointerLifecycleProbe' -as [type])){
  Add-Type @'
using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
public sealed class LumaPointerLifecycleProbe : IDisposable {
 public delegate bool EnumProc(IntPtr hwnd,IntPtr unused);
 [StructLayout(LayoutKind.Sequential)] public struct Point {public int X,Y;}
 [StructLayout(LayoutKind.Sequential)] public struct Rect {public int Left,Top,Right,Bottom;}
 [StructLayout(LayoutKind.Sequential)] struct FileTime {public uint Low,High;}
 [StructLayout(LayoutKind.Sequential)] struct CursorInfo {public uint Size,Flags;public IntPtr Cursor;public Point Position;}
 [DllImport("kernel32.dll",SetLastError=true)] static extern IntPtr OpenProcess(uint access,bool inherit,uint pid);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool GetProcessTimes(IntPtr process,out FileTime created,out FileTime exited,out FileTime kernel,out FileTime user);
 [DllImport("kernel32.dll",SetLastError=true,CharSet=CharSet.Unicode)] static extern bool QueryFullProcessImageNameW(IntPtr process,uint flags,StringBuilder path,ref uint size);
 [DllImport("kernel32.dll",SetLastError=true)] static extern uint WaitForSingleObject(IntPtr process,uint timeout);
 [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
 [DllImport("kernel32.dll")] static extern bool ProcessIdToSessionId(uint pid,out uint session);
 [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc callback,IntPtr unused);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsWindow(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsIconic(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsZoomed(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll")] static extern bool GetClientRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll")] static extern bool ClientToScreen(IntPtr hwnd,ref Point point);
 [DllImport("user32.dll")] static extern bool ShowWindowAsync(IntPtr hwnd,int command);
 [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr hwnd);
 [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll",SetLastError=true)] static extern bool SetWindowPos(IntPtr hwnd,IntPtr after,int x,int y,int width,int height,uint flags);
 [DllImport("user32.dll",SetLastError=true)] static extern bool PostMessageW(IntPtr hwnd,uint message,UIntPtr wparam,IntPtr lparam);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern IntPtr GetPropW(IntPtr hwnd,string name);
 [DllImport("user32.dll")] static extern bool GetCursorPos(out Point point);
 [DllImport("user32.dll")] static extern bool GetCursorInfo(ref CursorInfo info);
 [DllImport("user32.dll")] static extern short GetAsyncKeyState(int key);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
 public sealed class Identity {public uint PID,Thread;public string Executable,CreatedFileTime,HWND,Class,Title;internal IntPtr Process,Window;}
 public sealed class Snapshot {
  public string ObservedAt,Foreground,WorkerSourceHWND;public long Observation,Serial,WorkerSourcePID;
  public bool EngineAlive,WorkerAlive,SourceAlive,SourceWindowExists,SourceIconic,SourceVisible,OverlayVisible,PointerVisible,Active,Hidden;
  public Rect SourceWindow,SourceClient,OverlayWindow,PointerWindow;public Point NativeCursor,WorkerReal,Projected;
  public uint NativeCursorFlags;
 }
 public sealed class CleanupResult {public bool Attempted,Restored,SourceAlive,EngineAlive,WorkerAlive;public Rect Wanted,Actual;}
 public Identity Engine,Worker,Source;public Rect OriginalWindow,RequestedResize;public bool CloseQueued;public Snapshot LastSnapshot;
 bool disposed;
 static string Hex(IntPtr v){return "0x"+v.ToInt64().ToString("x");}
 static Exception Error(string action){return new Exception(action+"; Win32="+Marshal.GetLastWin32Error());}
 static long Creation(IntPtr process){FileTime a,b,c,d;if(!GetProcessTimes(process,out a,out b,out c,out d))throw Error("GetProcessTimes");return ((long)a.High<<32)|a.Low;}
 static string Text(IntPtr hwnd,bool cls){var b=new StringBuilder(256);if(cls)GetClassNameW(hwnd,b,b.Capacity);else GetWindowTextW(hwnd,b,b.Capacity);return b.ToString();}
 public static string ReadCreationFileTime(uint pid,string executable){
  IntPtr process=OpenProcess(0x1000|0x100000,false,pid);if(process==IntPtr.Zero)throw Error("OpenProcess creation discovery");
  try{if(WaitForSingleObject(process,0)!=0x102)throw new Exception("Process exited during creation discovery");
   var path=new StringBuilder(32768);uint size=(uint)path.Capacity;if(!QueryFullProcessImageNameW(process,0,path,ref size))throw Error("Creation discovery image path");
   if(!String.Equals(Path.GetFullPath(path.ToString()),Path.GetFullPath(executable),StringComparison.OrdinalIgnoreCase))throw new Exception("Creation discovery executable mismatch");
   return Creation(process).ToString();
  }finally{CloseHandle(process);}
 }
 static bool Alive(Identity target){return target!=null&&WaitForSingleObject(target.Process,0)==0x102;}
 bool SourceAliveForObservation(){
  uint status=WaitForSingleObject(Source.Process,0);
  if(status==0)return false;
  if(status==0x102)return true;
  throw Error("WaitForSingleObject exact source handle");
 }
 static void Validate(Identity target,bool window){
  if(!Alive(target))throw new Exception("Exact process exited: "+(target==null?"missing":target.Executable));
  if(Creation(target.Process).ToString()!=target.CreatedFileTime)throw new Exception("Creation identity changed");
  var path=new StringBuilder(32768);uint length=(uint)path.Capacity;
  if(!QueryFullProcessImageNameW(target.Process,0,path,ref length))throw Error("QueryFullProcessImageNameW for PID "+target.PID+" ("+target.Executable+")");
  if(!String.Equals(Path.GetFullPath(path.ToString()),target.Executable,StringComparison.OrdinalIgnoreCase))throw new Exception("Executable identity changed");
  if(window){uint owner;uint thread=GetWindowThreadProcessId(target.Window,out owner);if(owner!=target.PID||thread!=target.Thread||Text(target.Window,true)!=target.Class||Text(target.Window,false)!=target.Title)throw new Exception("Window identity changed");}
 }
 static Identity Bind(uint pid,string path,string created,string cls,string title){
  uint a,b;if(!ProcessIdToSessionId((uint)Process.GetCurrentProcess().Id,out a)||!ProcessIdToSessionId(pid,out b)||a==0||a!=b)throw new Exception("Must run in the same interactive session");
  long expected;if(pid==0||!Int64.TryParse(created,out expected)||expected<=0)throw new Exception("Require exact PID and creation FILETIME");
  var target=new Identity {PID=pid,Executable=Path.GetFullPath(path),CreatedFileTime=expected.ToString(),Class=cls,Title=title};
  target.Process=OpenProcess(0x1000|0x100000,false,pid);if(target.Process==IntPtr.Zero)throw Error("OpenProcess");
  try{if(Creation(target.Process)!=expected)throw new Exception("Creation FILETIME no longer matches discovery");int count=0;
   EnumProc callback=delegate(IntPtr hwnd,IntPtr unused){uint owner;uint thread=GetWindowThreadProcessId(hwnd,out owner);if(owner==pid&&Text(hwnd,true)==cls&&Text(hwnd,false)==title){target.Window=hwnd;target.Thread=thread;count++;}return true;};
   if(!EnumWindows(callback,IntPtr.Zero)||count!=1)throw new Exception("Expected exactly one bound "+cls+" / "+title+" window");
   target.HWND=Hex(target.Window);Validate(target,true);return target;
  }catch{CloseHandle(target.Process);throw;}
 }
 public LumaPointerLifecycleProbe(uint enginePID,string engineExe,string engineCreated,uint workerPID,string workerExe,string workerCreated,uint sourcePID,string sourceExe,string sourceCreated){
  if(enginePID==workerPID||enginePID==sourcePID||workerPID==sourcePID)throw new Exception("Distinct process identities required");
  try{Engine=Bind(enginePID,engineExe,engineCreated,"GLFW30","LumaTape overlay");Worker=Bind(workerPID,workerExe,workerCreated,"LumaTape.PointerProjection","LumaTape pointer");Source=Bind(sourcePID,sourceExe,sourceCreated,"LumaTape.Native.TestCard","LumaTape pointer test");
   if(IsIconic(Source.Window)||IsZoomed(Source.Window))throw new Exception("Start with the ordinary restored testcard window");
   IntPtr previous=Dpi();try{if(!GetWindowRect(Source.Window,out OriginalWindow))throw Error("GetWindowRect original");}finally{SetThreadDpiAwarenessContext(previous);}
  }catch{Dispose();throw;}
 }
 static IntPtr Dpi(){IntPtr previous=SetThreadDpiAwarenessContext(new IntPtr(-4));if(previous==IntPtr.Zero)throw Error("DPI v2");return previous;}
 long Prop(string name){return GetPropW(Worker.Window,"LumaTape.Pointer."+name).ToInt64();}
 void Guards(bool sourceWindow){Validate(Engine,true);Validate(Worker,true);if(sourceWindow)Validate(Source,true);}
 public Snapshot Capture(){
  IntPtr previous=Dpi();try{
   Guards(false);
   // Once our own WM_CLOSE is queued, process teardown may deny image-path
   // queries before the process handle becomes signaled. The retained handle
   // already pins the exact validated process object; wait for its exit without
   // reacquiring a PID or querying dying process metadata. All non-close source
   // checks, and every engine/worker validation, remain strict.
   bool sourceAlive=SourceAliveForObservation();if(!CloseQueued&&sourceAlive)Validate(Source,false);
   for(int i=0;i<30;i++){
    long sequence=Prop("Observation");if(sequence<=0||(sequence&1)!=0){Thread.Sleep(1);continue;}
    var s=new Snapshot {ObservedAt=DateTime.UtcNow.ToString("o"),Observation=sequence,EngineAlive=true,WorkerAlive=true,SourceAlive=sourceAlive,SourceWindowExists=IsWindow(Source.Window)};
    s.Foreground=Hex(GetForegroundWindow());s.Active=Prop("Active")!=0;s.Hidden=Prop("Hidden")!=0;s.Serial=Prop("Serial");s.WorkerSourcePID=Prop("SourcePID");s.WorkerSourceHWND=Hex(new IntPtr(Prop("SourceHWND")));
    s.WorkerReal=new Point {X=unchecked((int)Prop("RealX")),Y=unchecked((int)Prop("RealY"))};s.Projected=new Point {X=unchecked((int)Prop("ScreenX")),Y=unchecked((int)Prop("ScreenY"))};
    s.OverlayVisible=IsWindowVisible(Engine.Window);s.PointerVisible=IsWindowVisible(Worker.Window);
    if(!GetWindowRect(Engine.Window,out s.OverlayWindow)||!GetWindowRect(Worker.Window,out s.PointerWindow))throw Error("Read projection windows");
    if(s.SourceWindowExists&&CloseQueued){
     uint owner;uint thread=GetWindowThreadProcessId(Source.Window,out owner);
     if(thread==0){s.SourceWindowExists=false;}
     else if(owner!=Source.PID||thread!=Source.Thread)throw new Exception("Source HWND ownership changed while waiting for its close");
     // No source HWND mutation or process metadata query is allowed in this
     // terminal branch. A later bounded sample must observe the HWND gone and
     // the retained process handle signaled before the close test can pass.
    }
    if(s.SourceWindowExists&&!CloseQueued){Validate(Source,true);s.SourceIconic=IsIconic(Source.Window);s.SourceVisible=IsWindowVisible(Source.Window);if(!GetWindowRect(Source.Window,out s.SourceWindow))throw Error("Read source bounds");
     Rect c;if(!GetClientRect(Source.Window,out c))throw Error("GetClientRect");Point a=new Point {X=c.Left,Y=c.Top},b=new Point {X=c.Right,Y=c.Bottom};if(!ClientToScreen(Source.Window,ref a)||!ClientToScreen(Source.Window,ref b))throw Error("ClientToScreen");s.SourceClient=new Rect {Left=a.X,Top=a.Y,Right=b.X,Bottom=b.Y};}
    if(!GetCursorPos(out s.NativeCursor))throw Error("GetCursorPos");var cursor=new CursorInfo {Size=(uint)Marshal.SizeOf(typeof(CursorInfo))};if(!GetCursorInfo(ref cursor))throw Error("GetCursorInfo");s.NativeCursorFlags=cursor.Flags;
    if(Prop("Observation")==sequence){LastSnapshot=s;return s;}
   }throw new Exception("Could not read coherent pointer state");
  }finally{SetThreadDpiAwarenessContext(previous);}
 }
 public void ValidateBeforeHelper(){Guards(true);if(CloseQueued)throw new Exception("Source close was already queued");}
 public void Minimize(){Guards(true);NoButtons();if(!ShowWindowAsync(Source.Window,6))throw Error("Minimize own source");}
 public void Restore(){Guards(true);if(!ShowWindowAsync(Source.Window,9))throw Error("Restore own source");SetForegroundWindow(Source.Window);}
 public void Resize(){
  Guards(true);NoButtons();IntPtr previous=Dpi();try{
   Rect r;if(!GetWindowRect(Source.Window,out r))throw Error("Read resize bounds");int width=r.Right-r.Left-96,height=r.Bottom-r.Top-72;
   Rect c;if(!GetClientRect(Source.Window,out c)||c.Right-c.Left<416||c.Bottom-c.Top<312)throw new Exception("Testcard too small for bounded resize");
   RequestedResize=new Rect {Left=r.Left,Top=r.Top,Right=r.Left+width,Bottom=r.Top+height};
   if(!SetWindowPos(Source.Window,IntPtr.Zero,r.Left,r.Top,width,height,0x4|0x10))throw Error("Resize own source");
  }finally{SetThreadDpiAwarenessContext(previous);}
 }
 public void CloseSource(){Guards(true);NoButtons();if(!PostMessageW(Source.Window,0x0010,UIntPtr.Zero,IntPtr.Zero))throw Error("Queue WM_CLOSE only to exact source");CloseQueued=true;}
 static void NoButtons(){if((GetAsyncKeyState(1)&0x8000)!=0||(GetAsyncKeyState(2)&0x8000)!=0||(GetAsyncKeyState(4)&0x8000)!=0)throw new Exception("Release mouse buttons before source lifecycle operations");}
 static bool Same(Rect a,Rect b){return a.Left==b.Left&&a.Top==b.Top&&a.Right==b.Right&&a.Bottom==b.Bottom;}
 public Snapshot Wait(string phase,long newerSerial){
  var watch=Stopwatch.StartNew();Snapshot last=null;
  while(watch.ElapsedMilliseconds<4000){
   last=Capture();bool restored=!last.Active&&!last.Hidden&&!last.PointerVisible&&last.NativeCursorFlags==1;
   bool active=last.Active&&last.Hidden&&last.PointerVisible&&last.OverlayVisible&&last.NativeCursorFlags==1&&last.WorkerSourcePID==Source.PID&&last.WorkerSourceHWND==Source.HWND&&last.Foreground==Source.HWND&&Same(last.SourceClient,last.OverlayWindow)&&last.Serial>newerSerial;
   if(phase=="minimized"&&last.SourceAlive&&last.SourceIconic&&restored&&!last.OverlayVisible)return last;
   if(phase=="active"&&last.SourceAlive&&!last.SourceIconic&&active)return last;
   if(phase=="closed"&&!last.SourceAlive&&!last.SourceWindowExists&&restored&&!last.OverlayVisible)return last;
   Thread.Sleep(25);
  }throw new Exception("Native lifecycle state timed out for "+phase);
 }
 public CleanupResult RestoreOriginalOnFailure(){
  var result=new CleanupResult {SourceAlive=Alive(Source),EngineAlive=Alive(Engine),WorkerAlive=Alive(Worker),Wanted=OriginalWindow};
  if(CloseQueued||!result.SourceAlive)return result;
  // Cleanup only depends on the still-owned source. An engine/worker failure is
  // precisely when restoration must remain possible.
  Validate(Source,true);result.Attempted=true;IntPtr previous=Dpi();try{
   ShowWindowAsync(Source.Window,9);var wait=Stopwatch.StartNew();
   while(IsIconic(Source.Window)&&wait.ElapsedMilliseconds<2000){Validate(Source,true);Thread.Sleep(25);}
   if(IsIconic(Source.Window))throw new Exception("Cleanup source did not leave minimized state");
   if(!SetWindowPos(Source.Window,IntPtr.Zero,OriginalWindow.Left,OriginalWindow.Top,OriginalWindow.Right-OriginalWindow.Left,OriginalWindow.Bottom-OriginalWindow.Top,0x4|0x10))throw Error("Restore original source bounds");SetForegroundWindow(Source.Window);
   wait.Restart();while(wait.ElapsedMilliseconds<2000){Validate(Source,true);if(!GetWindowRect(Source.Window,out result.Actual))throw Error("Verify restored source bounds");if(!IsIconic(Source.Window)&&Same(result.Actual,OriginalWindow)){result.Restored=true;return result;}Thread.Sleep(25);}
   throw new Exception("Cleanup source did not regain its exact original rectangle");
  }finally{SetThreadDpiAwarenessContext(previous);}
 }
 public void Dispose(){if(disposed)return;foreach(var target in new[]{Source,Worker,Engine})if(target!=null&&target.Process!=IntPtr.Zero){CloseHandle(target.Process);target.Process=IntPtr.Zero;}disposed=true;}
}
'@
 }
 # CIM dates can truncate 100ns FILETIME precision. Read native identities first,
 # then bind new retained handles that must match those exact values.
 $engineCreated=[LumaPointerLifecycleProbe]::ReadCreationFileTime($engine.ProcessId,$engine.ExecutablePath)
 $workerCreated=[LumaPointerLifecycleProbe]::ReadCreationFileTime($worker.ProcessId,$worker.ExecutablePath)
 $sourceCreated=[LumaPointerLifecycleProbe]::ReadCreationFileTime($source.ProcessId,$source.ExecutablePath)
 $probe=[LumaPointerLifecycleProbe]::new($engine.ProcessId,$engine.ExecutablePath,$engineCreated,$worker.ProcessId,$worker.ExecutablePath,$workerCreated,$source.ProcessId,$source.ExecutablePath,$sourceCreated)
 $report.Engine=$probe.Engine;$report.Worker=$probe.Worker;$report.Source=$probe.Source
 function Invoke-PointerMove([string]$phase,[int]$target){
  $probe.ValidateBeforeHelper()
  if((Get-FileHash -LiteralPath $HelperPath -Algorithm SHA256).Hash -ne $report.Helper.SHA256){throw 'Pointer helper changed during lifecycle script'}
  $helperOutput=Join-Path $evidence ('lifecycle-'+$phase+'-pointer.json')
  $helperStdout=Join-Path $evidence ('lifecycle-'+$phase+'-stdout.txt');$helperStderr=Join-Path $evidence ('lifecycle-'+$phase+'-stderr.txt')
  & $HelperPath -pid $source.ProcessId -exe $source.ExecutablePath -worker-pid $worker.ProcessId -worker-exe $worker.ExecutablePath -action move -target $target -projection visible -curvature $Curvature -output $helperOutput 1> $helperStdout 2> $helperStderr
  $exitCode=$LASTEXITCODE
  $item=[ordered]@{Phase=$phase;Target=$target;ExitCode=$exitCode;JSON=$helperOutput;Stdout=$helperStdout;Stderr=$helperStderr;Result=$null}
  if(Test-Path -LiteralPath $helperOutput){$item.Result=Get-Content -LiteralPath $helperOutput -Raw -Encoding UTF8|ConvertFrom-Json}
  $report.HelperResults+=@($item)
  if($exitCode -ne 0){throw "Pointer input helper failed during $phase"}
  $probe.ValidateBeforeHelper()
  if($null -eq $item.Result -or $item.Result.source_identity.hwnd -ne $probe.Source.HWND -or $item.Result.source_identity.created_filetime.ToString() -ne $probe.Source.CreatedFileTime -or $item.Result.worker_identity.hwnd -ne $probe.Worker.HWND -or $item.Result.worker_identity.created_filetime.ToString() -ne $probe.Worker.CreatedFileTime){throw 'Pointer helper did not preserve the pinned source/worker identities'}
 }
 Invoke-PointerMove 'baseline' 1
 $report.Before=$probe.Wait('active',0)
 $probe.Minimize();$report.Minimized=$probe.Wait('minimized',0)
 if($report.Minimized.NativeCursor.X -ne $report.Before.NativeCursor.X -or $report.Minimized.NativeCursor.Y -ne $report.Before.NativeCursor.Y){throw 'Minimize moved the real cursor'}
 $probe.Restore();Invoke-PointerMove 'restored' 1;$report.Restored=$probe.Wait('active',$report.Before.Serial)
 if($report.Restored.SourceClient.Left -ne $report.Before.SourceClient.Left -or $report.Restored.SourceClient.Top -ne $report.Before.SourceClient.Top -or $report.Restored.SourceClient.Right -ne $report.Before.SourceClient.Right -or $report.Restored.SourceClient.Bottom -ne $report.Before.SourceClient.Bottom){throw 'Restore changed source client bounds'}
 $probe.Resize();Invoke-PointerMove 'resized' 4;$report.Resized=$probe.Wait('active',$report.Restored.Serial)
 if($report.Resized.SourceWindow.Left -ne $probe.RequestedResize.Left -or $report.Resized.SourceWindow.Top -ne $probe.RequestedResize.Top -or $report.Resized.SourceWindow.Right -ne $probe.RequestedResize.Right -or $report.Resized.SourceWindow.Bottom -ne $probe.RequestedResize.Bottom){throw 'Actual source outer rectangle does not equal the exact requested resize'}
 if($report.Resized.SourceClient.Left -ne $report.Restored.SourceClient.Left -or $report.Resized.SourceClient.Top -ne $report.Restored.SourceClient.Top -or $report.Resized.SourceClient.Right -ne ($report.Restored.SourceClient.Right-96) -or $report.Resized.SourceClient.Bottom -ne ($report.Restored.SourceClient.Bottom-72)){throw 'Source client moved or did not shrink by exactly 96x72 physical pixels'}
 # Source closure is deliberately the last mutating action. Root may launch a new
 # testcard afterwards; this script never reacquires a replacement PID or window.
 $probe.CloseSource();$closing=$true;$report.CloseQueued=$true
 $report.Closed=$probe.Wait('closed',0)
 $report.SourceExited=-not $report.Closed.SourceAlive;$report.EngineAliveAfterClose=$report.Closed.EngineAlive;$report.WorkerAliveAfterClose=$report.Closed.WorkerAlive
 if($report.Closed.NativeCursor.X -ne $report.Resized.NativeCursor.X -or $report.Closed.NativeCursor.Y -ne $report.Resized.NativeCursor.Y){throw 'Source closure moved the real cursor'}
 $report.Passed=$true
} catch {$report.Error=$_.Exception.Message;if($null -ne $probe){$report.LastSnapshot=$probe.LastSnapshot}}
finally {
 if($null -ne $probe){
  try {if(-not $report.Passed -and -not $closing){$report.Cleanup=$probe.RestoreOriginalOnFailure()}}
  catch {$report.CleanupError=$_.Exception.Message}
  finally {$probe.Dispose()}
 }
 $json=$report|ConvertTo-Json -Depth 24
 if(-not [string]::IsNullOrWhiteSpace($Output)){[IO.File]::WriteAllText($Output,$json+[Environment]::NewLine,(New-Object Text.UTF8Encoding($false)))}
}
if(-not $report.Passed){throw ('Pointer lifecycle probe failed: '+$report.Error+' '+$report.CleanupError)}
