import test from "node:test";
import assert from "node:assert/strict";
import { clone, emergencyTransition, canEditWithoutSource, hotkeyProbe, selectPreset, reconcileDraft, backendValue, sourceKey, sourceLabel, previewSeconds, previewConfig, powerConfig, fullEffectConfig, powerPresentation, selectSource, initialSourceKey, draftIssues, runtimePresentation, updatePresentation } from "../state.mjs";

const config = () => ({ enabled: true, mode: "full", input_mode: "mouse-exact", preset: "Custom", capture: { transfer: "compatibility" },
  target: { kind: "window", monitor: 0, window_title: "Game" }, screen: { shape: "rounded", corner_radius: .04, glass: .15, curvature: .18 },
  effects: { intensity: .37, noise_seed: 42, freeze_noise: true, crt: { scanlines: .1 }, vhs: { noise: .2 } },
  aspect: { enabled: false, method: "mask", scale: "fit", source_dar: 0 }, hotkeys: { toggle: "Ctrl+Alt+F9", emergency: "Ctrl+Alt+F10" } });
const presets = ["Subtle CRT", "CRT Classic", "Soft TV", "VHS Light", "VHS Tape"].map((name, index) => ({ name, effects: { intensity: 1, noise_seed: 1, freeze_noise: false, crt: { scanlines: index / 10 }, vhs: { noise: index / 8 } } }));

