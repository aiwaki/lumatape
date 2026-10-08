param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][string]$Output,
 [ValidateSet('Inspect','OK','Cancel','ConfirmUpdate')][string]$Action='Inspect',
 [string]$DialogHWND='',
 [ValidateSet('LumaTape','Добавить эффект LumaTape')][string]$ExpectedTitle='LumaTape',
 [string]$ExpectedText='',
 [string]$UpdateVersion='',
 [uint32]$HostProcessId=0
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# Own-bundle native dialog smoke only. Inspect is read-only. OK is permitted
# only for an observed informational MessageBox with one OK button; confirming
# a two-button operation or opening a file is deliberately unsupported. Cancel
# is an explicit separate action for a LumaTape MessageBox or its file picker.
# ConfirmUpdate is opt-in for an authorized installed-update test, and requires
# the exact offered stable version and exact observed confirmation text.
$report=[ordered]@{SchemaVersion=1;ObservedAt=(Get-Date).ToUniversalTime().ToString('o');Bundle=[IO.Path]::GetFullPath($Bundle).TrimEnd('\');HostPID=$null;HostPath=$null;HostCreatedFileTime=$null;Action=$Action;Dialog=$null;DispatchQueued=$false;DialogClosed=$false;Error=$null;Passed=$false;Scope='PID/path/creation-bound native dialog inspection or WM_COMMAND dismissal. No input injection; no confirmation of product behavior beyond disappearance of this dialog.'}
try {
 if($Action -eq 'ConfirmUpdate'){
  if($UpdateVersion -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'){throw 'ConfirmUpdate requires an exact stable UpdateVersion'}
  $allowed=@("Version $UpdateVersion is available. Install it? The effect will turn off, window changes will be restored, and LumaTape will close.","Доступна версия $UpdateVersion. Установить её? Эффект будет отключён, изменения окна восстановлены, приложение завершится.")
  if($ExpectedTitle -ne 'LumaTape' -or $ExpectedText -cnotin $allowed){throw 'Confirmation text must match the exact offered LumaTape update'}
 }
 if($Action -ne 'Inspect' -and [string]::IsNullOrWhiteSpace($DialogHWND)){throw 'For a dismissal supply the previously observed DialogHWND'}
 if($Action -ne 'Inspect' -and $ExpectedTitle -eq 'LumaTape' -and [string]::IsNullOrWhiteSpace($ExpectedText)){throw 'For a MessageBox dismissal supply its exact observed ExpectedText'}
 if($Action -eq 'OK' -and $ExpectedTitle -ne 'LumaTape'){throw 'OK is only supported for informational LumaTape MessageBoxes'}
 $hostPath=Join-Path $report.Bundle 'lumatape.exe'
 $matches=@(Get-CimInstance Win32_Process -Filter "Name='lumatape.exe'"|Where-Object{$_.ExecutablePath -eq $hostPath})
 if($matches.Count -ne 1){throw 'Expected exactly one host from the requested bundle'}
 $exactHost=$matches[0];$hostIDValue=[uint32]$exactHost.ProcessId
 if($HostProcessId -ne 0 -and $hostIDValue -ne $HostProcessId){throw 'HostProcessId does not match this bundle'}
 $session=[Diagnostics.Process]::GetCurrentProcess().SessionId
 if($session -eq 0 -or $session -ne $exactHost.SessionId){throw 'Run in the same interactive Windows session as the target host'}
 $report.HostPID=$hostIDValue;$report.HostPath=$hostPath
 if(-not ('LumaNativeDialogSmoke' -as [type])){
  Add-Type @'
using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
public sealed class LumaNativeDialogSmoke {
 public delegate bool EnumProc(IntPtr hwnd,IntPtr data);
 [DllImport("user32.dll",SetLastError=true)] static extern bool EnumWindows(EnumProc callback,IntPtr data);
 [DllImport("user32.dll")] static extern bool EnumChildWindows(IntPtr parent,EnumProc callback,IntPtr data);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsWindowEnabled(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool IsWindow(IntPtr hwnd);
 [DllImport("user32.dll")] static extern int GetDlgCtrlID(IntPtr hwnd);
 [DllImport("user32.dll")] static extern IntPtr GetAncestor(IntPtr hwnd,uint flags);
 [DllImport("user32.dll",SetLastError=true)] static extern bool PostMessageW(IntPtr hwnd,uint message,UIntPtr wparam,IntPtr lparam);
 [DllImport("kernel32.dll",SetLastError=true)] static extern IntPtr OpenProcess(uint access,bool inherit,uint pid);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool GetProcessTimes(IntPtr process,out FileTime creation,out FileTime exit,out FileTime kernel,out FileTime user);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool QueryFullProcessImageNameW(IntPtr process,uint flags,StringBuilder path,ref uint size);
 [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
 [StructLayout(LayoutKind.Sequential)] struct FileTime {public uint Low,High;}
 public sealed class Control {public string HWND,Class,Text;public int ID;public bool Visible,Enabled;}
 public sealed class Dialog {public string HWND,Title,StaticText;public uint Thread;public List<Control> Controls;}
 readonly uint pid;readonly string path;readonly long created;readonly string title;readonly IntPtr requested;
 IntPtr observed;uint observedThread;
 public long CreatedFileTime {get{return created;}}
 static string Hex(IntPtr value){return "0x"+value.ToInt64().ToString("x");}
 static IntPtr Parse(string value){if(String.IsNullOrEmpty(value))return IntPtr.Zero;return new IntPtr(Convert.ToInt64(value.StartsWith("0x",StringComparison.OrdinalIgnoreCase)?value.Substring(2):value,16));}
 static string ClassOf(IntPtr hwnd){var value=new StringBuilder(256);GetClassNameW(hwnd,value,value.Capacity);return value.ToString();}
 static string TextOf(IntPtr hwnd){var value=new StringBuilder(32768);if(GetWindowTextW(hwnd,value,value.Capacity)>=32767)throw new Exception("Dialog text exceeds capture bound");return value.ToString();}
 static string Normalize(string value){return value.Replace("\r\n","\n").Trim();}
 long Identity(){
  IntPtr process=OpenProcess(0x1000,false,pid);if(process==IntPtr.Zero)throw new Exception("Cannot open bound process");
  try{uint size=32768;var actual=new StringBuilder((int)size);if(!QueryFullProcessImageNameW(process,0,actual,ref size)||!String.Equals(Path.GetFullPath(actual.ToString()),path,StringComparison.OrdinalIgnoreCase))throw new Exception("Executable identity changed");FileTime start,exit,kernel,user;if(!GetProcessTimes(process,out start,out exit,out kernel,out user))throw new Exception("Cannot read process creation time");return ((long)start.High<<32)|start.Low;}finally{CloseHandle(process);}
 }
 void ValidateProcess(){if(Identity()!=created)throw new Exception("PID was reused; refusing dialog commands");}
 void ValidateDialog(){ValidateProcess();uint actual;uint thread=GetWindowThreadProcessId(observed,out actual);if(actual!=pid||thread!=observedThread||ClassOf(observed)!="#32770"||TextOf(observed)!=title||!IsWindowVisible(observed))throw new Exception("Observed dialog identity changed");}
 public LumaNativeDialogSmoke(uint process,string executable,string expectedTitle,string hwnd){
  if(expectedTitle!="LumaTape"&&expectedTitle!="Добавить эффект LumaTape")throw new Exception("Dialog title is not permitted");pid=process;path=Path.GetFullPath(executable);title=expectedTitle;requested=Parse(hwnd);created=Identity();
 }
 public Dialog Read(){
  ValidateProcess();var matches=new List<IntPtr>();
  EnumProc top=(hwnd,unused)=>{uint actual;GetWindowThreadProcessId(hwnd,out actual);if(actual==pid&&ClassOf(hwnd)=="#32770"&&IsWindowVisible(hwnd)&&TextOf(hwnd)==title&&(requested==IntPtr.Zero||requested==hwnd))matches.Add(hwnd);return true;};
  if(!EnumWindows(top,IntPtr.Zero))throw new Exception("Could not enumerate dialogs");GC.KeepAlive(top);if(matches.Count!=1)throw new Exception("Expected exactly one matching visible native dialog");
  uint ignored;uint currentThread=GetWindowThreadProcessId(matches[0],out ignored);if(observed!=IntPtr.Zero&&(observed!=matches[0]||observedThread!=currentThread))throw new Exception("Dialog changed between observations");observed=matches[0];observedThread=currentThread;
  var controls=new List<Control>();var text=new List<string>();
  EnumProc child=(hwnd,unused)=>{uint actual;GetWindowThreadProcessId(hwnd,out actual);if(actual!=pid)return true;var value=new Control{HWND=Hex(hwnd),Class=ClassOf(hwnd),Text=TextOf(hwnd),ID=GetDlgCtrlID(hwnd),Visible=IsWindowVisible(hwnd),Enabled=IsWindowEnabled(hwnd)};controls.Add(value);if(value.Class=="Static"&&!String.IsNullOrWhiteSpace(value.Text)&&value.Visible)text.Add(value.Text);return true;};
  EnumChildWindows(observed,child,IntPtr.Zero);GC.KeepAlive(child);ValidateDialog();return new Dialog{HWND=Hex(observed),Title=title,StaticText=String.Join("\n",text.ToArray()),Thread=observedThread,Controls=controls};
 }
 public void Dismiss(string action,string expectedText){
  if(action!="OK"&&action!="Cancel"&&action!="ConfirmUpdate")throw new Exception("Only explicit OK or Cancel is supported");Dialog dialog=Read();
  if(title=="LumaTape"&&(String.IsNullOrWhiteSpace(expectedText)||Normalize(dialog.StaticText)!=Normalize(expectedText)))throw new Exception("Observed MessageBox text does not match ExpectedText");
  var visibleButtons=dialog.Controls.FindAll(control=>control.Class=="Button"&&control.Visible);
  var buttons=visibleButtons.FindAll(control=>control.Enabled);
  if(action=="OK"&&(title!="LumaTape"||visibleButtons.Count!=1||buttons.Count!=1))throw new Exception("OK only dismisses a single-button informational MessageBox");
  if(action=="ConfirmUpdate"&&(visibleButtons.Count!=2||buttons.Count!=2||buttons.FindAll(b=>b.ID==1).Count!=1||buttons.FindAll(b=>b.ID==2).Count!=1))throw new Exception("Expected the updater OK/Cancel dialog");
  // Windows' observed MB_OK template can use control ID 2 for its sole OK
  // button. Resolve the actual observed control; never infer Cancel from ID alone.
  var matches=buttons.FindAll(button=>action=="OK"?(button.ID==1||button.ID==2):(action=="ConfirmUpdate"?button.ID==1:button.ID==2));
  if(matches.Count!=1)throw new Exception("Expected exactly one permitted enabled native button");
  var selected=matches[0];int id=selected.ID;string label=selected.Text.Replace("&","").Trim();
  if((action=="OK"||action=="ConfirmUpdate")&&label!="OK"&&label!="ОК")throw new Exception("Informational button does not have an OK caption");
  if(action=="Cancel"&&label!="Cancel"&&label!="Отмена")throw new Exception("IDCANCEL does not have a Cancel caption");
  IntPtr buttonHandle=Parse(selected.HWND);uint buttonPID;uint buttonThread=GetWindowThreadProcessId(buttonHandle,out buttonPID);
  ValidateDialog();if(buttonPID!=pid||buttonThread!=observedThread||GetAncestor(buttonHandle,2)!=observed||GetDlgCtrlID(buttonHandle)!=id)throw new Exception("Button identity changed");
  if(!PostMessageW(observed,0x111,new UIntPtr((uint)id),buttonHandle))throw new Exception("Queue dialog dismissal failed: "+Marshal.GetLastWin32Error());
 }
 public bool WaitClosed(){for(int i=0;i<40;i++){uint actual;uint thread=GetWindowThreadProcessId(observed,out actual);if(!IsWindow(observed)||actual!=pid||thread!=observedThread)return true;Thread.Sleep(50);}return false;}
}
'@
 }
 $probe=[LumaNativeDialogSmoke]::new($hostIDValue,$hostPath,$ExpectedTitle,$DialogHWND)
 $report.HostCreatedFileTime=$probe.CreatedFileTime
 $report.Dialog=$probe.Read()
 if($Action -ne 'Inspect'){
  $probe.Dismiss($Action,$ExpectedText)
  $report.DispatchQueued=$true
  $report.DialogClosed=$probe.WaitClosed()
  if(-not $report.DialogClosed){throw 'Dismissal was queued, but the observed dialog did not disappear within 2 seconds'}
 }
 $report.Passed=($Action -eq 'Inspect' -or ($report.DispatchQueued -and $report.DialogClosed))
} catch {$report.Error=$_.Exception.Message}
$outputPath=[IO.Path]::GetFullPath($Output)
[void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($outputPath))
[IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 12),([Text.UTF8Encoding]::new($false)))
if(-not $report.Passed){throw "Native dialog smoke failed; inspect $outputPath"}
