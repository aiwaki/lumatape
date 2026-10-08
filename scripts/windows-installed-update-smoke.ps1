param(
 [Parameter(Mandatory=$true)][string]$Bundle,
 [Parameter(Mandatory=$true)][ValidateRange(1,4294967295)][uint32]$HostProcessId,
 [string]$ExpectedOldVersion='0.3.0',
 [string]$NewVersion='0.3.1',
 [Parameter(Mandatory=$true)][string]$Output,
 [Parameter(Mandatory=$true)][string]$ConfigPath,
 # Optional bounded JSON object: the six exact runtime paths mapped to SHA256.
 [string]$ExpectedRuntimeHashes=''
)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
# Explicitly authorized installed N -> N+1 test. This script sends only the
# observed updater menu action, Cancel, then the exact offered update's OK.
# It never starts/kills a source, host or installer and never writes the profile.
# A failed assertion leaves the current application/dialog intact for diagnosis.
$report=[ordered]@{
 SchemaVersion=1;StartedAt=[DateTime]::UtcNow.ToString('o');FinishedAt=$null
 Phase='admission';Passed=$false;Error=$null;Bundle=$null;ConfigPath=$null
 ExpectedOldVersion=$ExpectedOldVersion;NewVersion=$NewVersion
 OldHost=$null;NewHost=$null;OldHostExitCode=$null
 ConfigBefore=$null;ConfigAfterCancel=$null;ConfigAfter=$null
 RegistryBefore=$null;RegistryAfter=$null;BinaryBefore=$null;BinaryAfter=$null
 CancelVerified=$false;ConfirmationQueued=$false;OldHostExited=$false
 SuccessorVerified=$false;TrayOnlyVerified=$false;ConfigBytesUnchanged=$false
 RuntimeHashesRequested=(-not [string]::IsNullOrWhiteSpace($ExpectedRuntimeHashes))
 RuntimeHashesVerified=$false;RuntimeHashes=@();Artifacts=@()
 Scope='Actual native updater Cancel/retry/confirmation, installed successor identity/version, unchanged disabled profile and tray-only inventory. No physical input or rollback qualification. No source-window commands.'
}
$old=$null;$new=$null;$outputPath=$null;$artifactDirectory=$null;$reportWritable=$false
$script:artifactIndex=0;$script:oldIdentity=$null;$script:profileBytes=$null
$runtimeNames=@('lumatape.exe','engine/lumatape-engine.exe','engine/lumatape-watchdog.exe','engine/lumatape-testcard.exe','engine/glfw3.dll','engine/lumatape_capture.dll')
$expectedHashes=@{}

