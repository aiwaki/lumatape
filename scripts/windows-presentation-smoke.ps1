param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][uint32]$SourcePID,
 [Parameter(Mandatory=$true)][string]$SourceExe,
 [Parameter(Mandatory=$true)][string]$Output
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# Read-only geometry of the exact testcard and its bundle's overlay. Caller owns
# source activation and input-profile selection; this script performs neither.
$report=[ordered]@{
 SchemaVersion=1;ObservedAt=(Get-Date).ToUniversalTime().ToString('o')
 Bundle=[IO.Path]::GetFullPath($Bundle).TrimEnd('\');SourceExe=[IO.Path]::GetFullPath($SourceExe)
 Host=$null;Engine=$null;Source=$null;Geometry=$null;Error=$null;Passed=$false
 Checks=[ordered]@{BoundIdentities=$false;SameInteractiveSession=$false;GeometryRead=$false;SourceVisible=$false;OverlayVisible=$false;SourceStable=$false;WindowIdentityStable=$false;SurfaceMatchesSourceClient=$false;ProcessIdentityStable=$false}
 Scope='Physical-pixel native window geometry only. Input profile, game pixels, focus delivery and shader behavior are not inferred.'
}
function Evidence($Process){[pscustomobject]@{PID=[uint32]$Process.ProcessId;ParentPID=[uint32]$Process.ParentProcessId;Path=$Process.ExecutablePath;CreatedAt=$Process.CreationDate;SessionId=$Process.SessionId}}
function SameProcess($Before,$After){$null -ne $After -and $Before.ProcessId -eq $After.ProcessId -and $Before.ExecutablePath -eq $After.ExecutablePath -and $Before.CreationDate -eq $After.CreationDate -and $Before.SessionId -eq $After.SessionId}
try {
 $inventory=@(Get-CimInstance Win32_Process)
 $hostPath=Join-Path $report.Bundle 'lumatape.exe'
 $enginePath=Join-Path $report.Bundle 'engine\lumatape-engine.exe'
 $hosts=@($inventory|Where-Object{$_.ExecutablePath -eq $hostPath})
 if($hosts.Count -ne 1){throw 'Expected exactly one host from the requested bundle'}
 $exactHost=$hosts[0]
 $engines=@($inventory|Where-Object{$_.ExecutablePath -eq $enginePath -and $_.ParentProcessId -eq $exactHost.ProcessId})
 if($engines.Count -ne 1){throw 'Expected exactly one engine owned by this bundle host'}
 $exactEngine=$engines[0]
 $sources=@($inventory|Where-Object{$_.ProcessId -eq $SourcePID -and $_.ExecutablePath -eq $report.SourceExe})
 if($sources.Count -ne 1){throw 'SourcePID does not match SourceExe'}
 $exactSource=$sources[0]
 foreach($record in @($exactHost,$exactEngine,$exactSource)){if($null -eq $record.CreationDate){throw 'Process creation time is unavailable'}}
 if($exactEngine.CreationDate -lt $exactHost.CreationDate){throw 'Engine predates its alleged host; parent PID may have been reused'}
 $report.Host=Evidence $exactHost;$report.Engine=Evidence $exactEngine;$report.Source=Evidence $exactSource
 $report.Checks.BoundIdentities=$true
 $session=[Diagnostics.Process]::GetCurrentProcess().SessionId
 $report.Checks.SameInteractiveSession=($session -ne 0 -and $exactHost.SessionId -eq $session -and $exactEngine.SessionId -eq $session -and $exactSource.SessionId -eq $session)
 if(-not $report.Checks.SameInteractiveSession){throw 'Run the probe in the same interactive Windows session as all three processes'}
 if(-not ('LumaPresentationProbe' -as [type])){
  Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class LumaPresentationProbe {
 public delegate bool EnumProc(IntPtr hwnd,IntPtr data);
 [DllImport("user32.dll",SetLastError=true)] static extern bool EnumWindows(EnumProc callback,IntPtr data);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr hwnd,StringBuilder text,int size);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr hwnd,StringBuilder text,int size);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsIconic(IntPtr hwnd);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetClientRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetWindowRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll",SetLastError=true)] static extern bool ClientToScreen(IntPtr hwnd,ref Point point);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
 [DllImport("user32.dll")] static extern uint GetDpiForWindow(IntPtr hwnd);
 [StructLayout(LayoutKind.Sequential)] public struct Rect {public int Left,Top,Right,Bottom;}
 [StructLayout(LayoutKind.Sequential)] struct Point {public int X,Y;}
 public sealed class Result {public string SourceHWND,OverlayHWND,DpiContext;public uint SourceDpi,OverlayDpi;public Rect SourceClient,SourceClientAfter,OverlaySurface;public bool SourceVisible,SourceMinimized,OverlayVisible,OverlayMinimized,SourceStable,WindowIdentityStable,SurfaceMatchesSourceClient;}
 static string ClassOf(IntPtr hwnd){var value=new StringBuilder(256);GetClassNameW(hwnd,value,value.Capacity);return value.ToString();}
 static string TitleOf(IntPtr hwnd){var value=new StringBuilder(256);GetWindowTextW(hwnd,value,value.Capacity);return value.ToString();}
 static string Hex(IntPtr hwnd){return "0x"+hwnd.ToInt64().ToString("x");}
 static bool Matches(IntPtr hwnd,uint expectedPID,string cls,string title){uint actual;GetWindowThreadProcessId(hwnd,out actual);return actual==expectedPID&&ClassOf(hwnd)==cls&&(title==null||TitleOf(hwnd)==title);}
 static IntPtr Find(uint pid,string cls,string title){
  var matches=new List<IntPtr>();EnumProc callback=(hwnd,unused)=>{if(Matches(hwnd,pid,cls,title))matches.Add(hwnd);return true;};
  if(!EnumWindows(callback,IntPtr.Zero))throw new Exception("EnumWindows failed: "+Marshal.GetLastWin32Error());GC.KeepAlive(callback);
  if(matches.Count!=1)throw new Exception("Expected one "+cls+" window for PID "+pid+", observed "+matches.Count);return matches[0];
 }
 static Rect ClientOnScreen(IntPtr hwnd){
  Rect rect;if(!GetClientRect(hwnd,out rect))throw new Exception("GetClientRect failed: "+Marshal.GetLastWin32Error());
  var topLeft=new Point{X=rect.Left,Y=rect.Top};var bottomRight=new Point{X=rect.Right,Y=rect.Bottom};
  if(!ClientToScreen(hwnd,ref topLeft)||!ClientToScreen(hwnd,ref bottomRight))throw new Exception("ClientToScreen failed: "+Marshal.GetLastWin32Error());
  return new Rect{Left=topLeft.X,Top=topLeft.Y,Right=bottomRight.X,Bottom=bottomRight.Y};
 }
 static bool Equal(Rect a,Rect b){return a.Left==b.Left&&a.Top==b.Top&&a.Right==b.Right&&a.Bottom==b.Bottom;}
 public static Result Read(uint sourcePID,uint enginePID){
  IntPtr previous=SetThreadDpiAwarenessContext(new IntPtr(-4));if(previous==IntPtr.Zero)throw new Exception("Cannot enter Per-Monitor V2 DPI context: "+Marshal.GetLastWin32Error());
  try{
   IntPtr source=Find(sourcePID,"LumaTape.Native.TestCard",null),overlay=Find(enginePID,"GLFW30","LumaTape overlay");
   uint sourceOwner,overlayOwner;uint sourceThread=GetWindowThreadProcessId(source,out sourceOwner),overlayThread=GetWindowThreadProcessId(overlay,out overlayOwner);
   var result=new Result{SourceHWND=Hex(source),OverlayHWND=Hex(overlay),DpiContext="Per-Monitor V2 (temporary probe thread only)",SourceDpi=GetDpiForWindow(source),OverlayDpi=GetDpiForWindow(overlay),SourceClient=ClientOnScreen(source)};
   Rect surface;if(!GetWindowRect(overlay,out surface))throw new Exception("GetWindowRect failed: "+Marshal.GetLastWin32Error());result.OverlaySurface=surface;
   result.SourceVisible=IsWindowVisible(source);result.SourceMinimized=IsIconic(source);result.OverlayVisible=IsWindowVisible(overlay);result.OverlayMinimized=IsIconic(overlay);
   result.SourceClientAfter=ClientOnScreen(source);result.SourceStable=Equal(result.SourceClient,result.SourceClientAfter);
   uint lastSource,lastOverlay;result.WindowIdentityStable=Matches(source,sourcePID,"LumaTape.Native.TestCard",null)&&Matches(overlay,enginePID,"GLFW30","LumaTape overlay")&&GetWindowThreadProcessId(source,out lastSource)==sourceThread&&GetWindowThreadProcessId(overlay,out lastOverlay)==overlayThread;
   result.SurfaceMatchesSourceClient=result.SourceClient.Right>result.SourceClient.Left&&result.SourceClient.Bottom>result.SourceClient.Top&&Equal(result.SourceClient,result.OverlaySurface);
   return result;
  }finally{if(SetThreadDpiAwarenessContext(previous)==IntPtr.Zero)throw new Exception("Cannot restore probe DPI context: "+Marshal.GetLastWin32Error());}
 }
}
'@
 }
 $geometry=[LumaPresentationProbe]::Read($SourcePID,[uint32]$exactEngine.ProcessId)
 $report.Geometry=$geometry;$report.Checks.GeometryRead=$true
 $report.Checks.SourceVisible=($geometry.SourceVisible -and -not $geometry.SourceMinimized)
 $report.Checks.OverlayVisible=($geometry.OverlayVisible -and -not $geometry.OverlayMinimized)
 $report.Checks.SourceStable=$geometry.SourceStable
 $report.Checks.WindowIdentityStable=$geometry.WindowIdentityStable
 $report.Checks.SurfaceMatchesSourceClient=$geometry.SurfaceMatchesSourceClient
 $after=@(Get-CimInstance Win32_Process)
 $hostAfter=$after|Where-Object{$_.ProcessId -eq $exactHost.ProcessId}
 $engineAfter=$after|Where-Object{$_.ProcessId -eq $exactEngine.ProcessId}
 $sourceAfter=$after|Where-Object{$_.ProcessId -eq $exactSource.ProcessId}
 $report.Checks.ProcessIdentityStable=((SameProcess $exactHost $hostAfter) -and (SameProcess $exactEngine $engineAfter) -and (SameProcess $exactSource $sourceAfter) -and $engineAfter.ParentProcessId -eq $exactHost.ProcessId)
} catch {$report.Error=$_.Exception.Message}
$report.Passed=(@($report.Checks.Values|Where-Object{$_ -ne $true}).Count -eq 0)
$outputPath=[IO.Path]::GetFullPath($Output)
[void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($outputPath))
[IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 12),([Text.UTF8Encoding]::new($false)))
if(-not $report.Passed){throw "Presentation smoke failed; inspect $outputPath"}
