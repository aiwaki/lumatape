import { useEffect, useId, useRef, useState } from "react"
import { Check, ChevronDown, Copy, FileUp, LoaderCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { copyText } from "./api.js"
import { performShaderImport, shaderImportPrompt, SHADER_MAX_BYTES } from "./shader-import.mjs"

export type ShaderImportProps = {
  disabled: boolean
  demo: boolean
  importing: boolean
  onImport: (source: string) => Promise<boolean>
}
type ImportState = { phase: "idle" | "reading" | "validating" | "done" | "error"; message: string }

export function ShaderImport({ disabled, demo, importing, onImport }: ShaderImportProps) {
  const id = useId(), fileInput = useRef<HTMLInputElement>(null)
  const [intent, setIntent] = useState(""), [code, setCode] = useState(""), [fileName, setFileName] = useState("")
  const [state, setState] = useState<ImportState>({ phase: "idle", message: "" })
  const [copy, setCopy] = useState({ busy: false, done: false, error: "" })
  const active = useRef(false), operation = useRef<AbortController | null>(null), copyEpoch = useRef(0)
  const latest = useRef({ disabled, demo, importing, onImport })
  const phase = useRef<ImportState["phase"]>("idle")
  latest.current = { disabled, demo, importing, onImport }
  useEffect(() => {
    active.current = true
    return () => { active.current = false; operation.current?.abort(); copyEpoch.current++ }
  }, [])

  const busy = importing || state.phase === "reading" || state.phase === "validating"
  const blocked = disabled || demo || busy
  const startImport = async (input: { file?: File; source?: string }) => {
    if (!active.current || latest.current.disabled || latest.current.demo || latest.current.importing || phase.current === "validating") return
    operation.current?.abort()
    const request = new AbortController()
    operation.current = request
    setFileName(input.file?.name || "Код из ответа")
    await performShaderImport(input, {
      signal: request.signal,
      canImport: () => active.current && !latest.current.disabled && !latest.current.demo && !latest.current.importing,
      onImport: (source: string) => latest.current.onImport(source),
      onState: (next: ImportState) => { phase.current = next.phase; setState(next) },
    })
  }
  const copyPrompt = async () => {
    const epoch = ++copyEpoch.current
    setCopy({ busy: true, done: false, error: "" })
    try {
      await copyText(shaderImportPrompt(intent))
      if (active.current && epoch === copyEpoch.current) setCopy({ busy: false, done: true, error: "" })
    } catch {
      if (active.current && epoch === copyEpoch.current) setCopy({ busy: false, done: false, error: "Не удалось скопировать запрос. Попробуйте ещё раз." })
    }
  }

  return <div className="shader-import space-y-5 text-sm">
    <section className="shader-import-file space-y-2" aria-labelledby={`${id}-file-heading`}>
      <h3 id={`${id}-file-heading`} className="font-medium">Есть файл эффекта? Откройте его.</h3>
      <p className="text-muted-foreground text-xs leading-relaxed">LumaTape проверит файл и добавит готовый эффект. Настраивать параметры не нужно.</p>
      <Button type="button" className="w-full" disabled={blocked} onClick={() => fileInput.current?.click()}><FileUp aria-hidden="true"/>Открыть файл эффекта…</Button>
      <input ref={fileInput} id={`${id}-file`} type="file" className="sr-only" tabIndex={-1} aria-label="Файл эффекта LumaTape" accept=".lumatape.glsl,.glsl,text/plain" disabled={blocked} onChange={(event) => {
        const file = event.target.files?.[0]
        event.target.value = ""
        if (file) void startImport({ file })
      }}/>
      <p className="text-muted-foreground text-xs">Файл .lumatape.glsl или .glsl · до 64 КиБ</p>
      {demo ? <p className="shader-import-demo text-xs text-muted-foreground">Проверка файлов доступна в приложении LumaTape для Windows. Здесь можно подготовить запрос для AI.</p> : null}
      {disabled && !demo && !busy ? <p className="text-xs text-muted-foreground">Импорт станет доступен, когда приложение будет готово.</p> : null}
    </section>

    {fileName ? <div className="shader-import-result space-y-1 rounded-md border border-border p-3" aria-live="polite" aria-busy={busy}>
      <p className="shader-import-filename break-all font-medium">{fileName}</p>
      {busy ? <p className="flex items-center gap-2 text-xs text-muted-foreground" role="status"><LoaderCircle className="size-3.5 animate-spin" aria-hidden="true"/>{state.phase === "reading" ? "Читаем файл…" : "Проверяем эффект в LumaTape…"}</p> : state.phase === "done" ? <p className="flex items-center gap-2 text-xs" role="status"><Check className="size-3.5" aria-hidden="true"/>Эффект добавлен.</p> : null}
      {state.phase === "error" ? <div className="shader-import-error break-words text-xs text-destructive" role="alert"><p className="font-medium">Не удалось добавить эффект</p><p className="mt-1 whitespace-pre-wrap">{state.message}</p><p className="mt-2 text-muted-foreground">Если файл создан AI, отправьте ему эту ошибку и откройте исправленный файл.</p></div> : null}
    </div> : null}

    <section className="shader-import-ai space-y-3 border-t border-border pt-4" aria-labelledby={`${id}-ai-heading`}>
      <h3 id={`${id}-ai-heading`} className="font-medium">Нет файла? Создайте эффект с AI.</h3>
      <ol className="shader-import-steps space-y-4">
        <li className="space-y-2"><p className="font-medium">1. Скопируйте готовый запрос</p>
          <Label htmlFor={`${id}-intent`} className="text-xs text-muted-foreground">Какой эффект хотите? Необязательно</Label>
          <Input id={`${id}-intent`} value={intent} maxLength={1000} disabled={copy.busy} onChange={(event) => { setIntent(event.target.value); setCopy({ busy: false, done: false, error: "" }) }} placeholder="Например, тёплый CRT без сильного шума"/>
          <Button type="button" variant="outline" size="sm" disabled={copy.busy} onClick={() => void copyPrompt()}>{copy.done ? <Check aria-hidden="true"/> : <Copy aria-hidden="true"/>}{copy.busy ? "Копируем…" : copy.done ? "Запрос скопирован" : "Скопировать запрос"}</Button>
          {copy.done ? <p className="text-xs text-muted-foreground" role="status">Теперь вставьте его в ChatGPT или Claude.</p> : null}
          {copy.error ? <p className="text-xs text-destructive" role="alert">{copy.error}</p> : null}
        </li>
        <li className="space-y-1"><p className="font-medium">2. Вставьте запрос в ChatGPT или Claude</p><p className="text-xs text-muted-foreground leading-relaxed">Запрос уже содержит формат LumaTape. Отправьте его и дождитесь готового файла.</p></li>
        <li className="space-y-1"><p className="font-medium">3. Сохраните и откройте файл</p><p className="text-xs text-muted-foreground leading-relaxed">Скачайте .lumatape.glsl из ответа и откройте его кнопкой выше. Если AI вернул только текст, вставьте его ниже.</p></li>
      </ol>
      <p className="text-xs text-muted-foreground leading-relaxed">LumaTape ничего не отправляет в AI. Вы сами выбираете сервис и отправляете запрос.</p>
    </section>

    <details className="shader-import-paste group border-t border-border pt-3">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-2 text-xs font-medium">Вставить код из ответа<ChevronDown className="size-4 group-open:rotate-180" aria-hidden="true"/></summary>
      <div className="space-y-2 pt-3"><Label htmlFor={`${id}-source`} className="text-xs">Код эффекта целиком, включая заголовок LumaTape</Label>
        <textarea id={`${id}-source`} value={code} spellCheck={false} disabled={blocked} onChange={(event) => {
          const value = event.target.value
          // Do not let HTML maxLength silently truncate a complete AI response.
          if (new TextEncoder().encode(value).length > SHADER_MAX_BYTES) {
            setCode(""); setFileName("Код из ответа"); phase.current = "error"
            setState({ phase: "error", message: "Код больше 64 КиБ и не был вставлен. Попросите AI сократить код или комментарии." })
          } else {
            setCode(value); setFileName(""); phase.current = "idle"; setState({ phase: "idle", message: "" })
          }
        }} className="shader-import-code min-h-32 w-full resize-y rounded-md border border-input bg-background p-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50" placeholder="Вставьте ответ AI с кодом эффекта…"/>
        <div className="flex flex-wrap gap-2"><Button type="button" size="sm" disabled={blocked || !code.trim()} onClick={() => void startImport({ source: code })}>Проверить и добавить</Button><Button type="button" size="sm" variant="ghost" disabled={busy || (!code && !fileName)} onClick={() => { setCode(""); setFileName(""); setState({ phase: "idle", message: "" }); phase.current = "idle" }}>Очистить</Button></div>
      </div>
    </details>
  </div>
}
