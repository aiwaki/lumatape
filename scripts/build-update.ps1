param(
  [Parameter(Mandatory=$true)][string]$PublicKeyFile,
  [Parameter(Mandatory=$true)][string]$PublishedAt,
  [string]$Version='0.3.2',
  [string]$Commit='working-tree',
  [string]$BuildTime='unknown',
  [string]$NativeFrom='',
  [string]$LicenseFrom='',
  [string]$WindowsSdkVersion=''
)
# Local signed installer candidate only. No key generation or publication.
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
if($Version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'){throw 'Version must be a stable X.Y.Z version'}
foreach($part in $Version.Split('.')){if([uint64]$part -gt 65535){throw 'Version exceeds the Windows resource range'}}
if(-not $env:TAURI_SIGNING_PRIVATE_KEY){throw 'Supply your dedicated LumaTape TAURI_SIGNING_PRIVATE_KEY before building. This script never creates a key.'}
$PublicKeyFile=(Resolve-Path -LiteralPath $PublicKeyFile).Path
$key=(Get-Content -LiteralPath $PublicKeyFile -Raw).Trim()
if(-not $key -or $key.Length -gt 4096){throw 'Invalid public key file'}
$endpoint='https://github.com/aiwaki/lumatape/releases/latest/download/latest.json'
$output=Join-Path $root "build/update-$Version"
if(Test-Path -LiteralPath $output){throw 'Update staging already exists; preserve it and choose a new version or move it explicitly.'}
$oldEndpoint=$env:LUMATAPE_UPDATE_ENDPOINT
$oldKey=$env:LUMATAPE_UPDATE_PUBLIC_KEY
Push-Location $root
try {
  $env:LUMATAPE_UPDATE_ENDPOINT=$endpoint
  $env:LUMATAPE_UPDATE_PUBLIC_KEY=$key
  & "$PSScriptRoot/build-desktop.ps1" -Version $Version -Commit $Commit -BuildTime $BuildTime -NativeFrom $NativeFrom -LicenseFrom $LicenseFrom -WindowsSdkVersion $WindowsSdkVersion
  if(-not $?){throw 'Desktop build failed'}
  New-Item -ItemType Directory -Path $output | Out-Null
  $overlay=Join-Path $output 'tauri-update.json'
  $overlayJson=@{bundle=@{createUpdaterArtifacts=$true};plugins=@{updater=@{pubkey=$key;endpoints=@($endpoint)}}}|ConvertTo-Json -Depth 6
  [System.IO.File]::WriteAllText($overlay,$overlayJson,(New-Object System.Text.UTF8Encoding($false)))
  Push-Location (Join-Path $root 'desktop')
  try {
    npm run tauri -- bundle --target x86_64-pc-windows-msvc --bundles nsis --config $overlay --ci
    if($LASTEXITCODE -ne 0){throw 'Signed NSIS build failed'}
  } finally {Pop-Location}
  $name="LumaTape_${Version}_x64-setup.exe"
  $bundle=Join-Path $root 'desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis'
  foreach($file in @($name,"$name.sig")) {
    if(-not (Test-Path -LiteralPath (Join-Path $bundle $file) -PathType Leaf)){throw "Missing signed artifact: $file"}
    Copy-Item -LiteralPath (Join-Path $bundle $file) -Destination $output
  }
  python scripts/prepare-update.py --installer (Join-Path $output $name) --signature (Join-Path $output "$name.sig") --public-key $PublicKeyFile --version $Version --published-at $PublishedAt --output (Join-Path $output 'index')
  if($LASTEXITCODE -ne 0){throw 'Installer signature/admission failed; do not publish the candidate'}
  python scripts/package-desktop.py --shell desktop/src-tauri/target/x86_64-pc-windows-msvc/release/lumatape.exe --engine desktop/src-tauri/resources/engine --licenses build/desktop-licenses --version $Version --revision $Commit --build-time $BuildTime --update-channel configured
  if($LASTEXITCODE -ne 0){throw 'Final package failed'}
  Write-Output "Local update candidate ready: $output. Not published or runtime-qualified."
} finally {
  $env:LUMATAPE_UPDATE_ENDPOINT=$oldEndpoint
  $env:LUMATAPE_UPDATE_PUBLIC_KEY=$oldKey
  Pop-Location
}
