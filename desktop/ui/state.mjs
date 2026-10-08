export const clone = (value) => structuredClone(value);
export const equal = (left, right) => JSON.stringify(left) === JSON.stringify(right);

export function emergencyTransition(previous, incoming, observedSequence = 0) {
  const sequence = incoming?.emergency_sequence;
  if (!Number.isSafeInteger(sequence) || sequence < 0) throw new Error("Движок не подтвердил состояние аварийной остановки.");
  // An event can arrive before its authoritative snapshot. Reject replies from
  // before that event even while the displayed snapshot still has the old seq.
  if (sequence < observedSequence) return "stale";
  if (!previous) return "initial";
  if (sequence < previous.emergency_sequence) return "stale";
  return sequence > previous.emergency_sequence ? "advanced" : "same";
}

export function canEditWithoutSource(applied, draft) {
  if (!applied || !draft || !equal(applied.target, draft.target) || applied.mode !== draft.mode || !equal(applied.capture, draft.capture)) return false;
  const newGeometry = draft.aspect.enabled && ["window", "system"].includes(draft.aspect.method)
    && (!applied.aspect.enabled || applied.aspect.method !== draft.aspect.method);
  return !newGeometry;
}

export function selectPreset(config, presets, name) {
  const next = clone(config);
  if (name === "Custom") { next.preset = name; return next; }
  const preset = presets.find((item) => item.name === name);
  if (!preset?.effects?.crt || !preset?.effects?.vhs) throw new Error("Неизвестный пресет");
  next.preset = name;
  if (next.shader) next.shader = { id: "", params: Array(8).fill(0) };
  next.effects.crt = clone(preset.effects.crt);
  next.effects.vhs = clone(preset.effects.vhs);
  return next;
}

export function reconcileDraft(applied, draft, incoming, force = false) {
  if (force || !draft || equal(applied, draft)) return clone(incoming);
  // Enabled follows authoritative live state; Start changes only the request copy. The caller forces
  // a complete replacement for emergency sequence changes, including failures.
  return { ...draft, enabled: incoming.enabled };
}

export function backendValue(config) {
  const automaticWindow = config.mode === "overlay" && config.capture.transfer === "auto" && config.target.kind === "window";
  return config.mode === "full" || automaticWindow ? `full-${config.capture.transfer}` : "overlay";
}

export function sourceKey(source) {
  return source.id || `window:${source.hwnd}:${source.pid}:${source.process_created || ""}`;
}

export function sourceLabel(source, windows) {
  const sameTitle = windows.filter((item) => item.title === source.title);
  if (sameTitle.length < 2) return source.title;
  const sameProcess = sameTitle.filter((item) => item.pid === source.pid).length > 1;
  return `${source.title} · PID ${source.pid}${sameProcess ? ` · ${source.hwnd}` : ""}`;
}

export function previewSeconds(elapsedMilliseconds) {
  // The engine bounds preview time to one day. Keep previews working when the
  // settings window stays open overnight; this clock never drives the game.
  return (Math.max(0, elapsedMilliseconds) % 86400000) / 1000;
}

export function previewConfig(config) {
  const preview = clone(config);
  preview.enabled = true;
  preview.effects.intensity = 1;
  preview.target = { kind: "window", window_title: "LumaTape preview", monitor: 0 };
  return preview;
}

// One desktop power control also stops an in-flight Start and restores format.
// Requested draft geometry alone is not evidence that an OS change was made.
export function powerPresentation(state) {
  const runtime = state.snapshot?.runtime || {};
  const shouldStop = !!(runtime.enabled || state.applied?.enabled || state.snapshot?.config?.enabled || state.applied?.aspect?.enabled || state.snapshot?.config?.aspect?.enabled
    || state.startingEffect || state.livePending || state.shaderImporting || state.stopping || state.recoveryPending
    || state.snapshot?.confirmation_deadline || runtime.confirmation_deadline
    || runtime.phase === "recovery-error" || runtime.unsaved);
  return { shouldStop, stopping: state.stopping === true,
    label: state.stopping ? "Выключается…" : shouldStop ? "Выключить" : "Включить" };
}

