param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][string]$Output
)
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
# Developer-only, interactive Windows test. Run against an unconfigured portable
# candidate after stopping the live app separately. Never stops an existing host.
# All writes use unique temporary profiles; no effect is enabled or game selected.
# Native messages target observed, pinned own processes only. No physical input.
$bundlePath=[IO.Path]::GetFullPath($Bundle).TrimEnd('\')
$outputPath=[IO.Path]::GetFullPath($Output)
$evidence=Join-Path ([IO.Path]::GetDirectoryName($outputPath)) ('localization-'+[Guid]::NewGuid().ToString('N'))
$profiles=Join-Path ([IO.Path]::GetTempPath()) ('LumaTape-localization-'+[Guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($evidence)
$menuHelper=Join-Path $PSScriptRoot 'windows-tray-menu-smoke.ps1'
$inventoryHelper=Join-Path $PSScriptRoot 'windows-tray-only-smoke.ps1'
$dialogHelper=Join-Path $PSScriptRoot 'windows-dialog-smoke.ps1'
$report=[ordered]@{SchemaVersion=1;StartedAt=[DateTime]::UtcNow.ToString('o');Bundle=$bundlePath;Evidence=$evidence;Profiles=$profiles;Passed=$false;Runs=@();MenuMetadataEqual=$false;NativeIDsEqual=$null;NativeIDsUnique=$false;Error=$null;Scope='Native RU/EN menu and unconfigured-updater message inspection, child testcard caption, isolated profiles and owned cleanup. Not physical input, automatic system-language detection, network telemetry, or installed update qualification.'}
$run=$null;$ownedHost=$null;$ownedChildren=@();$ownDialog=$null
function Pin-Process($process,[string]$path,[string]$role) {
 $null=$process.Handle
 if($process.HasExited -or [LumaLocaleCardProbe]::ImagePath($process.Handle) -ine $path -or $process.SessionId -ne [Diagnostics.Process]::GetCurrentProcess().SessionId){throw "Cannot bind own $role process"}
 $owner=[pscustomobject]@{Role=$role;Process=$process;PID=$process.Id;Path=$path;Created=$process.StartTime.ToFileTimeUtc().ToString()}
 $owner
}
function Assert-Own($owner) {
 $owner.Process.Refresh()
 if($owner.Process.HasExited){throw "Own $($owner.Role) process exited"}
 # Process.Path depends on MainModule, which can be empty before the loader is
 # ready. Query the image from the retained kernel handle instead; never accept
 # a missing or different path, and retain the creation/session checks.
 if([LumaLocaleCardProbe]::ImagePath($owner.Process.Handle) -ine $owner.Path){throw "Own $($owner.Role) executable identity changed"}
 if($owner.Process.SessionId -ne [Diagnostics.Process]::GetCurrentProcess().SessionId -or $owner.Process.StartTime.ToFileTimeUtc().ToString() -ne $owner.Created){throw "Own $($owner.Role) creation/session identity changed"}
}
function Identity($owner) {
 [ordered]@{Role=$owner.Role;PID=$owner.PID;Path=$owner.Path;CreatedFileTime=$owner.Created}
}
function Read-JSON([string]$path) {Get-Content -LiteralPath $path -Raw -Encoding UTF8|ConvertFrom-Json}
function Select-Item($items,[string]$label) {
 $found=@($items|Where-Object{-not $_.Separator -and $_.Label -ceq $label})
 if($found.Count -ne 1){throw "Native menu label absent or ambiguous: $label"}
 $found[0]
}
function Native-Menu([string]$name,[string[]]$path=@()) {
 Assert-Own $script:ownedHost
 $file=Join-Path $script:run.Directory ($name+'.json')
 & $script:menuHelper -Bundle $script:bundlePath -HostProcessId $script:ownedHost.PID -Output $file -MenuPath $path|Out-Null
 $result=Read-JSON $file
 if(-not $result.Passed -or $result.HostCreatedFileTime.ToString() -ne $script:ownedHost.Created){throw 'Tray probe inspected a different host'}
 $result
}
function Bind-Children {
 if($null -eq $script:ownedHost -or $script:ownedHost.Process.HasExited){return}
 Assert-Own $script:ownedHost
 foreach($row in @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$($script:ownedHost.PID)")){
  if($row.ExecutablePath -notin @((Join-Path $script:bundlePath 'engine\lumatape-engine.exe'),(Join-Path $script:bundlePath 'engine\lumatape-testcard.exe'))){continue}
  if(@($script:ownedChildren|Where-Object PID -eq $row.ProcessId).Count){continue}
  $process=Get-Process -Id $row.ProcessId
  if($process.StartTime.ToFileTimeUtc() -lt [long]$script:ownedHost.Created){$process.Dispose();throw 'Child creation predates its owned parent'}
  $role=if($row.Name -eq 'lumatape-testcard.exe'){'testcard'}else{'engine'}
  $child=Pin-Process $process $row.ExecutablePath $role
  $script:ownedChildren+=,$child
  $script:run.Identities+=,(Identity $child)
 }
}
function Menu-Metadata($items,[string]$prefix='') {
 foreach($item in $items){
  $path=$prefix+'/'+$item.Position
  # The dynamic game list is excluded below before this function is called.
  [ordered]@{Position=$path;Enabled=$item.Enabled;Checked=$item.Checked;Separator=$item.Separator;Children=@($item.Children).Count}
  if(@($item.Children).Count){Menu-Metadata $item.Children $path}
 }
}
function Native-CommandIDs($items) {
 foreach($item in $items){
  if(-not $item.Separator -and @($item.Children).Count -eq 0 -and $item.Submenu -eq '0x0' -and $item.ID -gt 0 -and $item.ID -lt 65535){[uint32]$item.ID}
  if(@($item.Children).Count){Native-CommandIDs $item.Children}
 }
}
function Dismiss-OwnDialog {
 if($null -eq $script:ownDialog){return}
 Assert-Own $script:ownedHost
 $file=Join-Path $script:run.Directory 'dialog-dismiss.json'
 $buttons=@($script:ownDialog.Dialog.Controls|Where-Object{$_.Class -eq 'Button' -and $_.Visible -and $_.Enabled})
 $action=if($buttons.Count -eq 1){'OK'}else{'Cancel'}
 & $script:dialogHelper -Bundle $script:bundlePath -HostProcessId $script:ownedHost.PID -Output $file -Action $action -DialogHWND $script:ownDialog.Dialog.HWND -ExpectedText $script:ownDialog.Dialog.StaticText|Out-Null
 $result=Read-JSON $file
 if(-not $result.Passed -or -not $result.DialogClosed -or $result.HostCreatedFileTime.ToString() -ne $script:ownedHost.Created){throw 'Own updater dialog did not close'}
 $script:ownDialog=$null
}
function Close-Owned {
 if($null -eq $script:ownedHost){return}
 $cleanup=[ordered]@{GracefulHost=$false;GracefulChildren=$true;Forced=@();Errors=@()}
 try {
  if(-not $script:ownedHost.Process.HasExited){
   Bind-Children
   if($null -ne $script:ownDialog){Dismiss-OwnDialog}
   foreach($child in @($script:ownedChildren|Where-Object Role -eq 'testcard')){
    if(-not $child.Process.HasExited){Assert-Own $child;[LumaLocaleCardProbe]::Close($child.PID);if(-not $child.Process.WaitForExit(3000)){$cleanup.GracefulChildren=$false}}
   }
   $quit=if($script:run.Language -eq 'ru'){'Выход'}else{'Quit'}
   $null=Native-Menu 'quit' @($quit)
   $cleanup.GracefulHost=$script:ownedHost.Process.WaitForExit(10000)
  } else {$cleanup.GracefulHost=$true}
 } catch {$cleanup.Errors+=,$_.Exception.Message}
 # A hung owned test is a failure, but it must not leave hotkeys or children.
 # Force cleanup is restricted to retained process handles after identity checks.
 foreach($owner in @($script:ownedHost)+$script:ownedChildren){
  try {
   if(-not $owner.Process.HasExited -and -not $owner.Process.WaitForExit(2000)){
    Assert-Own $owner;$owner.Process.Kill();[void]$owner.Process.WaitForExit(2000)
    $cleanup.Forced+=,$owner.Role
   }
  } catch {$cleanup.Errors+=,$_.Exception.Message}
  finally {$owner.Process.Dispose()}
 }
 $script:run.Cleanup=$cleanup
 $script:ownedHost=$null;$script:ownedChildren=@();$script:ownDialog=$null
 if(-not $cleanup.GracefulHost -or -not $cleanup.GracefulChildren -or $cleanup.Forced.Count -or $cleanup.Errors.Count){throw 'Owned-process cleanup did not complete entirely gracefully'}
}
try {
 if([Diagnostics.Process]::GetCurrentProcess().SessionId -eq 0){throw 'Run in an interactive Windows session'}
 if(@(Get-CimInstance Win32_Process|Where-Object{$_.Name -in @('lumatape.exe','lumatape-engine.exe')}).Count){throw 'An existing LumaTape host or engine is running; leave it untouched and stop it separately before this test'}
 $build=Read-JSON (Join-Path $bundlePath 'BUILD-STATUS.json')
 if($build.updater -notlike 'unconfigured*'){throw 'This helper only tests an explicitly unconfigured update-channel build'}
 foreach($path in @($menuHelper,$inventoryHelper,$dialogHelper,(Join-Path $bundlePath 'lumatape.exe'),(Join-Path $bundlePath 'engine\lumatape-testcard.exe'))){if(-not(Test-Path -LiteralPath $path)){throw "Missing required file: $path"}}
 if(-not ('LumaLocaleCardProbe' -as [type])){Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class LumaLocaleCardProbe {
 public delegate bool EnumProc(IntPtr h,IntPtr p);
 [DllImport("user32.dll",SetLastError=true)] static extern bool EnumWindows(EnumProc cb,IntPtr p);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassNameW(IntPtr h,StringBuilder t,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowTextW(IntPtr h,StringBuilder t,int n);
 [DllImport("user32.dll")] static extern IntPtr GetDlgItem(IntPtr h,int id);
 [DllImport("user32.dll")] static extern IntPtr GetParent(IntPtr h);
 [DllImport("user32.dll")] static extern int GetDlgCtrlID(IntPtr h);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll",SetLastError=true)] static extern bool PostMessageW(IntPtr h,uint m,UIntPtr w,IntPtr l);
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool QueryFullProcessImageNameW(IntPtr p,uint flags,StringBuilder path,ref uint size);
 public static string ImagePath(IntPtr process){uint size=32768;var path=new StringBuilder((int)size);if(!QueryFullProcessImageNameW(process,0,path,ref size)||size==0)throw new Exception("Cannot read pinned process image: Win32 "+Marshal.GetLastWin32Error());return path.ToString();}
 public sealed class Card {public string HWND,Title,ButtonHWND,ButtonText;}
 static string Class(IntPtr h){var b=new StringBuilder(256);GetClassNameW(h,b,b.Capacity);return b.ToString();}
 static string Text(IntPtr h){var b=new StringBuilder(4096);if(GetWindowTextW(h,b,b.Capacity)>=4095)throw new Exception("Window text exceeded bound");return b.ToString();}
 static IntPtr Find(uint pid){var found=new List<IntPtr>();EnumProc cb=(h,p)=>{uint actual;GetWindowThreadProcessId(h,out actual);if(actual==pid&&Class(h)=="LumaTape.Native.TestCard"&&IsWindowVisible(h))found.Add(h);return true;};if(!EnumWindows(cb,IntPtr.Zero))throw new Exception("EnumWindows failed");GC.KeepAlive(cb);if(found.Count!=1)throw new Exception("Expected one own visible testcard");return found[0];}
 static void Verify(IntPtr h,uint pid){uint actual;GetWindowThreadProcessId(h,out actual);if(actual!=pid||Class(h)!="LumaTape.Native.TestCard")throw new Exception("Own card identity changed");}
 public static Card Read(uint pid){IntPtr h=Find(pid),b=GetDlgItem(h,1001);uint actual;GetWindowThreadProcessId(b,out actual);if(b==IntPtr.Zero||actual!=pid||GetParent(b)!=h||GetDlgCtrlID(b)!=1001||Class(b)!="Button")throw new Exception("Own fullscreen control identity mismatch");var r=new Card{HWND="0x"+h.ToInt64().ToString("x"),Title=Text(h),ButtonHWND="0x"+b.ToInt64().ToString("x"),ButtonText=Text(b)};Verify(h,pid);return r;}
 public static void Close(uint pid){IntPtr h=Find(pid);Verify(h,pid);if(!PostMessageW(h,0x10,UIntPtr.Zero,IntPtr.Zero))throw new Exception("Could not close own testcard");}
}
'@
 }
 $config=@'
{"version":1,"mode":"overlay","enabled":false,"target":{"kind":"window","monitor":0,"window_title":"__LumaTape isolated locale test__"},"capture":{"transfer":"gpu"},"preset":"Subtle CRT","shader":{"id":"","params":[0,0,0,0,0,0,0,0]},"effects":{"intensity":1,"crt":{"scanlines":0.12,"mask":0.04,"bloom":0.06,"softness":0.08,"vignette":0.1,"curvature":0},"vhs":{"chroma_bleed":0,"noise":0,"jitter":0,"tracking":0},"noise_seed":1,"freeze_noise":false},"screen":{"shape":"flat","corner_radius":0.04,"curvature":0.18,"glass":0.15},"aspect":{"enabled":false,"method":"mask","scale":"fit","source_dar":0},"input_mode":"mouse-exact","hotkeys":{"toggle":"Ctrl+Shift+9","emergency":"Ctrl+Shift+0"}}
'@
 foreach($language in @('ru','en')){
  if(@(Get-CimInstance Win32_Process|Where-Object{$_.Name -in @('lumatape.exe','lumatape-engine.exe')}).Count){throw 'Another host/engine appeared; refusing to launch a second instance'}
  $runDir=Join-Path $evidence $language;[void][IO.Directory]::CreateDirectory($runDir)
  $appData=Join-Path $profiles ($language+'\roaming');$localData=Join-Path $profiles ($language+'\local')
  $cfg=Join-Path $appData 'LumaTape\config.json'
  [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($cfg));[void][IO.Directory]::CreateDirectory($localData)
  [IO.File]::WriteAllText($cfg,$config,[Text.UTF8Encoding]::new($false))
  $run=[ordered]@{Language=$language;Directory=$runDir;Identities=@();Menu=$null;Metadata=@();NativeCommandIDs=@();Dialog=$null;Testcard=$null;ConfigBeforeSHA256=(Get-FileHash -LiteralPath $cfg -Algorithm SHA256).Hash;ConfigAfterSHA256=$null;ProfileUnchanged=$false;Cleanup=$null;Passed=$false}
  $report.Runs+=,$run
  try {
   $start=[Diagnostics.ProcessStartInfo]::new((Join-Path $bundlePath 'lumatape.exe'))
   $start.WorkingDirectory=$bundlePath;$start.UseShellExecute=$false;$start.CreateNoWindow=$true
   $start.EnvironmentVariables['APPDATA']=$appData;$start.EnvironmentVariables['LOCALAPPDATA']=$localData
   $start.EnvironmentVariables['LUMATAPE_NATIVE_UI_TEST']='1';$start.EnvironmentVariables['LUMATAPE_TEST_LANGUAGE']=$language
   $process=[Diagnostics.Process]::Start($start)
   $null=$process.Handle
   $ownedHost=[pscustomobject]@{Role='host';Process=$process;PID=$process.Id;Path=(Join-Path $bundlePath 'lumatape.exe');Created=$process.StartTime.ToFileTimeUtc().ToString()}
   Assert-Own $ownedHost
   $run.Identities+=,(Identity $ownedHost)
   $inventory=$null
   for($attempt=0;$attempt -lt 8;$attempt++){
    Assert-Own $ownedHost;Start-Sleep -Milliseconds 500
    try {
     $inventoryFile=Join-Path $runDir ('inventory-'+$attempt+'.json')
     & $inventoryHelper -Bundle $bundlePath -HostProcessId $ownedHost.PID -Output $inventoryFile|Out-Null
     $inventory=Read-JSON $inventoryFile;if($inventory.Passed){break}
    } catch {if($attempt -eq 7){throw}}
   }
   if(-not $inventory.Passed){throw 'Tray-only startup inspection failed'}
   Bind-Children
   $menu=Native-Menu 'menu'
   $run.Menu=Join-Path $runDir 'menu.json'
   $labels=if($language -eq 'ru'){@{Game='Игра';Effect='Эффект';Settings='Настройки';Tools='Сервис';Distortion='Искажения изображения';Exact='Точные клики';Arbitrary='Разрешить произвольные искажения';Devices='Клавиатура и мышь работают в обоих режимах';Clicks='При произвольных искажениях клики могут смещаться';Updates='Проверить обновления…';Scene='Тестовая сцена';Fullscreen='На весь экран · F11';UpdaterText='Подписанный канал обновлений ещё не настроен. Эта сборка не проверяет обновления в сети.'}}else{@{Game='Game';Effect='Effect';Settings='Settings';Tools='Tools';Distortion='Image distortion';Exact='Accurate clicks';Arbitrary='Allow arbitrary distortion';Devices='Keyboard and mouse work in both modes';Clicks='Arbitrary distortion may misalign clicks';Updates='Check for updates…';Scene='Test scene';Fullscreen='Fullscreen · F11';UpdaterText='The signed update channel is not configured yet. This build does not check for updates online.'}}
   $quitLabel=if($language -eq 'ru'){'Выход'}else{'Quit'}
   $aboutLabel=if($language -eq 'ru'){'О LumaTape'}else{'About LumaTape'}
   $quitItem=Select-Item $menu.Menu $quitLabel
   $versionItem=Select-Item $menu.Menu ('LumaTape '+$build.version)
   if($versionItem.Enabled -or $versionItem.Checked -or $versionItem.Position -ne ($quitItem.Position-1)){throw 'Version is not an inactive row immediately above Quit'}
   $null=Select-Item $menu.Menu $labels.Game
   $effects=Select-Item $menu.Menu $labels.Effect
   foreach($name in @('Subtle CRT','CRT Classic','Soft TV','VHS Light','VHS Tape')){$null=Select-Item $effects.Children $name}
   $settings=Select-Item $menu.Menu $labels.Settings
   $distortion=Select-Item $settings.Children $labels.Distortion
   $exact=Select-Item $distortion.Children $labels.Exact;$arbitrary=Select-Item $distortion.Children $labels.Arbitrary
   if(-not $exact.Checked -or $arbitrary.Checked){throw 'Input-mode checkmarks differ from the isolated profile'}
   foreach($label in @($labels.Devices,$labels.Clicks)){$info=Select-Item $distortion.Children $label;if($info.Enabled -or $info.Checked){throw 'Informational row is actionable or checked'}}
   $tools=Select-Item $menu.Menu $labels.Tools;$null=Select-Item $tools.Children $labels.Updates;$null=Select-Item $tools.Children $aboutLabel
   # Native integer IDs are observed as-is. Dynamic foreign game titles and HWNDs
   # are excluded, since their lifecycle is unrelated to localization.
   $stable=@($menu.Menu|Where-Object{$_.Label -cne $labels.Game})
   $run.Metadata=@(Menu-Metadata $stable)
   $run.NativeCommandIDs=@(Native-CommandIDs $menu.Menu)
   if(@($run.NativeCommandIDs|Select-Object -Unique).Count -ne $run.NativeCommandIDs.Count){throw 'Native menu command IDs are not unique'}
   $null=Native-Menu 'updates-command' @($labels.Tools,$labels.Updates)
   for($attempt=0;$attempt -lt 12;$attempt++){
    try {
     $dialogFile=Join-Path $runDir 'dialog-inspect.json'
     & $dialogHelper -Bundle $bundlePath -HostProcessId $ownedHost.PID -Output $dialogFile|Out-Null
     $ownDialog=Read-JSON $dialogFile
     if($ownDialog.Passed){break}
    } catch {if($attempt -eq 11){throw};Start-Sleep -Milliseconds 150}
   }
   if($ownDialog.HostCreatedFileTime.ToString() -ne $ownedHost.Created){throw 'Dialog probe bound a different process'}
   $run.Dialog=$ownDialog.Dialog
   if($ownDialog.Dialog.StaticText.Trim() -cne $labels.UpdaterText){throw 'Unconfigured updater message is missing or in the wrong language'}
   Dismiss-OwnDialog
   $null=Native-Menu 'testcard-command' @($labels.Tools,$labels.Scene)
   $card=$null
   for($attempt=0;$attempt -lt 20;$attempt++){
    Bind-Children;$matches=@($ownedChildren|Where-Object Role -eq 'testcard')
    if($matches.Count -gt 1){throw 'Multiple owned testcard processes'}
    if($matches.Count -eq 1){$card=$matches[0];try {Assert-Own $card;$run.Testcard=[LumaLocaleCardProbe]::Read($card.PID);break}catch {if($attempt -eq 19){throw}}}
    Start-Sleep -Milliseconds 150
   }
   if($null -eq $run.Testcard -or $run.Testcard.ButtonText -cne $labels.Fullscreen){throw 'Child testcard did not inherit the host language'}
   $run.ConfigAfterSHA256=(Get-FileHash -LiteralPath $cfg -Algorithm SHA256).Hash
   $run.ProfileUnchanged=$run.ConfigAfterSHA256 -ceq $run.ConfigBeforeSHA256
   if(-not $run.ProfileUnchanged){throw 'Read-only locale/menu checks changed the isolated profile'}
   $run.Passed=$true
  } finally {Close-Owned}
 }
 $ru=$report.Runs[0];$en=$report.Runs[1]
 $report.MenuMetadataEqual=($ru.Metadata|ConvertTo-Json -Depth 12 -Compress) -ceq ($en.Metadata|ConvertTo-Json -Depth 12 -Compress)
 if(-not $report.MenuMetadataEqual){throw 'Native menu positions/checkmarks/enabled state differ between RU and EN; inspect both menus'}
 $report.NativeIDsEqual=($ru.NativeCommandIDs|ConvertTo-Json -Compress) -ceq ($en.NativeCommandIDs|ConvertTo-Json -Compress)
 $report.NativeIDsUnique=$true
 $report.Passed=$true
} catch {$report.Error=$_.Exception.Message}
finally {
 if($null -ne $ownedHost){try {Close-Owned}catch{$report.Error=($report.Error+'; cleanup: '+$_.Exception.Message)}}
 $report.CompletedAt=[DateTime]::UtcNow.ToString('o')
 [IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 30),[Text.UTF8Encoding]::new($false))
}
if(-not $report.Passed){throw "Native localization smoke failed; inspect $outputPath"}
