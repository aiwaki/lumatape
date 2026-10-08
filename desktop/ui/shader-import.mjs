import { SHADER_PROMPT } from "./shader-prompt.mjs";

export const SHADER_MAX_BYTES = 64 * 1024;

export function shaderImportPrompt(intent = "") {
  const description = intent.trim().slice(0, 1000);
  return SHADER_PROMPT.replace(
    /^Желаемый эффект:.*$/m,
    `Желаемый эффект: ${description || "Предложи законченный атмосферный эффект для старых игр."}`,
  ) + "\n\nСоздай законченный внешний вид без ручных настроек: parameters должен быть пустым массивом [], а выбранные значения — константами в коде GLSL. Верни файл .lumatape.glsl, который можно сразу открыть в LumaTape.";
}

export function validateShaderText(source) {
  if (typeof source !== "string" || !source.trim()) throw new Error("Файл пуст. Откройте файл эффекта или вставьте код из ответа.");
  if (new TextEncoder().encode(source).length > SHADER_MAX_BYTES) throw new Error("Эффект больше 64 КиБ. Попросите AI сократить код или комментарии.");
  return source;
}

// Cancellation stops a pending file read from starting an import. Once the
// engine has received the request, its owner controls the commit/close epoch.
export async function performShaderImport(input, { signal, canImport, onImport, onState }) {
  const active = () => !signal.aborted;
  const publish = (phase, message = "") => { if (active()) onState({ phase, message }); };
  if (!active() || !canImport()) return false;
  try {
    let source = input.source;
    if (input.file) {
      publish("reading");
      if (input.file.size > SHADER_MAX_BYTES) throw new Error("Файл больше 64 КиБ. Попросите AI сократить код или комментарии.");
      try { source = await input.file.text(); }
      catch { throw new Error("Не удалось прочитать файл. Попробуйте открыть его ещё раз."); }
    }
    if (!active()) return false;
    if (!canImport()) { publish("idle"); return false; }
    validateShaderText(source);
    publish("validating");
    const imported = await onImport(source);
    if (!active()) return false;
    publish(imported ? "done" : "idle");
    return imported === true;
  } catch (error) {
    publish("error", error instanceof Error ? error.message : String(error || "Не удалось добавить эффект."));
    return false;
  }
}
