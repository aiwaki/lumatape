// Keep keyboard capture aligned with config.ParseHotkey. Registration and
// rollback still happen in Windows; a captured chord is never "registered".
export function captureShortcut(event) {
  const modifiers = [event.ctrlKey && "Ctrl", event.altKey && "Alt", event.shiftKey && "Shift", event.metaKey && "Win"].filter(Boolean);
  if (!modifiers.length && event.key === "Escape") return { cancelled: true };
  if (event.repeat || ["Control", "Alt", "Shift", "Meta"].includes(event.key)) return null;
  if (!modifiers.length) return { error: "Добавьте Ctrl, Alt, Shift или Win." };
  if (event.getModifierState?.("AltGraph")) return { error: "Выберите сочетание без AltGr." };
  const names = { Escape: "Esc", Space: "Space", Pause: "Pause", Home: "Home", End: "End", Insert: "Insert", Delete: "Delete", PageUp: "PageUp", PageDown: "PageDown", ArrowLeft: "Left", ArrowRight: "Right", ArrowUp: "Up", ArrowDown: "Down" };
  const code = event.code || "";
  const key = /^[A-Za-z]$/.test(event.key || "") ? event.key.toUpperCase() : /^Key[A-Z]$/.test(code) ? code.slice(3) : /^Digit[0-9]$/.test(code) ? code.slice(5) : /^F([1-9]|1[0-9]|2[0-4])$/.test(code) ? code : names[code];
  if (!key) return { error: "Выберите букву, цифру, F-клавишу или клавишу навигации." };
  if (key === "F12") return { error: "F12 зарезервирована Windows. Выберите другую клавишу." };
  return { value: [...modifiers, key].join("+") };
}

// Advanced changes are isolated from live signal edits. Apply only fields the
// user changed in this dialog, rebased onto the latest controller intent.
const advancedFields = ["mode", "capture", "input_mode", "screen", "aspect", "hotkeys"];
export function mergeAdvanced(current, original, edited) {
  const next = structuredClone(current);
  for (const field of advancedFields) {
    if (JSON.stringify(original[field]) !== JSON.stringify(edited[field])) next[field] = structuredClone(edited[field]);
  }
  return next;
}

// Rebase only edits made after request dispatch over the authoritative ACK.
// Advanced fields may exist only in the request copy, never in the live draft.
// Keeping the entire old draft would silently undo that successful change.
export function rebaseAfterApply(atSend, current, applied) {
  function merge(before, now, acknowledged) {
    if (JSON.stringify(before) === JSON.stringify(now)) return structuredClone(acknowledged);
    if (before && now && typeof before === "object" && typeof now === "object" && !Array.isArray(before) && !Array.isArray(now)) {
      const next = structuredClone(acknowledged || {});
      for (const key of new Set([...Object.keys(before), ...Object.keys(now)])) {
        if (!(key in now)) delete next[key];
        else next[key] = merge(before[key], now[key], acknowledged?.[key]);
      }
      return next;
    }
    return structuredClone(now);
  }
  return { ...merge(atSend, current, applied), enabled: applied.enabled };
}
