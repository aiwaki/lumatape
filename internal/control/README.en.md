# Local renderer control, protocol v1

[Русский](README.md) · English

The Tauri host starts the exact bundled engine with `--control-stdio
--headless-settings --controller-pid <host PID>` and inherited stdin/stdout
pipes. No network listener, shared endpoint or shell execution command exists.
Without these flags the native settings/tray fallback remains.

## Messages and commands

Each UTF-8 line is one JSON object. Request:
`{"v":1,"id":"unique string","type":"snapshot","payload":{}}`.
Response: `{"v":1,"id":"...","ok":true,"result":...}` or
`{"v":1,"id":"...","ok":false,"error":{"code":"...","message":"...","applied":false,"unsaved":false},"result":...}`.
Preserve the whole error envelope. A failed save can have `applied:true`, and
result remains the authoritative post-operation snapshot. Never automatically
retry a mutable request after an uncertain result.

Events use `{"v":1,"event":"ready|state_changed|show_settings|fatal|stopped","data":...}`.
`ready.data` is a snapshot. `state_changed` asks the host to refresh state and
can be dropped under pressure. If a reply or terminal event cannot be delivered,
the engine stops and requests cleanup.

| Command | Payload | Result |
| --- | --- | --- |
| `snapshot` | `{}` | config, snake_case runtime, five presets `{name,effects}`, screen_shapes, hotkeys, exact source or null, confirmation_deadline RFC3339 or null |
| `sources` | `{}` | windows `{id,hwnd,pid,process_created,title}` and monitors `{id,index,device,bounds,work_area,primary}` |
| `apply` | `{config,source?,expected_emergency_sequence,expected_config?}` | Authoritative snapshot, also on failure |
| `toggle`, `emergency`, `restore`, `confirm`, `reload` | `{}` | Authoritative snapshot |
| `preview` | `{config,width,height,time,before}` | `{mime:"image/png",width,height,png_base64}` |
| `ui_state` | `{hwnd,visible}` | `{accepted:true}` after verifying host PID |
| `diagnostics` | `{}` | Sanitized diagnostic object |
| `quit` | `{}` | `{clean_shutdown:true}` only after cleanup |

## Sources and preview

HWND/process creation time are strings, never JavaScript numbers. Treat source
IDs as opaque and return the complete selected window object with config. The
engine re-enumerates it, checks HWND/PID/creation identity, and uses its current
title. Controller windows cannot be selected. Rectangles use
`{x,y,width,height}` in physical pixels. A focused host window pauses the
overlay; the engine does not activate a game or settings window itself.

Preview invokes the same shader renderer on the locked initial GL thread.
It enables a copy of the draft and sets only that copy's intensity to zero for
the before image. PNG compression runs on a worker. Limits: 640×480, two
requests per second, one outstanding request. Preview never changes live
preferences or captures another application.

## Queue and stale-request protection

Limits: 128 KiB input line, 2 MiB output line, 16 ordinary queued commands and
responses. Quit/emergency have dedicated priority slots. Emergency cancels
ordinary pending commands so old Apply requests cannot re-enable the effect.
Stdin EOF/stdout failure signals shutdown independently of queue capacity.
Native/render/config work runs only on the locked main thread.

Every snapshot includes `emergency_sequence` (initially 0). Every emergency
invocation increments it before cleanup, including a failed restore or save.
Apply must echo the sequence of the snapshot from which its draft was made.
Missing or stale values return `stale_emergency_sequence` plus current snapshot
before any mutation. On an increased sequence, discard the entire local draft;
do not rebase an old queued Apply automatically. This closes the race where a
request reaches the pipe after emergency already drained queued commands.

New clients also echo the complete `snapshot.config` as `expected_config`.
After the emergency barrier, the engine compares these decoded settings with
the current config on the locked main thread, before source resolution or any
native change/save. A mismatch returns `stale_config` and the authoritative
snapshot. Cancel the old intent and let the user make a new explicit selection;
never automatically refresh the precondition and retry. Malformed or null
`expected_config` returns `invalid_payload`. Absence remains compatible with
older clients, which retain only the emergency-sequence guard. This compares
config values, not JSON formatting, and does not replace live source identity
validation or detect a change that was subsequently reverted to equal values.

## Shutdown and recovery

The host reads stdout throughout the child process's lifetime. For quit/update, wait
for the clean quit response **and actual exit 0**; EOF alone proves neither
cleanup nor restoration. Abort update on failure. Do not use process-tree kill
or inherited kill-on-close jobs: the display watchdog must remain alive.
Host crash closes stdin and requests normal engine cleanup. Forced engine
termination still uses the documented next-launch window recovery fallback.
