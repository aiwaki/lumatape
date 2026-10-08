// Explicit ?demo=1 only. These are UI fixtures, never a fallback engine.
import { clone } from "./state.mjs";
const crt = (scanlines, mask, bloom, softness, vignette) => ({ scanlines, mask, bloom, softness, vignette, curvature: 0 });
const vhs = (chroma_bleed = 0, noise = 0) => ({ chroma_bleed, noise, jitter: 0, tracking: 0 });
const effects = (c, v) => ({ intensity: 1, noise_seed: 1, freeze_noise: false, crt: c, vhs: v });
const presets = [
  { name: "Subtle CRT", effects: effects(crt(.12, .04, .06, .08, .1), vhs()) },
  { name: "CRT Classic", effects: effects(crt(.62, .42, .18, .1, .24), vhs()) },
  { name: "Soft TV", effects: effects(crt(.16, .025, .44, .76, .18), vhs(.12, .01)) },
  { name: "VHS Light", effects: effects(crt(.14, .02, .1, .2, .14), vhs(.5, .24)) },
  { name: "VHS Tape", effects: effects(crt(.1, .015, .08, .28, .16), vhs(.92, .62)) },
];
let config = {
  version: 1, mode: "overlay", enabled: false, target: { kind: "monitor", monitor: 0, window_title: "" },
  capture: { transfer: "auto" }, preset: "VHS Tape", effects: clone(presets[4].effects),
  screen: { shape: "flat", corner_radius: .04, curvature: .18, glass: .15 },
  aspect: { enabled: false, method: "mask", scale: "fit", source_dar: 0 }, input_mode: "mouse-exact",
  hotkeys: { toggle: "Ctrl+Alt+F9", emergency: "Ctrl+Alt+F10" },
};
let emergencySequence = 0;
const sources = {
  windows: [{ id: "demo-window", hwnd: "0x1234", pid: 1234, process_created: "1", title: "Тестовая игра · демонстрация" }],
  monitors: [{ index: 0, device: "Demo display", bounds: { x: 0, y: 0, width: 1920, height: 1080 }, work_area: { x: 0, y: 0, width: 1920, height: 1040 } }],
};
export const updater = { state: "unconfigured", current_version: "demo", available_version: null, reason: "Это демонстрация интерфейса. Проверка и установка обновлений недоступны.", downloaded_bytes: 0, total_bytes: null };
function snapshot() {
  return { config: clone(config), emergency_sequence: emergencySequence, source: config.target.kind === "window" ? clone(sources.windows[0]) : null, presets: clone(presets), screen_shapes: ["flat", "rounded", "convex"],
    hotkeys: { registered: false, toggle_received: 0, emergency_received: 0 },
    runtime: { requested_mode: config.mode, requested_transfer: config.capture.transfer, backend: "",
      phase: config.enabled ? "ready" : "disabled", reason: "Данные-примеры. Игра и движок не подключены.",
      enabled: config.enabled, effective_intensity: config.enabled ? config.effects.intensity : 0, preset: config.preset,
      gpu: { state: "unavailable", code: "gpu_interop_unavailable", reason: "Пример Parallels: драйвер не поддерживает необходимый обмен кадрами GPU. Выберите Full CPU явно." }, compatibility: { state: "unknown", reason: "Проверится при запуске выбранной игры." },
      format_active: false, format_requested: config.aspect.enabled, format_method: config.aspect.method, recovery_pending: false, unsaved: false } };
}
const slugs = { "Subtle CRT": "subtle", "CRT Classic": "crt-classic", "Soft TV": "soft-tv", "VHS Light": "vhs-light", "VHS Tape": "vhs-tape" };
export async function request(type, payload = {}) {
  await new Promise((resolve) => setTimeout(resolve, 60));
  if (type === "snapshot" || type === "reload") return snapshot();
  if (type === "sources") return clone({ ...sources, suggested: sources.windows[0] });
  if (type === "shaders") return { items: [] };
  if (type === "shader_import") throw new Error("Демо не компилирует шейдеры. Импорт доступен в установленном LumaTape.");
  if (type === "apply") {
    if (payload.expected_emergency_sequence !== emergencySequence) throw Object.assign(new Error("Запрос предшествовал аварийной остановке. Черновик отменён."), { code: "stale_emergency_sequence", snapshot: snapshot(), applied: false });
    if (payload.config.enabled && payload.config.mode === "full" && payload.config.capture.transfer === "gpu") throw Object.assign(new Error("Пример: драйвер Parallels не поддерживает WGL_NV_DX_interop2."), { code: "gpu_interop_unavailable", suggested_backend: "full-compatibility", snapshot: snapshot(), applied: false });
    config = clone(payload.config); return snapshot();
  }
  if (type === "disable") { config.enabled = false; return snapshot(); }
  if (type === "toggle") { config.enabled = !config.enabled; return snapshot(); }
  if (type === "emergency") { emergencySequence++; config.enabled = false; config.aspect.enabled = false; return snapshot(); }
  if (type === "restore") { config.aspect.enabled = false; return snapshot(); }
  if (type === "confirm") return snapshot();
  if (type === "diagnostics") return { demonstration: true, engine_connected: false, config: { preset: config.preset, mode: config.mode }, screenshots_included: false };
  if (type === "preview") {
    if (payload.config.shader?.id && !payload.before) throw new Error("Демо не рендерит пользовательские шейдеры.");
    const original = payload.before || payload.config.effects.intensity === 0;
    const slug = slugs[payload.config.preset] || "vhs-tape";
    const shape = ["flat", "rounded", "convex"].includes(payload.config.screen.shape) ? payload.config.screen.shape : "flat";
    return { mime: "image/png", width: 960, height: 720, fixture: true,
      png_url: original ? new URL("./fixtures/original.png", import.meta.url).href : new URL(`./fixtures/${slug}-${shape}.png`, import.meta.url).href };
  }
  throw new Error(`Действие ${type} отсутствует в демонстрации.`);
}
