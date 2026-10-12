// Shared by the bundled page and dependency-free behavior tests.
export function remainingSeconds(deadline, now = Date.now()) {
  const end = Date.parse(deadline);
  return Number.isFinite(end) ? Math.max(0, Math.ceil((end - now) / 1000)) : 0;
}
export function selected(entries = []) { return entries.find(entry => entry.checked === true); }
export function plainLabel(label = '') { return label.replaceAll('&&', '&'); }
export function settingGroups(model) {
  const group = prefix => model.settings.find(g => g.entries.some(e => e.id.startsWith(`model:${prefix}:`)));
  return { shape: group('shape'), format: group('format'), backend: group('backend'), extra: model.settings.filter(g => !['shape','format','backend'].some(p => g === group(p))) };
}
export function powerIntent(model, pending, unavailable = false) { return unavailable || pending || model.confirmation_pending || model.wants_off ? 'model:off' : 'model:power'; }
export function confirmationAllows(id) { return ['confirm','model:format:off','model:off','quit'].includes(id); }
// Responses started before a mutation cannot overwrite its result or resurrect
// controls after an emergency. Only one ordinary mutation is admitted in the UI.
export class RequestGate {
  generation = 0; pending = 0;
  beginRead() { return this.generation; }
  acceptRead(ticket) { return ticket === this.generation && this.pending === 0; }
  beginAction(off = false) { if (this.pending && !off) return false; this.generation++; this.pending++; return true; }
  endAction() { this.pending = Math.max(0, this.pending - 1); this.generation++; }
}
