import test from "node:test";
import assert from "node:assert/strict";
import { createController } from "../controller.mjs";
import { powerPresentation } from "../state.mjs";
import { request as fixture } from "../demo.mjs";

const original = await fixture("snapshot");
const sources = await fixture("sources");
// Existing-window baseline for lifecycle tests. Browser demo starts from a
// disabled monitor profile; dedicated launch tests below cover that workflow.
original.config.mode = "full"; original.config.capture.transfer = "compatibility"; original.config.enabled = original.runtime.enabled = true;
original.config.target = { kind: "window", monitor: 0, window_title: sources.windows[0].title };
original.source = structuredClone(sources.windows[0]);
const copy = (value) => structuredClone(value);
const settle = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function rig() {
  let actual = copy(original), clock = 0, nextID = 0;
  const events = new Map(), timers = new Map(), intervals = new Map(), delays = new Map(), calls = [];
  const stopCalls = [];
  const api = {
    demoMode: false,
    async subscribe(name, handler) { events.set(name, handler); return () => { events.delete(name); stopCalls.push(name); }; },
    async request(type, payload) {
      calls.push({ type, payload: copy(payload), time: clock });
      const pending = delays.get(type)?.shift();
      if (pending) return pending.promise;
      if (type === "snapshot") return copy(actual);
      if (type === "sources") return copy(sources);
      if (type === "shaders") return { items: [] };
      if (type === "preview") return { mime: "image/png", png_base64: `frame-${calls.length}` };
      if (type === "apply") { assert.equal(payload.expected_emergency_sequence, actual.emergency_sequence); actual.config = copy(payload.config); actual.runtime.enabled = actual.config.enabled; return copy(actual); }
      if (type === "disable") { actual.config.enabled = actual.runtime.enabled = false; return copy(actual); }
      if (type === "emergency") { api.stop(); return copy(actual); }
      throw new Error(`unexpected test request ${type}`);
    },
    stop() { actual.emergency_sequence++; actual.config.enabled = actual.runtime.enabled = false; actual.config.aspect.enabled = false; actual.runtime.phase = "disabled"; },
    async updaterStatus() { return { state: "unconfigured" }; },
    async hostStatus() { return { tray_available: true, tray_error: null }; },
  };
  const controller = createController(api, {
    now: () => clock, hidden: () => false,
    setTimeout(fn, delay) { const id = ++nextID; timers.set(id, { fn, at: clock + delay }); return id; },
    clearTimeout(id) { timers.delete(id); },
    setInterval(fn, delay) { const id = ++nextID; intervals.set(id, { fn, delay }); return id; },
    clearInterval(id) { intervals.delete(id); },
  });
  return { api, controller, events, timers, intervals, calls, stopCalls,
    actual: () => copy(actual), setActual: (value) => { actual = copy(value); },
    defer(type) { const request = deferred(); delays.set(type, [...delays.get(type) || [], request]); return request; },
    timer() { const [id, timer] = [...timers].sort((a, b) => a[1].at - b[1].at)[0] || []; assert.ok(timer, "expected a scheduled timer"); timers.delete(id); clock = timer.at; return timer.fn(); },
    emit(event) { events.get("engine_event")?.(event); },
  };
}

test("React store lifecycle removes all subscriptions/timers and ignores disposed callbacks", async () => {
  const r = rig(); await r.controller.start();
  assert.equal(r.events.size, 4);
  assert.equal(r.intervals.size, 1);
  let renders = 0; const unsubscribe = r.controller.subscribe(() => renders++);
  r.controller.edit("effects.intensity", .5);
  assert.ok(renders > 0);
  unsubscribe(); const renderCount = renders;
  const obsoleteEvent = r.events.get("engine_event");
  r.controller.dispose();
  assert.equal(r.events.size, 0); assert.equal(r.timers.size, 0); assert.equal(r.intervals.size, 0);
  const disposed = r.controller.getSnapshot();
  obsoleteEvent({ event: "state_changed", data: { emergency_sequence: 9 } });
  assert.equal(r.controller.getSnapshot(), disposed);
  assert.equal(renders, renderCount);
  await r.controller.start();
  assert.equal(r.events.size, 4); assert.equal(r.intervals.size, 1);
  r.api.stop(); r.emit({ event: "state_changed", data: { emergency_sequence: 1 } }); await settle();
  assert.equal(r.controller.getSnapshot().snapshot.emergency_sequence, 1);
  r.controller.dispose(); assert.equal(r.stopCalls.length, 8);
});

test("startup listener resolving after unmount is removed rather than leaked", async () => {
  const pending = deferred(); let removed = 0;
  const controller = createController({ subscribe: () => pending.promise }, {});
  const start = controller.start(); controller.dispose();
  pending.resolve(() => removed++); await start;
  assert.equal(removed, 1);
  assert.equal(controller.getSnapshot().connected, false);
});

test("Apply preserves edits made while awaiting acknowledgement, including exact source identity", async () => {
  const r = rig(); await r.controller.start();
  r.controller.edit("effects.vhs.noise", .4);
  const delayed = r.defer("apply"); const apply = r.controller.apply();
  const sent = r.calls.findLast((item) => item.type === "apply").payload;
  assert.equal(sent.expected_emergency_sequence, 0);
  assert.equal(sent.source.process_created, "1");
  r.controller.edit("effects.vhs.noise", .6);
  const response = r.actual(); response.config = sent.config;
  delayed.resolve(response); await apply;
  assert.equal(r.controller.getSnapshot().applied.effects.vhs.noise, .4);
  assert.equal(r.controller.getSnapshot().draft.effects.vhs.noise, .6);
  assert.equal(r.controller.getSnapshot().busy, false);
  r.controller.dispose();
});

