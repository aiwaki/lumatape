import test from 'node:test';
import assert from 'node:assert/strict';
import {RequestGate, remainingSeconds, powerIntent, confirmationAllows, settingGroups, plainLabel} from '../tray-assets/settings-model.mjs';
import {fixture,invoke} from '../tray-assets/preview.mjs';
test('late reads cannot overwrite changes or emergency state', () => {
 const gate=new RequestGate(), read=gate.beginRead(); assert(gate.acceptRead(read)); assert(gate.beginAction()); assert(!gate.beginAction()); assert(!gate.acceptRead(read));
 assert(gate.beginAction(true)); gate.endAction(); assert(!gate.acceptRead(gate.beginRead())); gate.endAction(); assert(!gate.acceptRead(read)); assert(gate.acceptRead(gate.beginRead()));
});
test('only a valid unexpired engine deadline enables Keep',()=>{
 const now=Date.parse('2026-10-11T00:00:00Z'); assert.equal(remainingSeconds('2026-10-11T00:00:15Z',now),15); assert.equal(remainingSeconds('2026-10-11T00:00:15Z',now+14999),1); assert.equal(remainingSeconds('2026-10-11T00:00:15Z',now+15000),0); assert.equal(remainingSeconds('invalid',now),0);
});
test('pending work and wanted-on state always make power an Off intent',()=>{
 assert.equal(powerIntent({wants_off:false},0),'model:power'); assert.equal(powerIntent({wants_off:true},0),'model:off'); assert.equal(powerIntent({wants_off:false},1),'model:off');
 assert.equal(powerIntent({wants_off:false,confirmation_pending:true},0),'model:off');
});
test('failed refresh keeps the visible Turn off action safe with an older off snapshot',()=>{
 const oldModel={wants_off:false};
 assert.equal(powerIntent(oldModel,0,true),'model:off');
 assert.equal(powerIntent(oldModel,0,false),'model:power');
});
test('display confirmation keeps modal actions out of its countdown',()=>{
 for (const id of ['import','testcard','diagnostics','updates','autostart','about','hotkey-test']) assert.equal(confirmationAllows(id),false,id);
 for (const id of ['confirm','model:format:off','model:off','quit']) assert.equal(confirmationAllows(id),true,id);
});
test('localized group identity does not depend on translated labels',()=>{
 for(const lang of ['ru','en']) { const data=fixture(lang),groups=settingGroups(data.model);assert.equal(groups.shape.entries[0].id,'model:shape:flat');assert.equal(groups.format.entries.length,3);assert.equal(groups.backend.entries.length,4); assert.equal(groups.extra.length,4); assert.deepEqual(groups.extra.find(group=>group.entries.some(entry=>entry.id==='model:dar:auto')).entries.map(entry=>entry.id),['model:dar:auto','model:dar:4:3']); }
 assert.equal(plainLabel('A && B'),'A & B');
});
test('preview is explicitly separate, shows countdown and rollback',async()=>{
 const data=fixture('ru');await invoke(data,'settings_action',{id:'model:format:system'});assert(data.snapshot.confirmation_deadline);await invoke(data,'settings_action',{id:'model:format:off'});assert.equal(data.snapshot.confirmation_deadline,null);assert.equal(data.snapshot.config.aspect.enabled,false);
});
