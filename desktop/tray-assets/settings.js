import {remainingSeconds, selected, plainLabel, settingGroups, powerIntent, confirmationAllows, RequestGate} from './settings-model.mjs';
const $ = id => document.getElementById(id);
const setText = (element, value) => { if (element.textContent !== value) element.textContent = value; };
const params = new URLSearchParams(location.search);
const demo = !window.__TAURI__ && params.has('demo');
const gate = new RequestGate();
let data = null, language = 'en', deadline = null, reading = false, unavailable = true;
let actionError = '', connectionError = '';
const strings = {
 ru: {title:'Настройки',game:'Игра и эффект',window:'Окно',fullscreenHelp:'Для эффекта на весь экран включи в игре режим «Без рамки». LumaTape следует размеру окна.',effect:'Эффект',shape:'Форма экрана',refresh:'Обновить',import:'Добавить эффект из файла…',testcard:'Открыть тестовую сцену',format:'Формат 4:3',windowHelp:'Меняет размер самого окна, не разрешение Windows. Полноэкранное окно тоже уменьшится. Если игра сохраняет широкую картинку, выбери 4:3 и в её настройках.',darHelp:'Исправляет пропорции изображения старых игр, например 320×200 → 4:3. Размер окна не меняется.',window43:'Сделать окно 4:3',original:'Вернуть исходный формат',windowsResolution:'Разрешение Windows',systemHelp:'Меняет разрешение экрана с игрой. Если не подтвердить выбор за 15 секунд, вернётся прежнее разрешение. Растяжение и поля зависят от монитора и видеодрайвера.',system43:'Попробовать разрешение 4:3…',performance:'Производительность',processing:'Обработка',newFrames:'Новые кадры',presentation:'Показ',diagnostics:'Скопировать диагностику',advanced:'Другие настройки',autostart:'Запускать вместе с Windows',hotkeyTest:'Проверить сочетания',updates:'Проверить обновления',project:'О проекте ↗',keepTitle:'Оставить это разрешение?',keep:'Оставить',revert:'Вернуть',closeReverts:'Закрытие этого окна отменит смену разрешения и отключит эффект.',choose:'Выбери окно…',none:'Нет доступных окон',turnOff:'Выключить',working:'Применение…',demo:'Предпросмотр интерфейса · без подключения к Windows',backend:'Сейчас: ',auto:'Определится при включении',rates:'Новые кадры — обновления изображения; показ включает повторение кадров. Лимит CPU — до 30 кадров/с, а не обещанная скорость.',stale:'Для измерения вернись в игру. Здесь показаны только текущие измерения.',warm:'Измерение… Нужно хотя бы 1 с показа.',formatOn:'Формат применён',formatOff:'Формат не изменён',formatChanged:'Игра изменила размер окна. Можно снова сделать окно 4:3.',lastSample:'Последнее измерение в игре',restoring:'Время вышло. Ожидаем восстановления от Windows…',remaining:s=>`Прежнее разрешение вернётся через ${s} с`,notConnected:'Нет подключения к движку Windows. Открой настройки через трей LumaTape.',fps:'кадр/с'},
 en: {title:'Settings',game:'Game & effect',window:'Window',fullscreenHelp:"For a fullscreen effect, use the game's borderless mode. LumaTape follows the window size.",effect:'Effect',shape:'Screen shape',refresh:'Refresh',import:'Add effect from file…',testcard:'Open test scene',format:'4:3 format',windowHelp:'Resizes the window itself without changing Windows resolution. A fullscreen window will become smaller too. If the game keeps a wide image, select 4:3 in its settings too.',darHelp:'Corrects image proportions in older games, for example 320×200 → 4:3. Does not resize the window.',window43:'Make window 4:3',original:'Restore original format',windowsResolution:'Windows resolution',systemHelp:'Changes the resolution of the display with your game. Without confirmation within 15 seconds, the previous resolution returns. Stretching and bars depend on your display and graphics driver.',system43:'Try 4:3 resolution…',performance:'Performance',processing:'Processing',newFrames:'New frames',presentation:'Presentation',diagnostics:'Copy diagnostics',advanced:'More settings',autostart:'Start with Windows',hotkeyTest:'Test shortcuts',updates:'Check for updates',project:'About the project ↗',keepTitle:'Keep this resolution?',keep:'Keep',revert:'Revert',closeReverts:'Closing this window cancels the resolution change and turns the effect off.',choose:'Select a window…',none:'No available windows',turnOff:'Turn off',working:'Applying…',demo:'Interface preview · not connected to Windows',backend:'Currently: ',auto:'Detected when enabled',rates:'New frames counts image updates; presentation includes repeated frames. CPU is capped at 30 fps, not guaranteed to reach it.',stale:'Return to the game to measure. Only current measurements are shown here.',warm:'Measuring… At least 1 s of presentation is needed.',formatOn:'Format applied',formatOff:'Format unchanged',formatChanged:'The game changed its window size. You can fit it to 4:3 again.',lastSample:'Last measurement in the game',restoring:'Time is up. Waiting for Windows to restore the resolution…',remaining:s=>`Previous resolution returns in ${s} s`,notConnected:'No connection to the Windows engine. Open settings from the LumaTape tray.',fps:'fps'}
};
const t = key => strings[language][key];
function localize(code) {
 language = String(code).toLowerCase().startsWith('ru') ? 'ru' : 'en'; document.documentElement.lang = language;
 for (const el of document.querySelectorAll('[data-i18n]')) setText(el, t(el.dataset.i18n));
}
function renderError() {
 const runtime = data?.snapshot.runtime;
 const message = actionError || connectionError || runtime?.last_error || (runtime?.capture_recovery?.state === 'blocked' ? runtime.reason : '') || '';
 $('error').textContent = String(message); $('error').hidden = !message;
}
function error(message) { actionError = String(message); renderError(); }
function options(select, entries = [], placeholder = false) {
 const signature = JSON.stringify(entries);
 if (select.dataset.entries !== signature) {
  const nodes = entries.filter(e => e.checked !== null).map(e => {
   const option = document.createElement('option'); option.value = e.id; option.textContent = plainLabel(e.label); option.disabled = !e.enabled; return option;
  });
  if (placeholder || !nodes.length) { const option = document.createElement('option'); option.value = ''; option.textContent = nodes.length ? t('choose') : t('none'); option.disabled = true; nodes.unshift(option); }
  select.replaceChildren(...nodes); select.dataset.entries = signature;
 }
 select.value = selected(entries)?.id || '';
 select.disabled = !!gate.pending || !!data?.busy || !!data?.model.confirmation_pending || !entries.some(e => e.enabled);
}
function countdown() {
 if (!deadline) return;
 const remaining = remainingSeconds(deadline);
 $('countdown').textContent = remaining ? t('remaining')(remaining) : t('restoring');
 $('keep').disabled = !remaining || !!gate.pending || !!data?.busy;
}
function render(next) {
 data = next; localize(next.language);
 const model = data.model, snapshot = data.snapshot, runtime = snapshot.runtime, groups = settingGroups(model);
 setText($('status'), gate.pending ? t('working') : plainLabel(model.status));
 setText($('power'), gate.pending || data.busy || model.confirmation_pending ? t('turnOff') : plainLabel(model.power.label));
 $('power').disabled = !gate.pending && !data.busy && !model.power.enabled;
 $('source').setAttribute('aria-label', t('window'));
 options($('source'), model.sources, !selected(model.sources)); options($('effect'), model.effects); options($('shape'), groups.shape?.entries); options($('backend'), groups.backend?.entries);
 const extras = $('extra-settings');
 for (const group of groups.extra) {
  const key = group.entries.find(e => e.checked !== null)?.id.split(':')[1]; if (!key) continue;
  let select = document.getElementById(`extra-${key}`);
  if (!select) { const label = document.createElement('label'); label.className = 'field'; const name = document.createElement('span'); name.textContent = group.label; select = document.createElement('select'); select.id = `extra-${key}`; select.addEventListener('change', () => action(select.value)); label.append(name, select); extras.append(label); const help = document.createElement('p'); help.className = 'help'; help.id = `help-${key}`; extras.append(help); }
  options(select, group.entries);
  const help = $(`help-${key}`);
  help.textContent = [key === 'dar' ? t('darHelp') : '', ...group.entries.filter(e => e.checked === null).map(e => plainLabel(e.label))].filter(Boolean).join(' · ');
  if (help.textContent) select.setAttribute('aria-describedby', help.id); else select.removeAttribute('aria-describedby');
 }
 for (const button of document.querySelectorAll('[data-action]')) {
  const entry = groups.format?.entries.find(e => e.id === button.dataset.action);
  button.disabled = !!gate.pending || !!data.busy || (!!model.confirmation_pending && !confirmationAllows(button.dataset.action)) || (entry ? !entry.enabled : false);
 }
 $('refresh').disabled = !!gate.pending;
 $('original').disabled ||= !snapshot.config.aspect.enabled;
 $('autostart').checked = !!data.autostart.enabled; $('autostart').disabled = !!gate.pending || !!data.busy || !!model.confirmation_pending || !data.autostart.can_toggle;
 $('backend-status').textContent = t('backend') + ({'full-gpu':'GPU','full-compatibility':'CPU','lightweight':'Lightweight'}[runtime.backend] || t('auto'));
 const rates = runtime.frame_rates;
 const sample = rates?.state === 'current' ? rates : rates?.last_sample;
 for (const [id,key] of [['fresh-fps','fresh_frame_fps'],['present-fps','present_fps']]) $(id).textContent = Number.isFinite(sample?.[key]) ? `${sample[key].toFixed(1)} ${t('fps')}` : '—';
 $('rate-status').textContent = (rates?.state === 'current' ? t('rates') : sample ? `${t('lastSample')} · ${new Date(sample.observed_at).toLocaleTimeString(language)}. ${t('rates')}` : rates?.state === 'warming-up' ? t('warm') : t('stale'));
 const client = runtime.source_client;
 $('format-status').textContent = (runtime.format_reason || (runtime.format_active ? t('formatOn') : snapshot.config.aspect.enabled && snapshot.config.aspect.method === 'window' ? t('formatChanged') : t('formatOff'))) + (client?.width ? ` · ${client.width} × ${client.height}` : '');
 renderError();
 const newDeadline = snapshot.confirmation_deadline;
 $('confirmation').hidden = !newDeadline;
 if (newDeadline && newDeadline !== deadline) { $('revert').focus(); $('confirmation').scrollIntoView({block:'nearest'}); }
 deadline = newDeadline; $('revert').disabled = !!gate.pending || !!data.busy; countdown();
 $('version').textContent = `LumaTape ${data.version}`;
}
let demoData;
async function invoke(command, payload) {
 if (demo) { demoData ||= (await import('./preview.mjs')).fixture(language); return (await import('./preview.mjs')).invoke(demoData, command, payload); }
 if (!window.__TAURI__) throw new Error(t('notConnected'));
 return window.__TAURI__.core.invoke(command, payload);
}
async function refresh(catalogues = false) {
 if (reading || gate.pending) return;
 reading = true; const ticket = gate.beginRead();
 try { const next = await invoke('settings_snapshot', {catalogues}); if (gate.acceptRead(ticket)) { unavailable = false; connectionError = ''; render(next); } }
 catch (e) { if (gate.acceptRead(ticket)) { unavailable = true; connectionError = String(e); renderError(); $('power').textContent = t('turnOff'); $('power').disabled = false; } }
 finally { reading = false; }
}
async function action(id) {
 if (data?.model.confirmation_pending && !confirmationAllows(id)) return;
 if (!id || !gate.beginAction(id === 'model:off')) return;
 error(''); if (data) render(data);
 try { await invoke('settings_action', {id, sequence:data?.model.emergency_sequence || 0, intentEpoch:data?.intent_epoch || 0, confirmationToken:id === 'confirm' ? deadline : null}); }
 catch(e) { error(e); }
 finally { gate.endAction(); await refresh(true); }
}
for (const id of ['source','effect','shape','backend']) $(id).addEventListener('change', event => action(event.target.value));
for (const button of document.querySelectorAll('[data-action]')) button.addEventListener('click', () => action(button.dataset.action));
$('refresh').addEventListener('click', () => refresh(true));
$('power').addEventListener('click', () => action(data ? powerIntent(data.model, gate.pending || data.busy, unavailable) : 'model:off'));
$('keep').addEventListener('click', () => action('confirm'));
$('revert').addEventListener('click', () => action('model:format:off'));
$('autostart').addEventListener('change', () => action('autostart'));
localize(params.get('lang') || navigator.language);
if (demo) { $('demo').hidden = false; $('demo').textContent = t('demo'); }
await refresh(true);
setInterval(() => { if (!document.hidden && !gate.pending) refresh(); }, 1000);
setInterval(countdown, 200);
document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(true); });