test("early external STOP fences in-flight Apply and PNG, resetting the entire dirty draft", async () => {
  const r = rig(); await r.controller.start();
  r.controller.setPreviewOpen(true);
  await r.timer(); // establish original preview
  const png = r.defer("preview"); const rendering = r.timer();
  r.controller.edit("effects.intensity", .7); r.controller.aspect("system");
  const delayed = r.defer("apply"); const apply = r.controller.apply();
  const old = r.actual(); old.config = copy(r.calls.findLast((item) => item.type === "apply").payload.config);
  r.api.stop(); r.emit({ event: "state_changed", data: { emergency_sequence: 1 } }); await settle();
  let state = r.controller.getSnapshot();
  assert.equal(state.draft.aspect.enabled, false);
  assert.equal(state.draft.effects.intensity, original.config.effects.intensity);
  assert.equal(state.draft.enabled, false); assert.equal(state.busy, false);
  assert.equal(state.preview.after, null);
  delayed.resolve(old); png.resolve({ mime: "image/png", png_base64: "obsolete-image" });
  await Promise.all([apply, rendering]);
  state = r.controller.getSnapshot();
  assert.equal(state.snapshot.emergency_sequence, 1);
  assert.equal(state.draft.enabled, false); assert.equal(state.draft.aspect.enabled, false);
  assert.equal(state.preview.after, null); assert.match(state.notice.message, /аварийная команда/);
  r.controller.dispose();
});

test("UI STOP bypasses Apply busy and retains restoration failure instead of claiming success", async () => {
  const r = rig(); await r.controller.start();
  r.controller.aspect("system");
  const delayed = r.defer("apply"); const apply = r.controller.apply();
  assert.equal(r.controller.getSnapshot().busy, true);
  r.api.stop();
  const failed = r.actual(); failed.runtime.phase = "recovery-error"; failed.runtime.last_error = "restore failed";
  r.setActual(failed);
  const stopReply = r.defer("emergency"); const stop = r.controller.emergency();
  stopReply.reject(Object.assign(new Error("restore failed"), { snapshot: failed, applied: false }));
  await stop;
  delayed.resolve(original); await apply;
  assert.equal(r.calls.filter((item) => item.type === "emergency").length, 1);
  const state = r.controller.getSnapshot();
  assert.equal(state.draft.aspect.enabled, false);
  assert.equal(state.snapshot.runtime.phase, "recovery-error");
  assert.match(state.notice.message, /restore failed/); assert.equal(state.notice.tone, "warning");
  r.controller.dispose();
});

test("preview stays single-outstanding, bounded and paced, and ignores post-unmount PNG", async () => {
  const r = rig(); await r.controller.start();
  r.controller.setPreviewOpen(true);
  const held = r.defer("preview"); const rendering = r.timer();
  const first = r.calls.find((item) => item.type === "preview");
  assert.equal(first.payload.width, 640); assert.equal(first.payload.height, 360); assert.equal(first.payload.before, true);
  r.controller.edit("effects.intensity", .3); await r.timer();
  assert.equal(r.calls.filter((item) => item.type === "preview").length, 1);
  held.resolve({ mime: "image/png", png_base64: "old-before" }); await rendering;
  assert.equal(r.controller.getSnapshot().preview.before, null); // edited after request
  await r.timer();
  const previews = r.calls.filter((item) => item.type === "preview");
  assert.ok(previews[1].time - previews[0].time >= 510);
  const late = r.defer("preview"); const pending = r.timer();
  r.controller.dispose(); const disposed = r.controller.getSnapshot();
  late.resolve({ mime: "image/png", png_base64: "unmounted" }); await pending;
  assert.equal(r.controller.getSnapshot(), disposed);
  assert.equal(r.timers.size, 0);
});

test("switching a builtin clears its old after frame and fences the outstanding PNG without resetting before", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; r.setActual(initial);
  await r.controller.start(); r.controller.setPreviewOpen(true);
  await r.timer(); await r.timer();
  const before = r.controller.getSnapshot().preview.before;
  assert.ok(before); assert.ok(r.controller.getSnapshot().preview.after);
  const held = r.defer("preview"), rendering = r.timer();
  const outstanding = r.calls.filter((call) => call.type === "preview").length;
  r.controller.livePreset("CRT Classic");
  assert.equal(r.controller.getSnapshot().preview.after, null);
  assert.equal(r.controller.getSnapshot().preview.before, before);
  await r.timer();
  assert.equal(r.calls.filter((call) => call.type === "preview").length, outstanding, "no concurrent preview during a change");
  held.resolve({ mime: "image/png", png_base64: "obsolete-tape" }); await rendering;
  assert.equal(r.controller.getSnapshot().preview.after, null, "old preset completion cannot restore the stale image");
  await r.timer();
  const requests = r.calls.filter((call) => call.type === "preview"), newest = requests.at(-1);
  assert.equal(newest.payload.config.preset, "CRT Classic"); assert.equal(newest.payload.before, false);
  assert.ok(newest.time - requests.at(-2).time >= 510, "replacement remains rate limited");
  assert.equal(r.controller.getSnapshot().preview.after, `data:image/png;base64,frame-${r.calls.length}`);
  assert.equal(r.controller.getSnapshot().preview.before, before);
  assert.equal(r.calls.filter((call) => call.type === "apply").length, 0, "a preview change never enables the game");
  r.controller.dispose();
});

test("snapshot shape changes invalidate after, but runtime and normalized non-rendering preferences retain it", async () => {
  const r = rig(); await r.controller.start(); r.controller.setPreviewOpen(true);
  await r.timer(); await r.timer();
  const cached = copy(r.controller.getSnapshot().preview);
  const preferences = r.actual(); preferences.config.enabled = preferences.runtime.enabled = false;
  preferences.config.effects.intensity = 0; preferences.config.hotkeys.toggle = "Ctrl+Shift+9";
  preferences.config.capture.transfer = "gpu"; preferences.runtime.phase = "disabled";
  r.setActual(preferences); await r.controller.refreshStatus();
  assert.deepEqual(r.controller.getSnapshot().preview, cached, "same effective synthetic render retains its frame");
  const shape = r.actual(), nextShape = shape.config.screen.shape === "flat" ? "rounded" : "flat";
  shape.config.screen.shape = nextShape; r.setActual(shape);
  await r.controller.refreshStatus();
  assert.equal(r.controller.getSnapshot().preview.after, null);
  assert.equal(r.controller.getSnapshot().preview.before, cached.before);
  for (let i = 0; i < 3 && !r.controller.getSnapshot().preview.after; i++) await r.timer();
  assert.ok(r.controller.getSnapshot().preview.after);
  const rendered = r.calls.findLast((call) => call.type === "preview").payload;
  assert.equal(rendered.config.screen.shape, nextShape); assert.equal(rendered.config.effects.intensity, 1);
  assert.equal(rendered.before, false);
  const after = r.controller.getSnapshot().preview.after;
  r.controller.edit("hotkeys.emergency", "Ctrl+Shift+0");
  assert.equal(r.controller.getSnapshot().preview.after, after, "local non-rendering edits keep a valid image");
  r.controller.edit("screen.glass", .4);
  assert.equal(r.controller.getSnapshot().preview.after, null, "local rendering edits clear the image");
  r.controller.dispose();
});

