import test from "node:test";
import assert from "node:assert/strict";
import { performShaderImport, shaderImportPrompt, validateShaderText, SHADER_MAX_BYTES } from "../shader-import.mjs";

const deferred = () => { let resolve; const promise = new Promise((done) => { resolve = done; }); return { promise, resolve }; };
function harness(overrides = {}) {
  const abort = new AbortController(), states = [], sources = [];
  return { abort, states, sources, options: { signal: abort.signal, canImport: () => true, onImport: async (source) => { sources.push(source); return true; }, onState: (value) => states.push(value), ...overrides } };
}

test("prompt includes full engine contract and natural-language intent, with a finished look", () => {
  const prompt = shaderImportPrompt("  Тёплый CRT без шума  ");
  assert.match(prompt, /Желаемый эффект: Тёплый CRT без шума/);
  for (const part of ["vec3 lumatape(vec2 uv)", "ltSample", "GLSL 330", "64 KiB", "parameters", "[]", ".lumatape.glsl"]) assert.ok(prompt.includes(part), part);
  assert.doesNotMatch(prompt, /\[замени эту строку/);
  assert.match(shaderImportPrompt(), /Предложи законченный атмосферный эффект/);
});

test("file selection reads once and imports automatically with reading and compile phases", async () => {
  const h = harness();
  assert.equal(await performShaderImport({ file: { size: 4, text: async () => "code" } }, h.options), true);
  assert.deepEqual(h.sources, ["code"]);
  assert.deepEqual(h.states.map((state) => state.phase), ["reading", "validating", "done"]);
});

test("oversize files are rejected before reading, UTF-8 text after reading and empty input locally", async () => {
  const h = harness();
  await performShaderImport({ file: { size: SHADER_MAX_BYTES + 1, text: () => { throw new Error("must not read"); } } }, h.options);
  assert.match(h.states.at(-1).message, /64 КиБ/);
  assert.deepEqual(h.sources, []);
  assert.throws(() => validateShaderText("я".repeat(SHADER_MAX_BYTES / 2 + 1)), /64 КиБ/);
  assert.throws(() => validateShaderText(" \n"), /пуст/);
  assert.equal(validateShaderText("x".repeat(SHADER_MAX_BYTES)).length, SHADER_MAX_BYTES);
});

test("closing or replacing the form during file read never imports the late result", async () => {
  const read = deferred(), h = harness();
  const pending = performShaderImport({ file: { size: 4, text: () => read.promise } }, h.options);
  h.abort.abort();
  read.resolve("late");
  assert.equal(await pending, false);
  assert.deepEqual(h.sources, []);
  assert.deepEqual(h.states.map((state) => state.phase), ["reading"]);
});

test("disabled/demo readiness change while reading does not start engine mutation", async () => {
  let ready = true;
  const read = deferred(), h = harness({ canImport: () => ready });
  const pending = performShaderImport({ file: { size: 4, text: () => read.promise } }, h.options);
  ready = false;
  read.resolve("code");
  assert.equal(await pending, false);
  assert.deepEqual(h.sources, []);
  assert.equal(h.states.at(-1).phase, "idle");
});

test("engine compile failure remains readable inline; cancelled import is not an error", async () => {
  const failure = harness({ onImport: async () => { throw new Error("shader_compile_failed: unknownFunction"); } });
  assert.equal(await performShaderImport({ source: "code" }, failure.options), false);
  assert.deepEqual(failure.states.at(-1), { phase: "error", message: "shader_compile_failed: unknownFunction" });
  const cancelled = harness({ onImport: async () => false });
  assert.equal(await performShaderImport({ source: "code" }, cancelled.options), false);
  assert.equal(cancelled.states.at(-1).phase, "idle");
});

test("closing during engine validation ignores late completion and late rejection", async () => {
  for (const fail of [false, true]) {
    const done = deferred(), h = harness({ onImport: async () => { await done.promise; if (fail) throw new Error("late"); return true; } });
    const pending = performShaderImport({ source: "code" }, h.options);
    h.abort.abort(); done.resolve();
    assert.equal(await pending, false);
    assert.deepEqual(h.states.map((state) => state.phase), ["validating"]);
  }
});

test("a failed file read has a clear local error without sending source", async () => {
  const h = harness();
  await performShaderImport({ file: { size: 4, text: async () => { throw new Error("denied"); } } }, h.options);
  assert.match(h.states.at(-1).message, /прочитать файл/);
  assert.deepEqual(h.sources, []);
});
