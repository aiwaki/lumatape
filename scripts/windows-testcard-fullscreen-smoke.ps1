param(
 [Parameter(Mandatory=$true)][string]$Exe,
 [Parameter(Mandatory=$true)][string]$Output
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# Run in the interactive Windows desktop. This starts and closes ONLY its own
# testcard processes. It never changes display modes, LumaTape settings or input
# state. Keyboard coverage here is bounded Win32 message delivery, not SendInput
# or delivery from a physical keyboard through Parallels.
$report=[ordered]@{
 SchemaVersion=1;ObservedAt=(Get-Date).ToUniversalTime().ToString('o')
 Exe=$Exe;SHA256=$null;ProbeSessionId=[Diagnostics.Process]::GetCurrentProcess().SessionId
 Scope='Own native testcard HWND: physical monitor/client geometry, borderless styles, exact restore, F11 repeat, Alt+Enter, Escape, native button, startup flag. Keys use PostMessage; physical delivery and cursor doubling are not qualified.'
 Checks=@();Processes=@();Snapshots=@();NativeCalls=@();FailureResponsiveness=@();Cleanup=@();Errors=@();Passed=$false
}
$owned=New-Object 'System.Collections.Generic.List[object]'
function Check([string]$Name,[bool]$OK,[string]$Detail){
 $report.Checks+=@([pscustomobject]@{Name=$Name;Passed=$OK;Detail=$Detail})
 if(-not $OK){throw "$Name`: $Detail"}
}

try {
 $Exe=(Resolve-Path -LiteralPath $Exe).ProviderPath
 $report.Exe=$Exe
 if(-not [IO.File]::Exists($Exe)){throw 'Exe must be an existing executable file'}
 $report.SHA256=(Get-FileHash -LiteralPath $Exe -Algorithm SHA256).Hash.ToLowerInvariant()
 if($report.ProbeSessionId -eq 0){throw 'Run this probe in an interactive Windows session, not Session 0'}
 if(-not ('LumaTestcardFullscreenProbe' -as [type])){
  Add-Type @'
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
public static class LumaTestcardFullscreenProbe {
 public const string ClassName="LumaTape.Native.TestCard";
 [StructLayout(LayoutKind.Sequential)] public struct Point { public int X,Y; }
 [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left,Top,Right,Bottom; }
 [StructLayout(LayoutKind.Sequential)] public struct Placement { public uint Length,Flags,ShowCommand;public Point Minimum,Maximum;public Rect Normal; }
 [StructLayout(LayoutKind.Sequential)] struct MonitorInfo { public uint Size;public Rect Monitor,Work;public uint Flags; }
 public class Snapshot { public string HWND,Monitor;public uint PID,Style,ExStyle,DPI;public bool Visible,Maximized,Minimized,Fullscreen;public Rect Window,ClientScreen,MonitorBounds,WorkBounds;public Placement Placement; }
 public class MessageCall { public bool Completed;public int Win32Error;public uint TimeoutMs;public double ElapsedMs;public string Result; }
 public delegate bool EnumProc(IntPtr hwnd,IntPtr value);
 [DllImport("user32.dll",SetLastError=true)] static extern bool EnumWindows(EnumProc callback,IntPtr value);
 [DllImport("user32.dll")] static extern bool IsWindow(IntPtr hwnd);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr hwnd,StringBuilder name,int size);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetWindowRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetClientRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll",SetLastError=true)] static extern bool ClientToScreen(IntPtr hwnd,ref Point point);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetWindowPlacement(IntPtr hwnd,ref Placement placement);
 [DllImport("user32.dll")] static extern int GetWindowLongW(IntPtr hwnd,int index);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsZoomed(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsIconic(IntPtr hwnd);
 [DllImport("user32.dll")] static extern uint GetDpiForWindow(IntPtr hwnd);
 [DllImport("user32.dll")] static extern IntPtr MonitorFromWindow(IntPtr hwnd,uint flags);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetMonitorInfoW(IntPtr monitor,ref MonitorInfo info);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SendMessageTimeoutW(IntPtr hwnd,uint message,UIntPtr wp,IntPtr lp,uint flags,uint timeout,out UIntPtr result);
 [DllImport("user32.dll",SetLastError=true)] static extern bool PostMessageW(IntPtr hwnd,uint message,UIntPtr wp,IntPtr lp);
 [DllImport("user32.dll")] static extern IntPtr GetDlgItem(IntPtr hwnd,int id);
 [DllImport("user32.dll")] static extern IntPtr GetParent(IntPtr hwnd);
 [DllImport("kernel32.dll")] static extern void SetLastError(uint error);
 static void Require(bool ok,string operation){if(!ok)throw new Win32Exception(Marshal.GetLastWin32Error(),operation);}
 static bool Equal(Rect a,Rect b){return a.Left==b.Left&&a.Top==b.Top&&a.Right==b.Right&&a.Bottom==b.Bottom;}
 public static void Verify(IntPtr hwnd,uint expectedPID){
  Require(IsWindow(hwnd),"HWND no longer exists");uint pid;GetWindowThreadProcessId(hwnd,out pid);
  var name=new StringBuilder(256);GetClassNameW(hwnd,name,name.Capacity);
  if(pid!=expectedPID||name.ToString()!=ClassName)throw new Exception("HWND PID/class identity changed");
 }
 public static IntPtr Find(uint expectedPID){
  var found=new List<IntPtr>();EnumProc callback=(hwnd,value)=>{
   uint pid;GetWindowThreadProcessId(hwnd,out pid);if(pid!=expectedPID)return true;
   var name=new StringBuilder(256);GetClassNameW(hwnd,name,name.Capacity);
   if(name.ToString()==ClassName)found.Add(hwnd);return true;
  };
  Require(EnumWindows(callback,IntPtr.Zero),"EnumWindows");GC.KeepAlive(callback);
  if(found.Count>1)throw new Exception("Own process exposes multiple testcard HWNDs");
  return found.Count==0?IntPtr.Zero:found[0];
 }
 public static Snapshot Read(IntPtr hwnd,uint expectedPID){
  Verify(hwnd,expectedPID);IntPtr previous=SetThreadDpiAwarenessContext(new IntPtr(-4));
  Require(previous!=IntPtr.Zero,"SetThreadDpiAwarenessContext(PMv2)");
  try{
   Rect window,client;Require(GetWindowRect(hwnd,out window),"GetWindowRect");Require(GetClientRect(hwnd,out client),"GetClientRect");
   var top=new Point{X=client.Left,Y=client.Top};var bottom=new Point{X=client.Right,Y=client.Bottom};
   Require(ClientToScreen(hwnd,ref top),"ClientToScreen(top)");Require(ClientToScreen(hwnd,ref bottom),"ClientToScreen(bottom)");
   var clientScreen=new Rect{Left=top.X,Top=top.Y,Right=bottom.X,Bottom=bottom.Y};
   var placement=new Placement{Length=(uint)Marshal.SizeOf(typeof(Placement))};Require(GetWindowPlacement(hwnd,ref placement),"GetWindowPlacement");
   IntPtr monitor=MonitorFromWindow(hwnd,2);Require(monitor!=IntPtr.Zero,"MonitorFromWindow");
   var info=new MonitorInfo{Size=(uint)Marshal.SizeOf(typeof(MonitorInfo))};Require(GetMonitorInfoW(monitor,ref info),"GetMonitorInfoW");
   uint style=unchecked((uint)GetWindowLongW(hwnd,-16)),exStyle=unchecked((uint)GetWindowLongW(hwnd,-20));
   bool visible=IsWindowVisible(hwnd),minimized=IsIconic(hwnd);
   return new Snapshot{HWND="0x"+hwnd.ToInt64().ToString("x"),Monitor="0x"+monitor.ToInt64().ToString("x"),PID=expectedPID,
    Style=style,ExStyle=exStyle,DPI=GetDpiForWindow(hwnd),Visible=visible,Maximized=IsZoomed(hwnd),Minimized=minimized,
    Window=window,ClientScreen=clientScreen,MonitorBounds=info.Monitor,WorkBounds=info.Work,Placement=placement,
    Fullscreen=visible&&!minimized&&(style&0x00c40000u)==0&&(exStyle&8u)==0&&Equal(window,info.Monitor)&&Equal(clientScreen,info.Monitor)};
  }finally{Require(SetThreadDpiAwarenessContext(previous)!=IntPtr.Zero,"Restore thread DPI context");}
 }
 public static bool SameRestored(Snapshot before,Snapshot after){
  return before.Visible==after.Visible&&before.Maximized==after.Maximized&&before.Minimized==after.Minimized&&
   before.Style==after.Style&&before.ExStyle==after.ExStyle&&Equal(before.Window,after.Window)&&Equal(before.ClientScreen,after.ClientScreen)&&
   before.Placement.ShowCommand==after.Placement.ShowCommand&&Equal(before.Placement.Normal,after.Placement.Normal);
 }
 public static void Send(IntPtr hwnd,uint expectedPID,uint message,uint key,long flags){
  Verify(hwnd,expectedPID);UIntPtr result;
  Require(SendMessageTimeoutW(hwnd,message,new UIntPtr(key),new IntPtr(flags),2,2000,out result)!=IntPtr.Zero,"SendMessageTimeout");
 }
 public static void Post(IntPtr hwnd,uint expectedPID,uint message,uint key,long flags){
  Verify(hwnd,expectedPID);
  Require(PostMessageW(hwnd,message,new UIntPtr(key),new IntPtr(flags)),"PostMessage");
 }
 static MessageCall TimedSend(IntPtr hwnd,uint message,uint timeout){
  UIntPtr result;var watch=System.Diagnostics.Stopwatch.StartNew();SetLastError(0);
  IntPtr status=SendMessageTimeoutW(hwnd,message,UIntPtr.Zero,IntPtr.Zero,2,timeout,out result);
  int error=Marshal.GetLastWin32Error();watch.Stop();
  return new MessageCall{Completed=status!=IntPtr.Zero,Win32Error=error,TimeoutMs=timeout,ElapsedMs=watch.Elapsed.TotalMilliseconds,Result="0x"+result.ToUInt64().ToString("x")};
 }
 public static MessageCall Ping(IntPtr hwnd,uint expectedPID){Verify(hwnd,expectedPID);return TimedSend(hwnd,0,500);}
 public static MessageCall ClickFullscreenButton(IntPtr hwnd,uint expectedPID){
  Verify(hwnd,expectedPID);IntPtr button=GetDlgItem(hwnd,1001);uint pid;GetWindowThreadProcessId(button,out pid);
  var name=new StringBuilder(256);GetClassNameW(button,name,name.Capacity);
  if(button==IntPtr.Zero||pid!=expectedPID||GetParent(button)!=hwnd||!String.Equals(name.ToString(),"Button",StringComparison.OrdinalIgnoreCase))throw new Exception("Own native fullscreen button identity mismatch");
  // Synchronous BN_CLICKED also performs the style/placement transition. The
  // first VM run returned failure with the original 2-second restore budget.
  // Keep a bound and record duration/error separately from actual geometry.
  return TimedSend(button,0xf5,5000);
 }
}
'@
 }
 function Assert-Process($Owner){
  $Owner.Process.Refresh()
  if($Owner.Process.HasExited){throw "Owned testcard exited: $($Owner.ID)"}
  if($Owner.Process.StartTime.ToUniversalTime().ToFileTimeUtc() -ne $Owner.Created){throw 'Owned process creation time changed'}
  if(-not [string]::Equals($Owner.Process.MainModule.FileName,$Exe,[StringComparison]::OrdinalIgnoreCase)){throw 'Owned process executable path changed'}
  if($Owner.Process.SessionId -ne $report.ProbeSessionId){throw 'Owned process is outside the interactive probe session'}
 }
 function Snapshot($Owner,[string]$Name){
  Assert-Process $Owner
  $value=[LumaTestcardFullscreenProbe]::Read($Owner.Window,$Owner.ID)
  $report.Snapshots+=@([pscustomobject]@{Name=$Name;At=(Get-Date).ToUniversalTime().ToString('o');State=$value})
  return $value
 }
 function Wait-State($Owner,[scriptblock]$Predicate,[string]$Name){
  $deadline=[DateTime]::UtcNow.AddSeconds(4)
  $consecutive=0
  do{
   Assert-Process $Owner
   $value=[LumaTestcardFullscreenProbe]::Read($Owner.Window,$Owner.ID)
   if((& $Predicate $value)){$consecutive++}else{$consecutive=0}
   if($consecutive -ge 3){return (Snapshot $Owner $Name)}
   Start-Sleep -Milliseconds 80
  }while([DateTime]::UtcNow -lt $deadline)
  [void](Snapshot $Owner "$Name-timeout")
  throw "Timed out waiting for $Name (physical Win32 state did not match)"
 }
 function Send-Message($Owner,[uint32]$Message,[uint32]$Key,[long]$Flags){
  Assert-Process $Owner
  [LumaTestcardFullscreenProbe]::Send($Owner.Window,$Owner.ID,$Message,$Key,$Flags)
 }
 function Post-KeyMessage($Owner,[uint32]$Message,[uint32]$Key,[long]$Flags){
  Assert-Process $Owner
  [LumaTestcardFullscreenProbe]::Post($Owner.Window,$Owner.ID,$Message,$Key,$Flags)
 }
 function Click-FullscreenButton($Owner,[string]$Name){
  Assert-Process $Owner
  $call=[LumaTestcardFullscreenProbe]::ClickFullscreenButton($Owner.Window,$Owner.ID)
  $report.NativeCalls+=@([pscustomobject]@{Name=$Name;At=(Get-Date).ToUniversalTime().ToString('o');PID=$Owner.ID;Call=$call})
  if(-not $call.Completed){throw "$Name failed: Win32 error $($call.Win32Error), elapsed $($call.ElapsedMs) ms, timeout $($call.TimeoutMs) ms"}
 }
 function Send-Key($Owner,[uint32]$Key,[bool]$System=$false){
  $context=0L;if($System){$context=0x20000000L}
  Post-KeyMessage $Owner $(if($System){0x104}else{0x100}) $Key (1L -bor $context)
  Post-KeyMessage $Owner $(if($System){0x105}else{0x101}) $Key (0xc0000001L -bor $context)
 }
 function Start-Card([bool]$Fullscreen){
  $si=New-Object Diagnostics.ProcessStartInfo
  $si.FileName=$Exe;$si.WorkingDirectory=[IO.Path]::GetDirectoryName($Exe)
  $si.Arguments='--pointer-test --width 960 --height 720';if($Fullscreen){$si.Arguments+=' --fullscreen'}
  # Do not use WindowStyle Hidden: it changes the first ShowWindow in testcard.
  $si.UseShellExecute=$false;$si.CreateNoWindow=$true
  $process=[Diagnostics.Process]::Start($si)
  $handle=$process.Handle # Hold the process identity through cleanup.
  $owner=[pscustomobject]@{Process=$process;ID=[uint32]$process.Id;Created=$process.StartTime.ToUniversalTime().ToFileTimeUtc();Window=[IntPtr]::Zero}
  $owned.Add($owner)
  $report.Processes+=@([pscustomobject]@{PID=$owner.ID;Created=$owner.Created;Arguments=$si.Arguments})
  $deadline=[DateTime]::UtcNow.AddSeconds(8)
  do{
   Assert-Process $owner
   $owner.Window=[LumaTestcardFullscreenProbe]::Find($owner.ID)
   if($owner.Window -ne [IntPtr]::Zero){return $owner}
   Start-Sleep -Milliseconds 80
  }while([DateTime]::UtcNow -lt $deadline)
  throw 'Own testcard did not create its expected native HWND'
 }
 function Close-Card($Owner){
  $result=[ordered]@{PID=$Owner.ID;ClosedNormally=$false;Forced=$false;Error=$null}
  try{
   if($Owner.Process.HasExited){$result.ClosedNormally=$true;return}
   Assert-Process $Owner
   if($Owner.Window -eq [IntPtr]::Zero){$Owner.Window=[LumaTestcardFullscreenProbe]::Find($Owner.ID)}
   if($Owner.Window -ne [IntPtr]::Zero){
    try{Send-Message $Owner 0x10 0 0}catch{$result.Error=$_.Exception.Message;$report.Errors+=@('WM_CLOSE: '+$_.Exception.Message)}
   }
   if($Owner.Process.WaitForExit(3000)){$result.ClosedNormally=$true}
   else{
    # Last resort applies only to the exact newly spawned process, never a PID
    # selected from the user's running testcards.
    Assert-Process $Owner;$Owner.Process.Kill();$result.Forced=$true
    [void]$Owner.Process.WaitForExit(2000)
    $report.Errors+=@("Owned testcard $($Owner.ID) required forced cleanup")
   }
  }catch{
   $result.Error=$_.Exception.Message;$report.Errors+=@('Cleanup: '+$_.Exception.Message)
  }finally{$report.Cleanup+=@([pscustomobject]$result)}
 }

 $normal=Start-Card $false
 $before=Wait-State $normal {param($s) $s.Visible -and -not $s.Fullscreen -and -not $s.Maximized -and -not $s.Minimized -and ($s.Style -band 0x00c40000) -eq 0x00c40000} 'normal-before'
 Check 'Normal startup' $true 'Visible framed window; physical screen coordinates captured'
 Post-KeyMessage $normal 0x100 0x7a 1
 $full=Wait-State $normal {param($s) $s.Fullscreen} 'F11-fullscreen'
 Check 'F11 enters fullscreen' $full.Fullscreen 'Both physical window and client equal nearest rcMonitor; no caption, resize border or topmost'
 # A held key generates repeated WM_KEYDOWN, without an intervening key-up.
 Post-KeyMessage $normal 0x100 0x7a 0x40000002L
 Start-Sleep -Milliseconds 250
 $repeat=Snapshot $normal 'F11-repeat'
 Check 'F11 repeat ignored' $repeat.Fullscreen 'WM_KEYDOWN previous-state bit 30 set, repeat count 2'
 Post-KeyMessage $normal 0x101 0x7a 0xc0000001L
 Send-Key $normal 0x7a
 $restored=Wait-State $normal {param($s) [LumaTestcardFullscreenProbe]::SameRestored($before,$s)} 'normal-restored'
 Check 'F11 restores normal window' ([LumaTestcardFullscreenProbe]::SameRestored($before,$restored)) 'Exact physical window/client, styles, show state and normal placement restored'

 Send-Key $normal 0x0d $true
 [void](Wait-State $normal {param($s) $s.Fullscreen} 'AltEnter-fullscreen')
 Send-Key $normal 0x1b
 $escape=Wait-State $normal {param($s) [LumaTestcardFullscreenProbe]::SameRestored($before,$s)} 'Escape-normal'
 Check 'Alt+Enter enters, Escape restores' ([LumaTestcardFullscreenProbe]::SameRestored($before,$escape)) 'WM_SYSKEYDOWN/UP context bit 29; Escape exits without closing the source'
 Send-Key $normal 0x1b
 Start-Sleep -Milliseconds 250
 $escapeAgain=Snapshot $normal 'Escape-in-normal'
 Check 'Escape in normal is harmless' ([LumaTestcardFullscreenProbe]::SameRestored($before,$escapeAgain)) 'Window remains open and unchanged'

 Click-FullscreenButton $normal 'BM_CLICK normal to fullscreen'
 [void](Wait-State $normal {param($s) $s.Fullscreen} 'button-fullscreen')
 Click-FullscreenButton $normal 'BM_CLICK fullscreen to normal'
 $buttonRestore=Wait-State $normal {param($s) [LumaTestcardFullscreenProbe]::SameRestored($before,$s)} 'button-normal'
 Check 'Native button fullscreen round trip' ([LumaTestcardFullscreenProbe]::SameRestored($before,$buttonRestore)) 'BM_CLICK on exact PID/class/parent control ID 1001 enters and restores; not physical mouse delivery'

 Send-Message $normal 0x112 0xf030 0 # WM_SYSCOMMAND / SC_MAXIMIZE
 $maximized=Wait-State $normal {param($s) $s.Maximized -and -not $s.Fullscreen} 'maximized-before'
 Send-Key $normal 0x7a
 [void](Wait-State $normal {param($s) $s.Fullscreen} 'maximized-fullscreen')
 Send-Key $normal 0x7a
 $maxRestored=Wait-State $normal {param($s) [LumaTestcardFullscreenProbe]::SameRestored($maximized,$s)} 'maximized-restored'
 Check 'Maximized window restored' ([LumaTestcardFullscreenProbe]::SameRestored($maximized,$maxRestored)) 'F11 round trip preserves maximized state and its pre-maximize normal placement'
 Send-Message $normal 0x112 0xf120 0 # SC_RESTORE
 $normalAfterMax=Wait-State $normal {param($s) [LumaTestcardFullscreenProbe]::SameRestored($before,$s)} 'normal-after-maximized'
 Check 'Normal placement survives maximize round trip' ([LumaTestcardFullscreenProbe]::SameRestored($before,$normalAfterMax)) 'SC_RESTORE returns to original physical normal bounds'
 Close-Card $normal

 $startup=Start-Card $true
 $startupFull=Wait-State $startup {param($s) $s.Fullscreen} 'startup-fullscreen'
 Check 'Fullscreen CLI startup' $startupFull.Fullscreen '--fullscreen starts on full nearest monitor with a borderless, non-topmost client'
 Send-Key $startup 0x1b
 $startupNormal=Wait-State $startup {param($s) $s.Visible -and -not $s.Fullscreen -and -not $s.Maximized -and -not $s.Minimized -and ($s.Style -band 0x00c40000) -eq 0x00c40000} 'startup-Escape-normal'
 Check 'Escape restores startup window' ($startupNormal.ClientScreen.Right - $startupNormal.ClientScreen.Left -eq 960 -and $startupNormal.ClientScreen.Bottom - $startupNormal.ClientScreen.Top -eq 720) 'Restored client is the requested 960 x 720 physical pixels'
 Close-Card $startup
}catch{
 $report.Errors+=@($_.Exception.Message)
 # Record the failed call's actual result BEFORE WM_CLOSE can destroy evidence.
 # A timed-out send is not assumed to mean that the operation never happened.
 foreach($owner in $owned){
  if($owner.Process.HasExited -or $owner.Window -eq [IntPtr]::Zero){continue}
  try{
   [void](Snapshot $owner 'failure-before-cleanup')
   Assert-Process $owner
   $ping=[LumaTestcardFullscreenProbe]::Ping($owner.Window,$owner.ID)
   $report.FailureResponsiveness+=@([pscustomobject]@{PID=$owner.ID;WM_NULL=$ping})
  }catch{$report.Errors+=@('Failure-state observation: '+$_.Exception.Message)}
 }
}finally{
 foreach($owner in $owned){
  if(-not $owner.Process.HasExited){Close-Card $owner}
  $owner.Process.Dispose()
 }
 $report.Passed=($report.Errors.Count -eq 0 -and $report.Checks.Count -eq 11 -and @($report.Checks|Where-Object{-not $_.Passed}).Count -eq 0 -and @($report.Cleanup|Where-Object{-not $_.ClosedNormally}).Count -eq 0)
 $parent=[IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($Output))
 [void][IO.Directory]::CreateDirectory($parent)
 [IO.File]::WriteAllText([IO.Path]::GetFullPath($Output),($report|ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false))
}
if(-not $report.Passed){throw "Fullscreen smoke failed; see $Output"}
Write-Output "PASS: $($report.Checks.Count) fullscreen runtime checks; $Output"