test("an Advanced ACK invalidates a preview rendered from newer signal edits and the previous shape", async () => {
  const r = rig(); await r.controller.start(); r.controller.setPreviewOpen(true);
  await r.timer(); await r.timer();
  const before = r.controller.getSnapshot().preview.before;
  const baseline = copy(r.controller.getSnapshot().draft), edited = copy(baseline);
  edited.screen.shape = baseline.screen.shape === "flat" ? "rounded" : "flat";
  const held = r.defer("apply"), apply = r.controller.applyAdvanced(baseline, edited);
  r.controller.liveEdit("effects.vhs.noise", .31);
  for (let i = 0; i < 4 && !r.controller.getSnapshot().preview.after; i++) await r.timer();
  assert.ok(r.controller.getSnapshot().preview.after, "preview completes while Advanced acknowledgement is pending");
  const rendered = r.calls.findLast((call) => call.type === "preview").payload.config;
  assert.equal(rendered.screen.shape, baseline.screen.shape);
  assert.equal(rendered.effects.vhs.noise, .31, "the frame contains edits made after dispatch, not the earlier cached signal");
  const response = r.actual(); response.config = copy(r.calls.findLast((call) => call.type === "apply").payload.config);
  held.resolve(response); await apply;
  assert.equal(r.controller.getSnapshot().draft.screen.shape, edited.screen.shape);
  assert.equal(r.controller.getSnapshot().draft.effects.vhs.noise, .31);
  assert.equal(r.controller.getSnapshot().preview.after, null, "rebased shape replaces the frame rendered before ACK");
  assert.equal(r.controller.getSnapshot().preview.before, before);
  r.controller.dispose();
});

test("retry installs fresh event handlers and no-source preferences do not fabricate a source", async () => {
  const r = rig(); const initialFailure = r.defer("snapshot");
  initialFailure.reject(new Error("connection interrupted"));
  await r.controller.start();
  assert.equal(r.controller.getSnapshot().connected, false);
  await r.controller.start(); assert.equal(r.events.size, 4); assert.equal(r.stopCalls.length, 4);
  const sourceReply = r.defer("sources"); const refresh = r.controller.refreshSources();
  sourceReply.resolve({ windows: [], monitors: [] }); await refresh;
  r.controller.edit("hotkeys.toggle", "Ctrl+Shift+9"); await r.controller.apply();
  const sent = r.calls.findLast((item) => item.type === "apply").payload;
  assert.equal(sent.source, undefined); assert.equal(sent.config.target.window_title, original.config.target.window_title);
  r.controller.dispose();
});

test("fatal during delayed startup remains terminal until explicit reconnect", async () => {
  const r = rig(), reply = r.defer("snapshot");
  const starting = r.controller.start(); await settle();
  r.emit({ event: "fatal", data: { message: "engine died" } });
  reply.resolve(original); await starting;
  assert.equal(r.controller.getSnapshot().connected, false);
  assert.equal(r.controller.getSnapshot().connectionError, "engine died");
  assert.equal(r.controller.getSnapshot().snapshot, null);
  assert.equal(r.events.size, 0); assert.equal(r.timers.size, 0);
  await r.controller.start(); assert.equal(r.controller.getSnapshot().connected, true);
  r.controller.dispose();
});

test("late source enumeration cannot replace a newer list or a selection edited during startup", async () => {
  const r = rig(); await r.controller.start();
  const old = r.defer("sources"), latest = r.defer("sources");
  const first = r.controller.refreshSources(), second = r.controller.refreshSources();
  const fresh = copy(sources); fresh.monitors.push({ ...fresh.monitors[0], index: 1 });
  latest.resolve(fresh); await second; old.resolve(sources); await first;
  assert.equal(r.controller.getSnapshot().sources.monitors.length, 2);
  const startupList = r.defer("sources"); const restarting = r.controller.start(); await settle();
  r.controller.source("monitor:0");
  startupList.resolve(sources); await restarting;
  assert.equal(r.controller.getSnapshot().selectedKey, "monitor:0");
  assert.equal(r.controller.getSnapshot().draft.target.kind, "monitor");
  r.controller.dispose();
});

test("default timer adapters retain the browser-global receiver required by WebKit", async () => {
  const keys = ["setTimeout", "clearTimeout", "setInterval", "clearInterval"];
  const saved = Object.fromEntries(keys.map((key) => [key, globalThis[key]]));
  const calls = [];
  let controller;
  try {
    for (const key of keys) globalThis[key] = function () {
      assert.equal(this, globalThis, `${key} must not use the controller env as Window`);
      calls.push(key);
      return calls.length;
    };
    controller = createController({
      subscribe: async () => () => {},
      request: async (type) => copy(type === "snapshot" ? original : sources),
      updaterStatus: async () => ({ state: "unconfigured" }),
    });
    await controller.start();
    assert.equal(controller.getSnapshot().connected, true);
    controller.dispose();
    for (const key of keys) assert.ok(calls.includes(key), key);
  } finally {
    controller?.dispose();
    for (const key of keys) globalThis[key] = saved[key];
  }
});

test("desktop preview labels the before frame and renders legacy intensity at authored strength", async () => {
  const r = rig(), request = r.api.request;
  r.api.request = async (...args) => {
    const value = await request(...args);
    return args[0] === "preview" ? { ...value, fixture: true } : value;
  };
  await r.controller.start(); r.controller.setPreviewOpen(true); await r.timer();
  assert.equal(r.controller.getSnapshot().preview.feedback, "Исходный кадр · без эффекта");
  r.controller.edit("effects.intensity", 0);
  await r.timer(); await r.timer(); // debounce, then minimum preview spacing
  assert.equal(r.controller.getSnapshot().preview.feedback, "Готовый GLSL-пример · 100%");
  assert.equal(r.calls.findLast((call) => call.type === "preview").payload.config.effects.intensity, 1);
  r.controller.edit("effects.intensity", .5);
  await r.timer(); await r.timer();
  assert.equal(r.controller.getSnapshot().preview.feedback, "Готовый GLSL-пример · 100%");
  r.controller.dispose();
});