for (const preset of presets) test(`${preset.name} changes only its signal, preserves draft intensity / shape / target`, () => {
  const before = config(), original = clone(before);
  const next = selectPreset(before, presets, preset.name);
  assert.equal(next.preset, preset.name);
  assert.deepEqual(next.effects.crt, preset.effects.crt);
  assert.deepEqual(next.effects.vhs, preset.effects.vhs);
  assert.deepEqual({ ...next, preset: before.preset, effects: before.effects }, before);
  assert.deepEqual({ ...next.effects, crt: before.effects.crt, vhs: before.effects.vhs }, before.effects);
  next.effects.crt.scanlines = 99;
  assert.deepEqual(before, original);
  assert.notEqual(preset.effects.crt.scanlines, 99);
});
test("unknown preset fails without altering the draft; Custom preserves all effects", () => {
  const before = config();
  assert.throws(() => selectPreset(before, presets, "Missing"));
  assert.deepEqual(selectPreset(before, presets, "Custom"), before);
});
test("ordinary live toggle refresh retains pending edits but follows enabled state", () => {
  const applied = config(), pending = clone(applied), incoming = clone(applied);
  pending.effects.intensity = .6;
  pending.hotkeys.toggle = "Ctrl+Shift+9";
  incoming.enabled = false;
  const next = reconcileDraft(applied, pending, incoming);
  assert.equal(next.enabled, false);
  assert.equal(next.effects.intensity, .6);
  assert.equal(next.hotkeys.toggle, "Ctrl+Shift+9");
  assert.equal(pending.enabled, true);
  assert.deepEqual(reconcileDraft(applied, pending, incoming, true), incoming);
});
test("emergency sequence rejects stale snapshots and resets every draft field even on restoration failure", () => {
  const previous = { emergency_sequence: 2 }, live = config(), pending = clone(live);
  pending.aspect = { enabled: true, method: "system", scale: "fit", source_dar: 0 };
  pending.screen.shape = "convex";
  pending.input_mode = "keyboard-gamepad";
  pending.effects.intensity = .95;
  live.enabled = false;
  const incoming = { emergency_sequence: 3, config: live, runtime: { phase: "recovery-error" } };
  assert.equal(emergencyTransition(previous, incoming), "advanced");
  const next = reconcileDraft(config(), pending, incoming.config, emergencyTransition(previous, incoming) === "advanced");
  assert.deepEqual(next, live);
  assert.equal(next.aspect.enabled, false);
  assert.equal(emergencyTransition(incoming, previous), "stale");
  assert.equal(emergencyTransition(incoming, { ...incoming }), "same");
  assert.throws(() => emergencyTransition(previous, {}));
  assert.throws(() => emergencyTransition(previous, { emergency_sequence: -1 }));
});
test("early STOP event fences replies before its final snapshot arrives", () => {
  const shown = { emergency_sequence: 4 };
  const observed = 5;
  // These are valid snapshots, but both predate an already received STOP event.
  assert.equal(emergencyTransition(shown, { emergency_sequence: 4 }, observed), "stale");
  assert.equal(emergencyTransition(undefined, { emergency_sequence: 4 }, observed), "stale");
  assert.equal(emergencyTransition(shown, { emergency_sequence: 5 }, observed), "advanced");
  assert.equal(emergencyTransition(shown, { emergency_sequence: 6 }, observed), "advanced");
  assert.equal(emergencyTransition({ emergency_sequence: 6 }, { emergency_sequence: 5 }, 6), "stale");
});
test("no-Alt draft does not inherit the registered status or old receipts of active keys", () => {
  const current = { config: config(), hotkeys: { registered: true, toggle_received: 7, emergency_received: 3 } };
  const noAlt = { toggle: "Ctrl+Shift+9", emergency: "Ctrl+Shift+0" };
  let view = hotkeyProbe(current, noAlt);
  assert.equal(view.pending, true);
  assert.deepEqual(view.bindings, current.config.hotkeys);
  assert.equal(view.toggleDelta, 0);
  assert.equal(view.toggleTotal, 7);
  const applied = clone(current); applied.config.hotkeys = noAlt;
  view = hotkeyProbe(applied, noAlt, view.baseline);
  assert.equal(view.pending, false);
  assert.equal(view.toggleDelta, 0);
  assert.equal(view.emergencyDelta, 0);
  applied.hotkeys.toggle_received++;
  applied.hotkeys.emergency_received++;
  view = hotkeyProbe(applied, noAlt, view.baseline);
  assert.equal(view.toggleDelta, 1);
  assert.equal(view.emergencyDelta, 1);
});
test("hotkey probe never invents receipt and rebases after engine counters restart", () => {
  const current = { config: config(), hotkeys: { registered: false, toggle_received: 5, emergency_received: 3 } };
  const first = hotkeyProbe(current, current.config.hotkeys);
  current.hotkeys.toggle_received = current.hotkeys.emergency_received = 0;
  const restarted = hotkeyProbe(current, current.config.hotkeys, first.baseline);
  assert.equal(restarted.registered, false);
  assert.equal(restarted.toggleDelta, 0);
  assert.equal(restarted.emergencyDelta, 0);
  current.hotkeys = {};
  assert.equal(hotkeyProbe(current, current.config.hotkeys).toggleDelta, null);
  const unavailable = hotkeyProbe(current, current.config.hotkeys);
  current.hotkeys = { registered: true, toggle_received: 9, emergency_received: 2 };
  const available = hotkeyProbe(current, current.config.hotkeys, unavailable.baseline);
  assert.equal(available.toggleDelta, 0);
  assert.equal(available.emergencyDelta, 0);
});
test("missing game permits preference edits but never a new source/backend/window-system mutation", () => {
  const applied = config(), next = clone(applied);
  next.hotkeys = { toggle: "Ctrl+Shift+9", emergency: "Ctrl+Shift+0" };
  next.effects.intensity = .5;
  assert.equal(canEditWithoutSource(applied, next), true);
  next.aspect.enabled = true; next.aspect.method = "mask";
  assert.equal(canEditWithoutSource(applied, next), true);
  for (const method of ["window", "system"]) {
    next.aspect.method = method;
    assert.equal(canEditWithoutSource(applied, next), false);
  }
  next.aspect.enabled = false; next.mode = "overlay";
  assert.equal(canEditWithoutSource(applied, next), false);
  next.mode = applied.mode; next.capture.transfer = "gpu";
  assert.equal(canEditWithoutSource(applied, next), false);
  next.capture = clone(applied.capture); next.target.window_title = "Other";
  assert.equal(canEditWithoutSource(applied, next), false);
});
test("clean draft follows authoritative config and does not alias its response", () => {
  const before = config(), incoming = config(); incoming.effects.intensity = .2;
  const next = reconcileDraft(before, clone(before), incoming);
  assert.deepEqual(next, incoming);
  next.effects.intensity = .8;
  assert.equal(incoming.effects.intensity, .2);
});
const windows = [{ id: "first", hwnd: "0x123456789abcdef0", pid: 23, process_created: "134039488294079118", title: "Game" },
  { id: "second", hwnd: "0x123456789abcdef1", pid: 24, process_created: "134039488294079119", title: "Game" }];
