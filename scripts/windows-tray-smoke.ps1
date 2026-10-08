param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][string]$Output,
 [ValidateSet('go','tauri')][string]$ExpectedOwner='tauri'
)
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
# Read-only inventory of this exact bundle. Run in the interactive user session;
# Session 0 cannot enumerate another desktop's notification windows.
$bundlePath=[IO.Path]::GetFullPath($Bundle).TrimEnd('\')
$hosts=@(Get-CimInstance Win32_Process -Filter "Name='lumatape.exe'"|Where-Object{$_.ExecutablePath -eq "$bundlePath\lumatape.exe"})
if($hosts.Count -ne 1){throw 'Expected exactly one host from the requested bundle'}
$hostPIDValue=[uint32]$hosts[0].ProcessId
$engines=@(Get-CimInstance Win32_Process -Filter "Name='lumatape-engine.exe'"|Where-Object{$_.ParentProcessId -eq $hostPIDValue -and $_.ExecutablePath -eq "$bundlePath\engine\lumatape-engine.exe"})
if($engines.Count -ne 1){throw 'Expected exactly one engine owned by the requested host'}
$enginePIDValue=[uint32]$engines[0].ProcessId
Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class LumaTrayProbe {
 public delegate bool EnumProc(IntPtr hwnd,IntPtr parameter);
 [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc callback,IntPtr parameter);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("shell32.dll")] static extern int Shell_NotifyIconGetRect(ref IconID id,out Rect rect);
 [StructLayout(LayoutKind.Sequential)] struct IconID { public uint Size;public IntPtr Window;public uint ID;public Guid Guid; }
 [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left,Top,Right,Bottom; }
 public class Result {public uint PID;public string HWND,Class;public bool WindowVisible,RegisteredIcon;public int IconQueryResult;public Rect Bounds;}
 public static List<Result> Read(uint host,uint engine) {
  var values=new List<Result>();
  EnumProc callback=(hwnd,parameter)=>{
   uint pid;GetWindowThreadProcessId(hwnd,out pid);if(pid!=host&&pid!=engine)return true;
   var name=new StringBuilder(256);GetClassName(hwnd,name,name.Capacity);
   string cls=name.ToString();if(cls!="LumaTape.Control"&&cls!="tray_icon_app")return true;
   // LumaTape.Control uses uID=1. In tray-icon 0.24.2 the builder consumes
   // counter 1 for its default public ID before with_id replaces it; the
   // native icon then receives counter 2. Query only our verified windows.
   var id=new IconID{Size=(uint)Marshal.SizeOf(typeof(IconID)),Window=hwnd,ID=cls=="tray_icon_app"?2u:1u,Guid=Guid.Empty};Rect rect;
   int hr=Shell_NotifyIconGetRect(ref id,out rect);
   values.Add(new Result{PID=pid,HWND="0x"+hwnd.ToInt64().ToString("x"),Class=cls,WindowVisible=IsWindowVisible(hwnd),RegisteredIcon=hr==0,IconQueryResult=hr,Bounds=rect});return true;
  };
  if(!EnumWindows(callback,IntPtr.Zero))throw new Exception("EnumWindows failed");
  GC.KeepAlive(callback);return values;
 }
}
'@
$windows=@([LumaTrayProbe]::Read($hostPIDValue,$enginePIDValue))
$go=@($windows|Where-Object{$_.PID -eq $enginePIDValue -and $_.Class -eq 'LumaTape.Control'})
$tauri=@($windows|Where-Object{$_.PID -eq $hostPIDValue -and $_.Class -eq 'tray_icon_app'})
$goIcons=@($go|Where-Object RegisteredIcon).Count;$tauriIcons=@($tauri|Where-Object RegisteredIcon).Count
$passed=if($ExpectedOwner -eq 'tauri'){$goIcons -eq 0 -and $tauriIcons -eq 1}else{$goIcons -eq 1 -and $tauriIcons -eq 0}
$result=[pscustomobject]@{ObservedAt=(Get-Date).ToUniversalTime().ToString('o');ExpectedOwner=$ExpectedOwner;HostPID=$hostPIDValue;EnginePID=$enginePIDValue;GoIconCount=$goIcons;TauriIconCount=$tauriIcons;Windows=$windows;Passed=$passed;Scope='Read-only Shell_NotifyIconGetRect ownership; not menu click qualification'}
$result|ConvertTo-Json -Depth 5|Set-Content $Output -Encoding UTF8
if(-not $passed){throw 'Tray ownership assertion failed; inspect the saved report'}
