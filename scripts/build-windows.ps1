param(
    [switch]$LightweightOnly,
    [string]$Configuration = 'Release',
    [string]$Version = '',
    [string]$Commit = 'unknown',
    [string]$BuildTime = 'unknown',
    [string]$NativeFrom = '',
    [string]$LicenseFrom = '',
    [string]$WindowsSdkVersion = ''
)
$ErrorActionPreference = 'Stop'

function Resolve-LumaTapeWindowsSdk {
    param([string]$RequestedVersion, [bool]$WithCapture)
    if ($RequestedVersion -and $RequestedVersion -notmatch '^10\.0\.\d+\.\d+$') {
        throw 'WindowsSdkVersion must be a full Windows 10 SDK version, for example 10.0.26100.0.'
    }
    $minimum = if ($WithCapture) { [version]'10.0.26100.0' } else { [version]'10.0.0.0' }
    if ($RequestedVersion -and [version]$RequestedVersion -lt $minimum) {
        throw "Windows SDK $RequestedVersion is too old; capture requires $minimum or newer."
    }
    $sdkRoots = @()
    if ($env:CMAKE_WINDOWS_KITS_10_DIR) {
        # Respect a caller's explicit SDK location instead of silently choosing another.
        $sdkRoots += $env:CMAKE_WINDOWS_KITS_10_DIR
    } else {
        foreach ($key in @('HKLM:\SOFTWARE\Microsoft\Windows Kits\Installed Roots', 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows Kits\Installed Roots')) {
            $entry = Get-ItemProperty -LiteralPath $key -Name KitsRoot10 -ErrorAction SilentlyContinue
            if ($entry.KitsRoot10) { $sdkRoots += $entry.KitsRoot10 }
        }
        if (${env:ProgramFiles(x86)}) { $sdkRoots += Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10' }
    }
    $candidates = @()
    $inventory = @()
    foreach ($sdkRoot in @($sdkRoots | Select-Object -Unique)) {
        $include = Join-Path $sdkRoot 'Include'
        foreach ($directory in @(Get-ChildItem -LiteralPath $include -Directory -ErrorAction SilentlyContinue)) {
            if ($directory.Name -notmatch '^10\.0\.\d+\.\d+$') { continue }
            $sdkVersion = [version]$directory.Name
            $inventory += $directory.Name
            if ($sdkVersion -lt $minimum -or ($RequestedVersion -and $directory.Name -ne $RequestedVersion)) { continue }
            $required = @(
                "Include\$sdkVersion\um\Windows.h", "Include\$sdkVersion\shared\sdkddkver.h",
                "Include\$sdkVersion\ucrt\stdlib.h", "Lib\$sdkVersion\um\x64\user32.lib",
                "Lib\$sdkVersion\ucrt\x64\ucrt.lib", "bin\$sdkVersion\x64\rc.exe"
            )
            if ($WithCapture) {
                $required += @("Include\$sdkVersion\cppwinrt\winrt\Windows.Graphics.Capture.h", "Lib\$sdkVersion\um\x64\windowsapp.lib")
            }
            $missing = @($required | Where-Object { -not (Test-Path -LiteralPath (Join-Path $sdkRoot $_) -PathType Leaf) })
            if ($missing.Count -gt 0) {
                Write-Host "Skipping incomplete Windows SDK ${sdkVersion}: $($missing -join ', ')"
                continue
            }
            $candidates += [pscustomobject]@{ Version = $sdkVersion; Root = $sdkRoot }
        }
    }
    Write-Host "Installed Windows SDK versions: $(@($inventory | Sort-Object -Unique) -join ', ')"
    $chosen = $candidates | Sort-Object Version -Descending | Select-Object -First 1
    if (-not $chosen) {
        $wanted = if ($RequestedVersion) { $RequestedVersion } else { "$minimum or newer" }
        throw "No complete Windows x64 SDK $wanted was found. Install its UM/UCRT libraries, resource compiler and C++/WinRT headers with Visual Studio Installer, or pass -WindowsSdkVersion for another installed compatible SDK."
    }
    Write-Host "Selected Windows SDK $($chosen.Version) at $($chosen.Root)"
    return $chosen
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    if ($env:OS -ne 'Windows_NT') { throw 'Run on Windows with Go, CMake, MSVC and Windows SDK, or pass -NativeFrom to reuse explicit native artifacts.' }
    if (-not $Version) {
        $Version = (Get-Content -LiteralPath (Join-Path $root 'desktop/src-tauri/tauri.conf.json') -Raw | ConvertFrom-Json).version
    }
    if ($Version -notmatch '^\d+\.\d+\.\d+([-+][A-Za-z0-9.-]+)?$') { throw 'Invalid semantic version.' }
    if ($Commit -notmatch '^[A-Za-z0-9._-]+$' -or $BuildTime -notmatch '^[A-Za-z0-9:._+-]+$') { throw 'Commit and BuildTime must be single safe metadata values.' }
    $variant = if ($LightweightOnly) { 'lightweight' } else { 'full' }
    $work = Join-Path $root "build/windows-$variant"
    $nativeBuild = Join-Path $work 'native'
    $stage = Join-Path $work 'stage'
    foreach ($reuse in @($NativeFrom, $LicenseFrom)) {
        if ($reuse) {
            $reuseFull = [IO.Path]::GetFullPath($reuse).TrimEnd([IO.Path]::DirectorySeparatorChar)
            if ($reuseFull -eq $stage -or $reuseFull.StartsWith($stage + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Reused native/license inputs must be outside generated staging.' }
        }
    }
    # Clear only this generated staging directory, never unrelated dist files.
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
    New-Item -ItemType Directory -Force $stage | Out-Null
    if ($NativeFrom) {
        $nativeFiles = @('glfw3.dll')
        if (-not $LightweightOnly) { $nativeFiles += @('lumatape_capture.dll', 'lumatape_capture_abi_smoke.exe') }
        foreach ($name in $nativeFiles) { Copy-Item (Join-Path $NativeFrom $name) (Join-Path $stage $name) }
        if (-not $LicenseFrom) { $LicenseFrom = Join-Path $NativeFrom 'licenses' }
        Copy-Item $LicenseFrom (Join-Path $stage 'licenses') -Recurse
    } else {
        $capture = if ($LightweightOnly) { 'OFF' } else { 'ON' }
        $platform = 'x64'
        $sdkArguments = @()
        $sdk = $null
        if (-not $LightweightOnly -or $WindowsSdkVersion) {
            $sdk = Resolve-LumaTapeWindowsSdk -RequestedVersion $WindowsSdkVersion -WithCapture (-not $LightweightOnly)
            # Keep each selected SDK in its own generated CMake cache. Existing
            # build trees are preserved when an SDK is changed or pinned in CI.
            $nativeBuild = Join-Path $work "native-sdk-$($sdk.Version)"
            $cmakeDescription = cmake --version
            if ($LASTEXITCODE -ne 0) { throw 'CMake version query failed' }
            $cmakeMatch = [regex]::Match(($cmakeDescription -join "`n"), 'cmake version (\d+\.\d+\.\d+)')
            if (-not $cmakeMatch.Success) { throw 'Could not determine the CMake version.' }
            if ([version]$cmakeMatch.Groups[1].Value -ge [version]'3.27.0') {
                $platform = "x64,version=$($sdk.Version)"
            } else {
                # CMake 3.24-3.26 selects the exact SDK through system version.
                $sdkArguments += "-DCMAKE_SYSTEM_VERSION=$($sdk.Version)"
            }
        }
        $oldSdkRoot = $env:CMAKE_WINDOWS_KITS_10_DIR
        try {
            if ($sdk) { $env:CMAKE_WINDOWS_KITS_10_DIR = $sdk.Root }
            cmake -S . -B $nativeBuild -G 'Visual Studio 17 2022' -A $platform @sdkArguments "-DLUMATAPE_BUILD_CAPTURE=$capture" -DBUILD_TESTING=ON
            if ($LASTEXITCODE -ne 0) { throw 'CMake configure failed' }
        } finally {
            $env:CMAKE_WINDOWS_KITS_10_DIR = $oldSdkRoot
        }
        cmake --build $nativeBuild --config $Configuration --parallel 2
        if ($LASTEXITCODE -ne 0) { throw 'Native build failed' }
        if (-not $LightweightOnly) {
            ctest --test-dir $nativeBuild -C $Configuration --output-on-failure
            if ($LASTEXITCODE -ne 0) { throw 'Native ABI smoke failed' }
        }
        cmake --install $nativeBuild --config $Configuration --prefix $stage
        if ($LASTEXITCODE -ne 0) { throw 'Native install failed' }
    }
    if ($NativeFrom -and -not $LightweightOnly) {
        & (Join-Path $stage 'lumatape_capture_abi_smoke.exe')
        if ($LASTEXITCODE -ne 0) { throw 'Reused native ABI smoke failed' }
    }
    go run ./scripts/resources -version $Version
    if ($LASTEXITCODE -ne 0) { throw 'Icon/version resource generation failed' }
    $oldOS = $env:GOOS; $oldArch = $env:GOARCH; $oldCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
        $metadata = "-X github.com/aiwaki/lumatape/internal/diagnostics.Version=$Version -X github.com/aiwaki/lumatape/internal/diagnostics.Commit=$Commit -X github.com/aiwaki/lumatape/internal/diagnostics.BuildTime=$BuildTime"
        go build -trimpath -ldflags "-H=windowsgui $metadata" -o (Join-Path $stage 'lumatape.exe') ./cmd/lumatape
        if ($LASTEXITCODE -ne 0) { throw 'Application build failed' }
        go build -trimpath -ldflags $metadata -o (Join-Path $stage 'lumatape-watchdog.exe') ./cmd/lumatape-watchdog
        if ($LASTEXITCODE -ne 0) { throw 'Watchdog build failed' }
        go build -trimpath -ldflags "-H=windowsgui $metadata" -o (Join-Path $stage 'lumatape-testcard.exe') ./cmd/lumatape-testcard
        if ($LASTEXITCODE -ne 0) { throw 'Testcard build failed' }
    } finally {
        $env:GOOS = $oldOS; $env:GOARCH = $oldArch; $env:CGO_ENABLED = $oldCGO
    }
    $folder = if ($LightweightOnly) { 'lumatape-windows-x64-lightweight' } else { 'lumatape-windows-x64' }
    $packageArgs = @('-input', $stage, '-output', "dist/$folder", '-zip', "dist/$folder-unqualified.zip", '-version', $Version, '-commit', $Commit, '-build-time', $BuildTime)
    if ($LightweightOnly) { $packageArgs += '-lightweight' }
    go run ./scripts/package-windows @packageArgs
    if ($LASTEXITCODE -ne 0) { throw 'Clean packaging failed' }
    Write-Host "Built dist/$folder/lumatape.exe and matching ZIP. Unit/ABI checks do not qualify runtime; see docs/WINDOWS_VALIDATION.md."
} finally { Pop-Location }
