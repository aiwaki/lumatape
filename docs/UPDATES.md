# LumaTape signed updates

The tray checks a configured channel once, 20 seconds after startup, and on demand.
An available version never installs automatically: the user must confirm. Builds
without the embedded endpoint **and** public key report that the channel is not
configured and send no update requests. No GitHub release or signing key is
created by normal builds.

## Supported installation

In-place updates support the **current-user x64 NSIS installation**. The updater
compares the real running executable with NSIS's two HKCU installation records,
its version and uninstaller. A portable ZIP can discover a release but cannot run
the installer; extract a new portable ZIP separately. Copying an installed folder
elsewhere does not turn it into a supported installation.

The updater admits stable versions newer than the running version, for
`windows-x86_64` only. The exact asset name is
`LumaTape_<version>_x64-setup.exe` under the immutable release tag `v<version>` in
`aiwaki/lumatape`. The discovery endpoint is
`https://github.com/aiwaki/lumatape/releases/latest/download/latest.json`.
Official release builds embed this endpoint and the LumaTape public key in
`desktop/src-tauri/updater.pub`; ordinary unsigned developer builds remain unconfigured.

## Local release preparation

Use a **dedicated LumaTape key**, supplied by the release owner. Never use the
Slipstream key. The private key stays in the release process environment; these
scripts neither generate nor save it. Follow Tauri's signer instructions when
establishing a channel for the first time. The public-key file contains the base64
value produced by Tauri's signer.

Before a release, update the version consistently in Cargo.toml, package.json and
tauri.conf.json. On the Windows build host with the normal native dependencies,
NSIS toolchain and Python `cryptography` package available:

```powershell
# TAURI_SIGNING_PRIVATE_KEY and, if needed, TAURI_SIGNING_PRIVATE_KEY_PASSWORD
# are already supplied by the release owner; do not put them in command history.
./scripts/build-update.ps1 -PublicKeyFile C:\release-keys\lumatape.pub `
  -PublishedAt '2026-10-08T00:00:00Z' -Version '0.3.1' `
  -Commit '<source-commit>' -BuildTime '<build-time>' `
  -NativeFrom '<native-input>' -LicenseFrom '<license-input>'
```

On macOS/Linux with cargo-xwin, LLVM, NSIS, and an explicit previously built
native install directory, the equivalent command is:

```sh
# Signing environment supplied privately; never commit the private key.
# LUMATAPE_COMMIT and LUMATAPE_BUILD_TIME identify the source and UTC build time.
sh scripts/build-update.sh /absolute/native-install 0.3.1
```

The cross-build uses the committed `desktop/src-tauri/updater.pub`. Both build
paths include verified dependency notices and public documentation in NSIS.
These minisign signatures authenticate updates; they are not an Authenticode
certificate and do not establish a trusted Windows publisher.

This builds locally, embeds the public trust configuration, generates Tauri NSIS
updater artifacts, verifies the actual minisign signature and the PE's file/product
version resources, and writes a bounded
`latest.json` plus a SHA256 receipt. It does **not** upload or publish anything.
Already signed assets can be admitted without a rebuild:

```sh
python3 scripts/prepare-update.py \
  --installer LumaTape_0.3.1_x64-setup.exe \
  --signature LumaTape_0.3.1_x64-setup.exe.sig \
  --public-key lumatape.pub --version 0.3.1 \
  --published-at 2026-10-08T00:00:00Z --output new-update-index
```

Publish only after separately authorizing and qualifying a release. The release
must contain the exact installer, its `.sig`, and `latest.json`. Keep the previous
signed installer and portable ZIP available for manual recovery. Publishing the
index last avoids advertising an incomplete release.

## Admission and failure behavior

- Metadata: 64 KiB maximum, 5-second connect timeout, 10-second request timeout.
- Installer: 256 MiB maximum, 120-second request timeout; size is checked both
  before and during reading. Redirects are HTTPS-only, allowlisted and bounded.
- Bytes are verified using minisign before disk admission or engine shutdown.
  The installer PE version resource must also match the offered version, so a
  signed older binary renamed as a new release is rejected.
- One operation owns the updater. New checks invalidate stale offers; failed or
  cancelled work can be retried and progress starts at zero. The offer is fetched
  again and compared before downloading. Emergency-off and quit cancel pending
  network I/O within a 200 ms polling interval; engine cleanup, once started,
  completes its acknowledgement/exit path before cancellation returns. An atomic
  handoff marks the point after which installer launch is no longer cancellable.
- The staged installer is held without write/delete sharing. Testcard shutdown
  and acknowledged engine cleanup must finish before launch. Shutdown can cancel
  the operation before launch. No game process is killed.
- The installer refuses to force-close a running LumaTape, including a portable
  copy. Update mode waits up to ten seconds for the old process to exit; manual
  installation asks the user to quit it first. NSIS starts through checked process creation with Tauri's `/P /R /UPDATE`
  arguments. Launch failure keeps the tray open with a restart instruction; only
  successful launch authorizes the host to exit. The installer performs its normal
  update and relaunch. A successful process launch is **not** proof of installation.
- Staging keeps at most one owned `pending-installer.exe`. Failed attempts remove
  it; a later attempt reclaims the previous file only after Windows releases it.
  No arbitrary files or directories are swept.

The design reuses the boundaries learned in Slipstream (bounded discovery,
signature admission, immutable version identity, cleanup before replacement),
with an independent Windows implementation. Slipstream's macOS bundle watchdog,
daemon/PF cleanup, keys and endpoints are not copied. No automatic rollback or
post-install heartbeat is claimed for NSIS.

Exact implementation references inspected:
[Tauri updater 2.9.0](https://docs.rs/tauri-plugin-updater/2.9.0/tauri_plugin_updater/),
[Tauri CLI 2.11.3 NSIS template](https://github.com/tauri-apps/tauri/blob/tauri-cli-v2.11.3/crates/tauri-bundler/src/bundle/windows/nsis/installer.nsi).

## Qualification before enabling a public channel

Unit tests cover a real disposable signed fixture, byte tampering/truncation,
version/URL/target binding, byte limits, redirects, overlapping operations,
cancellation, retry and portable rejection. `scripts/test_prepare_update.py`
checks the offline release index, signature and structural PE version admission,
including a signed old binary renamed as a newer version. The fixture key is public
test data; its private key was discarded and must never be a release trust root.

Still required on a disposable installed Windows profile before public release:
install version N → configure a test release N+1 → offer/decline → confirm → verify
restoration → installer completes → exactly one new tray/engine starts with the
same user profile. Exercise launch failure, disk-full, unavailable network,
shutdown while downloading, and installer cancellation. Verify an actual generated
NSIS version resource and registry identity, then test a renamed older signed
installer rejection and a copied/portable installation rejection. A portable
runtime smoke or unit test does not qualify installed NSIS replacement or rollback.
