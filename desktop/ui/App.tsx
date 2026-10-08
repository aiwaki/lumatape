import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react"
import { Settings2, Power, Columns2, Maximize2, ChevronDown, RefreshCw, Copy, Download, LoaderCircle, Gamepad2, Minimize2, LogOut, ChevronRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Progress } from "@/components/ui/progress"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { createController } from "./controller.mjs"
import { captureShortcut, mergeAdvanced } from "./interaction.mjs"
import { runEmergency } from "./actions.mjs"
import { clone, equal, powerPresentation, backendValue, sourceKey, sourceLabel, draftIssues, startIssues, canEditWithoutSource, runtimePresentation, updatePresentation } from "./state.mjs"
import * as api from "./api.js"
import type { Config, ControllerState, EngineController } from "./types"
import packageInfo from "../package.json"
import { ShaderImport } from "./ShaderImport"
import { EffectLibrary, effectDescriptions } from "./EffectLibrary"

type Context = { state: ControllerState; controller: EngineController }
type Modal = { kind: "preview" } | { kind: "advanced" } | { kind: "import" } | { kind: "warp"; id: string } | { kind: "quit" } | { kind: "update"; version: string }

function Hint({ children }: { children: ReactNode }) { return <p className="hint">{children}</p> }
function IconButton({ label, children, onClick, disabled }: { label: string; children: ReactNode; onClick: () => void; disabled?: boolean }) {
  return <Tooltip><TooltipTrigger asChild><Button variant="ghost" size="icon-sm" aria-label={label} onClick={onClick} disabled={disabled}>{children}</Button></TooltipTrigger><TooltipContent>{label}</TooltipContent></Tooltip>
}
function Field({ id, label, value, options, onChange, disabled }: { id: string; label: string; value: string; options: { value: string; label: string; disabled?: boolean }[]; onChange: (value: string) => void; disabled?: boolean }) {
  return <div className="field"><Label htmlFor={id}>{label}</Label><Select value={value} onValueChange={onChange} disabled={disabled}><SelectTrigger id={id} className="w-full"><SelectValue placeholder="Выберите…"/></SelectTrigger><SelectContent position="popper">{options.map((option) => <SelectItem key={option.value} value={option.value} disabled={option.disabled}>{option.label}</SelectItem>)}</SelectContent></Select></div>
}
function TestScene({ state, onExpand, broadcast = false }: { state: ControllerState; onExpand?: () => void; broadcast?: boolean }) {
  const [view, setView] = useState("after"), preview = state.preview
  const choices = [...(state.snapshot?.presets || []).map(p => `builtin:${p.name}`), ...state.shaders.map(s => `shader:${s.id}`)]
  const selected = state.draft?.shader?.id ? `shader:${state.draft.shader.id}` : `builtin:${state.draft?.preset}`
  const ordinal = choices.indexOf(selected) + 1
  return <div className={broadcast ? "test-scene broadcast-scene" : "test-scene"}>
    {!broadcast ? <div className="scene-toolbar">{onExpand ? <button onClick={onExpand} aria-label="Развернуть предпросмотр">Тестовая сцена<Maximize2 className="scene-expand"/></button> : <span>Тестовая сцена</span>}<ToggleGroup type="single" value={view} onValueChange={(value) => value && setView(value)} size="sm" variant="outline" aria-label="Сравнение тестовой сцены"><ToggleGroupItem value="before">До</ToggleGroupItem><ToggleGroupItem value="split">A/B</ToggleGroupItem><ToggleGroupItem value="after">После</ToggleGroupItem></ToggleGroup></div> : null}
    <div className={broadcast ? "preview-stage air-monitor" : "preview-stage"} data-view={view} aria-label="Предпросмотр эффекта">
      {preview.after ? <img className="preview-after" src={preview.after} alt="Тестовая сцена после обработки"/> : null}
      {preview.before ? <div className="preview-before"><img src={preview.before} alt="Исходная тестовая сцена"/></div> : null}
      {!preview.after && view !== "before" ? <div className="preview-placeholder" role="status">{preview.feedback || "Готовим тестовую сцену…"}</div> : null}
      {preview.after && view === "split" ? <div className="preview-divider"/> : null}
      {broadcast ? <><div className="air-monitor-top"><span>{view === "before" ? "ОРИГИНАЛ" : "С ЭФФЕКТОМ"}</span>{ordinal > 0 ? <span className="air-channel-number" aria-hidden="true">{String(ordinal).padStart(2, "0")}</span> : null}</div><span className="scene-caption">{view === "before" ? "Без эффекта" : effectName(state)}</span><button type="button" className="air-compare" aria-pressed={view === "before"} disabled={!preview.before || (view !== "before" && !preview.after)} onClick={() => setView(view === "before" ? "after" : "before")}><Columns2 size={15}/>{view === "before" ? "Вернуть эффект" : "Сравнить"}</button></> : null}
    </div>
    <Hint>{preview.fixture ? "Сохранённый GLSL-пример · не захват игры" : "Предпросмотр работает независимо от эффекта в игре"}</Hint>
    {preview.feedback && (!preview.fixture || !preview.after) ? <Hint>{preview.feedback}</Hint> : null}
  </div>
}
function effectName(state: ControllerState) {
  return state.draft?.shader?.id ? state.shaders.find((item) => item.id === state.draft?.shader?.id)?.name || "Эффект недоступен" : state.draft?.preset === "Custom" ? "Сохранённый эффект" : state.draft?.preset || "Эффект"
}
function SourcePicker({ state, controller }: Context) {
  const draft = state.draft!
  const sourceValue = !state.monitorIntent && state.selectedKey.startsWith("monitor:") ? "" : state.selectedKey
  return (
    <section className="source-strip" aria-label="Окно игры">
      <Gamepad2 className="source-icon" aria-hidden="true"/>
      <div className="source-choice"><Label className="sr-only" htmlFor="source">Окно игры</Label><Select value={sourceValue} disabled={!state.connected || state.busy} onValueChange={(key) => void controller.chooseSource(key).catch(controller.reportError)}><SelectTrigger id="source"><SelectValue placeholder={state.sources.windows.length ? "Выберите окно" : "Откройте игру"}/></SelectTrigger><SelectContent position="popper">{state.monitorIntent && draft.target.kind === "monitor" && state.selectedKey.startsWith("monitor:") ? <SelectItem value={state.selectedKey} disabled>Весь монитор · сохранённый режим</SelectItem> : null}{state.sources.windows.map((source) => <SelectItem key={sourceKey(source)} value={sourceKey(source)} title={sourceLabel(source, state.sources.windows)}>{sourceLabel(source, state.sources.windows)}</SelectItem>)}{!state.sources.windows.length ? <SelectItem value="no-windows" disabled>Откройте игру и обновите список</SelectItem> : null}</SelectContent></Select></div>
      <IconButton label="Обновить окна" disabled={!state.connected} onClick={() => void controller.refreshSources().catch(controller.reportError)}><RefreshCw/></IconButton>
    </section>
  )
}
function Home({ state, controller, chooseEffect, onImport, onPreview }: Context & { onPreview: () => void; chooseEffect: (value: string) => void; onImport: () => void }) {
  const draft = state.draft!, shader = state.shaders.find(item => item.id === draft.shader?.id)
  const choices = [...(state.snapshot?.presets || []).map(p => `builtin:${p.name}`), ...state.shaders.map(s => `shader:${s.id}`)]
  const index = choices.indexOf(draft.shader?.id ? `shader:${draft.shader.id}` : `builtin:${draft.preset}`) + 1
  return <>
    <div className="air-source"><span><span className="air-source-light" aria-hidden="true"/>Тестовая сцена</span><button className="air-source-preview" onClick={onPreview} aria-label="Развернуть предпросмотр">ПРЕДПРОСМОТР</button></div>
    <section className="air-preview" aria-label="Выбранный эффект">
      <TestScene state={state} onExpand={onPreview} broadcast/>
      <div className="air-now"><div className="air-now-heading"><span>ВАШ СИГНАЛ</span><span className="air-now-count">{index > 0 ? String(index).padStart(2, "0") : "—"} / {String(choices.length).padStart(2, "0")}</span></div><h1 title={effectName(state)}>{effectName(state)}</h1><p>{shader?.description || (shader ? "Пользовательский шейдер" : effectDescriptions[draft.preset])}</p></div>
    </section>
    <EffectLibrary state={state} chooseEffect={chooseEffect} onImport={onImport}/>

    {state.monitorIntent && draft.mode === "overlay" && draft.target.kind === "monitor" ? <Hint>Лёгкий режим на мониторе: текстура экрана, без обработки цвета.</Hint> : null}
  </>
}
function Shortcut({ id, label, value, onChange }: { id: string; label: string; value: string; onChange: (value: string) => void }) {
  const [recording, setRecording] = useState(false), [error, setError] = useState("")
  return <div className="field"><Label htmlFor={id}>{label}</Label><Button id={id} data-shortcut-recorder={recording ? "active" : undefined} variant="outline" className="justify-start" onClick={() => { setRecording(true); setError("") }} onBlur={() => setRecording(false)} onKeyDown={(event) => { if (!recording) return; event.preventDefault(); event.stopPropagation(); const result = captureShortcut(event); if (result?.cancelled) setRecording(false); else if (result?.error) setError(result.error); else if (result?.value) { onChange(result.value); setRecording(false); setError("") } }}>{recording ? "Нажмите сочетание… (Esc — отмена)" : value}</Button>{error ? <p className="text-xs text-destructive">{error}</p> : null}</div>
}
function Advanced({ state, controller, value, edit, dialog }: Context & { value: Config | null; edit: (path: string, value: string | number | boolean) => void; dialog: (value: Modal) => void }) {
  const update = updatePresentation(state.updater || {}), gpuUnavailable = state.snapshot?.runtime.gpu?.state === "unavailable", cpuUnavailable = state.snapshot?.runtime.compatibility?.state === "unavailable"
  return <div className="advanced-sections">{value ? <><section><h3>Окно игры</h3><SourcePicker state={state} controller={controller}/><Hint>Выбор игры применяется сразу.</Hint></section><section><h3>Изображение и формат</h3><Field id="shape" label="Форма" value={value.screen.shape} onChange={(next) => edit("screen.shape", next)} options={[{ value: "flat", label: "Плоский" }, { value: "rounded", label: "Скруглённый" }, { value: "convex", label: "Выпуклый" }]}/><Field id="aspect" label="Формат 4:3" value={value.aspect.enabled ? value.aspect.method : "off"} onChange={(next) => { edit("aspect.enabled", next !== "off"); if (next !== "off") edit("aspect.method", next) }} options={[{ value: "off", label: "Исходный" }, { value: "mask", label: "Чёрные поля" }, { value: "window", label: "Изменить окно игры" }, { value: "system", label: "Системный видеорежим…" }]}/>{value.aspect.enabled && value.aspect.method === "system" ? <Hint>Изменяет режим монитора. Без подтверждения за 15 секунд он восстановится.</Hint> : null}<Field id="input-mode" label="Управление" value={value.input_mode} onChange={(next) => edit("input_mode", next)} options={[{ value: "mouse-exact", label: "Точная мышь" }, { value: "keyboard-gamepad", label: "Клавиатура / геймпад" }]}/><details className="disclosure"><summary>Масштаб изображения<ChevronDown/></summary><div className="format-fields"><Field id="scale" label="Масштаб" value={value.aspect.scale} onChange={(next) => edit("aspect.scale", next)} options={[{ value: "fit", label: "Вписать" }, { value: "crop", label: "Обрезать" }, { value: "stretch", label: "Растянуть" }]}/><Field id="dar" label="Исходные пропорции" value={String(value.aspect.source_dar)} onChange={(next) => edit("aspect.source_dar", Number(next))} options={[{ value: "0", label: "Авто" }, { value: String(4 / 3), label: "4:3 (например 320×200)" }, ...(![0, 4 / 3].includes(value.aspect.source_dar) ? [{ value: String(value.aspect.source_dar), label: "Из профиля" }] : [])]}/></div></details><Button variant="outline" size="sm" disabled={!state.connected} onClick={() => void controller.restore()}>Восстановить окно и экран</Button></section>
  <section><h3>Обработка</h3>{state.snapshot?.runtime.backend ? <Hint>Сейчас: {runtimePresentation(state.snapshot.runtime).backend} · {runtimePresentation(state.snapshot.runtime).label.toLocaleLowerCase()}</Hint> : null}<Field id="backend" label="Способ" value={backendValue(value)} onChange={(next) => { edit("mode", next === "overlay" ? "overlay" : "full"); edit("capture.transfer", next === "full-auto" ? "auto" : next === "full-compatibility" ? "compatibility" : "gpu") }} options={[{ value: "full-auto", label: "Автоматически" }, { value: "full-gpu", label: gpuUnavailable ? "GPU · недоступно" : "Только GPU", disabled: gpuUnavailable }, { value: "full-compatibility", label: "Совместимый CPU · до 30 FPS", disabled: cpuUnavailable }, { value: "overlay", label: "Лёгкий режим · без цветовых шейдеров" }]}/><Hint>Авто выбирает GPU или CPU. CPU — до 30 FPS. Выбор полного эффекта для окна включает Auto вместо лёгкого режима.</Hint></section>
  <section><h3>Горячие клавиши</h3><Shortcut id="hotkey-toggle" label="Включить / выключить" value={value.hotkeys.toggle} onChange={(next) => edit("hotkeys.toggle", next)}/><Shortcut id="hotkey-emergency" label="Аварийное отключение" value={value.hotkeys.emergency} onChange={(next) => edit("hotkeys.emergency", next)}/><Button variant="link" className="px-0" size="sm" onClick={controller.toggleProbe}>{state.probeOpen ? "Скрыть проверку" : "Проверить получение клавиш"}</Button>{state.probeOpen && state.probe ? <div className="probe-box"><Hint>{state.probe.registered ? "Применённые клавиши зарегистрированы Windows." : "Регистрация не подтверждена."} Регистрация не означает получение нажатия.</Hint>{!equal(value.hotkeys, state.snapshot?.config.hotkeys) ? <Hint>Новые сочетания ещё не сохранены. Сейчас проверяются прежние.</Hint> : null}<p>{state.probe.bindings.toggle}: получено {state.probe.toggleDelta ?? "—"}</p><p>{state.probe.bindings.emergency}: получено {state.probe.emergencyDelta ?? "—"}</p><Hint>Аварийная клавиша действительно остановит эффект.</Hint></div> : null}</section></> : null}
  <section><h3>Приложение · {state.updater?.current_version || packageInfo.version}</h3><Button size="sm" variant="outline" disabled={!state.connected} onClick={() => void controller.diagnostics()}><Copy/>Скопировать диагностику</Button><Hint>{update.label}</Hint>{update.state === "downloading" ? <Progress aria-label="Загрузка" value={state.updater?.total_bytes ? (state.updater.downloaded_bytes || 0) / state.updater.total_bytes * 100 : undefined}/> : null}<div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" disabled={!update.canCheck} onClick={() => void controller.checkUpdates()}><RefreshCw/>Проверить обновления</Button>{update.canInstall && update.version ? <Button size="sm" onClick={() => dialog({ kind: "update", version: update.version! })}><Download/>Установить</Button> : null}</div><Hint>{update.reason}</Hint><div className="app-action-list" role="group" aria-label="Управление приложением"><Button className="app-action-row" size="sm" variant="ghost" onClick={() => void api.hidePanel().catch(controller.reportError)}><Minimize2 aria-hidden="true"/><span>Свернуть в трей</span><ChevronRight className="action-chevron" aria-hidden="true"/></Button><Button id="quit" className="app-action-row" size="sm" variant="ghost" onClick={() => dialog({ kind: "quit" })}><LogOut aria-hidden="true"/><span>Завершить LumaTape</span><ChevronRight className="action-chevron" aria-hidden="true"/></Button></div></section></div>
}

