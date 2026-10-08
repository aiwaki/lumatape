param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][string]$Output,
 [string[]]$MenuPath=@(),
 [switch]$KeepOpen,
 [string]$Screenshot='',
 [uint32]$HostProcessId=0
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# Targeted native-message smoke, not physical input qualification. By default it
# only opens, reads and closes this exact bundle's native tray popup. MenuPath
# explicitly authorizes one observed enabled leaf command; KeepOpen only leaves
# the bound popup open for a screenshot. No keyboard/mouse input or movement.
$report=[ordered]@{
 SchemaVersion=1; ObservedAt=(Get-Date).ToUniversalTime().ToString('o')
 Bundle=[IO.Path]::GetFullPath($Bundle).TrimEnd('\'); ProbePID=$PID
 HostPID=$null; HostPath=$null; HostCreatedFileTime=$null; TrayHWND=$null
 PopupHWND=$null; HMENU=$null; OpenedByProbe=$false; MenuObserved=$false
 Menu=@(); RequestedPath=@($MenuPath); SelectedCommand=$null
 DispatchQueued=$false; CommandOutcome='not_requested'; MenuKeptOpen=$false
 Screenshot=$null; ScreenshotBounds=$null;
 CleanupAttempted=$false; MenuClosed=$false; Error=$null; CleanupError=$null
 Passed=$false
 Scope='Native popup inspection and optional queued WM_COMMAND to an observed leaf. Not a physical click, not Mac keyboard delivery, and not confirmation of the command result.'
 PinnedContract='tray-icon 0.24.2: native ID=2, callback=6002, right-button-up=0x205. WM_COMMAND=0x111; MN_GETHMENU=0x1e1. No generic message API is exposed by this script.'
}
$probe=$null

try {
 if($KeepOpen -and $MenuPath.Count -gt 0){throw 'KeepOpen cannot be combined with MenuPath'}
 if(@($MenuPath|Where-Object{[string]::IsNullOrWhiteSpace($_)}).Count -gt 0){throw 'MenuPath cannot contain an empty label'}
 $hostPath=Join-Path $report.Bundle 'lumatape.exe'
 $hosts=@(Get-CimInstance Win32_Process -Filter "Name='lumatape.exe'"|Where-Object{$_.ExecutablePath -eq $hostPath})
 if($hosts.Count -ne 1){throw 'Expected exactly one running host from the requested bundle'}
 $exactHost=$hosts[0]
 $hostIDValue=[uint32]$exactHost.ProcessId
 if($HostProcessId -ne 0 -and $HostProcessId -ne $hostIDValue){throw 'HostProcessId does not match this exact bundle'}
 $probeSession=[Diagnostics.Process]::GetCurrentProcess().SessionId
 if($probeSession -eq 0 -or $exactHost.SessionId -ne $probeSession){throw 'Run this helper in the same interactive Windows session as the target host'}
 $report.HostPID=$hostIDValue
 $report.HostPath=$hostPath
 if(-not ('LumaTrayMenuSmoke' -as [type])){
  Add-Type @'
using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
public sealed class LumaTrayMenuSmoke {
 public delegate bool EnumProc(IntPtr hwnd,IntPtr data);
 [DllImport("user32.dll",SetLastError=true)] static extern bool EnumWindows(EnumProc callback,IntPtr data);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll")] static extern int GetSystemMetrics(int index);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
 [DllImport("user32.dll",SetLastError=true)] static extern bool GetGUIThreadInfo(uint thread,ref GUIThreadInfo info);
 [DllImport("user32.dll",SetLastError=true)] static extern bool PostMessageW(IntPtr hwnd,uint message,UIntPtr wparam,IntPtr lparam);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SendMessageTimeoutW(IntPtr hwnd,uint message,UIntPtr wparam,IntPtr lparam,uint flags,uint timeout,out UIntPtr result);
 [DllImport("user32.dll")] static extern int GetMenuItemCount(IntPtr menu);
 [DllImport("user32.dll")] static extern uint GetMenuItemID(IntPtr menu,int position);
 [DllImport("user32.dll")] static extern uint GetMenuState(IntPtr menu,uint item,uint flags);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetMenuStringW(IntPtr menu,uint item,StringBuilder text,int max,uint flags);
 [DllImport("user32.dll")] static extern IntPtr GetSubMenu(IntPtr menu,int position);
 [DllImport("kernel32.dll",SetLastError=true)] static extern IntPtr OpenProcess(uint access,bool inherit,uint pid);
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool GetProcessTimes(IntPtr process,out FileTime creation,out FileTime exit,out FileTime kernel,out FileTime user);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool QueryFullProcessImageNameW(IntPtr process,uint flags,StringBuilder path,ref uint size);
 [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
 [DllImport("shell32.dll")] static extern int Shell_NotifyIconGetRect(ref IconID id,out Rect rect);
 [StructLayout(LayoutKind.Sequential)] struct FileTime {public uint Low,High;}
 [StructLayout(LayoutKind.Sequential)] struct Rect {public int Left,Top,Right,Bottom;}
 [StructLayout(LayoutKind.Sequential)] struct GUIThreadInfo {public uint Size,Flags;public IntPtr Active,Focus,Capture,MenuOwner,MoveSize,Caret;public Rect CaretRect;}
 [StructLayout(LayoutKind.Sequential)] struct IconID {public uint Size;public IntPtr Window;public uint ID;public Guid Guid;}
 public sealed class MenuItem {public int Position;public uint ID,State;public string Text,Label,Submenu;public bool Separator,Enabled,Checked;public List<MenuItem> Children;}
 public sealed class Snapshot {public string PopupHWND,HMENU;public List<MenuItem> Items;}
 public sealed class CaptureBounds {public string PopupHWND;public int Left,Top,Width,Height;}
 public sealed class Command {public uint ID;public string Label;public string[] Path;}
 readonly uint pid;
 readonly string path;
 readonly long created;
 readonly IntPtr owner;
 readonly uint ownerThread;
 public bool OpenedByProbe {get;private set;}
 public string OwnerHWND {get{return Hex(owner);}}
 public long CreatedFileTime {get{return created;}}
 static string Hex(IntPtr value){return "0x"+value.ToInt64().ToString("x");}
 static Exception NativeError(string action){return new Exception(action+": Win32 "+Marshal.GetLastWin32Error());}
 static string ClassOf(IntPtr hwnd){var text=new StringBuilder(256);GetClassNameW(hwnd,text,text.Capacity);return text.ToString();}
 long ReadIdentity(){
  IntPtr handle=OpenProcess(0x1000,false,pid);if(handle==IntPtr.Zero)throw NativeError("Open target process");
  try{
   uint size=32768;var actualPath=new StringBuilder((int)size);
   if(!QueryFullProcessImageNameW(handle,0,actualPath,ref size))throw NativeError("Read target executable path");
   if(!String.Equals(Path.GetFullPath(actualPath.ToString()),path,StringComparison.OrdinalIgnoreCase))throw new Exception("Target executable path changed");
   FileTime start,exit,kernel,user;if(!GetProcessTimes(handle,out start,out exit,out kernel,out user))throw NativeError("Read target process creation time");
   return ((long)start.High<<32)|start.Low;
  }finally{CloseHandle(handle);}
 }
 void ValidateOwner(){
  if(ReadIdentity()!=created)throw new Exception("Target PID was reused; refusing native messages");
  uint actualPID;uint thread=GetWindowThreadProcessId(owner,out actualPID);
  if(actualPID!=pid||thread!=ownerThread||ClassOf(owner)!="tray_icon_app")throw new Exception("Tray owner identity changed");
 }
 public LumaTrayMenuSmoke(uint process,string executable){
  pid=process;path=Path.GetFullPath(executable);created=ReadIdentity();
  var owners=new List<IntPtr>();
  EnumProc callback=(hwnd,unused)=>{uint actualPID;GetWindowThreadProcessId(hwnd,out actualPID);if(actualPID==pid&&ClassOf(hwnd)=="tray_icon_app")owners.Add(hwnd);return true;};
  if(!EnumWindows(callback,IntPtr.Zero))throw NativeError("Enumerate tray owners");GC.KeepAlive(callback);
  if(owners.Count!=1)throw new Exception("Expected exactly one tray_icon_app window belonging to this PID");
  owner=owners[0];uint verifiedPID;ownerThread=GetWindowThreadProcessId(owner,out verifiedPID);ValidateOwner();
  var icon=new IconID{Size=(uint)Marshal.SizeOf(typeof(IconID)),Window=owner,ID=2,Guid=Guid.Empty};Rect bounds;
  int hr=Shell_NotifyIconGetRect(ref icon,out bounds);if(hr!=0)throw new Exception("Pinned Tauri native icon ID 2 is not registered: HRESULT "+hr.ToString("x8"));
 }
 List<IntPtr> Popups(){
  ValidateOwner();var popups=new List<IntPtr>();bool foreignThread=false;
  EnumProc callback=(hwnd,unused)=>{uint actualPID;uint thread=GetWindowThreadProcessId(hwnd,out actualPID);if(actualPID==pid&&IsWindowVisible(hwnd)&&ClassOf(hwnd)=="#32768"){if(thread!=ownerThread)foreignThread=true;else popups.Add(hwnd);}return true;};
  if(!EnumWindows(callback,IntPtr.Zero))throw NativeError("Enumerate target popup");GC.KeepAlive(callback);
  if(foreignThread)throw new Exception("Another target-host thread has an open menu; refusing to select it");
  return popups;
 }
 IntPtr BoundPopup(){
  var popups=Popups();if(popups.Count==0)return IntPtr.Zero;
  if(popups.Count!=1)throw new Exception("Expected one root popup; close target submenus before probing");
  var info=new GUIThreadInfo{Size=(uint)Marshal.SizeOf(typeof(GUIThreadInfo))};
  if(!GetGUIThreadInfo(ownerThread,ref info))throw NativeError("Read menu ownership");
  if(info.MenuOwner!=owner)throw new Exception("Popup is not owned by the bound tray window");
  return popups[0];
 }
 public void Open(){
  ValidateOwner();if(BoundPopup()!=IntPtr.Zero)return;
  if(!PostMessageW(owner,6002,new UIntPtr(2),new IntPtr(0x205)))throw NativeError("Queue tray popup request");
  OpenedByProbe=true;
  for(int i=0;i<40;i++){Thread.Sleep(50);if(BoundPopup()!=IntPtr.Zero)return;}
  throw new Exception("Target tray popup did not appear within 2 seconds");
 }
 IntPtr MenuHandle(IntPtr popup){
  // MN_GETHMENU only reads the verified popup. Timeout prevents a hung UI thread
  // from blocking the helper. We never send this to another process/window.
  if(BoundPopup()!=popup)throw new Exception("Popup changed before reading its menu");
  UIntPtr result;if(SendMessageTimeoutW(popup,0x1e1,UIntPtr.Zero,IntPtr.Zero,2,1000,out result)==IntPtr.Zero)throw NativeError("Read popup HMENU");
  IntPtr menu=new IntPtr(unchecked((long)result.ToUInt64()));if(menu==IntPtr.Zero)throw new Exception("Popup returned a null HMENU");return menu;
 }
 static string LabelOf(string value){
  int tab=value.IndexOf('\t');if(tab>=0)value=value.Substring(0,tab);
  var result=new StringBuilder();
  for(int i=0;i<value.Length;i++){if(value[i]!='&'){result.Append(value[i]);}else if(i+1<value.Length&&value[i+1]=='&'){result.Append('&');i++;}}
  return result.ToString().Trim();
 }
 List<MenuItem> ReadMenu(IntPtr menu,int depth,bool ancestorsEnabled,HashSet<IntPtr> visited){
  if(depth>8||!visited.Add(menu))throw new Exception("Cyclic or excessive native menu nesting");
  int count=GetMenuItemCount(menu);if(count<0||count>256)throw new Exception("Invalid HMENU or more than 256 menu entries");
  var items=new List<MenuItem>();
  for(int i=0;i<count;i++){
   uint state=GetMenuState(menu,(uint)i,0x400);if(state==UInt32.MaxValue)throw new Exception("Native menu changed while reading item state");
   var text=new StringBuilder(4096);int copied=GetMenuStringW(menu,(uint)i,text,text.Capacity,0x400);if(copied>=text.Capacity-1)throw new Exception("Menu label exceeds 4094 characters");
   IntPtr child=GetSubMenu(menu,i);bool enabled=ancestorsEnabled&&(state&3)==0;
   items.Add(new MenuItem{Position=i,ID=GetMenuItemID(menu,i),State=state,Text=text.ToString(),Label=LabelOf(text.ToString()),Submenu=Hex(child),Separator=(state&0x800)!=0,Enabled=enabled,Checked=(state&8)!=0,Children=child==IntPtr.Zero?new List<MenuItem>():ReadMenu(child,depth+1,enabled,visited)});
  }
  return items;
 }
 public IntPtr BeginScreenshotDpi(){ValidateOwner();IntPtr previous=SetThreadDpiAwarenessContext(new IntPtr(-4));if(previous==IntPtr.Zero)throw NativeError("Set per-monitor screenshot DPI context");return previous;}
 public void EndScreenshotDpi(IntPtr previous){if(previous==IntPtr.Zero||SetThreadDpiAwarenessContext(previous)==IntPtr.Zero)throw NativeError("Restore screenshot DPI context");}
 public CaptureBounds ScreenshotBounds(){
  IntPtr popup=BoundPopup();if(popup==IntPtr.Zero)throw new Exception("Bound popup is not visible for capture");
  Rect rect;if(!GetWindowRect(popup,out rect))throw NativeError("Read popup screenshot bounds");
  long width=(long)rect.Right-rect.Left,height=(long)rect.Bottom-rect.Top;
  if(width<=0||height<=0||width>8192||height>8192||width*height>16777216)throw new Exception("Invalid or excessive popup screenshot area");
  int left=GetSystemMetrics(76),top=GetSystemMetrics(77);long right=(long)left+GetSystemMetrics(78),bottom=(long)top+GetSystemMetrics(79);
  if(rect.Left<left||rect.Top<top||rect.Right>right||rect.Bottom>bottom)throw new Exception("Popup is outside the visible desktop; refusing a partial capture");
  if(BoundPopup()!=popup)throw new Exception("Popup changed while reading capture bounds");
  return new CaptureBounds{PopupHWND=Hex(popup),Left=rect.Left,Top=rect.Top,Width=(int)width,Height=(int)height};
 }
 public void ValidateCapture(CaptureBounds before){
  CaptureBounds after=ScreenshotBounds();
  if(before.PopupHWND!=after.PopupHWND||before.Left!=after.Left||before.Top!=after.Top||before.Width!=after.Width||before.Height!=after.Height)throw new Exception("Popup changed during screenshot; frame rejected");
 }
 public Snapshot Read(){
  IntPtr popup=BoundPopup();if(popup==IntPtr.Zero)throw new Exception("The bound menu is not open");IntPtr menu=MenuHandle(popup);
  var result=new Snapshot{PopupHWND=Hex(popup),HMENU=Hex(menu),Items=ReadMenu(menu,0,true,new HashSet<IntPtr>())};
  if(MenuHandle(popup)!=menu)throw new Exception("Native popup menu changed during inspection");return result;
 }
 static int IDCount(List<MenuItem> items,uint id){int total=0;foreach(var item in items){if(item.Children.Count==0&&!item.Separator&&item.ID==id)total++;total+=IDCount(item.Children,id);}return total;}
 public Command Resolve(string[] labels){
  if(labels==null||labels.Length==0)throw new Exception("An explicit MenuPath is required for dispatch");
  Snapshot snapshot=Read();List<MenuItem> level=snapshot.Items;MenuItem selected=null;
  for(int index=0;index<labels.Length;index++){
   var matches=level.FindAll(item=>!item.Separator&&String.Equals(item.Label,labels[index],StringComparison.Ordinal));
   if(matches.Count!=1)throw new Exception("MenuPath label must match exactly one item: "+labels[index]);
   selected=matches[0];if(!selected.Enabled)throw new Exception("Selected menu item or its parent is disabled: "+selected.Label);
   if(index<labels.Length-1&&selected.Children.Count==0)throw new Exception("MenuPath continues beyond a leaf: "+selected.Label);
   level=selected.Children;
  }
  if(selected.Children.Count!=0||selected.Submenu!="0x0")throw new Exception("MenuPath must end at a leaf command");
  if(selected.ID==0||selected.ID>=65535)throw new Exception("Observed leaf ID cannot be represented safely as a WM_COMMAND menu ID");
  if(IDCount(snapshot.Items,selected.ID)!=1)throw new Exception("Observed leaf ID is not unique in this menu");
  return new Command{ID=selected.ID,Label=selected.Label,Path=(string[])labels.Clone()};
 }
 public void Close(){
  ValidateOwner();if(BoundPopup()==IntPtr.Zero)return;
  UIntPtr unused;if(SendMessageTimeoutW(owner,0x1f,UIntPtr.Zero,IntPtr.Zero,2,1000,out unused)==IntPtr.Zero)throw NativeError("Cancel bound tray popup");
  for(int i=0;i<30;i++){if(BoundPopup()==IntPtr.Zero)return;Thread.Sleep(50);}
  throw new Exception("Bound popup did not close after WM_CANCELMODE");
 }
 public Command Dispatch(string[] labels){
  // Resolve again immediately before cancel/dispatch; never accept an arbitrary
  // numeric ID supplied by a caller or an old report.
  Command command=Resolve(labels);Close();ValidateOwner();
  if(BoundPopup()!=IntPtr.Zero)throw new Exception("A new popup appeared before dispatch");
  if(!PostMessageW(owner,0x111,new UIntPtr(command.ID),IntPtr.Zero))throw NativeError("Queue observed menu command");
  return command;
 }
}
'@
 }
 $probe=[LumaTrayMenuSmoke]::new($hostIDValue,$hostPath)
 $report.HostCreatedFileTime=$probe.CreatedFileTime
 $report.TrayHWND=$probe.OwnerHWND
 $probe.Open()
 $report.OpenedByProbe=$probe.OpenedByProbe
 $snapshot=$probe.Read()
 $report.PopupHWND=$snapshot.PopupHWND
 $report.HMENU=$snapshot.HMENU
 $report.Menu=@($snapshot.Items.ToArray())
 $report.MenuObserved=$true
 if(-not [string]::IsNullOrWhiteSpace($Screenshot)){
  $screenshotPath=[IO.Path]::GetFullPath($Screenshot)
  if([IO.Path]::GetExtension($screenshotPath) -ine '.png'){throw 'Screenshot must be a .png path'}
  Add-Type -AssemblyName System.Drawing
  $bitmap=$null;$graphics=$null;$previousDpi=[IntPtr]::Zero
  try {
   $previousDpi=$probe.BeginScreenshotDpi()
   $bounds=$probe.ScreenshotBounds()
   $bitmap=[Drawing.Bitmap]::new($bounds.Width,$bounds.Height,[Drawing.Imaging.PixelFormat]::Format32bppArgb)
   $graphics=[Drawing.Graphics]::FromImage($bitmap)
   # Copy only the ownership-verified native popup rectangle, never the desktop.
   $graphics.CopyFromScreen($bounds.Left,$bounds.Top,0,0,$bitmap.Size,[Drawing.CopyPixelOperation]::SourceCopy)
   $probe.ValidateCapture($bounds)
   [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($screenshotPath))
   $bitmap.Save($screenshotPath,[Drawing.Imaging.ImageFormat]::Png)
   $report.Screenshot=$screenshotPath
   $report.ScreenshotBounds=$bounds
  } finally {
   if($null -ne $graphics){$graphics.Dispose()}
   if($null -ne $bitmap){$bitmap.Dispose()}
   if($previousDpi -ne [IntPtr]::Zero){$probe.EndScreenshotDpi($previousDpi)}
  }
 }
 if($MenuPath.Count -gt 0){
  $report.SelectedCommand=$probe.Resolve($MenuPath)
  $report.CommandOutcome='not_dispatched'
  $report.CleanupAttempted=$true
  $dispatched=$probe.Dispatch($MenuPath)
  $report.SelectedCommand=$dispatched
  $report.MenuClosed=$true
  $report.DispatchQueued=$true
  $report.CommandOutcome='queued_WM_COMMAND_result_not_verified'
 } elseif($KeepOpen){
  $report.MenuKeptOpen=$true
 } else {
  $report.CleanupAttempted=$true
  $probe.Close()
  $report.MenuClosed=$true
 }
 $report.Passed=$report.MenuObserved -and ($report.MenuKeptOpen -or $report.MenuClosed) -and ($MenuPath.Count -eq 0 -or $report.DispatchQueued)
} catch {
 $report.Error=$_.Exception.Message
 # Never attempt cleanup against a changed PID/window: Close verifies the bound
 # process path, creation time, tray HWND and popup owner again before messaging.
 if($null -ne $probe -and -not $report.DispatchQueued){
  try {$report.CleanupAttempted=$true;$probe.Close();$report.MenuClosed=$true}
  catch {$report.CleanupError=$_.Exception.Message}
 }
}

$outputPath=[IO.Path]::GetFullPath($Output)
[void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($outputPath))
[IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 24),([Text.UTF8Encoding]::new($false)))
if(-not $report.Passed){throw "Native tray menu smoke failed; inspect $outputPath"}
