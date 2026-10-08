import { ArrowUpRight, Plus } from "lucide-react"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import type { ControllerState } from "./types"

export const effectDescriptions: Record<string, string> = {
  "Subtle CRT": "Тихая текстура старого экрана", "CRT Classic": "Чёткие строки и фосфор",
  "Soft TV": "Мягкий свет и тёплый цвет", "VHS Light": "Цвет с аналоговым характером",
  "VHS Tape": "Шум, шлейф и несовершенство плёнки", Custom: "Сохранённый эффект из профиля",
}
export function EffectLibrary({ state, chooseEffect, onImport }: { state: ControllerState; chooseEffect: (value: string) => void; onImport: () => void }) {
  const draft = state.draft
  if (!draft) return null
  const value = draft.shader?.id ? `shader:${draft.shader.id}` : `builtin:${draft.preset}`
  const presets = state.snapshot?.presets || []
  const missing = Boolean(draft.shader?.id && !state.shaders.some(item => item.id === draft.shader?.id))
  // Radix does not emit a value change for the current option. Legacy profiles
  // still need the explicit re-selection to normalize the finished effect to 100%.
  const repeatSelection = (itemValue: string) => {
    if (itemValue === value && draft.effects.intensity !== 1) chooseEffect(itemValue)
  }
  const repeatHandlers = (itemValue: string) => ({
    onPointerUp: (event: React.PointerEvent) => { if (event.button === 0) repeatSelection(itemValue) },
    onKeyDown: (event: React.KeyboardEvent) => { if ((event.key === "Enter" || event.key === " ") && !event.altKey && !event.ctrlKey && !event.metaKey) repeatSelection(itemValue) },
  })
  return <section className="effect-library" aria-label="Выбор эффекта">
    <Label className="sr-only" htmlFor="effect-picker">Эффект</Label>
    <Select value={value} disabled={!state.connected} onValueChange={chooseEffect}>
      <SelectTrigger id="effect-picker"><SelectValue placeholder="Выберите эффект"/></SelectTrigger>
      <SelectContent position="popper" className="effect-options">
        {presets.map(preset => <SelectItem key={preset.name} value={`builtin:${preset.name}`} {...repeatHandlers(`builtin:${preset.name}`)}>{preset.name === "Custom" ? "Сохранённый эффект" : preset.name}</SelectItem>)}
        {draft.preset === "Custom" && !draft.shader?.id && !presets.some(p => p.name === "Custom") ? <SelectItem value="builtin:Custom" {...repeatHandlers("builtin:Custom")}>Сохранённый эффект</SelectItem> : null}
        {state.shaders.map(item => <SelectItem key={item.id} value={`shader:${item.id}`} {...repeatHandlers(`shader:${item.id}`)}>{item.name}</SelectItem>)}
        {missing ? <SelectItem value={value} disabled>Эффект недоступен</SelectItem> : null}
      </SelectContent>
    </Select>
    <button type="button" className="air-import" disabled={!state.connected} onClick={onImport} aria-label="Добавить свой эффект"><span className="air-import-icon" aria-hidden="true"><Plus size={24} strokeWidth={1.7}/></span><span><strong>Добавить эффект</strong><small>Загрузить файл или создать с AI</small></span><ArrowUpRight size={20} aria-hidden="true"/></button>
    {missing ? <div className="message warning">Выбранный эффект не найден. Добавьте файл снова или выберите другой.</div> : null}
  </section>
}