test("Start atomically sends the exact window and selected preset at authored strength", async () => {
  const r = rig(), disabled = r.actual(); disabled.config.enabled = disabled.runtime.enabled = false;
  disabled.config.mode = "overlay"; disabled.config.target = { kind: "monitor", monitor: 0, window_title: "" }; disabled.source = null;
  r.setActual(disabled); await r.controller.start();
  assert.equal(r.controller.getSnapshot().previewOpen, false);
  r.controller.source(sources.windows[0].id); r.controller.preset("VHS Light"); r.controller.edit("effects.intensity", .45);
  r.controller.backend("full-compatibility");
  const draft = copy(r.controller.getSnapshot().draft);
  const delayed = r.defer("apply"), starting = r.controller.startEffect();
  assert.equal(r.controller.getSnapshot().draft.enabled, false, "request copy must not pretend live enable before ACK");
  const mutations = r.calls.filter((call) => ["apply", "toggle", "disable"].includes(call.type));
  assert.equal(mutations.length, 1); assert.equal(mutations[0].type, "apply");
  const payload = mutations[0].payload;
  assert.deepEqual(payload.config, { ...draft, enabled: true, effects: { ...draft.effects, intensity: 1 } });
  assert.deepEqual(payload.source, sources.windows[0]); assert.equal(payload.expected_emergency_sequence, 0);
  const actual = r.actual(); actual.config = payload.config; actual.runtime.enabled = true;
  delayed.resolve(actual); await starting;
  assert.equal(r.controller.getSnapshot().applied.enabled, true);
  assert.match(r.controller.getSnapshot().notice.message, /Эффект включён/);
  r.controller.dispose();
});

test("disabled monitor profile stays unchanged and cannot start without explicit advanced monitor intent", async () => {
  const r = rig(), disabled = r.actual(); disabled.config.enabled = disabled.runtime.enabled = false;
  disabled.config.mode = "overlay"; disabled.config.target = { kind: "monitor", monitor: 0, window_title: "" }; disabled.source = null;
  const request = r.api.request; r.api.request = async (...args) => args[0] === "sources" ? { windows: [], monitors: copy(sources.monitors) } : request(...args);
  r.setActual(disabled); await r.controller.start(); await r.controller.startEffect();
  assert.deepEqual(r.controller.getSnapshot().draft, disabled.config);
  assert.equal(r.calls.filter((call) => call.type === "apply").length, 0);
  assert.match(r.controller.getSnapshot().notice.message, /Выберите окно игры/);
  r.controller.source("monitor:0"); await r.controller.startEffect();
  assert.equal(r.calls.filter((call) => call.type === "apply").length, 1);
  assert.equal(r.controller.getSnapshot().applied.target.kind, "monitor");
  await r.controller.emergency();
  assert.equal(r.controller.getSnapshot().monitorIntent, false);
  r.controller.dispose();
});

test("known unavailable GPU blocks Start and unknown GPU rejection preserves draft with explicit CPU choice", async () => {
  const r = rig(); await r.controller.start(); r.controller.backend("full-gpu");
  await r.controller.startEffect(); assert.equal(r.calls.filter((call) => call.type === "apply").length, 0);
  const unproven = r.actual(); unproven.runtime.gpu = { state: "unknown" }; r.setActual(unproven); await r.controller.refreshStatus();
  const delayed = r.defer("apply"), starting = r.controller.startEffect();
  const failed = r.actual(); failed.runtime.gpu = { state: "unavailable", code: "gpu_interop_unavailable" };
  delayed.reject(Object.assign(new Error("WGL_NV_DX_interop2 unavailable"), { code: "gpu_interop_unavailable", suggested_backend: "full-compatibility", snapshot: failed }));
  await starting;
  let state = r.controller.getSnapshot();
  assert.equal(state.applied.capture.transfer, "compatibility");
  assert.equal(state.draft.capture.transfer, "gpu");
  assert.equal(state.applyError.suggestedBackend, "full-compatibility");
  assert.equal(state.applyError.message, "Обработка через GPU недоступна.");
  assert.equal(state.applyError.details, "WGL_NV_DX_interop2 unavailable");
  assert.equal(state.notice, null);
  r.controller.backend("full-compatibility");
  assert.equal(r.calls.filter((call) => call.type === "apply").length, 1, "choosing CPU must not launch it");
  await r.controller.startEffect();
  assert.equal(r.calls.filter((call) => call.type === "apply").length, 2);
  assert.equal(r.calls.findLast((call) => call.type === "apply").payload.config.capture.transfer, "compatibility");
  r.controller.dispose();
});

test("explicit disable is idempotent with stale enabled status and retains unsaved effects/format", async () => {
  const r = rig(); await r.controller.start(); r.controller.edit("effects.intensity", .27); r.controller.aspect("window");
  const externallyDisabled = r.actual(); externallyDisabled.config.enabled = externallyDisabled.runtime.enabled = false; r.setActual(externallyDisabled);
  await r.controller.disable(); await r.controller.disable();
  assert.equal(r.calls.filter((call) => call.type === "disable").length, 2);
  assert.equal(r.calls.filter((call) => ["apply", "toggle"].includes(call.type)).length, 0);
  const state = r.controller.getSnapshot();
  assert.equal(state.draft.enabled, false); assert.equal(state.draft.effects.intensity, .27); assert.equal(state.draft.aspect.enabled, true);
  assert.equal(state.applied.aspect.enabled, false); assert.equal(state.snapshot.emergency_sequence, 0);
  r.controller.dispose();
});

