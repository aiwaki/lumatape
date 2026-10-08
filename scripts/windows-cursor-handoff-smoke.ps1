param(
 [Parameter(Mandatory=$true)][string]$RuntimeJson,
 [Parameter(Mandatory=$true)][string]$HelperPath,
 [Parameter(Mandatory=$true)][string]$OutputDirectory
)
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
# Explicit developer-only test of our exact pointer testcard. The makc helper
# owns every injected button/key and releases it in its deferred cleanup.
# No emergency hotkey, arbitrary target, host setting or window geometry change.
$report=[ordered]@{
 SchemaVersion=1;StartedAt=[DateTime]::UtcNow.ToString('o');Passed=$false
 Scope='Guest native input and cursor projection lifecycle. Does not prove visibility of a physical macOS cursor or reproduce a host-to-guest boundary crossing.'
 Identities=@();HelperSHA256=$null;Curvature=$null;Steps=@();Polls=@()
 ConfigBeforeSHA256=$null;ConfigAfterSHA256=$null;ConfigSemanticallyUnchanged=$false;ConfigBytesUnchanged=$false
 Error=$null;CleanupError=$null
}
$bound=@();$configPath=$null;$beforeRaw=$null;$beforeConfig=$null;$ownsDisabled=$false;$ready=$false
$out=[IO.Path]::GetFullPath($OutputDirectory)
if(-not [IO.Directory]::Exists($out)){throw 'Output directory must already exist'}
$helper=[IO.Path]::GetFullPath($HelperPath)
$runtime=Get-Content -LiteralPath $RuntimeJson -Raw -Encoding UTF8|ConvertFrom-Json
$bundle=[IO.Path]::GetFullPath([string]$runtime.Bundle)
$sourceExe=if($runtime.SourceExe){[IO.Path]::GetFullPath([string]$runtime.SourceExe)}else{Join-Path $bundle 'engine\lumatape-testcard.exe'}
$menuScript=Join-Path $PSScriptRoot 'windows-tray-menu-smoke.ps1'

function Config-Identity($config) {
 # Clone so this comparison never changes the profile object used for assertions.
 $copy=$config|ConvertTo-Json -Depth 30|ConvertFrom-Json
 $copy.enabled=$true
 return ($copy|ConvertTo-Json -Depth 30 -Compress)
}
function Assert-Identities {
 foreach($b in $script:bound){
  if($b.Process.HasExited){throw "Bound process exited: $($b.Role)"}
  if($b.Process.Path -ne $b.Executable -or $b.Process.StartTime.ToUniversalTime().ToFileTimeUtc().ToString() -ne $b.Created){throw "Bound process identity changed: $($b.Role)"}
 }
}
function Read-Config {
 return (Get-Content -LiteralPath $script:configPath -Raw -Encoding UTF8|ConvertFrom-Json)
}
function Assert-Profile([bool]$enabled) {
 $current=Read-Config
 if((Config-Identity $current) -cne (Config-Identity $script:beforeConfig)){throw 'Profile changed outside the owned enabled toggle; refusing to overwrite it'}
 if([bool]$current.enabled -ne $enabled){throw "Expected enabled=$enabled in the profile"}
}
function Wait-Enabled([bool]$enabled) {
 $until=[DateTime]::UtcNow.AddSeconds(10)
 do {
  Assert-Identities
  $current=Read-Config
  if((Config-Identity $current) -cne (Config-Identity $script:beforeConfig)){throw 'Profile changed outside the owned enabled toggle'}
  if([bool]$current.enabled -eq $enabled){return}
  Start-Sleep -Milliseconds 100
 }while([DateTime]::UtcNow -lt $until)
 throw "Profile did not reach enabled=$enabled"
}
function Invoke-Tray([string]$label,[string]$name) {
 Assert-Identities
 $file=Join-Path $script:out "$name-menu.json"
 & $script:menuScript -Bundle $script:bundle -HostProcessId ([uint32]$script:runtime.HostPID) -MenuPath @($label) -Output $file|Out-Null
 $result=Get-Content -LiteralPath $file -Raw -Encoding UTF8|ConvertFrom-Json
 if(-not $result.Passed -or -not $result.DispatchQueued -or $result.SelectedCommand.Label -ne $label){throw "Native tray command was not queued: $label"}
 if($result.HostCreatedFileTime.ToString() -ne $script:hostIdentity.Created){throw 'Tray helper bound a different host creation identity'}
 Assert-Identities
}
function Invoke-Pointer([string]$action,[string]$projection,[string]$name) {
 Assert-Identities
 $file=Join-Path $script:out "$name.json"
 $curvature=$script:report.Curvature.ToString('R',[Globalization.CultureInfo]::InvariantCulture)
 & $script:helper -pid $script:sourceIdentity.Process.Id -exe $script:sourceIdentity.Executable -worker-pid $script:workerIdentity.Process.Id -worker-exe $script:workerIdentity.Executable -action $action -target 5 -projection $projection -curvature $curvature -output $file *> (Join-Path $script:out "$name-helper.log")
 if($LASTEXITCODE -ne 0){throw "Pointer helper $name failed (exit $LASTEXITCODE); see $file"}
 Assert-Identities
 $result=Get-Content -LiteralPath $file -Raw -Encoding UTF8|ConvertFrom-Json
 if($result.error){throw "Pointer helper ${name}: $($result.error)"}
 if($result.source_identity.created_filetime.ToString() -ne $script:sourceIdentity.Created -or $result.worker_identity.created_filetime.ToString() -ne $script:workerIdentity.Created){throw 'Pointer helper bound a different source/worker creation identity'}
 if($result.after.values.Held -ne 0 -or $result.after.values.Captured -ne 0 -or $result.after.native_capture_hwnd -ne '0x0'){throw 'Pointer helper left a held button or native capture'}
 return $result
}
function Wait-Projection([bool]$visible,[string]$name) {
 # inspect is deliberately read-only, and does not itself assert visibility.
 # Preserve the final sample and bounded poll summaries rather than implying
 # that CURSOR_SHOWING proves compositor visibility.
 $until=[DateTime]::UtcNow.AddSeconds(12)
 do {
  $sample=Invoke-Pointer 'inspect' 'ignore' "$name-observation"
  $p=$sample.steps[-1].projection
  $good=if($visible){$p.visible -and $p.active -and $p.native_hidden}else{-not $p.visible -and -not $p.active -and -not $p.native_hidden}
  $script:report.Polls+=,[ordered]@{Name=$name;At=[DateTime]::UtcNow.ToString('o');ProjectionVisible=$p.visible;Active=$p.active;NativeHidden=$p.native_hidden;SourceForeground=$sample.after.foreground;Serial=$p.serial;Matches=$good}
  if($good){return}
  Start-Sleep -Milliseconds 100
 }while([DateTime]::UtcNow -lt $until)
 throw "Projection did not reach visible=$visible ($name)"
}

