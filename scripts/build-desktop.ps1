param([string]$Version='0.3.2',[string]$Commit='working-tree',[string]$BuildTime='unknown',[string]$NativeFrom='',[string]$LicenseFrom='',[string]$WindowsSdkVersion='')
$ErrorActionPreference='Stop'
$updateChannel='unconfigured'
$hasUpdateEndpoint=-not [string]::IsNullOrEmpty($env:LUMATAPE_UPDATE_ENDPOINT)
$hasUpdateKey=-not [string]::IsNullOrEmpty($env:LUMATAPE_UPDATE_PUBLIC_KEY)
if($hasUpdateEndpoint -ne $hasUpdateKey){throw 'Update channel requires both LUMATAPE_UPDATE_ENDPOINT and LUMATAPE_UPDATE_PUBLIC_KEY; no build started'}
if($hasUpdateEndpoint -and $hasUpdateKey){$updateChannel='configured'}
if(-not $env:CARGO_BUILD_JOBS){$env:CARGO_BUILD_JOBS='2'}
$root=Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
  foreach($file in @('desktop/package.json','desktop/src-tauri/tauri.conf.json')) {
    if((Get-Content $file -Raw|ConvertFrom-Json).version -ne $Version){throw "Version mismatch: $file"}
  }
  $cargoVersion=(Get-Content desktop/src-tauri/Cargo.toml|Select-String '^version\s*=\s*"([^"]+)"'|Select-Object -First 1).Matches.Groups[1].Value
  if($cargoVersion -ne $Version){throw 'Cargo.toml version mismatch'}
  & ./scripts/build-windows.ps1 -Version $Version -Commit $Commit -BuildTime $BuildTime -NativeFrom $NativeFrom -LicenseFrom $LicenseFrom -WindowsSdkVersion $WindowsSdkVersion
  if(-not $?){throw 'Native build failed'}
  $source=Join-Path $root 'build/windows-full/stage'
  $engine=Join-Path $root 'desktop/src-tauri/resources/engine'
  if(Test-Path $engine){Remove-Item $engine -Recurse -Force}
  New-Item -ItemType Directory -Force $engine|Out-Null
  Copy-Item "$source/lumatape.exe" "$engine/lumatape-engine.exe"
  foreach($name in @('lumatape-watchdog.exe','lumatape-testcard.exe','glfw3.dll','lumatape_capture.dll')){Copy-Item "$source/$name" "$engine/$name"}
  Push-Location desktop
  try {
    npm ci --ignore-scripts --no-audit --no-fund
    if($LASTEXITCODE -ne 0){throw 'npm install failed'}
  } finally {Pop-Location}
  $licenses=Join-Path $root 'build/desktop-licenses'
  if(Test-Path $licenses){Remove-Item $licenses -Recurse -Force}
  New-Item -ItemType Directory -Force $licenses|Out-Null
  Copy-Item "$source/licenses" "$licenses/native" -Recurse
  cargo fetch --manifest-path desktop/src-tauri/Cargo.toml --locked --target x86_64-pc-windows-msvc
  if($LASTEXITCODE -ne 0){throw 'Locked Rust dependency download failed'}
  python desktop/scripts/licenses.py --output "$licenses/rust"
  if($LASTEXITCODE -ne 0){throw 'License collection failed'}
  python desktop/scripts/npm-licenses.py --output "$licenses/npm"
  if($LASTEXITCODE -ne 0){throw 'Frontend license collection failed'}
  python scripts/stage-installer.py --licenses $licenses
  if($LASTEXITCODE -ne 0){throw 'Installer notice staging failed'}
  Push-Location desktop
  try {
    npm run tauri -- build --target x86_64-pc-windows-msvc --no-bundle -- --locked
    if($LASTEXITCODE -ne 0){throw 'Tauri build failed'}
  } finally {Pop-Location}
  python scripts/package-desktop.py --shell desktop/src-tauri/target/x86_64-pc-windows-msvc/release/lumatape.exe --engine $engine --licenses $licenses --version $Version --revision $Commit --build-time $BuildTime --update-channel $updateChannel
  if($LASTEXITCODE -ne 0){throw 'Packaging failed'}
} finally {Pop-Location}
