param(
 [Parameter(Mandatory=$true)][string]$Runtime,
 [Parameter(Mandatory=$true)][string]$OutputDirectory,
 [ValidatePattern('^[a-zA-Z0-9_-]+$')][string]$Label='visibility',
 [ValidateRange(1,300)][int]$Seconds=90
)
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
# Read-only developer observer for an explicitly identified LumaTape pointer test.
# Run in its interactive session. No screenshots, injected input or profile changes.
$out=[IO.Path]::GetFullPath($OutputDirectory)
if(-not [IO.Directory]::Exists($out)){throw 'Output directory must exist'}
$x=Get-Content $Runtime -Raw -Encoding UTF8|ConvertFrom-Json
$sourceExe=Join-Path $x.Bundle 'engine\lumatape-testcard.exe';$engineExe=Join-Path $x.Bundle 'engine\lumatape-engine.exe';$workerExe=Join-Path $x.Bundle 'engine\lumatape-watchdog.exe'
$all=@(Get-CimInstance Win32_Process)
$engine=@($all|Where-Object{$_.ParentProcessId -eq $x.HostPID -and $_.ExecutablePath -eq $engineExe});if($engine.Count -ne 1){throw 'Exact engine missing'}
$worker=@($all|Where-Object{$_.ParentProcessId -eq $engine[0].ProcessId -and $_.ExecutablePath -eq $workerExe -and $_.CommandLine -like '*--pointer-stdio*'});if($worker.Count -ne 1){throw 'Exact worker missing'}
$source=Get-Process -Id $x.SourcePID;$parent=Get-Process -Id $engine[0].ProcessId;$child=Get-Process -Id $worker[0].ProcessId
$handles=@($source.Handle,$parent.Handle,$child.Handle)
if($source.Path -ne $sourceExe -or $parent.Path -ne $engineExe -or $child.Path -ne $workerExe){throw 'Bound process path mismatch'}
Add-Type @'
using System;using System.Text;using System.Collections.Generic;using System.Runtime.InteropServices;using System.Threading;
public static class StripeProbe {
 public delegate bool EP(IntPtr h,IntPtr p);
 [StructLayout(LayoutKind.Sequential)] public struct Pt {public int X,Y;}
 [DllImport("user32.dll")] static extern bool EnumWindows(EP cb,IntPtr p);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] static extern bool GetCursorPos(out Pt p);
 [DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr c);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern IntPtr GetPropW(IntPtr h,string n);
 public static IntPtr Find(uint pid,string cls,string title){IntPtr found=IntPtr.Zero;int n=0;EP cb=(h,p)=>{uint owner;GetWindowThreadProcessId(h,out owner);var c=new StringBuilder(200);var t=new StringBuilder(200);GetClassNameW(h,c,200);GetWindowTextW(h,t,200);if(owner==pid&&c.ToString()==cls&&t.ToString()==title){found=h;n++;}return true;};EnumWindows(cb,IntPtr.Zero);GC.KeepAlive(cb);if(n!=1)throw new Exception("Expected exact single window: "+cls);return found;}
 public static object Run(IntPtr source,IntPtr overlay,IntPtr cursor,int seconds,string csv){
 var rows=new StringBuilder("ms,overlay,pointer,foreground,serial,x,y\n");var watch=System.Diagnostics.Stopwatch.StartNew();
 var start=DateTime.UtcNow;int n=0,hidden=0,lostFocus=0;long hiddenStart=-1,longest=0;
 while(watch.Elapsed.TotalSeconds<seconds){var ms=watch.ElapsedMilliseconds;bool v=IsWindowVisible(overlay);bool fg=GetForegroundWindow()==source;Pt p;GetCursorPos(out p);
 rows.Append(ms).Append(',').Append(v?1:0).Append(',').Append(IsWindowVisible(cursor)?1:0).Append(',').Append(fg?1:0).Append(',').Append(GetPropW(cursor,"LumaTape.Pointer.Serial").ToInt64()).Append(',').Append(p.X).Append(',').Append(p.Y).Append('\n');
 n++;if(!v){hidden++;if(hiddenStart<0)hiddenStart=ms;}else if(hiddenStart>=0){longest=Math.Max(longest,ms-hiddenStart);hiddenStart=-1;}if(!fg)lostFocus++;Thread.Sleep(10);
 }if(hiddenStart>=0)longest=Math.Max(longest,watch.ElapsedMilliseconds-hiddenStart);System.IO.File.WriteAllText(csv,rows.ToString());
 return new {StartedAt=start.ToString("o"),EndedAt=DateTime.UtcNow.ToString("o"),Samples=n,Hidden=hidden,LostFocus=lostFocus,LongestHiddenMS=longest};
 }
 public static object Read(IntPtr source,IntPtr overlay,IntPtr cursor){Pt p;if(!GetCursorPos(out p))throw new Exception("Read cursor");return new{Time=DateTime.UtcNow.ToString("o"),Overlay=IsWindowVisible(overlay),Pointer=IsWindowVisible(cursor),Active=GetPropW(cursor,"LumaTape.Pointer.Active").ToInt64()!=0,Hidden=GetPropW(cursor,"LumaTape.Pointer.Hidden").ToInt64()!=0,Serial=GetPropW(cursor,"LumaTape.Pointer.Serial").ToInt64(),Foreground=GetForegroundWindow()==source,X=p.X,Y=p.Y};}
}
'@
$priorDpi=[StripeProbe]::SetThreadDpiAwarenessContext([IntPtr]::new(-4));$phases=@();$failure=$null
try {
 $s=[StripeProbe]::Find($x.SourcePID,'LumaTape.Native.TestCard','LumaTape pointer test');$o=[StripeProbe]::Find($parent.Id,'GLFW30','LumaTape overlay');$c=[StripeProbe]::Find($child.Id,'LumaTape.PointerProjection','LumaTape pointer')
 $result=[StripeProbe]::Run($s,$o,$c,$Seconds,"$out\$Label-samples.csv")
 if($source.HasExited -or $parent.HasExited -or $child.HasExited){throw 'Identity-bound process exited during observation; samples are not a stability pass'}
} catch {$failure=$_.Exception.Message} finally {[void][StripeProbe]::SetThreadDpiAwarenessContext($priorDpi)}
[pscustomobject]@{Error=$failure;HostPID=$x.HostPID;EnginePID=$parent.Id;WorkerPID=$child.Id;SourcePID=$source.Id;Measurement=$result}|ConvertTo-Json -Depth 10|Set-Content "$out\$Label-visibility.json" -Encoding UTF8
if($failure){throw $failure}
