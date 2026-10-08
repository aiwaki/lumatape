param(
    [Parameter(Mandatory=$true)][string]$TestExecutable,
    [Parameter(Mandatory=$true)][string]$Output
)
# Run a compiled Rust unit-test executable in the interactive Windows session.
# The selected test creates only its own delayed, hidden HWND and closes it with
# the production TestCard close loop. No app instance, profile or input changes.
$ErrorActionPreference='Stop'
$report=[ordered]@{
    SchemaVersion=1; StartedAt=[DateTime]::UtcNow.ToString('o'); FinishedAt=$null
    Passed=$false; Error=$null; TestExecutable=$null; SHA256=$null
    Test='testcard::tests::delayed_native_hwnd_is_closed'
    SessionId=[Diagnostics.Process]::GetCurrentProcess().SessionId
    PID=$null; ExitCode=$null; Stdout=$null; Stderr=$null
    Scope='Delayed hidden HWND after the first production close scan; own child only. No installed-app or physical-input qualification.'
}
$outputPath=[IO.Path]::GetFullPath($Output)
if(Test-Path -LiteralPath $outputPath){throw 'Preserve the existing smoke receipt; choose a new output path'}
$process=$null
try {
    if($report.SessionId -eq 0){throw 'Run in an interactive Windows session, not Session 0'}
    $exe=Get-Item -LiteralPath $TestExecutable
    if($exe.PSIsContainer -or $exe.Extension -ine '.exe'){throw 'Expected a compiled Rust test executable'}
    $report.TestExecutable=$exe.FullName
    $report.SHA256=(Get-FileHash -LiteralPath $exe.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    $start=New-Object Diagnostics.ProcessStartInfo
    $start.FileName=$exe.FullName
    $start.Arguments='--exact testcard::tests::delayed_native_hwnd_is_closed --ignored --nocapture --test-threads=1'
    $start.UseShellExecute=$false
    $start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true
    $start.RedirectStandardError=$true
    $process=New-Object Diagnostics.Process
    $process.StartInfo=$start
    if(-not $process.Start()){throw 'Could not launch the owned regression process'}
    $report.PID=$process.Id
    $stdout=$process.StandardOutput.ReadToEndAsync()
    $stderr=$process.StandardError.ReadToEndAsync()
    if(-not $process.WaitForExit(15000)){throw 'Owned regression process exceeded 15 seconds; it was not forcibly terminated'}
    $report.ExitCode=$process.ExitCode
    $report.Stdout=$stdout.GetAwaiter().GetResult()
    $report.Stderr=$stderr.GetAwaiter().GetResult()
    if($report.ExitCode -ne 0){throw 'Delayed-HWND regression failed; inspect the captured output'}
    # An older executable silently running zero matching tests must not pass.
    if($report.Stdout -notmatch 'test result: ok\. 1 passed; 0 failed; 0 ignored'){
        throw 'The executable did not run exactly one delayed-HWND regression test'
    }
    $report.Passed=$true
} catch {
    $report.Error=$_.Exception.Message
} finally {
    $report.FinishedAt=[DateTime]::UtcNow.ToString('o')
    if($process){$process.Dispose()}
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($outputPath))|Out-Null
    [IO.File]::WriteAllText($outputPath,($report|ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
}
if(-not $report.Passed){throw $report.Error}
Write-Output "Delayed-HWND close regression passed: $outputPath"
