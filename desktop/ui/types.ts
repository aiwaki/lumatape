export interface Effects {
  intensity: number
  noise_seed: number
  freeze_noise: boolean
  crt: { scanlines: number; mask: number; bloom: number; softness: number; vignette: number; curvature: number }
  vhs: { chroma_bleed: number; noise: number; jitter: number; tracking: number }
}
export interface ShaderDescriptor { id: string; version: 1; name: string; description: string; coordinates: "preserve" | "warp"; parameters: { name: string; min: number; max: number; step: number; default: number }[] }
export interface Config {
  shader?: { id: string; params: number[] }
  version: number
  enabled: boolean
  mode: "overlay" | "full"
  input_mode: "mouse-exact" | "keyboard-gamepad"
  preset: string
  capture: { transfer: "gpu" | "compatibility" | "auto" }
  target: { kind: "window" | "monitor"; monitor: number; window_title?: string }
  screen: { shape: "flat" | "rounded" | "convex"; corner_radius: number; curvature: number; glass: number }
  effects: Effects
  aspect: { enabled: boolean; method: "mask" | "window" | "system"; scale: "fit" | "crop" | "stretch"; source_dar: number }
  hotkeys: { toggle: string; emergency: string }
}
export interface Source { id: string; hwnd: string; pid: number; process_created: string; title: string }
export interface Monitor { index: number; device: string; bounds: { x: number; y: number; width: number; height: number } }
export interface Runtime {
  format_active?: boolean
  recovery_pending?: boolean
  enabled: boolean
  phase: string
  backend: string
  reason: string
  last_error?: string
  unsaved: boolean
  confirmation_deadline?: string | null
  gpu?: { state: string; code?: string; reason?: string }
  compatibility?: { state: string; reason?: string }
}
export interface Snapshot {
  config: Config
  runtime: Runtime
  source: Source | null
  presets: { name: string; effects: Effects }[]
  emergency_sequence: number
  confirmation_deadline?: string | null
  hotkeys: { registered: boolean; toggle_received: number; emergency_received: number }
}
export interface Updater {
  state: string
  current_version?: string
  available_version?: string | null
  reason?: string | null
  downloaded_bytes?: number
  total_bytes?: number | null
}
export interface ControllerState {
  snapshot: Snapshot | null
  applied: Config | null
  draft: Config | null
  sources: { windows: Source[]; monitors: Monitor[]; suggested?: Source }
  selectedKey: string
  appliedKey: string
  connected: boolean
  connecting: boolean
  busy: boolean
  startingEffect: boolean
  stopping: boolean
  recoveryPending: boolean
  launching: boolean
  livePending: boolean
  shaders: ShaderDescriptor[]
  shaderImporting: boolean
  monitorIntent: boolean
  previewOpen: boolean
  applyError: { code: string; message: string; nextStep: string; details: string; suggestedBackend: string | null } | null
  notice: { message: string; tone: string } | null
  connectionError: string | null
  tab: string
  preview: { before: string | null; after: string | null; feedback: string; fixture: boolean }
  updater: Updater | null
  host: { tray_available: boolean; tray_error: string | null } | null
  probeOpen: boolean
  probe: { registered: boolean; pending: boolean; bindings: Config["hotkeys"]; toggleDelta: number | null; toggleTotal: number | null; emergencyDelta: number | null; emergencyTotal: number | null } | null
}
export interface EngineController {
  subscribe(listener: () => void): () => void
  getSnapshot(): ControllerState
  start(): Promise<void>
  dispose(): void
  activate(): void
  setTab(value: string): void
  setPreviewOpen(value: boolean): void
  edit(path: string, value: string | number | boolean, custom?: boolean): void
  preset(name: string): void
  liveEdit(path: string, value: number | boolean, custom?: boolean): void
  livePreset(name: string): void
  selectShader(id: string, allowWarp?: boolean): "consent-required" | undefined
  importShader(source: string): Promise<ShaderDescriptor | null>
  refreshShaders(): Promise<void>
  applyAdvanced(original: Config, edited: Config): Promise<void>
  chooseSource(key: string): Promise<void>
  source(key: string): void
  backend(value: string): void
  aspect(value: string): void
  noAlt(): void
  toggleProbe(): void
  discard(): void
  apply(): Promise<void>
  startEffect(): Promise<void>
  powerOff(): Promise<void>
  disable(): Promise<void>
  emergency(): Promise<void>
  toggle(): Promise<void>
  restore(): Promise<void>
  confirm(): Promise<void>
  refreshStatus(): Promise<void>
  refreshSources(preserve?: boolean): Promise<void>
  loadUpdater(): Promise<void>
  reportError(error: unknown): void
  launchTestcard(): Promise<void>
  diagnostics(): Promise<void>
  checkUpdates(): Promise<void>
  installUpdate(version: string): Promise<void>
  quit(): Promise<void>
}