test("Start normalizes legacy zero and STOP fences its delayed enabled acknowledgement", async () => {
  const r = rig(), disabled = r.actual(); disabled.config.enabled = disabled.runtime.enabled = false; r.setActual(disabled);
  await r.controller.start(); r.controller.edit("effects.intensity", 0); await r.controller.startEffect();
  assert.equal(r.controller.getSnapshot().applied.effects.intensity, 1);
  assert.match(r.controller.getSnapshot().notice.message, /Эффект включён/);
  r.controller.edit("effects.intensity", .8); r.controller.aspect("window");
  const delayed = r.defer("apply"), starting = r.controller.startEffect();
  const late = r.actual(); late.config = r.calls.findLast((call) => call.type === "apply").payload.config;
  await r.controller.emergency(); delayed.resolve(late); await starting;
  assert.equal(r.controller.getSnapshot().draft.enabled, false); assert.equal(r.controller.getSnapshot().draft.aspect.enabled, false);
  assert.equal(r.controller.getSnapshot().snapshot.emergency_sequence, 1);
  r.controller.dispose();
});

test("collapsed test scene makes no preview requests and opening retains bounded shader preview", async () => {
  const r = rig(); await r.controller.start(); await r.timer(); await r.timer();
  assert.equal(r.calls.filter((call) => call.type === "preview").length, 0);
  r.controller.setPreviewOpen(true); await r.timer();
  assert.equal(r.calls.filter((call) => call.type === "preview").length, 1);
  r.controller.setPreviewOpen(false); await r.timer();
  assert.equal(r.calls.filter((call) => call.type === "preview").length, 1);
  r.controller.dispose();
});

test("an already-running monitor profile can be disabled and resumed without changing its saved target", async () => {
  const r = rig(), monitor = r.actual();
  monitor.config.mode = "overlay"; monitor.config.target = { kind: "monitor", monitor: 0, window_title: "" }; monitor.source = null;
  r.setActual(monitor); await r.controller.start();
  assert.equal(r.controller.getSnapshot().monitorIntent, true);
  await r.controller.disable(); await r.controller.startEffect();
  assert.equal(r.calls.filter((call) => call.type === "apply").length, 1);
  assert.equal(r.controller.getSnapshot().applied.target.kind, "monitor");
  assert.equal(r.controller.getSnapshot().applied.enabled, true);
  r.controller.dispose();
});

test("target-specific failed capability permits deliberate retry; only unavailable blocks it", async () => {
  for (const transfer of ["gpu", "compatibility"]) {
    const r = rig(), failed = r.actual();
    failed.config.capture.transfer = transfer;
    failed.runtime[transfer === "gpu" ? "gpu" : "compatibility"] = { state: "failed", reason: "previous window closed during Open" };
    r.setActual(failed); await r.controller.start(); await r.controller.startEffect();
    assert.equal(r.calls.filter((call) => call.type === "apply").length, 1, `${transfer} retry should reach engine`);
    r.controller.dispose();
  }
});

const applyCalls = (r) => r.calls.filter((item) => item.type === "apply");
async function runUntilApply(r, count) {
  for (let i = 0; i < 8 && applyCalls(r).length < count; i++) { void r.timer(); await settle(); }
  assert.equal(applyCalls(r).length, count);
}

test("live signal edits coalesce and serialize only the latest intent after an in-flight acknowledgement", async () => {
  const r = rig(); await r.controller.start();
  r.controller.liveEdit("effects.vhs.noise", .2);
  r.controller.liveEdit("effects.vhs.noise", .3);
  const held = r.defer("apply"); await runUntilApply(r, 1);
  assert.equal(applyCalls(r)[0].payload.config.effects.vhs.noise, .3);
  r.controller.liveEdit("effects.vhs.noise", .4); r.controller.liveEdit("effects.vhs.noise", .5);
  void r.timer(); await settle(); void r.timer(); await settle();
  assert.equal(applyCalls(r).length, 1, "never send concurrent live applies");
  const response = r.actual(); response.config = applyCalls(r)[0].payload.config;
  held.resolve(response); await settle(); await runUntilApply(r, 2);
  assert.equal(applyCalls(r)[1].payload.config.effects.vhs.noise, .5);
  assert.equal(r.controller.getSnapshot().applied.effects.vhs.noise, .5);
  assert.equal(r.controller.getSnapshot().livePending, false);
  r.controller.dispose();
});

test("STOP cancels queued live edits and fences an older in-flight apply", async () => {
  const r = rig(); await r.controller.start();
  r.controller.liveEdit("effects.intensity", .2);
  const held = r.defer("apply"); await runUntilApply(r, 1);
  r.controller.liveEdit("effects.intensity", .9);
  await r.controller.emergency();
  held.resolve(original); await settle();
  for (let i = 0; i < 3; i++) { void r.timer(); await settle(); }
  assert.equal(applyCalls(r).length, 1);
  assert.equal(r.controller.getSnapshot().draft.enabled, false);
  assert.equal(r.controller.getSnapshot().snapshot.emergency_sequence, 1);
  assert.equal(r.controller.getSnapshot().livePending, false);
  r.controller.dispose();
});

test("Off cancels debounce and edits made while Off acknowledgement is pending cannot turn the effect back on", async () => {
  const r = rig(); await r.controller.start();
  r.controller.liveEdit("effects.intensity", .2);
  const held = r.defer("disable"), off = r.controller.disable();
  r.controller.liveEdit("effects.intensity", .8);
  const response = r.actual(); response.config.enabled = response.runtime.enabled = false;
  held.resolve(response); await off;
  for (let i = 0; i < 3; i++) { void r.timer(); await settle(); }
  assert.equal(applyCalls(r).length, 0);
  assert.equal(r.controller.getSnapshot().draft.effects.intensity, .8);
  assert.equal(r.controller.getSnapshot().draft.enabled, false);
  r.controller.dispose();
});

test("a rejected advanced change never contaminates subsequent live signal changes", async () => {
  const r = rig(); await r.controller.start();
  const originalDraft = copy(r.controller.getSnapshot().draft), edited = copy(originalDraft);
  edited.aspect = { ...edited.aspect, enabled: true, method: "system" };
  const held = r.defer("apply"), save = r.controller.applyAdvanced(originalDraft, edited);
  held.reject(Object.assign(new Error("CDS_TEST rejected"), { code: "apply_failed", snapshot: r.actual(), applied: false }));
  await save;
  assert.deepEqual(r.controller.getSnapshot().draft.aspect, originalDraft.aspect);
  r.controller.liveEdit("effects.intensity", .42); await runUntilApply(r, 2);
  assert.deepEqual(applyCalls(r)[1].payload.config.aspect, originalDraft.aspect);
  r.controller.dispose();
});

