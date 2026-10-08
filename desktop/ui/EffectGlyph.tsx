import { CodeXml } from "lucide-react"

/** Geometric UI marks, not previews of the rendered shader. */
export function EffectGlyph({ name, custom }: { name: string; custom: boolean }) {
  if (custom || name === "Сохранённый эффект") return <CodeXml aria-hidden="true" className="effect-glyph"/>
  return <svg className="effect-glyph" viewBox="0 0 80 64" fill="none" aria-hidden="true">
    {name === "Subtle CRT" ? <><rect x="7" y="8" width="64" height="46" rx="17" stroke="currentColor" strokeWidth="5"/><rect x="20" y="21" width="38" height="20" rx="8" fill="currentColor" opacity=".35"/></> :
    name === "CRT Classic" ? <>{[8, 21, 34, 47].map(y => <path key={y} d={`M5 ${y}h70v7H5z`} fill="currentColor"/>)}</> :
    name === "Soft TV" ? <><circle cx="31" cy="32" r="27" fill="currentColor" opacity=".3"/><circle cx="50" cy="32" r="27" fill="currentColor"/><circle cx="38" cy="32" r="12" fill="var(--signal-color)"/></> :
    name === "VHS Light" ? <>{[12, 30, 48].map(y => <path key={y} d={`M2 ${y}c13-18 25 18 38 0s25 18 38 0`} stroke="currentColor" strokeWidth="7"/>)}</> :
    <><path d="M3 6h54v12H3zm17 19h56v12H20zM3 44h54v12H3z" fill="currentColor"/><path d="M61 6h15v12H61zM3 25h11v12H3zm58 19h15v12H61z" fill="currentColor" opacity=".4"/></>}
  </svg>
}