function Save-Checkpoint([string]$Phase){
 $report.Phase=$Phase
 if($outputPath){[IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false))}
}
function Read-BoundedBytes([string]$Path,[long]$Maximum){
 $item=Get-Item -LiteralPath $Path
 if($item.PSIsContainer -or $item.Length -gt $Maximum -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'Expected a bounded regular input file'}
 $bytes=[IO.File]::ReadAllBytes($item.FullName)
 if($bytes.Length -gt $Maximum){throw 'Input grew beyond its byte limit'}
 return ,$bytes
}
function Bytes-Hash([byte[]]$Bytes){
 $hash=[Security.Cryptography.SHA256]::Create()
 try{return ([BitConverter]::ToString($hash.ComputeHash($Bytes))).Replace('-','').ToLowerInvariant()}
 finally{$hash.Dispose()}
}
function Read-ConfigEvidence {
 $bytes=Read-BoundedBytes $report.ConfigPath 1048576
 [pscustomobject]@{SHA256=(Bytes-Hash $bytes);Bytes=$bytes.Length}
}
function Assert-ConfigUnchanged {
 $bytes=Read-BoundedBytes $report.ConfigPath 1048576
 if([Convert]::ToBase64String($bytes) -cne [Convert]::ToBase64String($script:profileBytes)){throw 'Profile bytes changed; no automatic restoration will hide the difference'}
 return [pscustomobject]@{SHA256=(Bytes-Hash $bytes);Bytes=$bytes.Length}
}
function Bind-Host([uint32]$ProcessIdValue){
 $process=Get-Process -Id $ProcessIdValue
 try{
  $null=$process.Handle # Pin the kernel process object, including across PID reuse.
  if($process.HasExited -or $process.Path -ine (Join-Path $report.Bundle 'lumatape.exe')){throw 'Host executable does not match the requested installation'}
  if($process.SessionId -ne [Diagnostics.Process]::GetCurrentProcess().SessionId -or $process.SessionId -eq 0){throw 'Host and probe must share an interactive Windows session'}
  $identity=[pscustomobject]@{PID=$process.Id;Path=$process.Path;CreatedFileTime=$process.StartTime.ToFileTimeUtc().ToString();SessionId=$process.SessionId}
  return [pscustomobject]@{Process=$process;Identity=$identity}
 } catch{$process.Dispose();throw}
}
function Assert-OldHost {
 if($old.HasExited -or $old.Path -ine $script:oldIdentity.Path -or $old.StartTime.ToFileTimeUtc().ToString() -cne $script:oldIdentity.CreatedFileTime){throw 'The bound old host has exited or changed'}
}
function Read-Registration([string]$Version){
 $base=[Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser,[Microsoft.Win32.RegistryView]::Registry64)
 $key=$null;$manufacturer=$null
 try{
  $key=$base.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Uninstall\LumaTape')
  $manufacturer=$base.OpenSubKey('Software\aiwaki\LumaTape')
  if(-not $key -or -not $manufacturer){throw 'The current user has no matching NSIS installation'}
  $values=[ordered]@{}
  foreach($name in @('DisplayName','DisplayVersion','Publisher','InstallLocation','MainBinaryName','UninstallString')){$values[$name]=[string]$key.GetValue($name)}
  $values['ManufacturerPath']=[string]$manufacturer.GetValue('')
  if($values.DisplayName -cne 'LumaTape' -or $values.Publisher -cne 'aiwaki' -or $values.MainBinaryName -cne 'lumatape.exe' -or $values.DisplayVersion -cne $Version){throw 'NSIS product identity or version differs from the expected installation'}
  if($values.InstallLocation.Trim('"').TrimEnd('\') -ine $report.Bundle -or $values.ManufacturerPath.TrimEnd('\') -ine $report.Bundle -or $values.UninstallString -ine ('"'+(Join-Path $report.Bundle 'uninstall.exe')+'"')){throw 'NSIS installation paths do not identify the requested bundle'}
  return [pscustomobject]$values # Never serialize a registry provider or RegistryKey object.
 } finally{if($key){$key.Dispose()};if($manufacturer){$manufacturer.Dispose()};$base.Dispose()}
}
function Read-BinaryVersion([string]$Version){
 $info=[Diagnostics.FileVersionInfo]::GetVersionInfo((Join-Path $report.Bundle 'lumatape.exe'))
 $parts=$Version.Split('.')
 if($info.FileMajorPart -ne [int]$parts[0] -or $info.FileMinorPart -ne [int]$parts[1] -or $info.FileBuildPart -ne [int]$parts[2] -or $info.FilePrivatePart -ne 0 -or $info.ProductMajorPart -ne [int]$parts[0] -or $info.ProductMinorPart -ne [int]$parts[1] -or $info.ProductBuildPart -ne [int]$parts[2] -or $info.ProductPrivatePart -ne 0){throw 'Installed PE file/product version does not match the expected version'}
 return [pscustomobject]@{FileVersion=$info.FileVersion;ProductVersion=$info.ProductVersion;SHA256=(Get-FileHash -LiteralPath $info.FileName -Algorithm SHA256).Hash.ToLowerInvariant()}
}
function Invoke-OwnSmoke([string]$Name,[hashtable]$Arguments,[switch]$AllowFailure){
 $script:artifactIndex++
 if($script:artifactIndex -gt 160){throw 'Bounded smoke observation limit exceeded'}
 $file=Join-Path $artifactDirectory ('{0:d3}-{1}.json' -f $script:artifactIndex,$Name.Replace('windows-','').Replace('.ps1',''))
 $Arguments['Bundle']=$report.Bundle;$Arguments['Output']=$file
 $failure=$null
 try{& (Join-Path $PSScriptRoot $Name) @Arguments | Out-Null}catch{$failure=$_.Exception.Message}
 $observed=$null
 if(Test-Path -LiteralPath $file -PathType Leaf){
  if((Get-Item -LiteralPath $file).Length -gt 4194304){throw 'Child smoke report exceeds its byte limit'}
  $observed=Get-Content -LiteralPath $file -Raw -Encoding UTF8|ConvertFrom-Json
 }
 $report.Artifacts+=,[pscustomobject]@{File=$file;Passed=($null -ne $observed -and $observed.Passed -eq $true);Error=$failure}
 if($failure -and -not $AllowFailure){throw $failure}
 if($null -eq $observed){throw 'Child smoke did not write its report'}
 if(-not $AllowFailure -and -not $observed.Passed){throw 'Child smoke did not pass'}
 return $observed
}
function Assert-OldReport($Snapshot){
 if($Snapshot.HostPID -ne $script:oldIdentity.PID -or $Snapshot.HostCreatedFileTime.ToString() -cne $script:oldIdentity.CreatedFileTime -or $Snapshot.HostPath -ine $script:oldIdentity.Path){throw 'Native report does not belong to the pinned old host'}
}
function Request-Update {
 $watch=[Diagnostics.Stopwatch]::StartNew()
 $allowed=@('Check for updates…','Retry update check…','Проверить обновления…','Повторить проверку обновлений…',"Install version $NewVersion…","Установить версию $NewVersion…")
 while($watch.Elapsed.TotalSeconds -lt 30){
  Assert-OldHost
  $snapshot=Invoke-OwnSmoke 'windows-tray-menu-smoke.ps1' @{HostProcessId=$HostProcessId}
  Assert-OldReport $snapshot
  $tools=@($snapshot.Menu|Where-Object{$_.Label -cin @('Tools','Сервис')})
  if($tools.Count -ne 1){throw 'Expected exactly one native Tools submenu'}
  $actions=@($tools[0].Children|Where-Object{$_.Label -cin $allowed -and $_.Enabled})
  if($actions.Count -gt 1){throw 'The native update action is ambiguous'}
  if($actions.Count -eq 1){
   $dispatch=Invoke-OwnSmoke 'windows-tray-menu-smoke.ps1' @{HostProcessId=$HostProcessId;MenuPath=@($tools[0].Label,$actions[0].Label)}
   Assert-OldReport $dispatch
   if(-not $dispatch.DispatchQueued){throw 'Observed updater action was not queued'}
   return
  }
  # A background check may be busy; no disabled command or numeric ID is sent.
  Start-Sleep -Milliseconds 300
 }
 throw 'No permitted enabled update action appeared within 30 seconds'
}
function Wait-Confirmation {
 $watch=[Diagnostics.Stopwatch]::StartNew()
 $texts=@("Version $NewVersion is available. Install it? The effect will turn off, window changes will be restored, and LumaTape will close.","Доступна версия $NewVersion. Установить её? Эффект будет отключён, изменения окна восстановлены, приложение завершится.")
 while($watch.Elapsed.TotalSeconds -lt 30){
  Assert-OldHost
  $dialog=Invoke-OwnSmoke 'windows-dialog-smoke.ps1' @{HostProcessId=$HostProcessId;Action='Inspect';ExpectedTitle='LumaTape'} -AllowFailure
  if($dialog.Passed){
   Assert-OldReport $dialog
   if($dialog.Dialog.StaticText.Trim() -cnotin $texts){throw 'The observed dialog is not the exact expected update offer; it was left open'}
   return $dialog
  }
  Start-Sleep -Milliseconds 300
 }
 throw 'The exact new-version confirmation did not appear within 30 seconds'
}

try {
 $outputPath=[IO.Path]::GetFullPath($Output)
 $report.Bundle=[IO.Path]::GetFullPath($Bundle).TrimEnd('\')
 $report.ConfigPath=[IO.Path]::GetFullPath($ConfigPath)
 $requiredConfig=[IO.Path]::GetFullPath((Join-Path $env:APPDATA 'LumaTape\config.json'))
 if($report.ConfigPath -ine $requiredConfig -or -not (Test-Path -LiteralPath $requiredConfig -PathType Leaf)){throw 'ConfigPath must be the existing current APPDATA\LumaTape\config.json'}
 if($outputPath -ieq $report.ConfigPath -or $outputPath.StartsWith($report.Bundle+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Reports must not overwrite the profile or installed application'}
 foreach($version in @($ExpectedOldVersion,$NewVersion)){
  if($version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'){throw 'Expected exact stable X.Y.Z versions'}
  foreach($part in $version.Split('.')){if([uint64]$part -gt 65535){throw 'Version exceeds the PE version range'}}
 }
 if([version]$NewVersion -le [version]$ExpectedOldVersion){throw 'NewVersion must be newer than ExpectedOldVersion'}
 $artifactDirectory=$outputPath+'.artifacts'
 if((Test-Path -LiteralPath $outputPath) -or (Test-Path -LiteralPath $artifactDirectory)){throw 'Preserve previous evidence; Output and its artifact directory must be new'}
 [void][IO.Directory]::CreateDirectory($artifactDirectory)
 $reportWritable=$true
 $script:profileBytes=Read-BoundedBytes $report.ConfigPath 1048576
 $config=([Text.Encoding]::UTF8.GetString($script:profileBytes).TrimStart([char]0xfeff))|ConvertFrom-Json
 if($config.enabled -isnot [bool] -or $config.enabled -ne $false -or $config.aspect.enabled -isnot [bool] -or $config.aspect.enabled -ne $false){throw 'This smoke requires filter and format disabled before admission; it will not edit the profile'}
 $report.ConfigBefore=Read-ConfigEvidence
 if($ExpectedRuntimeHashes){
  $hashBytes=Read-BoundedBytes ([IO.Path]::GetFullPath($ExpectedRuntimeHashes)) 16384
  $hashMap=([Text.Encoding]::UTF8.GetString($hashBytes).TrimStart([char]0xfeff))|ConvertFrom-Json
  $properties=@($hashMap.PSObject.Properties)
  if($properties.Count -ne $runtimeNames.Count){throw 'ExpectedRuntimeHashes must contain exactly six runtime paths'}
  foreach($entry in $properties){
   if($entry.Name -cnotin $runtimeNames -or $entry.Value -isnot [string] -or $entry.Value -notmatch '^[a-fA-F0-9]{64}$'){throw 'Unexpected path or SHA256 in ExpectedRuntimeHashes'}
   $expectedHashes[$entry.Name]=$entry.Value.ToLowerInvariant()
  }
 }
 $bound=Bind-Host $HostProcessId;$old=$bound.Process;$script:oldIdentity=$bound.Identity;$report.OldHost=$bound.Identity
 $report.RegistryBefore=Read-Registration $ExpectedOldVersion
 $report.BinaryBefore=Read-BinaryVersion $ExpectedOldVersion
 Save-Checkpoint 'requesting_cancel_offer'
 Request-Update
 $first=Wait-Confirmation
 $cancel=Invoke-OwnSmoke 'windows-dialog-smoke.ps1' @{HostProcessId=$HostProcessId;Action='Cancel';DialogHWND=$first.Dialog.HWND;ExpectedTitle='LumaTape';ExpectedText=$first.Dialog.StaticText}
 Assert-OldReport $cancel
 if(-not $cancel.DispatchQueued -or -not $cancel.DialogClosed){throw 'Cancel did not close the bound offer'}
 Start-Sleep -Milliseconds 500
 Assert-OldHost
 $null=Read-Registration $ExpectedOldVersion;$null=Read-BinaryVersion $ExpectedOldVersion
 $report.ConfigAfterCancel=Assert-ConfigUnchanged
 $report.CancelVerified=$true
 Save-Checkpoint 'requesting_install_offer'
 Request-Update
 $second=Wait-Confirmation
 Assert-OldHost
 $null=Assert-ConfigUnchanged
 $confirmed=Invoke-OwnSmoke 'windows-dialog-smoke.ps1' @{HostProcessId=$HostProcessId;Action='ConfirmUpdate';DialogHWND=$second.Dialog.HWND;ExpectedTitle='LumaTape';ExpectedText=$second.Dialog.StaticText;UpdateVersion=$NewVersion}
 Assert-OldReport $confirmed
 if(-not $confirmed.DispatchQueued -or -not $confirmed.DialogClosed){throw 'The exact update confirmation was not accepted'}
 $report.ConfirmationQueued=$true
 Save-Checkpoint 'waiting_old_exit'
 $watch=[Diagnostics.Stopwatch]::StartNew()
 while(-not $old.WaitForExit(200) -and $watch.Elapsed.TotalSeconds -lt 150){}
 if(-not $old.HasExited){throw 'Old host did not exit within 150 seconds; no process was killed'}
 $report.OldHostExited=$true;$report.OldHostExitCode=$old.ExitCode
 if($old.ExitCode -ne 0){throw 'Old host exited abnormally'}
 Save-Checkpoint 'waiting_installed_successor'
 $watch=[Diagnostics.Stopwatch]::StartNew()
 while($watch.Elapsed.TotalSeconds -lt 90){
  $candidateProcesses=@(Get-CimInstance Win32_Process -Filter "Name='lumatape.exe'"|Where-Object{$_.ExecutablePath -ieq (Join-Path $report.Bundle 'lumatape.exe')})
  if($candidateProcesses.Count -gt 1){throw 'Multiple successor hosts appeared at the installation path'}
  if($candidateProcesses.Count -eq 1){
   $candidate=Bind-Host ([uint32]$candidateProcesses[0].ProcessId)
   if([long]$candidate.Identity.CreatedFileTime -le [long]$script:oldIdentity.CreatedFileTime){$candidate.Process.Dispose();throw 'Successor is not a newly created process'}
   $new=$candidate.Process;$report.NewHost=$candidate.Identity
   break
  }
  Start-Sleep -Milliseconds 500
 }
 if(-not $new){throw 'No installed successor appeared within 90 seconds'}
 $report.RegistryAfter=Read-Registration $NewVersion
 $report.BinaryAfter=Read-BinaryVersion $NewVersion
 $report.SuccessorVerified=$true
 $watch=[Diagnostics.Stopwatch]::StartNew()
 do{
  if($new.HasExited){throw 'Successor exited before tray/engine qualification'}
  $inventory=Invoke-OwnSmoke 'windows-tray-only-smoke.ps1' @{HostProcessId=[uint32]$new.Id;SkipOpenMenuInspection=$true} -AllowFailure
  if($inventory.Passed){break}
  Start-Sleep -Milliseconds 500
 }while($watch.Elapsed.TotalSeconds -lt 20)
 if(-not $inventory.Passed){throw 'Installed successor did not achieve one tray icon, one engine and no WebView'}
 $report.TrayOnlyVerified=$true
 if($expectedHashes.Count){
  foreach($name in $runtimeNames){
   $actual=(Get-FileHash -LiteralPath (Join-Path $report.Bundle $name.Replace('/','\')) -Algorithm SHA256).Hash.ToLowerInvariant()
   $hashMatches=($actual -ceq $expectedHashes[$name])
   $report.RuntimeHashes+=,[pscustomobject]@{Name=$name;Expected=$expectedHashes[$name];Actual=$actual;Matches=$hashMatches}
   if(-not $hashMatches){throw ('Installed runtime hash mismatch: '+$name)}
  }
  $report.RuntimeHashesVerified=$true
 }
 $report.ConfigAfter=Assert-ConfigUnchanged
 $report.ConfigBytesUnchanged=$true
 if($new.HasExited -or $new.StartTime.ToFileTimeUtc().ToString() -cne $report.NewHost.CreatedFileTime){throw 'Successor identity changed during qualification'}
 $report.Passed=$true
} catch{$report.Error=$_.Exception.Message}
finally{
 if($old){$old.Dispose()};if($new){$new.Dispose()}
 $report.FinishedAt=[DateTime]::UtcNow.ToString('o')
 # Admission failures before a safe new output path was created do not overwrite
 # existing evidence, a profile, or application files just to report that failure.
 if($reportWritable){
  Save-Checkpoint $(if($report.Passed){'passed'}else{'failed'})
 }
}
if(-not $report.Passed){throw ('Installed update smoke failed: '+$report.Error)}
