# Windows executable quoting

Vendored from the crates.io `auto-launch` 0.5.0 package used by
`tauri-plugin-autostart` 2.5.1. Upstream repository:
https://github.com/zzzgydi/auto-launch, commit
`fedaeba7a5ee6a5124b056907e03c399acd6df27` (published `.cargo_vcs_info.json`).
The upstream MIT license is preserved in `LICENSE`.
The original crates.io archive SHA-256 is
`1f012b8cc0c850f34117ec8252a44418f2e34a2cf501de89e29b241ae5f79471`.

Two narrowly scoped runtime changes quote the executable in the Windows Run
command and create the Run key on explicit enable if a fresh profile lacks it.
Upstream arguments and other registry operations are unchanged. The extracted pure
helper has regression tests for paths containing spaces and Cyrillic letters;
LumaTape passes no startup arguments.

LumaTape separately verifies installed-copy identity, command ownership, Windows
StartupApproved state and operation readback in `src/autostart.rs`. No startup
entry is created by plugin initialization or status checks.

Upstream integration tests are not vendored: they create operating-system login
items. The pure command helper tests do not change system configuration.

Remove this patch when a reviewed compatible upstream version always quotes
Windows executable paths; keep the ownership/readback checks in LumaTape.