const monitors = [{ index: 0 }, { index: 1 }];
test("duplicate window titles require exact identity, uint64 strings stay exact", () => {
  const sources = { windows, monitors };
  assert.equal(initialSourceKey(config(), sources, null), "");
  assert.equal(initialSourceKey(config(), sources, windows[1]), "second");
  const selected = selectSource(config(), windows, monitors, "second");
  assert.deepEqual(selected.source, windows[1]);
  assert.equal(selected.source.process_created, "134039488294079119");
  assert.equal(selected.source.hwnd, "0x123456789abcdef1");
});
test("stale process identity never falls back to a same-titled replacement", () => {
  const replacement = { ...windows[0], process_created: "134039488294079999" };
  assert.equal(initialSourceKey(config(), { windows: [replacement], monitors }, windows[0]), "");
  assert.throws(() => selectSource(config(), windows, monitors, "gone"));
  assert.notEqual(sourceKey({ ...windows[0], id: undefined }), sourceKey({ ...replacement, id: undefined }));
});
test("unique source titles stay simple even when windows share a process", () => {
  const another = { ...windows[0], id: "other", hwnd: "0x4444", title: "Другая игра" };
  const distinctTitles = [windows[0], another];
  assert.equal(sourceLabel(windows[0], distinctTitles), "Game");
  assert.equal(sourceLabel(another, distinctTitles), "Другая игра");
  assert.equal(sourceKey(another), "other");
  assert.equal(selectSource(config(), distinctTitles, monitors, "other").source, another);
});
test("duplicate source titles add only the identifiers needed to distinguish windows", () => {
  assert.equal(sourceLabel(windows[0], windows), "Game · PID 23");
  assert.equal(sourceLabel(windows[1], windows), "Game · PID 24");
  const another = { ...windows[0], id: "third", hwnd: "0x4444" };
  const duplicates = [...windows, another];
  assert.equal(sourceLabel(windows[0], duplicates), "Game · PID 23 · 0x123456789abcdef0");
  assert.equal(sourceLabel(another, duplicates), "Game · PID 23 · 0x4444");
  assert.equal(sourceLabel(windows[1], duplicates), "Game · PID 24");
  assert.equal(sourceLabel(another, [another]), "Game", "suffix disappears when competing windows close");
  assert.equal(sourceKey(another), "third");
});
test("preview time stays valid after an overnight settings session", () => {
  assert.equal(previewSeconds(12300), 12.3);
  assert.equal(previewSeconds(86400300), .3);
  assert.ok(previewSeconds(86400300 * 200) >= 0 && previewSeconds(86400300 * 200) < 86400);
});
test("desktop preview uses authored strength without mutating the legacy disabled profile", () => {
  const live = config(); live.enabled = false;
  const original = clone(live), preview = previewConfig(live);
  assert.equal(preview.enabled, true);
  assert.equal(preview.effects.intensity, 1);
  assert.deepEqual(preview.screen, live.screen);
  assert.deepEqual(live, original);
  live.effects.intensity = 0;
  assert.equal(previewConfig(live).effects.intensity, 1);
  assert.equal(live.effects.intensity, 0);
  preview.effects.crt.scanlines = .8;
  assert.equal(live.effects.crt.scanlines, .1);
});
test("desktop effect boundaries normalize only intensity and leave their input profile intact", () => {
  for (const intensity of [0, .5, 1]) {
    const saved = config(); saved.effects.intensity = intensity;
    saved.shader = { id: "old-shader", params: [1.7, .2, 0, 0, 0, 0, 0, 0] };
    const before = clone(saved);
    for (const transform of [powerConfig, fullEffectConfig, previewConfig]) {
      const request = transform(saved);
      assert.equal(request.effects.intensity, 1);
      assert.deepEqual(request.shader, saved.shader);
      assert.deepEqual(request.screen, saved.screen);
      assert.deepEqual(request.effects.crt, saved.effects.crt);
      assert.deepEqual(saved, before);
    }
  }
});
test("single power stops active format, pending work and recovery but not unapplied draft format", () => {
  const off = { snapshot: { config: { enabled: false, aspect: { enabled: false } }, runtime: { enabled: false, phase: "disabled" } }, draft: { aspect: { enabled: true } } };
  assert.deepEqual(powerPresentation(off), { shouldStop: false, stopping: false, label: "Включить" });
  for (const patch of [
    { startingEffect: true }, { livePending: true }, { shaderImporting: true }, { recoveryPending: true },
    { applied: { aspect: { enabled: true } } },
    { snapshot: { ...off.snapshot, confirmation_deadline: "2026-10-06T18:00:00Z" } },
    { snapshot: { ...off.snapshot, runtime: { enabled: true, phase: "waiting-source" } } },
    { snapshot: { ...off.snapshot, runtime: { enabled: false, phase: "recovery-error" } } },
    { snapshot: { ...off.snapshot, runtime: { enabled: false, unsaved: true } } },
  ]) assert.equal(powerPresentation({ ...off, ...patch }).shouldStop, true, JSON.stringify(patch));
  assert.deepEqual(powerPresentation({ ...off, stopping: true }), { shouldStop: true, stopping: true, label: "Выключается…" });
});
test("monitor choice clears stale window title and sends no source identity", () => {
  const result = selectSource(config(), windows, monitors, "monitor:1");
  assert.deepEqual(result.config.target, { kind: "monitor", monitor: 1, window_title: "" });
  assert.equal(result.source, null);
  assert.equal(initialSourceKey(result.config, { windows, monitors }), "monitor:1");
  assert.throws(() => selectSource(config(), windows, monitors, "monitor:99"));
});
test("backend selector distinguishes explicit compatibility from GPU", () => {
  const c = config(); assert.equal(backendValue(c), "full-compatibility");
  c.capture.transfer = "gpu"; assert.equal(backendValue(c), "full-gpu");
  c.mode = "overlay"; assert.equal(backendValue(c), "overlay");
});
test("exact mouse accepts rounded and mask but rejects Crop / DAR / convex without changing values", () => {
  const c = config(); c.aspect.enabled = true;
  assert.deepEqual(draftIssues(c), []);
  c.aspect.scale = "crop"; assert.match(draftIssues(c).join(" "), /точной мыши/);
  assert.equal(c.aspect.scale, "crop");
  c.aspect.scale = "fit"; c.aspect.source_dar = 4 / 3;
  assert.match(draftIssues(c).join(" "), /точной мыши/);
  c.aspect.source_dar = 0; c.screen.shape = "convex";
  assert.match(draftIssues(c).join(" "), /Выпуклый/);
  c.input_mode = "keyboard-gamepad"; assert.deepEqual(draftIssues(c), []);
  c.mode = "overlay"; assert.match(draftIssues(c).join(" "), /Выпуклый/);
});
test("missing source / monitor Full / duplicate hotkeys block Apply", () => {
  const c = config(); assert.match(draftIssues(c, false).join(" "), /Выберите/);
  c.target.kind = "monitor"; assert.match(draftIssues(c).join(" "), /Full/);
  c.mode = "overlay"; c.aspect.enabled = true; c.aspect.method = "window";
  assert.match(draftIssues(c).join(" "), /клиентского окна/);
  c.aspect.enabled = false; c.hotkeys.emergency = c.hotkeys.toggle.toLowerCase();
  assert.match(draftIssues(c).join(" "), /разные сочетания/);
});
test("unknown runtime cannot look active; known pause remains neutral", () => {
  assert.equal(runtimePresentation({}).tone, "muted");
  assert.notEqual(runtimePresentation({}).label, "Работает");
  assert.equal(runtimePresentation({ phase: "active" }).tone, "active");
  assert.equal(runtimePresentation({ phase: "paused-focus", reason: "not foreground" }).reason, "not foreground");
});
test("updates require an actual available version, disable network action when unconfigured/busy", () => {
  for (const state of ["unconfigured", "checking", "downloading", "installing"]) {
    const view = updatePresentation({ state, available_version: "0.3.0" });
    assert.equal(view.canCheck, false); assert.equal(view.canInstall, false);
  }
  assert.equal(updatePresentation({ state: "available" }).canInstall, false);
  assert.equal(updatePresentation({ state: "available", available_version: "0.3.0" }).canInstall, true);
  assert.equal(updatePresentation({ state: "idle", available_version: "0.3.0" }).canInstall, false);
});

test("new Auto window presents its Power backend without migrating saved config or promising Full monitor capture", () => {
  const value = config(); value.mode = "overlay"; value.capture.transfer = "auto";
  const original = clone(value);
  assert.equal(backendValue(value), "full-auto");
  assert.deepEqual(value, original);
  value.target.kind = "monitor"; assert.equal(backendValue(value), "overlay");
  value.target.kind = "window"; value.capture.transfer = "gpu"; assert.equal(backendValue(value), "overlay");
});