export function hotkeyProbe(snapshot, draftHotkeys, baseline) {
  const bindings = snapshot.config.hotkeys;
  const signature = JSON.stringify(bindings);
  const received = snapshot.hotkeys || {};
  const toggle = Number.isSafeInteger(received.toggle_received) && received.toggle_received >= 0 ? received.toggle_received : null;
  const emergency = Number.isSafeInteger(received.emergency_received) && received.emergency_received >= 0 ? received.emergency_received : null;
  // A new applied profile (or restarted engine) starts a new receipt check.
  // Registration of the old profile must never certify an uncommitted draft.
  if (!baseline || baseline.signature !== signature || toggle < baseline.toggle || emergency < baseline.emergency
    || (baseline.toggle === null && toggle !== null) || (baseline.emergency === null && emergency !== null)) {
    baseline = { signature, toggle, emergency };
  }
  return {
    baseline, bindings, registered: received.registered === true,
    pending: !equal(bindings, draftHotkeys),
    toggleTotal: toggle, emergencyTotal: emergency,
    toggleDelta: toggle === null || baseline.toggle === null ? null : toggle - baseline.toggle,
    emergencyDelta: emergency === null || baseline.emergency === null ? null : emergency - baseline.emergency,
  };
}

export function selectSource(config, windows, monitors, key) {
  const next = clone(config);
  const window = windows.find((item) => sourceKey(item) === key);
  if (window) {
    next.target = { kind: "window", monitor: 0, window_title: window.title };
    return { config: next, source: window };
  }
  const monitor = monitors.find((item) => `monitor:${item.index}` === key);
  if (monitor) {
    next.target = { kind: "monitor", monitor: monitor.index, window_title: "" };
    return { config: next, source: null };
  }
  throw new Error("Выберите доступное окно игры или монитор.");
}

export function initialSourceKey(config, sources, currentSource) {
  if (config.target.kind === "monitor") {
    return sources.monitors.some((item) => item.index === config.target.monitor) ? `monitor:${config.target.monitor}` : "";
  }
  if (currentSource) {
    const found = sources.windows.find((item) => item.hwnd === currentSource.hwnd && item.pid === currentSource.pid && (!currentSource.process_created || item.process_created === currentSource.process_created));
    return found ? sourceKey(found) : "";
  }
  const matches = sources.windows.filter((item) => item.title === config.target.window_title);
  return matches.length === 1 ? sourceKey(matches[0]) : "";
}

// These messages help the form before Apply. The Go engine remains authoritative
// and validates the same request again against live OS/backend state.
export function draftIssues(config, hasSource = true) {
  const issues = [];
  if (!hasSource) issues.push("Выберите доступное окно игры или монитор.");
  if (config.mode === "full" && config.target.kind !== "window") issues.push("Full работает с окном игры. Выберите окно вместо монитора.");
  if (config.screen.shape === "convex" && (config.mode !== "full" || config.input_mode !== "keyboard-gamepad")) issues.push("Выпуклый экран требует Full и управления «Клавиатура / геймпад». Можно выбрать плоскую или скруглённую форму.");
  if (config.input_mode === "mouse-exact" && (config.aspect.scale !== "fit" || config.aspect.source_dar !== 0)) issues.push("Для точной мыши выберите масштаб Fit и исходный формат «Авто» в настройках.");
  if (config.aspect.enabled && config.aspect.method === "window" && config.target.kind !== "window") issues.push("Изменение клиентского окна 4:3 требует выбранного окна игры.");
  if (!config.hotkeys.toggle.trim() || !config.hotkeys.emergency.trim()) issues.push("Укажите обе горячие клавиши.");
  if (config.hotkeys.toggle.trim().toLowerCase() === config.hotkeys.emergency.trim().toLowerCase()) issues.push("Для переключения и аварийного восстановления нужны разные сочетания.");
  return issues;
}

export function runtimePresentation(runtime = {}) {
  const labels = {
    starting: "Подготовка", active: "Работает", ready: "Готов", disabled: "Выключен", bypass: "Исходное изображение",
    "paused-settings": "Настройки открыты", "paused-stale": "Захват приостановлен", "waiting-source": "Ожидает источник",
    "paused-focus": "Ожидает фокус игры", "paused-moving": "Пауза при движении",
    "waiting-frame": "Ожидает кадр", "source-closed": "Игра закрыта", error: "Не удалось запустить",
    "recovery-error": "Нужно восстановление",
  };
  const phase = runtime.phase || "unknown";
  return {
    label: labels[phase] || "Состояние неизвестно",
    backend: ({ lightweight: "Lightweight", "full-gpu": "Full · GPU", "full-compatibility": "Full · CPU" })[runtime.backend] || (runtime.backend ? "Неизвестный способ обработки" : "Изображение не выводится"),
    tone: ["error", "recovery-error"].includes(phase) ? "error" : phase === "active" ? "active" : "muted",
    reason: runtime.reason || "Ожидаем подтверждённое состояние движка.",
  };
}