test("startup suggests one exact game in the off draft without mutating saved config or capturing", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false;
  initial.config.mode = "overlay"; initial.config.capture.transfer = "auto"; initial.config.target = { kind: "monitor", monitor: 0 }; initial.source = null;
  r.setActual(initial); await r.controller.start();
  const state = r.controller.getSnapshot();
  assert.equal(state.draft.target.kind, "window"); assert.equal(state.applied.target.kind, "monitor");
  assert.equal(state.selectedKey, sources.windows[0].id);
  assert.equal(applyCalls(r).length, 0);
  await r.controller.startEffect();
  assert.equal(applyCalls(r)[0].payload.config.mode, "full");
  assert.equal(applyCalls(r)[0].payload.config.capture.transfer, "auto");
  assert.deepEqual(applyCalls(r)[0].payload.source, sources.windows[0]);
  r.controller.dispose();
});

test("suggested foreground never silently switches an active exact source", async () => {
  const r = rig(), other = { ...sources.windows[0], id: "other", hwnd: "0x99", pid: 99, title: "Other game" }, request = r.api.request;
  r.api.request = async (type, payload) => type === "sources" ? { windows: [...sources.windows, other], monitors: sources.monitors, suggested: other } : request(type, payload);
  await r.controller.start(); await r.controller.refreshSources();
  assert.equal(r.controller.getSnapshot().selectedKey, sources.windows[0].id); assert.equal(applyCalls(r).length, 0);
  r.controller.dispose();
});

const descriptor = { id: "shader-123", version: 1, name: "Warm", description: "Warm colors", coordinates: "preserve", parameters: [{ name: "Warmth", min: 0, max: 2, step: .1, default: .5 }] };
test("legacy zero and partial profiles are read-only on connect, but desktop preview and Start use full strength", async () => {
  for (const intensity of [0, .5]) {
    const r = rig(), legacy = r.actual();
    legacy.config.enabled = legacy.runtime.enabled = false;
    legacy.config.effects.intensity = intensity;
    legacy.config.shader = { id: descriptor.id, params: [1.7, 0, 0, 0, 0, 0, 0, 0] };
    const saved = copy(legacy.config);
    r.setActual(legacy); await r.controller.start();
    assert.equal(applyCalls(r).length, 0);
    assert.deepEqual(r.controller.getSnapshot().applied, saved);
    assert.deepEqual(r.controller.getSnapshot().draft, saved);
    r.controller.setPreviewOpen(true); await r.timer(); await r.timer();
    assert.ok(r.calls.filter((call) => call.type === "preview").length >= 2);
    for (const call of r.calls.filter((call) => call.type === "preview")) {
      assert.equal(call.payload.config.effects.intensity, 1);
      assert.deepEqual(call.payload.config.shader.params, saved.shader.params);
    }
    assert.deepEqual(r.actual().config, saved, "preview never persists normalization");
    await r.controller.startEffect();
    const sent = applyCalls(r)[0].payload;
    assert.equal(sent.config.effects.intensity, 1);
    assert.deepEqual(sent.config.shader.params, saved.shader.params);
    assert.deepEqual(sent.config.screen, saved.screen);
    assert.deepEqual(sent.source, sources.windows[0]);
    assert.equal(r.controller.getSnapshot().applied.effects.intensity, 1);
    r.controller.dispose();
  }
});
test("explicit shader selection resets authored parameters and live-applies intensity one from a legacy zero profile", async () => {
  const r = rig(), legacy = r.actual(), request = r.api.request;
  legacy.config.effects.intensity = 0;
  legacy.config.shader = { id: "older", params: [1.7, 0, 0, 0, 0, 0, 0, 0] };
  r.setActual(legacy);
  r.api.request = async (type, payload) => type === "shaders" ? { items: [descriptor] } : request(type, payload);
  await r.controller.start();
  assert.deepEqual(r.controller.getSnapshot().draft.shader.params, legacy.config.shader.params);
  r.controller.selectShader(descriptor.id); await runUntilApply(r, 1);
  assert.equal(applyCalls(r)[0].payload.config.effects.intensity, 1);
  assert.deepEqual(applyCalls(r)[0].payload.config.shader.params, [.5, 0, 0, 0, 0, 0, 0, 0]);
  assert.deepEqual(applyCalls(r)[0].payload.config.screen, legacy.config.screen);
  r.controller.dispose();
});
test("ordinary Off cancels a pending Start, live intent and import before its restoration acknowledgement", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false;
  r.setActual(initial); await r.controller.start();
  r.controller.aspect("window");
  const heldStart = r.defer("apply"), start = r.controller.startEffect();
  assert.equal(powerPresentation(r.controller.getSnapshot()).shouldStop, true);
  r.controller.liveEdit("effects.vhs.noise", .8);
  const heldImport = r.defer("shader_import"), imported = r.controller.importShader("source");
  const heldOff = r.defer("emergency"), off = r.controller.powerOff();
  let state = r.controller.getSnapshot();
  assert.equal(state.stopping, true); assert.equal(state.startingEffect, false);
  assert.equal(state.livePending, false); assert.equal(state.shaderImporting, false);
  heldImport.resolve({ shader: descriptor });
  assert.equal(await imported, null, "an import finishing before the Off ACK cannot select anything");
  assert.equal(r.controller.getSnapshot().shaders.length, 0);
  const lateStart = r.actual(); lateStart.config = applyCalls(r)[0].payload.config; lateStart.runtime.enabled = true;
  r.api.stop(); heldOff.resolve(r.actual()); await off;
  heldStart.resolve(lateStart); await start;
  for (let i = 0; i < 3; i++) { void r.timer(); await settle(); }
  state = r.controller.getSnapshot();
  assert.equal(applyCalls(r).length, 1); assert.equal(state.snapshot.emergency_sequence, 1);
  assert.equal(state.snapshot.runtime.enabled, false); assert.equal(state.draft.enabled, false);
  assert.equal(state.draft.aspect.enabled, false); assert.equal(state.applied.aspect.enabled, false);
  assert.equal(state.startingEffect, false); assert.equal(state.stopping, false); assert.equal(state.recoveryPending, false);
  assert.equal(powerPresentation(state).shouldStop, false);
  assert.match(state.notice.message, /Эффект выключен/); assert.doesNotMatch(state.notice.message, /аварийн/);
  r.controller.dispose();
});
test("ordinary Off restores a format-only state and remains a retry after a restoration failure", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; initial.config.aspect.enabled = true;
  r.setActual(initial); await r.controller.start();
  assert.equal(powerPresentation(r.controller.getSnapshot()).shouldStop, true);
  const held = r.defer("emergency"), off = r.controller.powerOff();
  r.api.stop(); const failed = r.actual(); failed.runtime.phase = "recovery-error"; failed.runtime.last_error = "resize refused";
  r.setActual(failed); held.reject(Object.assign(new Error("resize refused"), { snapshot: failed })); await off;
  let state = r.controller.getSnapshot();
  assert.equal(state.notice.tone, "warning"); assert.match(state.notice.message, /resize refused/);
  assert.doesNotMatch(state.notice.message, /аварийн|экран восстановлены/);
  assert.equal(powerPresentation(state).shouldStop, true);
  await r.controller.powerOff(); state = r.controller.getSnapshot();
  assert.equal(powerPresentation(state).shouldStop, false); assert.equal(state.applied.aspect.enabled, false);
  assert.match(state.notice.message, /Окно и экран восстановлены/);
  assert.equal(r.calls.filter((call) => call.type === "disable").length, 0);
  r.controller.dispose();
});
test("unconfirmed ordinary Off cannot be followed by a new Start and its eventual event stays a normal message", async () => {
  const r = rig(); await r.controller.start();
  const held = r.defer("emergency"), off = r.controller.powerOff();
  held.reject(new Error("IPC response lost")); await off;
  assert.equal(r.controller.getSnapshot().recoveryPending, true);
  assert.equal(r.controller.getSnapshot().stopping, false);
  await r.controller.startEffect(); assert.equal(applyCalls(r).length, 0);
  r.api.stop(); r.emit({ event: "state_changed", data: { emergency_sequence: 1 } }); await settle();
  const state = r.controller.getSnapshot();
  assert.equal(state.recoveryPending, false); assert.equal(powerPresentation(state).shouldStop, false);
  assert.doesNotMatch(state.notice.message, /аварийн/); assert.match(state.notice.message, /Эффект выключен/);
  r.controller.dispose();
});
test("shader import uses the wrapped descriptor, selecting defaults uses authored strength and asks before warp", async () => {
  const r = rig(), request = r.api.request;
  r.api.request = async (type, payload) => type === "shader_import" ? { shader: copy(descriptor) } : request(type, payload);
  await r.controller.start();
  await r.controller.importShader("source");
  assert.equal(r.controller.getSnapshot().draft.shader?.id || "", "", "import itself never activates");
  r.controller.edit("effects.intensity", .5);
  r.controller.selectShader(descriptor.id);
  assert.deepEqual(r.controller.getSnapshot().draft.shader.params, [.5, 0, 0, 0, 0, 0, 0, 0]);
  assert.equal(r.controller.getSnapshot().draft.effects.intensity, 1);
  const warp = { ...descriptor, id: "warp-123", coordinates: "warp" };
  r.api.request = async (type, payload) => type === "shader_import" ? { shader: warp } : request(type, payload);
  await r.controller.importShader("warp");
  assert.equal(r.controller.selectShader(warp.id), "consent-required");
  assert.equal(r.controller.getSnapshot().draft.shader.id, descriptor.id);
  r.controller.selectShader(warp.id, true);
  assert.equal(r.controller.getSnapshot().draft.input_mode, "keyboard-gamepad");
  r.controller.dispose();
});

