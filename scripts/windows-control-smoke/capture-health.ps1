param(
 [Parameter(Mandatory=$true)][string]$Engine,
 [Parameter(Mandatory=$true)][string]$Testcard,
 [Parameter(Mandatory=$true)][string]$Helper,
 [Parameter(Mandatory=$true)][string]$OutputDirectory,
 [ValidateRange(320,16384)][int]$Width=960,
 [ValidateRange(240,16384)][int]$Height=720
)
$ErrorActionPreference='Stop'
# Run from the interactive user desktop. Never stop an existing application to
# make the fixture fit, and never use the installed application's profile.
$session=[Diagnostics.Process]::GetCurrentProcess().SessionId
if($session -eq 0 -or -not (Get-Process explorer -ErrorAction SilentlyContinue|Where-Object SessionId -eq $session)){
 throw 'Capture-health requires the interactive Windows user session'
}
if(Get-Process -Name 'LumaTape','lumatape-engine','lumatape-controlled' -ErrorAction SilentlyContinue){
 throw 'Another LumaTape process is running; leave it untouched and close it normally before this isolated smoke'
}
$out=[IO.Path]::GetFullPath($OutputDirectory)
if(Test-Path -LiteralPath $out){throw 'Output directory must be new'}
New-Item -ItemType Directory -Path $out|Out-Null
$receipt=[ordered]@{StartedAt=[DateTime]::UtcNow.ToString('o');SessionId=$session;Passed=$false;Files=@();SourcePID=$null;HelperPID=$null;Error=$null}
foreach($candidate in @($Engine,$Testcard,$Helper)){
 if(-not [IO.Path]::IsPathRooted($candidate) -or -not (Test-Path -LiteralPath $candidate -PathType Leaf)){throw 'Executable arguments must be existing absolute paths'}
 $receipt.Files+=@([pscustomobject]@{Path=$candidate;SHA256=(Get-FileHash -LiteralPath $candidate -Algorithm SHA256).Hash.ToLowerInvariant()})
}
function Start-Owned([string]$file,[string]$arguments){
 $info=New-Object Diagnostics.ProcessStartInfo
 $info.FileName=$file;$info.Arguments=$arguments;$info.WorkingDirectory=Split-Path -Parent $file
 $info.UseShellExecute=$false;$info.CreateNoWindow=$true
 $info.RedirectStandardOutput=$true;$info.RedirectStandardError=$true
 # Do not set WindowStyle Hidden: it overrides the testcard's first ShowWindow.
 $p=New-Object Diagnostics.Process;$p.StartInfo=$info
 if(-not $p.Start()){throw 'Owned fixture process did not start'}
 return $p
}
$card=$null;$smoke=$null
try {
 $card=Start-Owned $Testcard "--capture-smoke --width $Width --height $Height"
 $receipt.SourcePID=$card.Id
 $cardOut=$card.StandardOutput.ReadToEndAsync();$cardErr=$card.StandardError.ReadToEndAsync()
 $deadline=[DateTime]::UtcNow.AddSeconds(15)
 do {
  if($card.HasExited){throw 'Owned testcard exited before window creation'}
  $card.Refresh()
  if($card.MainWindowHandle -ne [IntPtr]::Zero){break}
  Start-Sleep -Milliseconds 100
 } while([DateTime]::UtcNow -lt $deadline)
 if($card.MainWindowHandle -eq [IntPtr]::Zero){throw 'Owned testcard has no window after 15 seconds'}
 $sampleOut=Join-Path $out 'engine-smoke'
 $arguments='--case capture-health --engine "{0}" --target-pid {1} --target-exe "{2}" --output "{3}"' -f $Engine,$card.Id,$Testcard,$sampleOut
 $smoke=Start-Owned $Helper $arguments
 $receipt.HelperPID=$smoke.Id
 $smokeOut=$smoke.StandardOutput.ReadToEndAsync();$smokeErr=$smoke.StandardError.ReadToEndAsync()
 if(-not $smoke.WaitForExit(180000)){
  # This helper never injects held input. Closing its pipe makes its own engine
  # perform EOF cleanup; do not kill an engine by name or touch unrelated PIDs.
  $smoke.Kill();[void]$smoke.WaitForExit(10000)
  throw 'Owned capture-health helper exceeded 180 seconds'
 }
 $receipt.HelperExitCode=$smoke.ExitCode
 if($smoke.ExitCode -ne 0){throw "Capture-health failed: exit $($smoke.ExitCode)"}
 if(-not $card.WaitForExit(10000)){throw 'Owned testcard did not close after the source-close check'}
 $receipt.SourceExitCode=$card.ExitCode
 if($card.ExitCode -ne 0){throw "Owned testcard failed: exit $($card.ExitCode)"}
 $receipt.Passed=$true
} catch {$receipt.Error=$_.Exception.Message} finally {
 if($null -ne $smoke -and $smoke.HasExited){
  [IO.File]::WriteAllText((Join-Path $out 'helper.stdout.jsonl'),$smokeOut.GetAwaiter().GetResult())
  [IO.File]::WriteAllText((Join-Path $out 'helper.stderr.txt'),$smokeErr.GetAwaiter().GetResult())
  $smoke.Dispose()
 }
 if($null -ne $card){
  if(-not $card.HasExited){[void]$card.CloseMainWindow();[void]$card.WaitForExit(10000)}
  if($card.HasExited){
   [IO.File]::WriteAllText((Join-Path $out 'testcard.stdout.txt'),$cardOut.GetAwaiter().GetResult())
   [IO.File]::WriteAllText((Join-Path $out 'testcard.stderr.txt'),$cardErr.GetAwaiter().GetResult())
  } else {$receipt.SourceCleanup='Owned testcard did not close; no force-kill was attempted'}
  $card.Dispose()
 }
 $receipt.EndedAt=[DateTime]::UtcNow.ToString('o')
 $receipt|ConvertTo-Json -Depth 8|Set-Content -LiteralPath (Join-Path $out 'launcher-result.json') -Encoding UTF8
}
if(-not $receipt.Passed){throw $receipt.Error}
