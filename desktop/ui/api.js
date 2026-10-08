export const demoMode = new URLSearchParams(location.search).get("demo") === "1";
let demoPromise;
const demo = () => demoPromise ||= import("./demo.mjs");

export function normalizeError(error) {
  let parsed = error;
  if (typeof parsed === "string") { try { parsed = JSON.parse(parsed); } catch { /* plain backend error */ } }
  const detail = parsed?.error || parsed;
  const message = typeof detail?.message === "string" ? detail.message : typeof detail === "string" ? detail : error?.message || "Не удалось выполнить действие.";
  return Object.assign(new Error(message), {
    code: detail?.code || "transport", applied: detail?.applied === true,
    unsaved: detail?.unsaved === true, suggested_backend: detail?.suggested_backend || null, snapshot: parsed?.result || null,
  });
}

async function invoke(command, payload = {}) {
  if (!window.__TAURI__?.core?.invoke) throw new Error("Откройте установленное приложение LumaTape. В браузере движок Windows недоступен.");
  try { return await window.__TAURI__.core.invoke(command, payload); }
  catch (error) { throw normalizeError(error); }
}

export async function request(type, payload = {}) {
  if (demoMode) return (await demo()).request(type, payload);
  return invoke("engine_request", { request: { type, payload } });
}

export async function updaterStatus() {
  return demoMode ? (await demo()).updater : invoke("updater_status");
}
export async function hostStatus() {
  return demoMode ? { tray_available: false, tray_error: "В демонстрации системный трей недоступен." } : invoke("host_status");
}
export async function checkUpdates() {
  return demoMode ? (await demo()).updater : invoke("check_updates");
}
export async function installUpdate(version) {
  if (demoMode) throw new Error("В демонстрации обновления не устанавливаются.");
  return invoke("install_update", { version });
}
export async function quitApplication() {
  if (demoMode) throw new Error("Это демонстрация интерфейса. Закройте вкладку, чтобы завершить просмотр.");
  return invoke("quit_application");
}
export async function launchTestcard() {
  if (demoMode) throw new Error("Тестовая игра доступна в установленном приложении Windows.");
  return invoke("launch_testcard");
}
export async function subscribe(event, handler) {
  if (demoMode) return () => {};
  if (!window.__TAURI__?.event?.listen) throw new Error("Не удалось подключить события приложения.");
  return window.__TAURI__.event.listen(event, ({ payload }) => handler(payload));
}

export async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    try { await navigator.clipboard.writeText(text); return; } catch { /* WebView fallback below */ }
  }
  const focused = document.activeElement;
  const area = document.createElement("textarea");
  area.value = text;
  area.readOnly = true;
  area.className = "clipboard-buffer";
  document.body.append(area);
  area.select();
  const copied = document.execCommand("copy");
  area.remove();
  focused?.focus({ preventScroll: true });
  if (!copied) throw new Error("Не удалось записать диагностику в буфер обмена.");
}

export async function hidePanel() {
  if (demoMode) return { hidden: false };
  return invoke("hide_panel");
}
