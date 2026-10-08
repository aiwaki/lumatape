import { mergeAdvanced, rebaseAfterApply } from "./interaction.mjs";
import { clone, equal, emergencyTransition, canEditWithoutSource, selectPreset, reconcileDraft, sourceKey, previewSeconds, previewConfig, hotkeyProbe, selectSource, initialSourceKey, draftIssues, startIssues, applyFailure, powerConfig, fullEffectConfig } from "./state.mjs";

function previewInputs(config) {
  if (!config) return null;
  // Match the synthetic preview's rendering/validation inputs after desktop
  // normalization; source identity, hotkeys and capture transfer do not draw it.
  const { mode, effects, screen, aspect, input_mode, shader } = previewConfig(powerConfig(config));
  return { mode, effects, screen, aspect, input_mode, shader };
}

// React reads one immutable snapshot. Request epochs, ownership and timers live
// here, not in render closures. The same controller is tested without a browser.
export function createController(api, environment = {}) {
  const env = {
    now: () => performance.now(), hidden: () => typeof document !== "undefined" && document.hidden,
    // Calling browser natives as env.method() gives them the wrong receiver in
    // WebKit. Keep the native Window receiver; injected test clocks stay local.
    setTimeout: (callback, delay) => globalThis.setTimeout(callback, delay),
    clearTimeout: (id) => globalThis.clearTimeout(id),
    setInterval: (callback, delay) => globalThis.setInterval(callback, delay),
    clearInterval: (id) => globalThis.clearInterval(id), ...environment,
  };
  let state = {
    snapshot: null, applied: null, draft: null, sources: { windows: [], monitors: [] }, selectedKey: "", appliedKey: "",
    connected: false, connecting: false, busy: false, startingEffect: false, stopping: false, recoveryPending: false, launching: false, notice: null, connectionError: null,
    monitorIntent: false, previewOpen: false, applyError: null, livePending: false, shaders: [], shaderImporting: false,
    tab: "screen", preview: { before: null, after: null, feedback: "", fixture: false },
    updater: null, host: null, probeOpen: false, probe: null,
  };
  const listeners = new Set();
  let revision = 0, mutation = 0, observedSequence = 0, generation = 0, running = false, starting = false;
  let previewRevision = 0, previewBusy = false, statusBusy = false, previewCompletedAt = -Infinity;
  let sourceRequest = 0, shaderRequest = 0, sourceChoice = 0, liveSuppressed = false, pendingEnable = 0;
  let shaderImportEpoch = 0, ordinaryStopSequence = null;
  let previewTimer, refreshTimer, recoveryTimer, sourceTimer, liveTimer, subscriptions = [], probeBaseline;
  const animationStart = env.now();
  const publish = (patch) => {
    const invalidated = Object.hasOwn(patch, "draft") && !equal(previewInputs(state.draft), previewInputs(patch.draft))
      ? { preview: { ...state.preview, after: null, feedback: "Готовим эффект…" } } : {};
    // Keep explicit emergency/recovery preview messages authoritative. All draft
    // publication paths, including snapshot reconciliation and ACK rebases, clear
    // obsolete after pixels while retaining the shared original scene.
    state = { ...state, ...invalidated, ...patch };
    listeners.forEach((listener) => listener());
  };
  const current = (life) => running && life === generation;
  const dirty = () => !!state.draft && (!equal(state.applied, state.draft) || state.selectedKey !== state.appliedKey);
  const preferenceOnly = () => !state.selectedKey && !state.appliedKey && canEditWithoutSource(state.applied, state.draft);
  const awaitingEmergency = () => observedSequence > (state.snapshot?.emergency_sequence ?? 0);
  const notify = (message, tone = "success") => publish({ notice: message ? { message, tone } : null });
  const connectionError = (error) => publish({ connected: false, connectionError: error.message || String(error) });
  const updateProbe = () => {
    if (!state.snapshot || !state.draft) return;
    const probe = hotkeyProbe(state.snapshot, state.draft.hotkeys, probeBaseline);
    if (state.probeOpen) probeBaseline = probe.baseline;
    publish({ probe });
  };
  const schedulePreview = (delay = 600) => {
    env.clearTimeout(previewTimer);
    if (running) previewTimer = env.setTimeout(previewTick, delay);
  };
  function cancelLive(suppress = false) { if (suppress) liveSuppressed = true; env.clearTimeout(liveTimer); publish({ livePending: false }); }
  function cancelPendingWork() {
    cancelLive(true);
    pendingEnable = 0;
    previewRevision++;
    shaderImportEpoch++; shaderRequest++; sourceChoice++;
    env.clearTimeout(sourceTimer);
    publish({ startingEffect: false, shaderImporting: false, launching: false, previewOpen: false });
  }
  function queueLive(delay = 180) {
    if (liveSuppressed || state.recoveryPending || !(state.snapshot?.runtime.enabled || pendingEnable === mutation) || awaitingEmergency() || !running) return;
    publish({ livePending: true });
    env.clearTimeout(liveTimer);
    liveTimer = env.setTimeout(async () => {
      if (liveSuppressed || !state.livePending || !(state.snapshot?.runtime.enabled || pendingEnable === mutation) || awaitingEmergency() || !running) return;
      if (state.busy) return; // the current request's finally drains the latest intent
      publish({ livePending: false });
      await applyDraft(true, true);
    }, delay);
  }
  const change = (draft, selectedKey = state.selectedKey) => {
    revision++;
    previewRevision++;
    publish({ draft, selectedKey, notice: null, applyError: null });
    updateProbe();
    schedulePreview(160);
  };

  function acceptSnapshot(value, force = false, knownKey) {
    if (!value?.config || !value?.runtime || !Array.isArray(value.presets)) throw new Error("Движок вернул неполное состояние.");
    const transition = emergencyTransition(state.snapshot, value, observedSequence);
    if (transition === "stale") return false;
    const completingEmergency = awaitingEmergency();
    observedSequence = value.emergency_sequence;
    const emergency = transition === "advanced" || completingEmergency;
    if (!state.snapshot?.runtime.enabled && value.runtime.enabled) liveSuppressed = false;
    if (!value.runtime.enabled) cancelLive();
    if (emergency) {
      cancelPendingWork();
      force = true;
      knownKey = undefined;
      mutation++;
      revision++;
      previewRevision++;
    }
    const existingMonitorIntent = !state.snapshot && value.config.enabled && value.config.target.kind === "monitor";
    const wasDirty = dirty();
    const draft = reconcileDraft(state.applied, state.draft, value.config, force || !wasDirty);
    const appliedKey = knownKey !== undefined ? knownKey : initialSourceKey(value.config, state.sources, value.source);
    const changed = !equal(state.draft, draft);
    publish({ snapshot: value, applied: clone(value.config), draft, appliedKey,
      selectedKey: !wasDirty || force ? appliedKey : state.selectedKey,
      connected: true, connectionError: null, monitorIntent: state.monitorIntent || existingMonitorIntent,
      ...(emergency ? { busy: false, stopping: false, recoveryPending: false, monitorIntent: false, applyError: null, preview: { ...state.preview, after: null, feedback: "Обновляем подтверждённые настройки" } } : {}),
    });
    if (emergency) {
      const problem = value.runtime.phase === "recovery-error" || value.runtime.unsaved;
      const reason = value.runtime.last_error || value.runtime.reason || "";
      const ordinary = ordinaryStopSequence !== null && value.emergency_sequence > ordinaryStopSequence;
      ordinaryStopSequence = null;
      notify(ordinary
        ? problem ? `Эффект выключен, но восстановление или сохранение требует внимания.${reason ? ` ${reason}` : ""}` : "Эффект выключен. Окно и экран восстановлены."
        : `Получена аварийная команда. Неприменённые изменения отменены.${problem && reason ? ` ${reason}` : ""}`, problem ? "warning" : "success");
    }
    if (changed) { revision++; previewRevision++; schedulePreview(100); }
    updateProbe();
    return true;
  }

  async function refreshSources(preserve = true) {
    const life = generation, request = ++sourceRequest, sentRevision = revision;
    const value = await api.request("sources");
    if (!current(life) || request !== sourceRequest) return;
    if (!Array.isArray(value.windows) || !Array.isArray(value.monitors)) throw new Error("Не удалось прочитать список источников.");
    let selectedKey = state.selectedKey;
    let appliedKey = initialSourceKey(state.applied, value, state.snapshot?.source);
    if (!preserve && revision === sentRevision) selectedKey = appliedKey;
    else if (selectedKey && !value.windows.some((item) => sourceKey(item) === selectedKey) && !value.monitors.some((item) => `monitor:${item.index}` === selectedKey)) {
      selectedKey = "";
      notify("Выбранный источник больше недоступен. Выберите окно из обновлённого списка.", "warning");
    }
    let draft = state.draft;
    if (draft && revision === sentRevision && !state.snapshot?.runtime.enabled && (!selectedKey || (!preserve && state.applied?.target.kind === "monitor"))) {
      const suggestion = value.suggested && value.windows.find((item) => item.hwnd === value.suggested.hwnd && item.pid === value.suggested.pid && item.process_created === value.suggested.process_created);
      const candidate = suggestion || (value.windows.length === 1 ? value.windows[0] : null);
      if (candidate) {
        selectedKey = sourceKey(candidate);
        draft = selectSource(draft, value.windows, value.monitors, selectedKey).config;
        revision++; previewRevision++;
      }
    }
    publish({ sources: value, selectedKey, appliedKey, draft });
  }

  async function previewTick() {
    if (!running) return;
    if (!state.draft || !state.connected || awaitingEmergency() || env.hidden() || state.tab !== "screen" || !state.previewOpen || previewBusy) { schedulePreview(); return; }
    const since = env.now() - previewCompletedAt;
    if (since < 510) { schedulePreview(510 - since); return; }
    const config = previewConfig(powerConfig(state.draft));
    if (draftIssues(config, true).length) {
      publish({ preview: { ...state.preview, feedback: "Исправьте сочетание настроек" } });
      schedulePreview(); return;
    }
    const before = !state.preview.before, ownRevision = previewRevision, life = generation;
    previewBusy = true;
    try {
      const result = await api.request("preview", { config, width: 640, height: 360, time: previewSeconds(env.now() - animationStart), before });
      if (!current(life) || ownRevision !== previewRevision) return;
      if (result.mime !== "image/png" || (!result.png_base64 && !(api.demoMode && result.png_url))) throw new Error("Предпросмотр не содержит PNG.");
      const image = api.demoMode && result.png_url ? result.png_url : `data:image/png;base64,${result.png_base64}`;
      const fixtureLabel = before || config.effects.intensity === 0 ? "Исходный кадр · без эффекта" : "Готовый GLSL-пример · 100%";
      publish({ preview: { ...state.preview, [before ? "before" : "after"]: image,
        fixture: result.fixture === true, feedback: result.fixture ? fixtureLabel : "" } });
    } catch (error) {
      if (current(life) && ownRevision === previewRevision) publish({ preview: { ...state.preview, feedback: error.message } });
    } finally {
      if (current(life)) { previewCompletedAt = env.now(); previewBusy = false; schedulePreview(); }
    }
  }

  async function refreshStatus() {
    const emergencyPending = awaitingEmergency();
    if (!running || !state.connected || statusBusy || (state.busy && !emergencyPending) || (env.hidden() && !emergencyPending)) return;
    statusBusy = true;
    const epoch = mutation, life = generation;
    try {
      const value = await api.request("snapshot");
      if (current(life) && epoch === mutation && (!state.busy || emergencyPending)) acceptSnapshot(value);
    } catch (error) {
      if (current(life) && epoch === mutation && (!state.busy || emergencyPending)) connectionError(error);
    } finally {
      if (current(life)) {
        statusBusy = false;
        if (state.connected && awaitingEmergency()) {
          env.clearTimeout(recoveryTimer);
          recoveryTimer = env.setTimeout(refreshStatus, 100);
        }
      }
    }
  }

  function observeEmergency(sequence) {
    if (!Number.isSafeInteger(sequence) || sequence <= observedSequence) return;
    observedSequence = sequence;
    cancelPendingWork();
    mutation++; revision++; previewRevision++;
    publish({ busy: true, recoveryPending: true, monitorIntent: false, applyError: null, draft: state.applied ? clone(state.applied) : null, selectedKey: state.appliedKey,
      preview: { ...state.preview, after: null, feedback: "Ожидаем состояние после остановки" } });
    notify(ordinaryStopSequence !== null ? "Выключаем эффект и восстанавливаем окно и экран…" : "Получена аварийная команда. Черновик отменён; ожидаем результат восстановления.", ordinaryStopSequence !== null ? "success" : "warning");
    updateProbe();
    void refreshStatus();
  }

  async function applyDraft(startEffect = false, automatic = false, override = null) {
    if (!state.draft || !state.connected || state.busy) return;
    if (startEffect && state.recoveryPending) { notify("Выключение ещё не подтверждено. Повторите выключение, чтобы восстановить окно и экран.", "warning"); return; }
    const requestDraft = override || state.draft;
    const launchIssues = startEffect ? startIssues({ ...state, draft: requestDraft }) : [];
    if (launchIssues.length) { notify(launchIssues.join(" "), "warning"); return; }
    let chosen;
    try { chosen = !startEffect && !state.selectedKey && !state.appliedKey && canEditWithoutSource(state.applied, requestDraft) ? { config: clone(requestDraft), source: null } : selectSource(requestDraft, state.sources.windows, state.sources.monitors, state.selectedKey); }
    catch (error) { notify(error.message, "error"); return; }
    if (startEffect) { chosen.config = powerConfig(chosen.config); chosen.config.enabled = true; }
    // Desktop effects have their authored strength. Loading a profile remains
    // read-only; legacy intensity is normalized only for an explicit apply.
    chosen.config.effects.intensity = 1;
    const issues = draftIssues(chosen.config, true);
    if (issues.length) { notify(issues.join(" "), "warning"); return; }
    const sentRevision = revision, sentDraft = clone(state.draft), sentKey = state.selectedKey, epoch = ++mutation, life = generation;
    pendingEnable = startEffect ? epoch : 0;
    publish({ busy: true, startingEffect: startEffect || chosen.config.enabled || chosen.config.aspect.enabled, applyError: null, notice: null });
    try {
      const payload = { config: chosen.config, expected_emergency_sequence: state.snapshot.emergency_sequence };
      if (chosen.source) payload.source = clone(chosen.source);
      const value = await api.request("apply", payload);
      if (!current(life) || epoch !== mutation) return;
      const rebased = revision !== sentRevision ? rebaseAfterApply(sentDraft, state.draft, value.config) : null;
      if (!acceptSnapshot(value, revision === sentRevision, sentKey) || epoch !== mutation) return;
      if (rebased) { publish({ draft: rebased }); previewRevision++; schedulePreview(100); }
      if (!automatic) notify(chosen.config.enabled
        ? "Эффект включён с выбранными настройками. Вернитесь в окно игры."
        : "Настройки сохранены. Эффект остаётся выключенным.");
    } catch (error) {
      if (!current(life) || epoch !== mutation) return;
      try {
        const actual = error.snapshot || await api.request("snapshot");
        if (!current(life) || epoch !== mutation) return;
        const rebased = error.applied && revision !== sentRevision ? rebaseAfterApply(sentDraft, state.draft, actual.config) : null;
        if (acceptSnapshot(actual, (error.applied || automatic) && revision === sentRevision, error.applied ? sentKey : undefined) && epoch === mutation && rebased) { publish({ draft: rebased }); previewRevision++; schedulePreview(100); }
      } catch { /* preserve edits until a confirmed status is available */ }
      if (!current(life) || epoch !== mutation) return;
      if (error.applied) notify(`Применено, но не сохранено: ${error.message}`, "warning");
      else publish({ applyError: applyFailure(error), notice: null, tab: "screen" });
    } finally { if (pendingEnable === epoch) pendingEnable = 0; if (current(life) && epoch === mutation) { publish({ busy: false, startingEffect: false }); if (state.livePending) queueLive(0); } }
  }

  async function engineAction(type, message, discard = false) {
    if (["disable", "emergency", "restore", "toggle"].includes(type)) cancelLive(["disable", "emergency"].includes(type));
    if (type === "emergency") { cancelPendingWork(); publish({ recoveryPending: true }); }
    if (!running) { notify("Нет связи с движком. Восстановление не подтверждено.", "error"); return; }
    const epoch = ++mutation, life = generation;
    publish({ busy: true, ...(type === "emergency" ? { stopping: true } : {}) });
    try {
      const value = await api.request(type);
      if (!current(life) || epoch !== mutation) return;
      if (!acceptSnapshot(value, discard) || epoch !== mutation) return;
      notify(message);
    } catch (error) {
      if (!current(life) || epoch !== mutation) return;
      try {
        const actual = error.snapshot || await api.request("snapshot");
        if (!current(life) || epoch !== mutation) return;
        acceptSnapshot(actual);
      } catch { /* no fabricated success when cleanup is unconfirmed */ }
      if (current(life) && epoch === mutation) notify(error.message, "error");
    } finally { if (current(life) && epoch === mutation) publish({ busy: false, ...(type === "emergency" ? { stopping: false } : {}) }); }
  }

  async function refreshShaders() {
    const life = generation;
    try {
      const request = ++shaderRequest;
      const result = await api.request("shaders");
      if (request === shaderRequest && current(life) && Array.isArray(result.items)) publish({ shaders: result.items });
    } catch (error) { if (current(life)) notify("Не удалось прочитать пользовательские эффекты.", "warning"); }
  }

  async function loadUpdater() {
    const life = generation;
    try { const updater = await api.updaterStatus(); if (current(life)) publish({ updater }); }
    catch (error) { if (current(life)) publish({ updater: { state: "error", reason: error.message } }); }
  }
  async function loadHost() {
    if (!api.hostStatus) return;
    const life = generation;
    try { const host = await api.hostStatus(); if (current(life)) publish({ host }); }
    catch (error) { if (current(life)) publish({ host: { tray_available: false, tray_error: error.message } }); }
  }

  async function start() {
    if (starting) return;
    starting = true; running = true;
    subscriptions.splice(0).forEach((stop) => stop());
    env.clearTimeout(previewTimer); env.clearTimeout(recoveryTimer); env.clearInterval(refreshTimer);
    mutation++; previewRevision++; previewBusy = false; statusBusy = false;
    const life = ++generation;
    publish({ connecting: true, busy: awaitingEmergency() });
    const pending = [];
    try {
      if (!subscriptions.length) {
        // One subscription per event, irrespective of component rerenders.
        const handlers = {
          engine_event: (event) => {
            if (!current(life)) return;
            if (event?.event === "fatal") {
              // Fatal is a lifecycle boundary, not an ordinary poll error.
              // No earlier reply may reconnect the UI until an explicit Retry.
              dispose();
              publish({ connected: false, connecting: false, busy: false,
                connectionError: event.data?.message || "Движок завершился без подтверждения восстановления.",
                preview: { ...state.preview, after: null } });
            }
            else if (event?.event === "state_changed" && event.data?.emergency_sequence !== undefined) observeEmergency(event.data.emergency_sequence);
            else if (state.connected) void refreshStatus();
          },
          updater_state: (updater) => { if (current(life)) publish({ updater }); },
          host_status_changed: (host) => { if (current(life)) publish({ host }); },
          host_error: (error) => { if (current(life)) notify(error?.message || "Не удалось выполнить действие из трея.", "error"); },
        };
        for (const [name, handler] of Object.entries(handlers)) {
          const stop = await api.subscribe(name, handler);
          if (!current(life)) { stop(); return; }
          pending.push(stop);
        }
        subscriptions = pending.splice(0);
      }
      let value = await api.request("snapshot");
      if (!current(life)) return;
      if (!acceptSnapshot(value)) {
        value = await api.request("snapshot");
        if (!current(life)) return;
        if (!acceptSnapshot(value)) throw new Error("Ожидается состояние после аварийной остановки. Повторите подключение.");
      }
      await refreshSources(false);
      if (!current(life)) return;
      schedulePreview(50);
      env.clearInterval(refreshTimer);
      refreshTimer = env.setInterval(refreshStatus, 1500);
      await Promise.all([loadUpdater(), loadHost(), refreshShaders()]);
    } catch (error) {
      if (current(life)) connectionError(error);
    } finally {
      pending.forEach((stop) => stop());
      if (current(life)) { starting = false; publish({ connecting: false }); }
    }
  }

  function dispose() {
    running = false; starting = false; generation++; mutation++; previewRevision++;
    env.clearTimeout(previewTimer); env.clearTimeout(recoveryTimer); env.clearTimeout(sourceTimer); env.clearTimeout(liveTimer); env.clearInterval(refreshTimer);
    subscriptions.splice(0).forEach((stop) => stop());
    previewBusy = false; statusBusy = false;
    publish({ livePending: false, startingEffect: false, stopping: false, launching: false, shaderImporting: false });
  }

  return {
    subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener); },
    getSnapshot: () => state, start, dispose, refreshStatus, refreshSources, loadUpdater, refreshShaders,
    activate() { void refreshStatus(); schedulePreview(100); },
    setPreviewOpen(previewOpen) { publish({ previewOpen }); if (previewOpen) schedulePreview(0); },
    setTab(tab) { publish({ tab }); if (tab === "screen") schedulePreview(100); if (tab === "about") void loadUpdater(); },
    edit(path, value, custom = false) {
      if (!state.draft) return;
      const draft = clone(state.draft), parts = path.split("."), key = parts.pop();
      parts.reduce((part, field) => part[field], draft)[key] = value;
      if (custom) draft.preset = "Custom";
      change(draft);
    },
    preset(name) { if (state.draft) change(selectPreset(state.draft, state.snapshot.presets, name)); },
    liveEdit(path, value, custom = false) {
      if (!state.draft || !/^(effects\.|shader\.params\.)/.test(path)) return;
      const draft = clone(state.draft), parts = path.split("."), key = parts.pop();
      parts.reduce((part, field) => part[field], draft)[key] = value;
      if (custom) draft.preset = "Custom";
      change(draft); queueLive();
    },
    livePreset(name) {
      if (!state.draft) return;
      change(fullEffectConfig(selectPreset(state.draft, state.snapshot.presets, name)));
      queueLive();
    },
    selectShader(id, allowWarp = false) {
      const descriptor = state.shaders.find((item) => item.id === id);
      if (!state.draft || !descriptor) return;
      if (state.draft.target.kind !== "window") { notify("Для своего эффекта выберите окно игры.", "warning"); return; }
      if (descriptor.coordinates === "warp" && state.draft.input_mode !== "keyboard-gamepad" && !allowWarp) return "consent-required";
      const draft = fullEffectConfig(state.draft);
      draft.shader = { id, params: Array.from({ length: 8 }, (_, index) => descriptor.parameters[index]?.default ?? 0) };
      if (descriptor.coordinates === "warp") draft.input_mode = "keyboard-gamepad";
      change(draft); queueLive();
    },
    async importShader(source) {
      if (!state.connected || state.shaderImporting || state.stopping || state.recoveryPending) return null;
      if (new TextEncoder().encode(source).length > 65536) throw new Error("Файл эффекта больше 64 КиБ.");
      if (new TextEncoder().encode(JSON.stringify({ v: 1, id: "shader-import", type: "shader_import", payload: { source } })).length > 120 * 1024 - 256) throw new Error("Код слишком велик для передачи. Уменьшите текст эффекта или комментарии.");
      const life = generation, sequence = observedSequence, importEpoch = ++shaderImportEpoch;
      shaderRequest++;
      publish({ shaderImporting: true });
      try {
        const { shader: descriptor } = await api.request("shader_import", { source });
        if (!current(life) || sequence !== observedSequence || importEpoch !== shaderImportEpoch) return null;
        publish({ shaders: [...state.shaders.filter((item) => item.id !== descriptor.id), descriptor] });
        return descriptor;
      } finally { if (current(life) && importEpoch === shaderImportEpoch) publish({ shaderImporting: false }); }
    },
    async applyAdvanced(original, edited) {
      if (!state.draft || state.busy) return;
      cancelLive();
      await applyDraft(false, false, mergeAdvanced(state.draft, original, edited));
    },
    async chooseSource(key) {
      if (!state.draft || state.busy) return;
      sourceChoice++;
      cancelLive();
      const chosen = selectSource(state.draft, state.sources.windows, state.sources.monitors, key);
      change(chosen.config, key);
      if (state.snapshot?.runtime.enabled) await applyDraft(true, true);
    },
    source(key) {
      if (!state.draft) return;
      sourceChoice++;
      if (key.startsWith("monitor:")) publish({ monitorIntent: true });
      else publish({ monitorIntent: false });
      try { change(key ? selectSource(state.draft, state.sources.windows, state.sources.monitors, key).config : clone(state.draft), key); }
      catch (error) { notify(error.message, "error"); }
    },
    backend(value) {
      if (!state.draft) return;
      const draft = clone(state.draft); draft.mode = value === "overlay" ? "overlay" : "full";
      if (draft.mode === "full") draft.capture.transfer = value === "full-gpu" ? "gpu" : "compatibility";
      change(draft);
    },
    aspect(value) {
      if (!state.draft) return;
      const draft = clone(state.draft); draft.aspect.enabled = value !== "off";
      if (draft.aspect.enabled) draft.aspect.method = value;
      change(draft);
    },
    noAlt() { if (state.draft) change({ ...clone(state.draft), hotkeys: { toggle: "Ctrl+Shift+9", emergency: "Ctrl+Shift+0" } }); },
    toggleProbe() { probeBaseline = undefined; publish({ probeOpen: !state.probeOpen }); updateProbe(); void refreshStatus(); },
    discard() { publish({ monitorIntent: false }); if (state.applied) change(clone(state.applied), state.appliedKey); },
    apply: () => applyDraft(false),
    startEffect: () => { liveSuppressed = false; return applyDraft(true); },
    powerOff: () => { ordinaryStopSequence = state.snapshot?.emergency_sequence ?? observedSequence; return engineAction("emergency", "Эффект выключен. Окно и экран восстановлены.", true); },
    disable: () => engineAction("disable", "Эффект выключен. Настройки и формат игры сохранены."),
    emergency: () => engineAction("emergency", "Фильтр остановлен. Движок подтвердил восстановление.", true),
    toggle: () => engineAction("toggle", "Состояние фильтра изменено."),
    restore: () => engineAction("restore", "Собственные изменения окна и видеорежима сняты.", true),
    confirm: () => engineAction("confirm", "Новый видеорежим подтверждён."),
    reportError: (error) => notify(error.message || String(error), "error"),
    async launchTestcard() {
      if (state.launching || state.busy) return;
      const life = generation, intent = ++sourceChoice, sentRevision = revision, sequence = observedSequence;
      publish({ launching: true });
      try {
        const owned = await api.launchTestcard();
        if (!current(life)) return;
        let attempts = 0;
        const poll = async () => {
          if (!current(life)) return;
          if (intent !== sourceChoice || sequence !== observedSequence || revision !== sentRevision) { publish({ launching: false }); return; }
          try {
            const request = ++sourceRequest, value = await api.request("sources");
            if (!current(life)) return;
            if (request !== sourceRequest || intent !== sourceChoice || sequence !== observedSequence || revision !== sentRevision) { publish({ launching: false }); return; }
            const card = value.windows?.find((item) => item.pid === owned.pid);
            publish({ sources: value });
            if (card) {
              // A launch request permits selecting this owned PID while off;
              // it never changes the currently running game's capture target.
              if (!state.snapshot?.runtime.enabled && !state.busy && !pendingEnable) {
                change(selectSource(state.draft, value.windows, value.monitors, sourceKey(card)).config, sourceKey(card));
                notify("Тестовая игра выбрана. Нажмите «Включить».");
              } else notify("Тестовая игра открыта; текущая игра не переключена.");
              publish({ launching: false });
            } else if (++attempts < 10) sourceTimer = env.setTimeout(poll, 300);
            else { publish({ launching: false }); notify("Тестовая игра запущена. Если окно ещё не появилось, обновите список.", "warning"); }
          } catch (error) { if (current(life)) { publish({ launching: false }); notify(error.message, "error"); } }
        };
        env.clearTimeout(sourceTimer);
        await poll();
      } catch (error) { if (current(life)) { publish({ launching: false }); notify(error.message, "error"); } }
    },
    async diagnostics() {
      const life = generation;
      try { const value = await api.request("diagnostics"); if (!current(life)) return; await api.copyText(JSON.stringify(value, null, 2)); if (current(life)) notify("Диагностика скопирована."); }
      catch (error) { if (current(life)) notify(error.message, "error"); }
    },
    async checkUpdates() {
      const life = generation;
      publish({ updater: { ...state.updater, state: "checking" } });
      try { const updater = await api.checkUpdates(); if (current(life)) publish({ updater }); }
      catch (error) { if (current(life)) publish({ updater: { ...state.updater, state: "error", reason: error.message } }); }
    },
    async installUpdate(version) {
      const life = generation;
      try { const updater = await api.installUpdate(version); if (current(life)) publish({ updater }); }
      catch (error) { if (current(life)) { publish({ updater: { ...state.updater, state: "error", reason: error.message } }); notify(error.message, "error"); } }
    },
    // The host can still quit after the engine has fatally exited.
    async quit() { const life = generation; try { await api.quitApplication(); } catch (error) { if (life === generation) notify(error.message, "error"); } },
  };
}
