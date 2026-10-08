import test from "node:test";
import assert from "node:assert/strict";
import { captureShortcut, mergeAdvanced } from "../interaction.mjs";

test("shortcut capture uses VK Latin letters, physical top-row digits and a Cyrillic physical fallback", () => {
  assert.deepEqual(captureShortcut({ code: "Digit9", key: "(", ctrlKey: true, shiftKey: true }), { value: "Ctrl+Shift+9" });
  assert.deepEqual(captureShortcut({ code: "KeyA", key: "ф", ctrlKey: true }), { value: "Ctrl+A" });
  assert.deepEqual(captureShortcut({ code: "KeyQ", key: "a", ctrlKey: true }), { value: "Ctrl+A" });
  assert.deepEqual(captureShortcut({ code: "Semicolon", key: "s", ctrlKey: true }), { value: "Ctrl+S" });
  assert.deepEqual(captureShortcut({ code: "PageDown", key: "PageDown", altKey: true }), { value: "Alt+PageDown" });
  assert.deepEqual(captureShortcut({ code: "Escape", key: "Escape" }), { cancelled: true });
  assert.equal(captureShortcut({ code: "ControlLeft", key: "Control", ctrlKey: true }), null);
  assert.equal(captureShortcut({ code: "Digit9", key: "9", ctrlKey: true, repeat: true }), null);
  for (const event of [{ code: "KeyA", key: "a" }, { code: "F12", key: "F12", ctrlKey: true }, { code: "Enter", key: "Enter", ctrlKey: true }, { code: "Numpad9", key: "9", ctrlKey: true }]) assert.ok(captureShortcut(event).error);
});

test("advanced Apply rebases changed geometry/keys without taking stale effects or source", () => {
  const initial = { enabled: true, effects: { intensity: .4 }, target: { kind: "window", window_title: "Old" }, mode: "full", capture: { transfer: "auto" }, screen: { shape: "flat" }, aspect: { enabled: false }, hotkeys: { toggle: "Ctrl+Shift+9" } };
  const edited = structuredClone(initial); edited.aspect.enabled = true; edited.hotkeys.toggle = "Ctrl+Shift+8";
  const live = structuredClone(initial); live.effects.intensity = .9; live.target.window_title = "New"; live.screen.shape = "rounded";
  const next = mergeAdvanced(live, initial, edited);
  assert.equal(next.effects.intensity, .9); assert.equal(next.target.window_title, "New"); assert.equal(next.screen.shape, "rounded");
  assert.equal(next.aspect.enabled, true); assert.equal(next.hotkeys.toggle, "Ctrl+Shift+8");
  assert.equal(live.aspect.enabled, false);
});
