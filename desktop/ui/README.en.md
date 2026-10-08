# Archived LumaTape settings frontend

[Русский](README.md) · **English**

**Historical source, not the current product UI.** LumaTape now uses only a native
Tauri tray, with no application window, WebView or frontend build. This directory
retains the previous panel and its browser demo for development and comparison.
The behavior described below belongs to that archived panel.

React 19 + TypeScript + Vite 8 + Tailwind CSS 4, with real shadcn/ui components
from the official registry. The panel previously ran inside a Tauri shell; the current tray uses the
native Windows menu without this frontend. No CDN, remote fonts, analytics, or browser network API.

From `desktop/`:

```sh
npm ci
npm test
npm run typecheck
npm run dev:ui       # http://127.0.0.1:1420
npm run build:ui     # desktop/dist; archived UI only
npm run preview:ui   # archived build output on the same local port
```

`npm run dev` and `npm run build` remain Tauri commands. `package.json` and
`package-lock.json` pin the dependencies. Development disables HMR/Fast Refresh
so the Tauri CSP does not need inline scripts or eval; reload after an edit.
The archived production build uses hashed local JS/CSS/images and no inline scripts.

## Components and provenance

`components/ui/*.tsx` were installed by the official **shadcn CLI 4.21.3** on
2026-10-06, with `components.json` style `new-york` (Radix, new-york-v4 registry):

```sh
npx --yes shadcn@4.21.3 add button select slider switch tabs dialog tooltip card input badge label separator toggle-group progress alert -y
```

Registry: <https://ui.shadcn.com/r/styles/new-york-v4/>
Setup documentation: <https://ui.shadcn.com/docs/installation/vite>
Upstream source: <https://github.com/shadcn-ui/ui>
The complete MIT notice is retained in `third-party/shadcn-LICENSE.md` and in the
distribution's third-party notices. React, Radix, Lucide and the CSS dependencies
also retain their own notices through the packaging license collector.

Local adaptations to generated components: `cn` imports use `lib/utils.ts`
(clsx + tailwind-merge), and Slider forwards accessible names to each Radix
thumb. The underlying accessible primitives and component implementations are
not replaced by CSS lookalikes. Product layout/theme live in `App.tsx` and
`styles.css`; copied component files remain separately identifiable.

## State and IPC

`state.mjs` retains the shared preset/source/config rules. `api.js` retains the
Tauri invoke contract and structured backend errors. `controller.mjs` owns one
immutable observable store, read by React with `useSyncExternalStore`. Request
sequence numbers, disposal, timers and command ownership live outside render
closures. Subscription cleanup invalidates late replies and frees every listener.

The Go engine owns configuration validation, capture, rendering, hotkey
registration, saving and restoration. The frontend holds a separate draft until
acknowledged Apply. Every `emergency_sequence` advance cancels the entire draft,
including Aspect and source, and fences old requests and preview replies even
when restoration fails. Apply always sends `expected_emergency_sequence`.
The main flow selects a game window and a finished effect. Desktop effect
selection, Start and preview use 100% intensity; no intensity or manual shader
parameter sliders are shown. Loading a legacy profile does not rewrite it.
Start sends one atomic Apply with the current draft and `enabled=true`.
The single Off action cancels pending operations and restores owned window and
display changes through the emergency IPC barrier. Native emergency shortcuts
remain available. Shape, format and compatibility controls live in Settings.
A disabled legacy monitor profile is preserved rather than used as the default
game target. Full Auto negotiates GPU or capped CPU; the actual backend remains
visible in diagnostics. Unavailable backends cannot be selected.
Fatal engine events are terminal until explicit reconnection; late status replies
cannot clear the error. Exact HWND/PID/process-creation source strings are kept.

Hotkey receipt is separate from registration and pending key edits. Probe counters
show real received events since opening the probe or applying a changed profile.
The tray status comes from `host_status`/`host_status_changed`; a tray failure is
shown without discarding the engine snapshot. Host errors are not treated as
successful engine actions.

## Preview and explicit browser demo

The archived panel preview is visible on the Air panel and resumes independently after Off/recovery. Hidden panels request no frames; opening settings does not start a second renderer.
It calls the actual Go renderer: one PNG request at a time,
640×360, at most two requests per second. A/B does not modify the live filter.
Preview intentionally demonstrates the draft even while the live filter is off;
desktop preview uses the finished effect at 100%. It previews shader/shape, not a game
capture or window/display geometry. Verify 4:3/Fit/Crop/DAR on the actual source.

Only the exact query `?demo=1` enables fixtures, with a visible demo banner.
The fixture starts disabled with a demonstration game window and Full Auto.
Without Tauri, a normal URL shows a connection error; it never silently falls
back to data fixtures. Demo images are saved real CGL shader output, not new
browser filters. They represent fixed 100% preset/shape examples; zero shows the
original. Other slider values do not pretend to calculate a new shader result.
Vite resolves the `new URL(..., import.meta.url)` fixture references to all 16
hashed local PNG assets in both development and the production bundle.

## Visual direction and local font

The selected 2026-10-07 Air prototype is connected to the retained panel controller.
Its monitor, wordmark, channel counter, spacing, import action and footer use the
original Air markup/styles. The five channel buttons are replaced by one Radix
Select containing all built-in and imported effects, with scrolling and keyboard
typeahead. The settings icon opens the full existing settings flow, including the
game window, shape, 4:3, input mode, scaling/DAR, backend, shortcuts, restoration,
diagnostics and updates. Escape or the settings action hides the panel to tray.
The browser demo adds the prototype's outer frame; the native panel fills its window.
Game selection applies immediately; other configuration edits keep explicit Apply
semantics. Closing dialogs restores focus to their opener. The effect dropdown
opens without animation; theme colors change atomically, and only pointer press
feedback uses a short transform transition. Reduced motion disables it.
Preview can compare while the live overlay is off and invalidates obsolete pixels
when the draft changes. No intensity/manual parameter controls or extra Stop button
were added. System light/dark themes and reduced motion remain supported.

For a deterministic large-library browser check, run `npm run dev:ui` and open
`/tests/effect-picker-smoke.html`. This isolated fixture uses the real Select with
55 effects, a long Cyrillic name and a visible selection output; it never connects
to the engine and is not an entry in the release bundle.

Mona Sans VF v2.0.27 is bundled locally in `assets/fonts/mona-sans.woff2`.
Its provenance, pinned Git commit, SHA256 and unmodified upstream OFL notices
are next to the file. Mona Sans has no Cyrillic glyphs: Russian uses the system
SF/Segoe fallback. `third_party/frontend-provenance.json` adds the font to the
existing distribution license collector. No font service is contacted.

Local backups preserve earlier design variants and compiled output. They are not
part of the public repository or release package. Restore them to a separate
folder when comparing historical `desktop/ui` and `desktop/dist` designs.


The original 32 portable tests are preserved. Additional controller tests use
controlled deferred replies and a fake clock to check unmount/retry cleanup,
late source lists, fatal-state retention, draft edits during Apply, STOP during
Apply/preview, recovery errors, pacing, dimensions, late PNG cancellation, and browser timer receivers.
Portable tests and browser review do not qualify the Tauri/Go pipe, actual global
hotkeys, restoration, update installation, or the Windows compositor.
