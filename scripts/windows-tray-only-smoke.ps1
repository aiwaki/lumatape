param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][string]$Output,
 [uint32]$HostProcessId=0,
 [uint32]$EngineProcessId=0,
 [switch]$SkipOpenMenuInspection
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# READ ONLY, apart from the requested JSON report. Run in the same interactive
# Windows session as the bundle. This never starts a process, opens a menu,
# sends input, invokes a menu item, or changes the clipboard/configuration.
# A separately launched second instance may be tested by running this probe again.
$report=[ordered]@{
 SchemaVersion=1; ObservedAt=(Get-Date).ToUniversalTime().ToString('o')
 Bundle=[IO.Path]::GetFullPath($Bundle).TrimEnd('\'); ProbePID=$PID
 ProbeSessionId=[Diagnostics.Process]::GetCurrentProcess().SessionId
 HostPID=$null; EnginePID=$null; Host=$null; Engine=$null
 Checks=[ordered]@{HostUnique=$false;EngineUnique=$false;SameInteractiveSession=$false;EnumerationSucceeded=$false;TauriIconOnly=$false;NoVisibleHostAppWindow=$false;NoWebView2Descendant=$false;NoWebView2Window=$false;ProcessIdentityStable=$false;ProcessTreeStable=$false}
 Processes=@(); ProcessesAfter=@(); ExcludedAncestry=@(); Windows=@(); Icons=@(); FrameworkExclusions=@(); VisibleHostWindows=@()
 WebView2Processes=@(); WebView2Windows=@(); AttachedMenus=@(); OpenMenus=@()
 MenuObservation='not_observed'; Errors=@(); Passed=$false
 Scope='Read-only exact-bundle ownership and tray-only inventory. Closed popup-menu contents, menu command behavior, and second-instance behavior are not inferred.'
 FrameworkContract='Exact host top-level class + empty/non-UI title + parentless + WS_VISIBLE|WS_POPUP (optional observed WS_CLIPSIBLINGS) + NOACTIVATE|TRANSPARENT|LAYERED|TOOLWINDOW, no APPWINDOW/caption/child. Pinned tao 0.35.3 and single-instance 2.4.2 intentionally set WS_VISIBLE for internal events without painting these layered helper windows.'
 IconQueryContract='tray-icon 0.24.2: one builder consumes public ID 1 and native ID 2; Go LumaTape.Control uses ID 1. Only exact host/engine windows are queried.'
}

function ProcessEvidence($Process) {
 [pscustomobject]@{PID=[uint32]$Process.ProcessId;ParentPID=[uint32]$Process.ParentProcessId;Name=$Process.Name;Path=$Process.ExecutablePath;CreatedAt=$Process.CreationDate;SessionId=$Process.SessionId}
}

try {
 $inventory=@(Get-CimInstance Win32_Process)
 $hostPath=Join-Path $report.Bundle 'lumatape.exe'
 $enginePath=Join-Path $report.Bundle 'engine\lumatape-engine.exe'
 $hostMatches=@($inventory|Where-Object{$_.Name -eq 'lumatape.exe' -and $_.ExecutablePath -eq $hostPath})
 if($hostMatches.Count -ne 1){throw 'Expected exactly one running host from the requested bundle'}
 $exactHost=$hostMatches[0]
 $hostIDValue=[uint32]$exactHost.ProcessId
 if($HostProcessId -ne 0 -and $HostProcessId -ne $hostIDValue){throw 'The requested HostProcessId does not match this bundle'}
 $report.HostPID=$hostIDValue
 $report.Host=ProcessEvidence $exactHost
 $report.Checks.HostUnique=$true
 $engineMatches=@($inventory|Where-Object{$_.Name -eq 'lumatape-engine.exe' -and $_.ExecutablePath -eq $enginePath -and $_.ParentProcessId -eq $hostIDValue})
 if($engineMatches.Count -ne 1){throw 'Expected exactly one engine from this bundle directly owned by its host'}
 $exactEngine=$engineMatches[0]
 $engineIDValue=[uint32]$exactEngine.ProcessId
 if($EngineProcessId -ne 0 -and $EngineProcessId -ne $engineIDValue){throw 'The requested EngineProcessId does not match this host'}
 if($null -eq $exactHost.CreationDate -or $null -eq $exactEngine.CreationDate -or $exactEngine.CreationDate -lt $exactHost.CreationDate){throw 'Cannot establish host/engine process identity and creation order'}
 $report.EnginePID=$engineIDValue
 $report.Engine=ProcessEvidence $exactEngine
 $report.Checks.EngineUnique=$true
 $report.Checks.SameInteractiveSession=($report.ProbeSessionId -ne 0 -and $exactHost.SessionId -eq $report.ProbeSessionId -and $exactEngine.SessionId -eq $report.ProbeSessionId)
 if(-not $report.Checks.SameInteractiveSession){throw 'Run this probe in the same interactive Windows session as LumaTape; Session 0 evidence is insufficient'}

 # Descendants are followed only from the exact host. Creation times prevent a
 # reused parent PID from attributing an older unrelated process to this bundle.
 $descendants=New-Object 'System.Collections.Generic.List[object]'
 $queue=New-Object 'System.Collections.Generic.Queue[object]'
 $seen=New-Object 'System.Collections.Generic.HashSet[uint32]'
 $queue.Enqueue($exactHost)
 [void]$seen.Add($hostIDValue)
 $ancestryUncertain=$false
 while($queue.Count -gt 0){
  $parentRecord=$queue.Dequeue()
  foreach($childRecord in @($inventory|Where-Object{$_.ParentProcessId -eq $parentRecord.ProcessId})){
   if($seen.Contains([uint32]$childRecord.ProcessId)){continue}
   if($null -eq $childRecord.CreationDate -or $null -eq $parentRecord.CreationDate){
    $ancestryUncertain=$true
    $report.ExcludedAncestry+=@([pscustomobject]@{PID=$childRecord.ProcessId;Reason='missing_creation_time'})
    continue
   }
   if($childRecord.CreationDate -lt $parentRecord.CreationDate){
    $report.ExcludedAncestry+=@([pscustomobject]@{PID=$childRecord.ProcessId;Reason='predates_parent_pid_reuse'})
    continue
   }
   [void]$seen.Add([uint32]$childRecord.ProcessId)
   $descendants.Add($childRecord)
   $queue.Enqueue($childRecord)
  }
 }
 $report.Processes=@($exactHost)+@($descendants.ToArray())|ForEach-Object{ProcessEvidence $_}
 $report.WebView2Processes=@($descendants.ToArray()|Where-Object{$_.Name -match '^msedgewebview2(?:\.exe)?$'}|ForEach-Object{ProcessEvidence $_})
 $report.Checks.NoWebView2Descendant=($report.WebView2Processes.Count -eq 0 -and -not $ancestryUncertain)

 if(-not ('LumaTrayOnlyProbe' -as [type])){
  Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class LumaTrayOnlyProbe {
 public delegate bool EnumProc(IntPtr hwnd,IntPtr parameter);
 [DllImport("user32.dll",SetLastError=true)] static extern bool EnumWindows(EnumProc callback,IntPtr parameter);
 [DllImport("user32.dll")] static extern bool EnumChildWindows(IntPtr parent,EnumProc callback,IntPtr parameter);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern IntPtr FindWindowExW(IntPtr parent,IntPtr after,string cls,string name);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd,out uint pid);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr hwnd,StringBuilder text,int max);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")] static extern IntPtr GetParent(IntPtr hwnd);
 [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr hwnd,out Rect rect);
 [DllImport("user32.dll")] static extern IntPtr GetMenu(IntPtr hwnd);
 [DllImport("user32.dll",EntryPoint="GetWindowLongW")] static extern int GetWindowLong(IntPtr hwnd,int index);
 [DllImport("user32.dll")] static extern int GetDlgCtrlID(IntPtr hwnd);
 [DllImport("user32.dll")] static extern int GetMenuItemCount(IntPtr menu);
 [DllImport("user32.dll")] static extern uint GetMenuItemID(IntPtr menu,int position);
 [DllImport("user32.dll")] static extern uint GetMenuState(IntPtr menu,uint item,uint flags);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetMenuStringW(IntPtr menu,uint item,StringBuilder text,int max,uint flags);
 [DllImport("user32.dll")] static extern IntPtr GetSubMenu(IntPtr menu,int position);
 [DllImport("shell32.dll")] static extern int Shell_NotifyIconGetRect(ref IconID id,out Rect rect);
 [DllImport("dwmapi.dll")] static extern int DwmGetWindowAttribute(IntPtr hwnd,uint attribute,out uint value,uint bytes);
 [StructLayout(LayoutKind.Sequential)] struct IconID { public uint Size;public IntPtr Window;public uint ID;public Guid Guid; }
 [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left,Top,Right,Bottom; }
 public class Window {public uint PID,RootPID,Style,ExStyle;public int ControlID;public bool FrameworkHelper;public string FrameworkReason;public string HWND,Parent,Class,Title,Tree;public bool Visible,Cloaked,RectAvailable;public int CloakQueryResult;public Rect Bounds;public string AttachedMenu;}
 public class Icon {public uint PID,NativeID;public string HWND,Class;public bool Registered;public int QueryResult;public Rect Bounds;}
 public class MenuItem {public int Position;public uint ID,State;public string Text;public bool Separator,Enabled,Checked;public List<MenuItem> Children;}
 public class Menu {public string HWND,HMENU;public List<MenuItem> Items;public string Error;}
 public class Result {public List<Window> Windows=new List<Window>();public List<Icon> Icons=new List<Icon>();public List<Menu> AttachedMenus=new List<Menu>();}
 static string Hex(IntPtr value){return "0x"+value.ToInt64().ToString("x");}
 static bool IsTarget(uint pid,uint host,uint engine){return pid==host||pid==engine;}
 static List<MenuItem> ReadMenu(IntPtr menu,int depth){
  if(depth>8)throw new Exception("Menu nesting exceeded 8 levels");
  int count=GetMenuItemCount(menu);
  if(count<0||count>256)throw new Exception("Cannot read menu count or menu exceeded 256 items");
  var items=new List<MenuItem>();
  for(int i=0;i<count;i++){
   var text=new StringBuilder(2048);GetMenuStringW(menu,(uint)i,text,text.Capacity,0x400);
   uint state=GetMenuState(menu,(uint)i,0x400);IntPtr child=GetSubMenu(menu,i);
   items.Add(new MenuItem{Position=i,ID=GetMenuItemID(menu,i),State=state,Text=text.ToString(),Separator=(state&0x800)!=0,Enabled=(state&3)==0,Checked=(state&8)!=0,Children=child==IntPtr.Zero?new List<MenuItem>():ReadMenu(child,depth+1)});
  }
  return items;
 }
 static void Record(IntPtr hwnd,uint rootPID,string tree,uint host,uint engine,Result result,HashSet<IntPtr> seen){
  if(!seen.Add(hwnd))return;
  uint pid;GetWindowThreadProcessId(hwnd,out pid);
  var cls=new StringBuilder(256);GetClassNameW(hwnd,cls,cls.Capacity);
  var title=new StringBuilder(2048);GetWindowTextW(hwnd,title,title.Capacity);
  Rect rect;bool hasRect=GetWindowRect(hwnd,out rect);uint cloaked=0;int cloakResult=DwmGetWindowAttribute(hwnd,14,out cloaked,4);
  uint style=unchecked((uint)GetWindowLong(hwnd,-16));uint exStyle=unchecked((uint)GetWindowLong(hwnd,-20));
  bool isChild=(style&0x40000000)!=0;IntPtr parent=GetParent(hwnd);
  // GetMenu is not an HMENU query for child controls: their ID can be returned.
  IntPtr menu=isChild?IntPtr.Zero:GetMenu(hwnd);
  string frameworkReason=null;
  // Windows runtime also reports WS_CLIPSIBLINGS on these exact helpers.
  // Permit only that additional recognized bit; all other style deviations fail.
  bool helperContract=pid==host&&tree=="top-level"&&parent==IntPtr.Zero&&menu==IntPtr.Zero&&(style&~0x04000000u)==0x90000000u&&(exStyle&0x080800a0u)==0x080800a0u&&(exStyle&0x00040001u)==0;
  if(helperContract&&cls.ToString()=="Tao Thread Event Target"&&title.Length==0)frameworkReason="tao 0.35.3 create_event_target_window: exact internal class and non-activating layered tool-window styles; WS_VISIBLE is for event delivery";
  if(helperContract&&cls.ToString()=="com.aiwaki.lumatape-sic"&&title.ToString()=="com.aiwaki.lumatape-siw")frameworkReason="single-instance 2.4.2 create_event_target_window: exact application IPC class/title and non-activating layered tool-window styles";
  result.Windows.Add(new Window{Style=style,ExStyle=exStyle,ControlID=isChild?GetDlgCtrlID(hwnd):0,FrameworkHelper=frameworkReason!=null,FrameworkReason=frameworkReason,PID=pid,RootPID=rootPID,HWND=Hex(hwnd),Parent=Hex(parent),Class=cls.ToString(),Title=title.ToString(),Tree=tree,Visible=IsWindowVisible(hwnd),Cloaked=cloakResult==0&&cloaked!=0,CloakQueryResult=cloakResult,RectAvailable=hasRect,Bounds=rect,AttachedMenu=Hex(menu)});
  if((pid==host&&cls.ToString()=="tray_icon_app")||(pid==engine&&cls.ToString()=="LumaTape.Control")){
   var id=new IconID{Size=(uint)Marshal.SizeOf(typeof(IconID)),Window=hwnd,ID=pid==host?2u:1u,Guid=Guid.Empty};Rect iconRect;
   int hr=Shell_NotifyIconGetRect(ref id,out iconRect);
   result.Icons.Add(new Icon{PID=pid,NativeID=id.ID,HWND=Hex(hwnd),Class=cls.ToString(),Registered=hr==0,QueryResult=hr,Bounds=iconRect});
  }
  if(menu!=IntPtr.Zero&&pid==host){
   var menuResult=new Menu{HWND=Hex(hwnd),HMENU=Hex(menu)};
   try{menuResult.Items=ReadMenu(menu,0);}catch(Exception error){menuResult.Error=error.Message;}
   result.AttachedMenus.Add(menuResult);
  }
 }
 static void RecordTree(IntPtr hwnd,uint pid,string kind,uint host,uint engine,Result result,HashSet<IntPtr> seen){
  Record(hwnd,pid,kind,host,engine,result,seen);
  EnumProc child=(value,unused)=>{Record(value,pid,kind+"/descendant",host,engine,result,seen);return true;};
  EnumChildWindows(hwnd,child,IntPtr.Zero);GC.KeepAlive(child);
 }
 public static Result Read(uint host,uint engine){
  var result=new Result();var seen=new HashSet<IntPtr>();
  EnumProc top=(hwnd,unused)=>{
   uint pid;GetWindowThreadProcessId(hwnd,out pid);
   if(IsTarget(pid,host,engine)){RecordTree(hwnd,pid,"top-level",host,engine,result,seen);}
   else{
    // Also find target-owned child windows embedded in a foreign top-level root.
    // Only PID is read for foreign windows; their titles/content are not collected.
    EnumProc child=(value,ignored)=>{uint childPID;GetWindowThreadProcessId(value,out childPID);if(IsTarget(childPID,host,engine))RecordTree(value,childPID,"foreign-root/owned-child",host,engine,result,seen);return true;};
    EnumChildWindows(hwnd,child,IntPtr.Zero);GC.KeepAlive(child);
   }
   return true;
  };
  if(!EnumWindows(top,IntPtr.Zero))throw new Exception("EnumWindows failed: "+Marshal.GetLastWin32Error());
  GC.KeepAlive(top);
  // EnumWindows excludes message-only windows; enumerate that separate tree too.
  IntPtr after=IntPtr.Zero;var messageSeen=new HashSet<IntPtr>();
  while(true){
   after=FindWindowExW(new IntPtr(-3),after,null,null);if(after==IntPtr.Zero)break;
   if(!messageSeen.Add(after)||messageSeen.Count>32768)throw new Exception("Message-only enumeration did not terminate");
   uint pid;GetWindowThreadProcessId(after,out pid);if(IsTarget(pid,host,engine))RecordTree(after,pid,"message-only",host,engine,result,seen);
  }
  return result;
 }
}
'@
 }
 $native=[LumaTrayOnlyProbe]::Read($hostIDValue,$engineIDValue)
 $report.Windows=@($native.Windows.ToArray())
 $report.Icons=@($native.Icons.ToArray())
 $report.AttachedMenus=@($native.AttachedMenus.ToArray())
 $report.Checks.EnumerationSucceeded=$true
 $tauriIcons=@($report.Icons|Where-Object{$_.PID -eq $hostIDValue -and $_.Registered}).Count
 $goIcons=@($report.Icons|Where-Object{$_.PID -eq $engineIDValue -and $_.Registered}).Count
 $report.Checks.TauriIconOnly=($tauriIcons -eq 1 -and $goIcons -eq 0)
 # An already open native popup menu/tooltip is permitted. Native dialogs count
 # as visible host windows: run the baseline probe with dialogs dismissed.
 $report.FrameworkExclusions=@($report.Windows|Where-Object{$_.FrameworkHelper})
 $report.VisibleHostWindows=@($report.Windows|Where-Object{($_.PID -eq $hostIDValue -or $_.RootPID -eq $hostIDValue) -and $_.Visible -and -not $_.Cloaked -and -not $_.FrameworkHelper -and $_.Class -notin @('#32768','tooltips_class32')})
 $report.Checks.NoVisibleHostAppWindow=($report.VisibleHostWindows.Count -eq 0)
 $report.WebView2Windows=@($report.Windows|Where-Object{($_.PID -eq $hostIDValue -or $_.RootPID -eq $hostIDValue) -and $_.Class -match 'Chrome_WidgetWin|Chrome_RenderWidgetHost|WebView'})
 $report.Checks.NoWebView2Window=($report.WebView2Windows.Count -eq 0)

 if(-not $SkipOpenMenuInspection){
  $openMenus=@($report.Windows|Where-Object{$_.PID -eq $hostIDValue -and $_.Visible -and $_.Class -eq '#32768'})
  if($openMenus.Count -gt 0){
   try {
    Add-Type -AssemblyName UIAutomationClient
    Add-Type -AssemblyName UIAutomationTypes
    foreach($menuWindow in $openMenus){
     $menuEvidence=[ordered]@{HWND=$menuWindow.HWND;Items=@();Error=$null}
     try {
      $windowHandle=[IntPtr]::new([Convert]::ToInt64($menuWindow.HWND.Substring(2),16))
      $element=[Windows.Automation.AutomationElement]::FromHandle($windowHandle)
      $items=$element.FindAll([Windows.Automation.TreeScope]::Descendants,[Windows.Automation.Condition]::TrueCondition)
      if($items.Count -gt 512){throw 'Open menu UI Automation tree exceeded 512 elements'}
      for($index=0;$index -lt $items.Count;$index++){
       $current=$items[$index].Current
       $menuEvidence.Items+=@([pscustomobject]@{Name=$current.Name;ControlType=$current.ControlType.ProgrammaticName;Enabled=$current.IsEnabled;Offscreen=$current.IsOffscreen;PID=$current.ProcessId;AutomationId=$current.AutomationId})
      }
     } catch {$menuEvidence.Error=$_.Exception.Message}
     $report.OpenMenus+=@([pscustomobject]$menuEvidence)
    }
   } catch {$report.Errors+=@('Open menu read-only inspection: '+$_.Exception.Message)}
  }
 } else {$report.MenuObservation='skipped'}
 if(@($report.AttachedMenus|Where-Object{$null -eq $_.Error -and $_.Items.Count -gt 0}).Count -gt 0 -or @($report.OpenMenus|Where-Object{$null -eq $_.Error -and $_.Items.Count -gt 0}).Count -gt 0){$report.MenuObservation='observed_read_only_not_invoked'}

 # Check again after enumeration so process exit/restart cannot make an old PID
 # snapshot look like successful evidence about the current bundle.
 $afterInventory=@(Get-CimInstance Win32_Process)
 $hostAfter=$afterInventory|Where-Object{$_.ProcessId -eq $hostIDValue}
 $engineAfter=$afterInventory|Where-Object{$_.ProcessId -eq $engineIDValue}
 $childrenAfter=@($afterInventory|Where-Object{$seen.Contains([uint32]$_.ParentProcessId) -and ($null -eq $_.CreationDate -or $_.CreationDate -ge $exactHost.CreationDate)})
 $report.ProcessesAfter=@($childrenAfter|ForEach-Object{ProcessEvidence $_})
 $beforeKeys=@($descendants.ToArray()|ForEach-Object{[string]$_.ProcessId+'/'+[string]$_.CreationDate.Ticks}|Sort-Object)
 $afterKeys=@($childrenAfter|ForEach-Object{[string]$_.ProcessId+'/'+[string]$_.CreationDate.Ticks}|Sort-Object)
 $report.Checks.ProcessTreeStable=(($beforeKeys -join ';') -eq ($afterKeys -join ';'))
 $report.Checks.ProcessIdentityStable=($null -ne $hostAfter -and $null -ne $engineAfter -and $hostAfter.ExecutablePath -eq $hostPath -and $engineAfter.ExecutablePath -eq $enginePath -and $engineAfter.ParentProcessId -eq $hostIDValue -and $hostAfter.CreationDate -eq $exactHost.CreationDate -and $engineAfter.CreationDate -eq $exactEngine.CreationDate)
} catch {
 $report.Errors+=@($_.Exception.Message)
}

$report.Passed=(@($report.Checks.Values|Where-Object{$_ -ne $true}).Count -eq 0)
$outputPath=[IO.Path]::GetFullPath($Output)
[void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($outputPath))
[IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 20),([Text.UTF8Encoding]::new($false)))
if(-not $report.Passed){throw "Tray-only smoke assertions failed; inspect $outputPath"}
