import { useState } from "react"
import { createRoot } from "react-dom/client"
import { EffectLibrary } from "../EffectLibrary"
import type { Config, ControllerState, Effects, ShaderDescriptor } from "../types"
import "../styles.css"

// Browser-only QA fixture. It uses the real picker but never opens an engine,
// saves a profile, imports a shader, or registers keyboard shortcuts.
const effects: Effects = {
  intensity: 1, noise_seed: 1, freeze_noise: true,
  crt: { scanlines: 0, mask: 0, bloom: 0, softness: 0, vignette: 0, curvature: 0 },
  vhs: { chroma_bleed: 0, noise: 0, jitter: 0, tracking: 0 },
}
const initialConfig: Config = {
  version: 1, enabled: false, mode: "full", input_mode: "mouse-exact", preset: "VHS Tape",
  capture: { transfer: "auto" }, target: { kind: "window", monitor: 0 },
  screen: { shape: "flat", corner_radius: 0, curvature: 0, glass: 0 }, effects,
  aspect: { enabled: false, method: "mask", scale: "fit", source_dar: 0 },
  hotkeys: { toggle: "Ctrl+Shift+9", emergency: "Ctrl+Shift+0" },
}
const presets = ["Subtle CRT", "CRT Classic", "Soft TV", "VHS Light", "VHS Tape"].map(name => ({ name, effects }))
const shaders: ShaderDescriptor[] = Array.from({ length: 50 }, (_, index) => ({
  id: `qa-custom-${index + 1}`,
  version: 1,
  name: index === 0
    ? "Очень длинное кириллическое название пользовательского эффекта с мягким свечением старого телевизора и цветными воспоминаниями о летних каникулах"
    : index === 49 ? "Янтарный вечер" : `Пользовательский эффект ${String(index + 1).padStart(2, "0")}`,
  description: `Тестовый шейдер ${index + 1} из 50. Используется только для проверки списка.`,
  coordinates: "preserve",
  parameters: [],
}))

function EffectPickerSmoke() {
  const [draft, setDraft] = useState<Config>(initialConfig)
  const [importCalls, setImportCalls] = useState(0)
  const selectedName = draft.shader?.id ? shaders.find(item => item.id === draft.shader?.id)?.name || "Эффект недоступен" : draft.preset
  const state: ControllerState = {
    snapshot: {
      config: draft,
      runtime: { enabled: false, phase: "disabled", backend: "qa-fixture", reason: "Только UI", unsaved: false },
      source: null, presets, emergency_sequence: 0,
      hotkeys: { registered: false, toggle_received: 0, emergency_received: 0 },
    },
    applied: draft, draft, sources: { windows: [], monitors: [] }, selectedKey: "", appliedKey: "",
    connected: true, connecting: false, busy: false, startingEffect: false, stopping: false,
    recoveryPending: false, launching: false, livePending: false, shaders, shaderImporting: false,
    monitorIntent: false, previewOpen: false, applyError: null, notice: null, connectionError: null,
    tab: "home", preview: { before: null, after: null, feedback: "QA fixture", fixture: true },
    updater: null, host: null, probeOpen: false, probe: null,
  }

  function chooseEffect(value: string) {
    setDraft(current => value.startsWith("shader:")
      ? { ...current, shader: { id: value.slice(7), params: [] }, effects: { ...current.effects, intensity: 1 } }
      : { ...current, preset: value.slice(8), shader: undefined, effects: { ...current.effects, intensity: 1 } })
  }

  return <div className="panel-frame" style={{ maxWidth: 440, marginInline: "auto", borderInline: "1px solid var(--border)" }}>
    <header className="panel-header"><h1 style={{ fontSize: 16, fontWeight: 650 }}>QA · список эффектов</h1></header>
    <div className="demo-banner">Тестовая страница · 5 встроенных + 50 пользовательских · без движка</div>
    <main className="panel-main" style={{ paddingTop: 16 }}>
      <EffectLibrary state={state} chooseEffect={chooseEffect} onImport={() => setImportCalls(count => count + 1)}/>
      <p className="hint">Проверьте прокрутку, Home / End, стрелки и поиск набором «Янтарный». Первое пользовательское название проверяет перенос длинного текста.</p>
      <p className="hint" role="status" data-testid="qa-import-status">{importCalls ? `Вызван импорт · ${importCalls}` : "Импорт ещё не вызван"}</p>
    </main>
    <footer className="panel-footer" style={{ padding: "12px 18px" }}>
      <span className="hint">Выбранный эффект</span>
      <output data-testid="qa-selected" data-value={draft.shader?.id ? `shader:${draft.shader.id}` : `builtin:${draft.preset}`} style={{ display: "block", fontSize: 13, lineHeight: 1.4, overflowWrap: "anywhere" }}>{selectedName}</output>
    </footer>
  </div>
}

const root = document.getElementById("root")
if (!root) throw new Error("QA fixture root is missing")
createRoot(root).render(<EffectPickerSmoke />)