test("STOP during shader import suppresses late selection/library update and oversized source never reaches IPC", async () => {
  const r = rig(); await r.controller.start();
  await assert.rejects(r.controller.importShader("x".repeat(65537)), /64 КиБ/);
  assert.equal(r.calls.some((call) => call.type === "shader_import"), false);
  const held = r.defer("shader_import"), imported = r.controller.importShader("source");
  await r.controller.emergency(); held.resolve({ shader: descriptor });
  assert.equal(await imported, null);
  assert.equal(r.controller.getSnapshot().shaders.length, 0);
  assert.equal(r.controller.getSnapshot().shaderImporting, false);
  r.controller.dispose();
});

test("owned testcard startup polling selects only its PID while off and never captures automatically", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; r.setActual(initial);
  const card = { ...sources.windows[0], id: "owned-card", pid: 456, hwnd: "0x456", process_created: "123" };
  const request = r.api.request; let sourcePoll = 0;
  r.api.launchTestcard = async () => ({ started: true, pid: card.pid });
  await r.controller.start();
  r.api.request = async (type, payload) => type === "sources" ? { windows: ++sourcePoll === 1 ? sources.windows : [...sources.windows, card], monitors: sources.monitors } : request(type, payload);
  await r.controller.launchTestcard();
  for (let i = 0; i < 5 && r.controller.getSnapshot().launching; i++) { void r.timer(); await settle(); }
  assert.equal(r.controller.getSnapshot().selectedKey, card.id);
  assert.equal(r.controller.getSnapshot().launching, false);
  assert.equal(applyCalls(r).length, 0);
  r.controller.dispose();
});

test("an authoritative external Off clears a pending debounce without sending or displaying an endless apply", async () => {
  const r = rig(); await r.controller.start();
  r.controller.liveEdit("effects.intensity", .4);
  assert.equal(r.controller.getSnapshot().livePending, true);
  const off = r.actual(); off.config.enabled = off.runtime.enabled = false; r.setActual(off);
  await r.controller.refreshStatus();
  assert.equal(r.controller.getSnapshot().livePending, false);
  for (let i = 0; i < 3; i++) { void r.timer(); await settle(); }
  assert.equal(applyCalls(r).length, 0);
  r.controller.dispose();
});