export function App() {
  const [controller] = useState(() => createController(api)), state = useSyncExternalStore(controller.subscribe, controller.getSnapshot)
  const [modal, setModal] = useState<Modal | null>(null), modalEpoch = useRef(0), lastEmergency = useRef<number | undefined>(undefined)
  const modalOrigin = useRef<HTMLElement | null>(null)
  const [advanced, setAdvanced] = useState<{ original: Config; value: Config } | null>(null)
  const open = (next: Modal | null) => { if (next && !modal) modalOrigin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; modalEpoch.current++; setModal(next); if (next?.kind === "advanced") { const value = controller.getSnapshot().draft; setAdvanced(value ? { original: clone(value), value: clone(value) } : null) } }
  useEffect(() => {
    void controller.start()
    const activate = () => { if (!document.hidden) controller.activate() }
    document.addEventListener("visibilitychange", activate)
    return () => { document.removeEventListener("visibilitychange", activate); controller.dispose() }
  }, [controller])
  useEffect(() => {
    if (modal?.kind === "advanced" && !advanced && state.draft) {
      const value = state.draft
      setAdvanced((previous) => previous || { original: clone(value), value: clone(value) })
    }
  }, [modal?.kind, advanced, state.draft])
  useEffect(() => {
    const sequence = state.snapshot?.emergency_sequence
    if (sequence !== undefined && lastEmergency.current !== undefined && sequence > lastEmergency.current) { modalEpoch.current++; setModal(null); setAdvanced(null); controller.setPreviewOpen(false) }
    lastEmergency.current = sequence
  }, [state.snapshot?.emergency_sequence, controller])
  useEffect(() => {
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented || modal || document.querySelector('[data-state="open"][role="listbox"], [data-shortcut-recorder="active"]')) return
      event.preventDefault(); void api.hidePanel().catch(controller.reportError)
    }
    document.addEventListener("keydown", escape)
    return () => document.removeEventListener("keydown", escape)
  }, [modal, controller])
  // The test scene belongs to the panel, independent of live overlay and dialogs.
  // Emergency cancels in-flight work; resume only the preview after recovery.
  useEffect(() => {
    const wanted = state.connected && !!state.draft && !state.stopping && !state.recoveryPending
    if (state.previewOpen !== wanted) controller.setPreviewOpen(wanted)
  }, [controller, state.connected, !!state.draft, state.stopping, state.recoveryPending, state.previewOpen])
  const power = powerPresentation(state)
  const powerOff = () => void runEmergency(() => open(null), controller.powerOff)
  const enabled = state.snapshot?.runtime.enabled === true, issues = state.draft ? startIssues(state) : []
  const runtime = runtimePresentation(state.snapshot?.runtime || {}), deadline = state.snapshot?.confirmation_deadline
  const editAdvanced = (path: string, value: string | number | boolean) => setAdvanced((previous) => { if (!previous) return previous; const next = clone(previous.value), parts = path.split("."), key = parts.pop()!; parts.reduce((part: any, field) => part[field], next)[key] = value; return { ...previous, value: next } })
  const advancedConfig = state.draft && advanced ? mergeAdvanced(state.draft, advanced.original, advanced.value) : null
  const advancedIssues = advancedConfig ? draftIssues(advancedConfig, !!state.selectedKey || canEditWithoutSource(state.applied, advancedConfig)) : []
  const chooseEffect = (value: string) => { const current = controller.getSnapshot().draft; if (value === (current?.shader?.id ? `shader:${current.shader.id}` : `builtin:${current?.preset}`) && current?.effects.intensity === 1) return; if (value.startsWith("builtin:")) controller.livePreset(value.slice(8)); else { const id = value.slice(7); if (controller.selectShader(id) === "consent-required") open({ kind: "warp", id }) } }
  const doImport = async (code: string): Promise<boolean> => {
    const epoch = modalEpoch.current
    const item = await controller.importShader(code)
    if (!item || epoch !== modalEpoch.current) return false
    open(null)
    if (controller.selectShader(item.id) === "consent-required") open({ kind: "warp", id: item.id })
    return true
  }
  const needsRecovery = state.recoveryPending || state.snapshot?.runtime.recovery_pending || state.snapshot?.runtime.phase === "recovery-error" || state.snapshot?.runtime.unsaved
  const formatActive = state.snapshot?.runtime.format_active
  const statusLabel = !state.connected ? "Нет связи" : power.stopping ? "Восстанавливаем окно…" : needsRecovery ? "Нужно восстановить окно" : state.startingEffect || state.livePending || state.busy ? "Применяется…" : enabled ? runtime.label : formatActive ? "Формат 4:3 включён" : "Эффект выключен"
  const statusTone = needsRecovery ? "error" : enabled ? runtime.tone : formatActive ? "active" : "muted"
  const failure = state.applyError
  return <TooltipProvider delayDuration={350}><div className={api.demoMode ? "air-demo-workbench" : undefined}>
  {api.demoMode ? <p className="air-demo-note">Предпросмотр интерфейса · без движка Windows</p> : null}
  <div className="panel-frame air-panel">
  <header className="air-header"><span className="air-wordmark" data-tauri-drag-region>luma<span data-tauri-drag-region>tape</span><span className="air-wordmark-dot" aria-hidden="true">●</span></span><button type="button" className="air-settings" title="Настройки" onClick={() => open({ kind: "advanced" })} aria-label="Настройки"><Settings2 size={19}/></button></header>
  <main className="panel-main air-content">
    {state.connectionError ? <div className="message error" role="alert"><p>{state.connectionError}</p><Button size="sm" variant="outline" disabled={state.connecting} onClick={() => void controller.start()}>Повторить</Button></div> : null}
    {!api.demoMode && state.host && !state.host.tray_available ? <div className="message" role="status">Трей недоступен. Панель останется открытой.</div> : null}
    {state.notice && state.notice.tone !== "success" ? <div className={`message ${state.notice.tone}`} role="status">{state.notice.message}</div> : null}
    {state.draft ? <Home state={state} controller={controller} chooseEffect={chooseEffect} onImport={() => open({ kind: "import" })} onPreview={() => open({ kind: "preview" })}/> : <div className="waiting">{state.connecting ? <><LoaderCircle className="size-4 animate-spin"/>Подключаем движок…</> : "Откройте приложение LumaTape."}</div>}
    {failure ? <div className="message error" role="alert"><p>{failure.message}</p><Hint>{failure.nextStep}</Hint>{failure.details ? <details><summary>Подробности</summary><p className="break-words mt-2 text-xs">{failure.details}</p></details> : null}</div> : null}
    {issues.length ? <Hint>{!state.monitorIntent && !state.sources.windows.some((source) => sourceKey(source) === state.selectedKey) ? "Откройте игру и выберите её окно в настройках." : issues[0]}</Hint> : null}
  </main>
  <footer className="panel-footer">
    {deadline ? <div className="display-confirmation" role="status"><span>Видеорежим: подтвердите до {new Date(deadline).toLocaleTimeString("ru-RU")}</span><Button id="confirm-display" size="sm" onClick={() => void controller.confirm()}>Оставить</Button></div> : null}

    <div className="power-row air-footer">
      <div className="power-copy"><span className="panel-status" aria-live="polite"><span className={`status-dot tone-${statusTone}`}/>{statusLabel}</span></div>
      <Button id="power" className="air-power" size="sm" variant={power.shouldStop ? "outline" : "default"} disabled={power.stopping || (!power.shouldStop && (!state.connected || state.busy || !!issues.length))} onClick={() => power.shouldStop ? powerOff() : void controller.startEffect()}><Power/>{power.label}</Button>
    </div>
  </footer>
  <Dialog open={!!modal} onOpenChange={(value) => !value && open(null)}>{modal ? <DialogContent className="panel-dialog" showCloseButton={false} onCloseAutoFocus={(event) => { if (modalOrigin.current?.isConnected) { event.preventDefault(); modalOrigin.current.focus({ preventScroll: true }) } }} onEscapeKeyDown={(event) => { if (document.querySelector('[data-shortcut-recorder="active"]')) event.preventDefault() }}><DialogHeader><DialogTitle>{modal?.kind === "preview" ? "Предпросмотр" : modal?.kind === "advanced" ? "Настройки" : modal?.kind === "import" ? "Добавить эффект" : modal?.kind === "warp" ? "Эффект меняет геометрию" : modal?.kind === "update" ? `Установить ${modal.version}?` : "Завершить LumaTape?"}</DialogTitle><DialogDescription>{modal?.kind === "preview" ? "Сравните эффект на тестовой сцене." : modal?.kind === "advanced" ? "Формат, управление и приложение." : modal?.kind === "import" ? "Готовый файл или новый эффект с помощью AI." : modal?.kind === "warp" ? "Такое изображение не совпадает с точными координатами мыши. Используйте клавиатуру или геймпад." : "Эффект остановится, собственные изменения окна и экрана будут восстановлены."}</DialogDescription></DialogHeader>
  <div className="dialog-body">{modal?.kind === "preview" ? <>{state.connectionError ? <div className="message error" role="alert">{state.connectionError}</div> : null}{state.notice ? <div className={`message ${state.notice.tone}`} role={state.notice.tone === "error" ? "alert" : "status"}>{state.notice.message}</div> : null}<TestScene state={state}/><Button variant="outline" size="sm" disabled={!state.connected || state.busy || state.launching || api.demoMode} onClick={() => void controller.launchTestcard()}>Открыть тестовую игру</Button></> : modal?.kind === "advanced" ? <>{failure ? <div className="message error mb-3"><p>{failure.message}</p><Hint>{failure.details || failure.nextStep}</Hint></div> : null}<Advanced state={state} controller={controller} value={advanced?.value || null} edit={editAdvanced} dialog={open}/>{advancedIssues.length ? <div className="message error">{advancedIssues.join(" ")}</div> : null}</> : modal?.kind === "import" ? <ShaderImport disabled={!state.connected} demo={api.demoMode} importing={state.shaderImporting} onImport={doImport}/> : null}</div>
  {deadline ? <div className="display-confirmation" role="status"><span>Видеорежим до {new Date(deadline).toLocaleTimeString("ru-RU")}</span><Button size="sm" onClick={() => void controller.confirm()}>Оставить</Button></div> : null}<DialogFooter className="panel-dialog-footer">{power.shouldStop ? <Button id="dialog-power-off" size="sm" variant="ghost" disabled={power.stopping} onClick={powerOff}><Power/>{power.label}</Button> : null}<Button size="sm" variant="outline" onClick={() => open(null)}>{(modal?.kind === "preview") ? "Готово" : modal?.kind === "advanced" ? "Закрыть" : "Отмена"}</Button>{modal?.kind === "advanced" && advanced && !equal(advanced.original, advanced.value) ? <Button size="sm" disabled={!state.connected || state.busy || !!advancedIssues.length} onClick={async () => { const epoch = modalEpoch.current; await controller.applyAdvanced(advanced.original, advanced.value); if (epoch === modalEpoch.current && !controller.getSnapshot().applyError) open(null) }}>Применить</Button> : modal?.kind === "warp" ? <Button size="sm" onClick={() => { const id = modal.id; open(null); controller.selectShader(id, true) }}>Использовать с геймпадом</Button> : modal?.kind === "quit" || modal?.kind === "update" ? <Button size="sm" onClick={() => { const current = modal; open(null); if (current.kind === "update") void controller.installUpdate(current.version); else void controller.quit() }}>{modal.kind === "update" ? "Установить" : "Завершить"}</Button> : null}</DialogFooter>
  </DialogContent> : null}</Dialog>
  </div></div></TooltipProvider>
}