export function updatePresentation(value = {}) {
  const state = value.state || "unconfigured";
  const version = typeof value.available_version === "string" ? value.available_version : null;
  const busy = ["checking", "downloading", "installing"].includes(state);
  const labels = {
    unconfigured: "Обновления пока не настроены", idle: "Можно проверить обновления",
    checking: "Проверяем обновления…", up_to_date: "Установлена последняя версия",
    available: version ? `Доступна версия ${version}` : "Доступно обновление",
    downloading: "Загружаем обновление…", installing: "Установка и перезапуск…", error: "Не удалось обновить",
  };
  return { state, label: labels[state] || "Состояние обновлений неизвестно", busy,
    canCheck: state !== "unconfigured" && !busy,
    canInstall: state === "available" && !!version,
    version, reason: typeof value.reason === "string" ? value.reason : "" };
}

// Launch is stricter than saving preferences: it must name a live window, or an
// explicitly selected advanced monitor. Reading an old profile is not consent
// to cover the entire desktop. The underlying saved config is never migrated.
export function startIssues(state) {
  if (!state.draft) return ["Ожидаем настройки движка."];
  const window = state.sources.windows.some((item) => sourceKey(item) === state.selectedKey);
  const monitor = state.sources.monitors.some((item) => `monitor:${item.index}` === state.selectedKey);
  const existingMonitor = state.applied?.enabled && state.applied.target.kind === "monitor" && state.selectedKey === state.appliedKey;
  const draft = powerConfig(state.draft);
  const issues = draftIssues(draft, true);
  if (!window && !(monitor && (state.monitorIntent || existingMonitor))) {
    issues.unshift("Выберите окно игры. Режим всего монитора можно явно выбрать в дополнительных настройках.");
  }
  if (backendValue(draft) === "full-gpu" && state.snapshot?.runtime.gpu?.state === "unavailable") {
    issues.push("Обработка через GPU недоступна. Выберите Full CPU или лёгкий фильтр.");
  }
  if (backendValue(draft) === "full-compatibility" && state.snapshot?.runtime.compatibility?.state === "unavailable") {
    issues.push("Режим Full CPU недоступен. Выберите лёгкий фильтр.");
  }
  return issues;
}

export function applyFailure(error) {
  const failures = {
    gpu_interop_unavailable: ["Обработка через GPU недоступна.", "Выберите Full CPU для обработки цвета и VHS или лёгкий фильтр без захвата."],
    source_unavailable: ["Выбранное окно игры недоступно.", "Обновите список и выберите открытое окно игры."],
    validation: ["Эти настройки нельзя применить вместе.", "Проверьте форму экрана, управление и формат игры."],
    backend_unavailable: ["Выбранный способ обработки не запустился.", "Выберите другой способ обработки или проверьте диагностику."],
    transport: ["Результат включения не подтверждён.", "Проверьте связь с движком. При необходимости нажмите Стоп."],
    stale_emergency_sequence: ["Изменения отменены аварийной остановкой.", "Проверьте актуальные настройки перед новым включением."],
  };
  const [message, nextStep] = failures[error.code] || ["Не удалось применить настройки.", "Проверьте окно игры и выбранный способ обработки; подробности ниже."];
  return { code: error.code || "unknown", message, nextStep,
    details: error.message || "", suggestedBackend: error.suggested_backend === "full-compatibility" ? "full-compatibility" : null };
}

// New profiles select Auto. An explicitly saved Lightweight profile uses its
// prior transfer preference, so merely opening/powering it never migrates it.
export function powerConfig(config) {
  const next = clone(config);
  next.effects.intensity = 1;
  if (next.mode === "overlay" && next.capture.transfer === "auto") next.mode = "full";
  return next;
}
export function fullEffectConfig(config) {
  const next = clone(config);
  next.effects.intensity = 1;
  if (next.mode === "overlay" && next.target.kind === "window") { next.mode = "full"; next.capture.transfer = "auto"; }
  return next;
}