try {
 if(-not [IO.File]::Exists($helper)){throw 'Exact pointer helper is missing'}
 $report.HelperSHA256=(Get-FileHash -LiteralPath $helper -Algorithm SHA256).Hash
 $all=@(Get-CimInstance Win32_Process)
 $hostProcesses=@($all|Where-Object{$_.ProcessId -eq [uint32]$runtime.HostPID -and $_.ExecutablePath -eq (Join-Path $bundle 'lumatape.exe')})
 if($hostProcesses.Count -ne 1){throw 'Exact bundle host is missing'}
 $engines=@($all|Where-Object{$_.ParentProcessId -eq [uint32]$runtime.HostPID -and $_.ExecutablePath -eq (Join-Path $bundle 'engine\lumatape-engine.exe')})
 if($engines.Count -ne 1){throw 'Expected one exact engine child'}
 $workers=@($all|Where-Object{$_.ParentProcessId -eq $engines[0].ProcessId -and $_.ExecutablePath -eq (Join-Path $bundle 'engine\lumatape-watchdog.exe') -and $_.CommandLine -like '*--pointer-stdio*'})
 if($workers.Count -ne 1){throw 'Expected one exact pointer worker child'}
 $sources=@($all|Where-Object{$_.ProcessId -eq [uint32]$runtime.SourcePID -and $_.ExecutablePath -eq $sourceExe -and $_.CommandLine -like '*--pointer-test*'})
 if($sources.Count -ne 1){throw 'Exact pointer testcard source is missing'}
 foreach($spec in @(@{Role='host';Info=$hostProcesses[0]},@{Role='engine';Info=$engines[0]},@{Role='worker';Info=$workers[0]},@{Role='source';Info=$sources[0]})){
  $p=Get-Process -Id $spec.Info.ProcessId
  $null=$p.Handle # Keep the original process object pinned for the whole test.
  if($p.Path -ne $spec.Info.ExecutablePath -or $p.SessionId -eq 0 -or $p.SessionId -ne [Diagnostics.Process]::GetCurrentProcess().SessionId){$p.Dispose();throw 'Expected the exact process in our interactive session'}
  $b=[pscustomobject]@{Role=$spec.Role;Process=$p;Executable=$p.Path;Created=$p.StartTime.ToUniversalTime().ToFileTimeUtc().ToString()}
  $bound+=,$b
  $report.Identities+=,[ordered]@{Role=$b.Role;PID=$p.Id;Executable=$p.Path;CreatedFileTime=$b.Created}
 }
 $hostIdentity=$bound|Where-Object Role -eq 'host';$workerIdentity=$bound|Where-Object Role -eq 'worker';$sourceIdentity=$bound|Where-Object Role -eq 'source'
 $configPath=Join-Path $runtime.AppData 'LumaTape\config.json'
 $beforeRaw=Get-Content -LiteralPath $configPath -Raw -Encoding UTF8
 $beforeConfig=$beforeRaw|ConvertFrom-Json
 Copy-Item -LiteralPath $configPath -Destination (Join-Path $out 'config-before.json')
 $report.ConfigBeforeSHA256=(Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash
 if(-not $beforeConfig.enabled -or $beforeConfig.mode -ne 'full' -or $beforeConfig.target.kind -ne 'window' -or $beforeConfig.target.window_title -ne 'LumaTape pointer test'){throw 'Start with Full enabled on the own pointer testcard'}
 if($beforeConfig.screen.shape -ne 'convex' -or [double]$beforeConfig.screen.curvature -le 0 -or [double]$beforeConfig.screen.curvature -gt 1){throw 'Expected a positive convex screen curvature'}
 if($beforeConfig.aspect.enabled -or $beforeConfig.shader.id -or [double]$beforeConfig.effects.intensity -ne 1 -or [double]$beforeConfig.effects.vhs.jitter -ne 0 -or [double]$beforeConfig.effects.vhs.tracking -ne 0){throw 'Independent oracle requires aspect off, built-in shader, 100% intensity and no temporal warp'}
 $report.Curvature=[double]$beforeConfig.screen.curvature
 Assert-Identities;$ready=$true
 $null=Invoke-Pointer 'move' 'ignore' 'focus'
 Wait-Projection $true 'initial-active'
 foreach($action in @('targets','drag','wheel')){
  Assert-Profile $true
  $null=Invoke-Pointer $action 'visible' $action
  $report.Steps+=,[ordered]@{Name=$action;Passed=$true}
 }
 Assert-Profile $true
 # Mark ownership before dispatch: a queued command may have taken effect even
 # if a later observation fails. Cleanup only restores this one unchanged toggle.
 $ownsDisabled=$true
 Invoke-Tray 'Выключить' 'disable'
 Wait-Enabled $false
 Wait-Projection $false 'disabled'
 $null=Invoke-Pointer 'move' 'hidden' 'disabled-native-input'
 $report.Steps+=,[ordered]@{Name='normal-disable-hidden-and-native-input';Passed=$true}
 Assert-Profile $false
 Invoke-Tray 'Включить' 'enable'
 Wait-Enabled $true
 $ownsDisabled=$false
 $null=Invoke-Pointer 'move' 'ignore' 'refocus'
 Wait-Projection $true 'reenabled'
 $null=Invoke-Pointer 'move' 'visible' 'reenabled-oracle'
 $report.Steps+=,[ordered]@{Name='normal-reenable-projected-oracle';Passed=$true}
 Assert-Profile $true
 $report.Passed=$true
} catch {$report.Error=$_.Exception.Message} finally {
 if($ready -and $ownsDisabled){
  try {
   Assert-Identities
   $current=Read-Config
   if((Config-Identity $current) -cne (Config-Identity $beforeConfig)){throw 'Cleanup refused: another profile setting changed'}
   if(-not $current.enabled){Invoke-Tray 'Включить' 'cleanup-enable';Wait-Enabled $true}
   $ownsDisabled=$false
  } catch {$report.CleanupError=$_.Exception.Message;$report.Passed=$false}
 }
 if($beforeConfig -and [IO.File]::Exists($configPath)){
  try {
   Copy-Item -LiteralPath $configPath -Destination (Join-Path $out 'config-after.json')
   $report.ConfigAfterSHA256=(Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash
   $after=Read-Config
   $report.ConfigBytesUnchanged=$report.ConfigBeforeSHA256 -eq $report.ConfigAfterSHA256
   $report.ConfigSemanticallyUnchanged=($beforeConfig|ConvertTo-Json -Depth 30 -Compress) -ceq ($after|ConvertTo-Json -Depth 30 -Compress)
   if(-not $report.ConfigSemanticallyUnchanged){$report.Passed=$false;if(-not $report.CleanupError){$report.CleanupError='Final configuration differs from the initial configuration'}}
  } catch {$report.CleanupError=$_.Exception.Message;$report.Passed=$false}
 }
 foreach($b in $bound){$b.Process.Dispose()}
 $report.CompletedAt=[DateTime]::UtcNow.ToString('o')
 $report|ConvertTo-Json -Depth 20|Set-Content -LiteralPath (Join-Path $out 'result.json') -Encoding UTF8
}
if(-not $report.Passed){throw "Cursor handoff smoke failed: $($report.Error); cleanup: $($report.CleanupError)"}