test("signal edits made during the first Start are sent after its successful acknowledgement", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; r.setActual(initial);
  await r.controller.start();
  const held = r.defer("apply"), start = r.controller.startEffect();
  r.controller.liveEdit("effects.vhs.noise", .27);
  assert.equal(r.controller.getSnapshot().livePending, true);
  const response = r.actual(); response.config = applyCalls(r)[0].payload.config; response.runtime.enabled = true;
  held.resolve(response); await start; await runUntilApply(r, 2);
  assert.equal(applyCalls(r)[1].payload.config.effects.vhs.noise, .27);
  assert.equal(r.controller.getSnapshot().applied.effects.vhs.noise, .27);
  r.controller.dispose();
});

test("a successful advanced ACK rebases later signal edits instead of reverting the newly applied shape", async () => {
  const r = rig(); await r.controller.start();
  const baseline = copy(r.controller.getSnapshot().draft), edited = copy(baseline);
  edited.screen.shape = baseline.screen.shape === "flat" ? "rounded" : "flat";
  const held = r.defer("apply"), apply = r.controller.applyAdvanced(baseline, edited);
  r.controller.liveEdit("effects.vhs.noise", .31);
  const response = r.actual(); response.config = applyCalls(r)[0].payload.config;
  held.resolve(response); await apply; await runUntilApply(r, 2);
  assert.equal(applyCalls(r)[0].payload.config.screen.shape, edited.screen.shape);
  assert.equal(applyCalls(r)[1].payload.config.screen.shape, edited.screen.shape);
  assert.equal(applyCalls(r)[1].payload.config.effects.vhs.noise, .31);
  assert.equal(r.controller.getSnapshot().draft.screen.shape, edited.screen.shape);
  r.controller.dispose();
});

test("Off while the first Start is in flight still cancels its pending live signal intent", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; r.setActual(initial);
  await r.controller.start();
  const held = r.defer("apply"), start = r.controller.startEffect();
  r.controller.liveEdit("effects.intensity", .27);
  await r.controller.disable();
  const obsolete = r.actual(); obsolete.config = applyCalls(r)[0].payload.config; obsolete.runtime.enabled = true;
  held.resolve(obsolete); await start;
  for (let i = 0; i < 3; i++) { void r.timer(); await settle(); }
  assert.equal(applyCalls(r).length, 1);
  assert.equal(r.controller.getSnapshot().snapshot.runtime.enabled, false);
  assert.equal(r.controller.getSnapshot().livePending, false);
  r.controller.dispose();
});

test("fatal engine exit clears import busy and a fresh connection can import again", async () => {
  const r = rig(); await r.controller.start();
  const held = r.defer("shader_import"), oldImport = r.controller.importShader("source");
  assert.equal(r.controller.getSnapshot().shaderImporting, true);
  r.emit({ event: "fatal", data: { message: "engine exited" } });
  held.resolve({ shader: descriptor }); assert.equal(await oldImport, null);
  await r.controller.start();
  assert.equal(r.controller.getSnapshot().shaderImporting, false);
  const fresh = r.defer("shader_import"), newImport = r.controller.importShader("new source");
  fresh.resolve({ shader: descriptor });
  assert.equal((await newImport).id, descriptor.id);
  assert.equal(r.calls.filter((item) => item.type === "shader_import").length, 2);
  r.controller.dispose();
});

test("a builtin selection retains an explicit monitor overlay, while custom shader selection requires a game window", async () => {
  const r = rig(), monitor = r.actual(); monitor.config.mode = "overlay"; monitor.config.capture.transfer = "gpu";
  monitor.config.target = { kind: "monitor", monitor: 0 }; monitor.source = null; r.setActual(monitor);
  const request = r.api.request;
  r.api.request = async (type, payload) => type === "shaders" ? { items: [descriptor] } : request(type, payload);
  await r.controller.start(); r.controller.livePreset("CRT Classic");
  assert.equal(r.controller.getSnapshot().draft.mode, "overlay");
  assert.equal(r.controller.getSnapshot().draft.capture.transfer, "gpu");
  r.controller.selectShader(descriptor.id);
  assert.equal(r.controller.getSnapshot().draft.shader?.id || "", "");
  assert.match(r.controller.getSnapshot().notice.message, /выберите окно игры/);
  r.controller.dispose();
});

test("testcard launch is ignored while the first game Start is pending", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; r.setActual(initial);
  let launches = 0; r.api.launchTestcard = async () => { launches++; return { started: true, pid: 456 }; };
  await r.controller.start();
  const held = r.defer("apply"), start = r.controller.startEffect();
  await r.controller.launchTestcard(); assert.equal(launches, 0);
  const response = r.actual(); response.config = applyCalls(r)[0].payload.config; response.runtime.enabled = true;
  held.resolve(response); await start;
  assert.equal(r.controller.getSnapshot().selectedKey, sources.windows[0].id);
  r.controller.dispose();
});

test("a testcard spawn already in flight cannot replace the game selected by a later pending Start", async () => {
  const r = rig(), initial = r.actual(); initial.config.enabled = initial.runtime.enabled = false; r.setActual(initial);
  const card = { ...sources.windows[0], id: "owned-card", title: "New test card", pid: 456, hwnd: "0x456", process_created: "123" };
  await r.controller.start();
  const request = r.api.request, spawn = deferred();
  r.api.launchTestcard = () => spawn.promise;
  r.api.request = async (type, payload) => type === "sources" ? { windows: [...sources.windows, card], monitors: sources.monitors } : request(type, payload);
  const launch = r.controller.launchTestcard(), held = r.defer("apply"), start = r.controller.startEffect();
  spawn.resolve({ started: true, pid: card.pid }); await launch;
  assert.equal(r.controller.getSnapshot().selectedKey, sources.windows[0].id);
  const response = r.actual(); response.config = applyCalls(r)[0].payload.config; response.runtime.enabled = true;
  held.resolve(response); await start;
  assert.equal(r.controller.getSnapshot().selectedKey, sources.windows[0].id);
  assert.equal(r.controller.getSnapshot().draft.target.window_title, sources.windows[0].title);
  r.controller.liveEdit("effects.intensity", .33); await runUntilApply(r, 2);
  assert.equal(applyCalls(r)[1].payload.source.id, sources.windows[0].id);
  r.controller.dispose();
});
