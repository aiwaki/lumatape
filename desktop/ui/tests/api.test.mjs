import test from "node:test";
import assert from "node:assert/strict";

globalThis.location = { search: "" };
globalThis.window = {};
const api = await import("../api.js");

test("production browser cannot silently receive demo engine data", async () => {
  delete window.__TAURI__;
  assert.equal(api.demoMode, false);
  await assert.rejects(api.request("snapshot"), /движок Windows недоступен/);
  await assert.rejects(api.subscribe("engine_event", () => {}), /события/);
});
test("invoke preserves exact source uint64 strings, request type and payload", async () => {
  const source = { id: "opaque", hwnd: "0x123456789abcdef0", pid: 42, process_created: "134039488294079118", title: "Game" };
  const result = { config: { preset: "VHS Tape" }, runtime: { phase: "ready" } };
  let sent;
  window.__TAURI__ = { core: { invoke: async (command, value) => { sent = { command, value }; return result; } } };
  assert.equal(await api.request("apply", { config: { enabled: false }, source, expected_emergency_sequence: 3 }), result);
  assert.deepEqual(sent, { command: "engine_request", value: { request: { type: "apply", payload: { config: { enabled: false }, source, expected_emergency_sequence: 3 } } } });
});
test("applied-but-unsaved rejection retains authoritative snapshot and flags", async () => {
  const result = { config: { enabled: false }, runtime: { unsaved: true } };
  const response = { v: 1, id: "request-7", ok: false, error: { code: "save_failed", message: "Disk unavailable", applied: true, unsaved: true }, result };
  window.__TAURI__ = { core: { invoke: async () => { throw JSON.stringify(response); } } };
  await assert.rejects(api.request("apply"), (error) => {
    assert.equal(error.code, "save_failed");
    assert.equal(error.applied, true);
    assert.equal(error.unsaved, true);
    assert.deepEqual(error.snapshot, result);
    return true;
  });
});
test("plain transport errors are never presented as successful application", () => {
  for (const original of ["engine disconnected", new Error("EOF"), { message: "timeout" }]) {
    const error = api.normalizeError(original);
    assert.equal(error.code, "transport");
    assert.equal(error.applied, false);
    assert.equal(error.snapshot, null);
    assert.ok(error.message.length > 0);
  }
});
test("event adapter unwraps payload and returns removable listener", async () => {
  const stop = () => {};
  let callback, observed;
  window.__TAURI__ = { event: { listen: async (name, listener) => { assert.equal(name, "engine_event"); callback = listener; return stop; } } };
  assert.equal(await api.subscribe("engine_event", (value) => { observed = value; }), stop);
  const payload = { v: 1, event: "runtime", data: { phase: "paused-settings" } };
  callback({ payload });
  assert.deepEqual(observed, payload);
});
test("only explicit demo=1 activates fixtures; fixture snapshot never claims live active capture", async () => {
  location.search = "?demo=true";
  assert.equal((await import("../api.js?not-demo")).demoMode, false);
  location.search = "?demo=1";
  const demo = await import("../api.js?explicit-demo");
  assert.equal(demo.demoMode, true);
  const value = await demo.request("snapshot");
  assert.equal(value.runtime.backend, "");
  assert.notEqual(value.runtime.phase, "active");
  assert.equal(value.hotkeys.registered, false);
  assert.equal(value.hotkeys.toggle_received, 0);
  assert.equal((await demo.updaterStatus()).state, "unconfigured");
});
test("an Apply prepared before STOP is rejected by the fixture IPC barrier and cannot restore old format", async () => {
  const demo = await import("../demo.mjs");
  const before = await demo.request("snapshot");
  const oldDraft = structuredClone(before.config);
  oldDraft.aspect.enabled = true;
  oldDraft.aspect.method = "system";
  const stopped = await demo.request("emergency");
  assert.equal(stopped.emergency_sequence, before.emergency_sequence + 1);
  assert.equal(stopped.config.enabled, false);
  assert.equal(stopped.config.aspect.enabled, false);
  await assert.rejects(demo.request("apply", { config: oldDraft, expected_emergency_sequence: before.emergency_sequence }), (error) => {
    assert.equal(error.code, "stale_emergency_sequence");
    assert.equal(error.snapshot.emergency_sequence, stopped.emergency_sequence);
    return true;
  });
  await assert.rejects(demo.request("apply", { config: oldDraft }), (error) => error.code === "stale_emergency_sequence");
  assert.deepEqual((await demo.request("snapshot")).config, stopped.config);
  const deliberate = structuredClone(stopped.config); deliberate.effects.intensity = .33;
  const current = await demo.request("apply", { config: deliberate, expected_emergency_sequence: stopped.emergency_sequence });
  assert.equal(current.config.effects.intensity, .33);
  assert.equal(current.config.enabled, false);
});

test("GPU refusal keeps explicit CPU suggestion and authoritative snapshot without switching backend", () => {
  const snapshot = { config: { mode: "overlay" }, runtime: { gpu: { state: "unavailable" } } };
  const error = api.normalizeError(JSON.stringify({ error: { code: "gpu_interop_unavailable", message: "GPU unavailable", suggested_backend: "full-compatibility", applied: false }, result: snapshot }));
  assert.equal(error.suggested_backend, "full-compatibility");
  assert.deepEqual(error.snapshot, snapshot);
  assert.equal(error.applied, false);
});
