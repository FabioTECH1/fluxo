import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { parse, compileScript } from '@vue/compiler-sfc';
import { renderToString } from '@vue/server-renderer';
import * as vue from 'vue';
let id=0;
async function module(source,bindings={}){
 const key=`nginxTest${++id}`;globalThis[key]={vue,...bindings};
 const js=stripTypeScriptTypes(source).replace(/^import ([\s\S]+?) from ['"]([^'"]+)['"];?$/gm,(_,names,path)=>{assert.ok(path in globalThis[key],path);return `const ${names.startsWith('{')?names.replace(/\bas\b/g,':'):names} = globalThis.${key}[${JSON.stringify(path)}];`;});
 return import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
}
const source=await readFile(new URL('../src/composables/useNginxUpdates.ts',import.meta.url),'utf8');
const snapshot={state:'update_available',reason:'Configured candidate',package:'nginx-core',architecture:'amd64',binary:'/usr/sbin/nginx',installed_version:'1.24.0-2ubuntu7.15',candidate_version:'1.24.0-2ubuntu7.21',sources:[],held:false,source_identity:'policy',identity:'candidate'};
const status=(patch={})=>({snapshot:structuredClone(snapshot),last_successful_check:100000,attempted_at:100000,error:'',stale:false,checking:false,notice:true,running_adoption:'not_verified',...patch});
async function fixture(){let next=status();const calls=[];const api={getNginxUpdates:async()=>structuredClone(next),checkNginxUpdates:async()=>{calls.push('check');return structuredClone(next)},dismissNginxUpdate:async identity=>{calls.push(['dismiss',identity]);return status({notice:false})}};const mod=await module(source,{'../api/client':{apiClient:api}});return {mod,flow:mod.useNginxUpdates(),api,calls,set:v=>next=v}}
const plain=await fixture();
test('current/found/held/unsupported/missing/stale/failure messages never confuse currentness',()=>{
 assert.match(plain.mod.nginxCheckMessage(status()),/update is available/);
 assert.match(plain.mod.nginxCheckMessage(status({snapshot:{...snapshot,state:'current'}})),/No Nginx update is offered/);
 for(const v of [status({error:'refresh failed'}),status({stale:true}),status({snapshot:null,stale:true}),status({snapshot:{...snapshot,state:'held'}}),status({snapshot:{...snapshot,state:'unsupported'}}),status({snapshot:{...snapshot,state:'unavailable'}})])assert.doesNotMatch(plain.mod.nginxCheckMessage(v),/No Nginx update is offered/);
});
test('instructions quote only safe reviewed package metadata and distinguish actual restart',()=>{
 const commands=plain.mod.nginxSSHCommands(snapshot);assert.match(commands,/--simulate install --only-upgrade 'nginx-core:amd64=1.24.0-2ubuntu7.21'/);assert.match(commands,/sudo nginx -t/);assert.match(commands,/systemctl restart nginx/);assert.doesNotMatch(commands,/allow-change-held|allow-unauthenticated|remove|autoremove|add-apt|curl| -y/);
 for(const patch of [{package:'nginx;bad'},{candidate_version:"1.2'$(bad)"},{architecture:'amd64;bad'},{state:'unsupported'}])assert.equal(plain.mod.nginxSSHCommands({...snapshot,...patch}),'');
});
test('checks/dismissals are explicit, candidate-bound and duplicate protected',async()=>{
 const f=await fixture();await f.flow.refresh();assert.deepEqual(f.calls,[]);await f.flow.dismiss();assert.deepEqual(f.calls,[['dismiss','candidate']]);assert.equal(f.flow.available.value,false);
 let resolve;f.api.checkNginxUpdates=()=>{f.calls.push('check');return new Promise(r=>resolve=r)};const first=f.flow.check();await f.flow.check();assert.equal(f.calls.filter(c=>c==='check').length,1);resolve(status({snapshot:{...snapshot,identity:'newer'},notice:true}));await first;assert.equal(f.flow.available.value,true);
});
test('failed check preserves last successful package info as stale without an availability notice',async()=>{
 const f=await fixture();await f.flow.refresh();f.api.checkNginxUpdates=async()=>{throw Error('refresh failed')};await f.flow.check();assert.equal(f.flow.state.value.snapshot.installed_version,snapshot.installed_version);assert.equal(f.flow.state.value.last_successful_check,100000);assert.equal(f.flow.state.value.stale,true);assert.equal(f.flow.available.value,false);assert.match(f.flow.error.value,/refresh failed/);
});
test('older GET cannot overwrite explicit check and SSH installed refresh removes obsolete notice',async()=>{
 const f=await fixture();let resolve;f.api.getNginxUpdates=()=>new Promise(r=>resolve=r);const reading=f.flow.refresh();f.set(status({snapshot:{...snapshot,state:'current',installed_version:snapshot.candidate_version},notice:false}));await f.flow.check();resolve(status());await reading;assert.equal(f.flow.state.value.snapshot.state,'current');assert.equal(f.flow.available.value,false);
});
test('API discovery explicitly bypasses inventory cache and keeps JSON bodies scoped',async()=>{
 const client=await readFile(new URL('../src/api/client.ts',import.meta.url),'utf8');assert.match(client,/getNginxUpdates\(\)[\s\S]{0,180}bypassCache: true, useCache: false/);assert.match(client,/checkNginxUpdates\(\)[\s\S]{0,140}body: '\{\}'/);
});
async function component(path,bindings={}){const {descriptor}=parse(await readFile(new URL(`../src/${path}.vue`,import.meta.url),'utf8'));return(await module(compileScript(descriptor,{id:path,inlineTemplate:true}).content,bindings)).default}
const box={setup:(_,{slots})=>()=>vue.h('div',[slots.default?.(),slots.actions?.(),slots.footer?.()])};
test('runtime retains reload/check controls and instructions after notice dismissal',async()=>{
 const f=await fixture();await f.flow.refresh();const c=await component('views/RuntimeNginx',{'../api/client':{apiClient:{}},'../composables/useToast':{useToast:()=>({addToast:()=>{}})},'../composables/useConfirm':{useConfirm:()=>({confirm:async()=>false})},'../composables/useNginxUpdates':{...f.mod,useNginxUpdates:()=>f.flow},'../components/SkeletonLoader.vue':box,'../components/Card.vue':box,'../components/AppButton.vue':box,'../components/ErrorAlert.vue':box,'@heroicons/vue/24/outline':{ArrowRightIcon:box}});const html=await renderToString(vue.createSSRApp(c));assert.match(html,/Reload Nginx/);assert.match(html,/Check for updates/);assert.match(html,/Update available/);assert.match(html,/View instructions/);assert.match(html,/1\.24\.0-2ubuntu7\.15/);assert.match(html,/1\.24\.0-2ubuntu7\.21/);assert.doesNotMatch(html,/Restart Nginx|Update now|Install update/);await f.flow.dismiss();const dismissed=await renderToString(vue.createSSRApp(c));assert.match(dismissed,/View instructions/);assert.match(dismissed,/Update available/);for(const v of [status({stale:true,notice:false}),status({error:'refresh failed',notice:false}),status({snapshot:{...snapshot,state:'current'},notice:false})]){f.set(v);await f.flow.refresh();const other=await renderToString(vue.createSSRApp(c));assert.doesNotMatch(other,/Update available/);}
});
test('actual shared notice opens instructions and keeps its separate dismiss event',async()=>{
 const s=await readFile(new URL('../src/components/UpdateNotice.vue',import.meta.url),'utf8');assert.match(s,/\$emit\('action'\)/);assert.match(s,/\$emit\('dismiss'\)/);assert.doesNotMatch(s,/→|➜/);
 const f=await fixture();await f.flow.refresh();const c=await component('components/NginxUpdateNotice',{'../composables/useNginxUpdates':{...f.mod,useNginxUpdates:()=>f.flow},'./UpdateNotice.vue':box,'./BaseModal.vue':box,'./AppButton.vue':box});const html=await renderToString(vue.createSSRApp(c));assert.match(html,/nginx-core/);assert.match(html,/1\.24\.0-2ubuntu7\.15/);assert.match(html,/1\.24\.0-2ubuntu7\.21/);assert.match(html,/apt-get --simulate install --only-upgrade/);assert.match(html,/nginx -t &amp;&amp; sudo systemctl restart nginx/);assert.match(html,/Updating Nginx may briefly interrupt sites/);
});
