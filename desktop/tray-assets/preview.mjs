// Explicit browser-only preview. Never used in a Tauri window or engine path.
const clone = x => structuredClone(x);
export function fixture(language='en') {
 const ru = language === 'ru';
 const entry = (id,label,checked=false) => ({id,label,checked,enabled:true});
 const choices = (kind,items) => items.map(([id,label],i)=>entry(`model:${kind}:${id}`,label,!i));
 return {language,version:'0.3.5 · preview',busy:false,autostart:{enabled:false,can_toggle:true},model:{emergency_sequence:0,status:ru?'Эффект выключен':'Effect off',wants_off:false,power:entry('model:power',ru?'Включить':'Turn on',null),confirmation_pending:false,sources:[entry('model:source:demo','LumaTape test card',true)],effects:choices('preset',[['subtle','Subtle CRT'],['classic','CRT Classic'],['soft','Soft TV'],['light','VHS Light'],['tape','VHS Tape']]),settings:[
  {label:ru?'Форма экрана':'Screen shape',entries:choices('shape',[['flat',ru?'Плоский':'Flat'],['rounded',ru?'Скруглённый CRT':'Rounded CRT'],['convex',ru?'Выпуклый CRT':'Curved CRT']])},
  {label:'4:3',entries:choices('format',[['off',ru?'Исходный формат':'Original aspect ratio'],['window',ru?'Окно игры 4:3':'4:3 game window'],['system',ru?'Разрешение Windows':'Windows resolution']])},
  {label:ru?'Обработка':'Processing',entries:choices('backend',[['full-auto',ru?'Автоматически':'Automatic'],['full-gpu','GPU'],['full-compatibility',ru?'CPU · до 30 кадров/с':'CPU · up to 30 fps'],['overlay','Lightweight']])},
  {label:ru?'Искажения':'Distortion',entries:choices('input',[['mouse-exact',ru?'Точные клики':'Accurate clicks'],['keyboard-gamepad',ru?'Произвольные искажения':'Arbitrary distortion']])},
  {label:ru?'Масштаб':'Scaling',entries:choices('scale',[['fit',ru?'Вписать целиком':'Fit entire image'],['crop',ru?'Заполнить с обрезкой':'Fill and crop'],['stretch',ru?'Растянуть':'Stretch']])},
  {label:ru?'Пропорции старой игры':'Legacy game aspect ratio',entries:choices('dar',[['auto',ru?'Как в окне':'As in the window'],['4:3',ru?'4:3 · например 320×200':'4:3 · e.g. 320×200']])},
  {label:ru?'Горячие клавиши':'Hotkeys',entries:[...choices('hotkeys',[['standard','Ctrl+Alt+F9 / Ctrl+Alt+F10'],['compact','Ctrl+Shift+9 / Ctrl+Shift+0']]),{id:'help',label:ru?'F9 — переключить · F10 — отключить и восстановить':'F9 — toggle · F10 — emergency off and restore',enabled:false,checked:null}]}
 ]},snapshot:{confirmation_deadline:null,config:{aspect:{enabled:false,method:'window'}},runtime:{phase:'disabled',backend:'none',format_active:false,source_client:{width:1280,height:720},frame_rates:{state:'unavailable',present_fps:null,fresh_frame_fps:null}}}};
}
export async function invoke(data,command,payload) {
 if(data.snapshot.confirmation_deadline && Date.parse(data.snapshot.confirmation_deadline)<=Date.now()) { data.snapshot.confirmation_deadline=null; data.snapshot.config.aspect.enabled=false; data.snapshot.runtime.format_active=false; data.model.confirmation_pending=false; }
 if(command==='settings_snapshot') return clone(data);
 const id=payload.id, ru=data.language==='ru';
 if(id==='model:off'||id==='model:power') { data.model.wants_off=id==='model:power'; data.model.power.label=data.model.wants_off?(ru?'Выключить':'Turn off'):(ru?'Включить':'Turn on');data.model.status=data.model.wants_off?(ru?'Пауза · вернитесь в игру':'Paused · return to the game'):(ru?'Эффект выключен':'Effect off'); }
 if(id.startsWith('model:format:')) { const kind=id.split(':')[2]; data.snapshot.config.aspect={enabled:kind!=='off',method:kind};data.snapshot.runtime.format_active=kind!=='off';data.snapshot.runtime.source_client={width:kind==='window'?960:1280,height:720}; data.snapshot.confirmation_deadline=kind==='system'?new Date(Date.now()+15000).toISOString():null;data.model.confirmation_pending=kind==='system'; }
 if(id==='confirm') { data.snapshot.confirmation_deadline=null; data.model.confirmation_pending=false; }
 if(id==='autostart') data.autostart.enabled=!data.autostart.enabled;
 for(const entries of [data.model.sources,data.model.effects,...data.model.settings.map(g=>g.entries)]) if(entries.some(e=>e.id===id)) for(const e of entries) if(e.checked!==null)e.checked=e.id===id;
}
