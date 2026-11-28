import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import * as vue from 'vue';
import { parse, compileScript } from '@vue/compiler-sfc';
import { renderToString } from '@vue/server-renderer';
let seq=0;
async function load(source,bindings={}) {
 const key=`systemTest${++seq}`;globalThis[key]={vue,...bindings};
 const js=stripTypeScriptTypes(source).replace(/^import ([^;\n]+?) from ['"]([^'"]+)['"];?$/gm,(_,names,path)=>{if (path.endsWith('/apiError')) return `import ${names} from ${JSON.stringify(new URL('../src/utils/apiError.ts', import.meta.url).href)};`;
    assert.ok(path in globalThis[key],path);return `const ${names.startsWith('{')?names.replace(/\bas\b/g,':'):names} = globalThis.${key}[${JSON.stringify(path)}];`;});
 return import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
}
const source=await readFile(new URL('../src/composables/useServerSystem.ts',import.meta.url),'utf8');
const oldBoot='00000000-0000-4000-8000-000000000001',newBoot='00000000-0000-4000-8000-000000000002';
const status=()=>({boot_id:oldBoot,updates:{state:'enabled',editable:true,automatic_reboot:'disabled',last_result:'unknown'},restart_required:{state:'required',packages:['libc6'],detail:'Ubuntu: libc6'},reboot:null});
async function fixture(options={}) {
 const store=new Map(Object.entries(options.storage||{}));globalThis.localStorage={getItem:k=>store.get(k)||null,setItem:(k,v)=>store.set(k,v),removeItem:k=>store.delete(k)};
 const data=status();let post=0;let yes=true;let invalidations=0;const confirmations=[];let resolveConfirmation;
 const api={invalidate:()=>invalidations++,getServerSystem:async()=>structuredClone(data),restartServer:async()=>{post++;return {id:'operation',boot_id:oldBoot,phase:'accepted'};},getServerRebootServices:async()=>[{unit:'nginx.service',state:'running',detail:'active/running'}],setSystemSecurityUpdates:async enabled=>({...data.updates,state:enabled?'enabled':'disabled'})};
 const mod=await load(source,{'../api/client':{apiClient:api},'./useConfirm':{useConfirm:()=>({confirm:async optionsValue=>{confirmations.push(optionsValue);if(options.deferConfirm)return new Promise(resolve=>{resolveConfirmation=resolve});return yes;}})}});
 return {flow:mod.useServerSystem(),mod,api,data,store,posts:()=>post,invalidations:()=>invalidations,cancel:()=>yes=false,confirmations,accept:()=>resolveConfirmation?.(true)};
}
async function noticeView(flow, overrides = {}) {
 const {loadVue}=await import('./helpers/loadVue.mjs');
 const bindings={'../composables/useServerSystem':{useServerSystem:()=>flow},'@heroicons/vue/24/outline':{ArrowPathIcon:{render:()=>null},XMarkIcon:{render:()=>null}}};
 for(const name of ['StatusNotice','NoticeBanner','ErrorAlert','AppButton','BaseModal','DataTable','StatusBadge']) bindings[`./${name}.vue`]=(await import(await loadVue(new URL(`../src/components/${name}.vue`,import.meta.url)))).default;
 const {descriptor}=parse(await readFile(new URL('../src/components/ServerSystemNotice.vue',import.meta.url),'utf8'));
 return (await load(compileScript(descriptor,{id:'server-system-notice',inlineTemplate:true}).content,{...bindings,...overrides})).default;
}
test('confirmed restart requirement survives a failed request, reload and unknown state; only confirmed absence clears it',async()=>{
 const f=await fixture();await f.flow.refresh();assert.equal(f.flow.restartRequired.value.state,'required');
 f.api.getServerSystem=async()=>{throw Error('offline')};await f.flow.refresh();assert.equal(f.flow.restartRequired.value.state,'required');
 const second=await fixture({storage:Object.fromEntries(f.store)});assert.equal(second.flow.restartRequired.value.state,'required');second.data.restart_required.state='unknown';await second.flow.refresh();assert.equal(second.flow.restartRequired.value.state,'required');second.data.restart_required.state='not_required';await second.flow.refresh();assert.equal(second.flow.restartRequired.value,null);
});
async function submit(f) { f.flow.restart();f.flow.reviewRestart();await f.flow.confirmRestart(); }
test('opening details and cancelling confirmation have no mutation; acceptance never reports finished on same boot',async()=>{
 const f=await fixture();f.flow.restart();assert.equal(f.posts(),0);assert.equal(f.flow.restartView.value,'details');f.flow.reviewRestart();assert.equal(f.posts(),0);f.flow.restartView.value='details';await f.flow.confirmRestart();assert.equal(f.posts(),0);
 const g=await fixture();await submit(g);assert.equal(g.posts(),1);assert.equal(g.flow.stage.value,'Restarting server…');await g.flow.refresh();assert.notEqual(g.flow.stage.value,'Services running');
 g.data.reboot={id:'operation',boot_id:oldBoot,phase:'accepted'};g.data.boot_id=newBoot;await g.flow.refresh();assert.equal(g.flow.stage.value,'Services running');assert.equal(g.posts(),1);assert.equal(g.invalidations(),1);await g.flow.refresh();assert.equal(g.invalidations(),1);
});
test('ambiguous request is not resubmitted; auth expiration is distinct from offline',async()=>{
 const f=await fixture();f.api.restartServer=async()=>{throw Error('network disconnected')};await submit(f);assert.equal(f.flow.stage.value,'Restart scheduling is unconfirmed');
 f.api.getServerSystem=async()=>{throw Error('Unauthorized')};await f.flow.refresh();assert.equal(f.flow.stage.value,'Sign in again to verify the restart');assert.match(f.flow.pollError.value,/session expired/);
});
test('server reported scheduling failure retains actual diagnostic and no success claim',async()=>{
 const f=await fixture();await submit(f);f.data.reboot={id:'operation',phase:'failed',detail:'systemd scheduling failed',boot_id:oldBoot};await f.flow.refresh();assert.equal(f.flow.stage.value,'Restart not confirmed');assert.equal(f.flow.error.value,'systemd scheduling failed');
});
test('service grace distinguishes actual running, failed and unknown; delayed returns remain waiting',async()=>{
 const f=await fixture();const flow={id:'operation',boot:oldBoot,requested:Date.now()-180000};
 assert.equal(f.mod.returnedStage(oldBoot,flow,[],180000),'Still waiting for the server');
 const failed=[{unit:'nginx.service',state:'failed'}];assert.equal(f.mod.returnedStage(newBoot,flow,failed,1000),'Server is back · Checking services');assert.match(f.mod.returnedStage(newBoot,flow,failed,100000),/need attention/);
 assert.match(f.mod.returnedStage(newBoot,flow,[{state:'unknown'}],100000),/need attention/);
 assert.match(f.mod.returnedStage(newBoot,flow,[],1000),/No managed services/);
});
test('shared warning uses existing notice frame/disclosure; no close, dismiss or arrow action',async()=>{
 const box={setup:()=>()=>vue.h('span')};
 async function component(file,bindings){const {descriptor}=parse(await readFile(new URL(`../src/components/${file}.vue`,import.meta.url),'utf8'));return (await load(compileScript(descriptor,{id:file,inlineTemplate:true}).content,bindings)).default;}
 const Notice=await component('NoticeBanner',{'@heroicons/vue/24/outline':{ArrowUpTrayIcon:box,XCircleIcon:box,ExclamationTriangleIcon:box}});
 const Status=await component('StatusNotice',{'./NoticeBanner.vue':Notice});
 const html=await renderToString(vue.createSSRApp(Status,{message:'Server restart required',tone:'warning',details:'Ubuntu: libc6'}));
 assert.match(html,/p-4 text-sm/);assert.match(html,/bg-amber-50/);assert.match(html,/Show details/);assert.match(html,/aria-expanded="false"/);assert.doesNotMatch(html,/Dismiss|aria-label="Close"|Ubuntu: libc6/);
 const f=await fixture();await f.flow.refresh();const view=await noticeView(f.flow,{'./StatusNotice.vue':Status});const output=await renderToString(vue.createSSRApp(view));assert.match(output,/Installed system updates require/);assert.match(output,/Show details/);assert.doesNotMatch(output,/>Restart server<|aria-expanded/);assert.doesNotMatch(output,/Dismiss|Hide notification|→/);
});

test('failed service observation after confirmed return remains unknown rather than server offline',async()=>{
 const f=await fixture();await submit(f);f.data.boot_id=newBoot;f.data.reboot={id:'operation',phase:'accepted',boot_id:oldBoot};f.api.getServerRebootServices=async()=>{throw Error('Service inspection unavailable')};await f.flow.refresh();assert.equal(f.flow.stage.value,'Server is back · Service status unavailable');assert.notEqual(f.flow.stage.value,'Services running');
});

test('duplicate confirmation clicks submit only once',async()=>{
 const f=await fixture();let release,posts=0;const wait=new Promise(done=>release=done);
 f.api.restartServer=async()=>{posts++;await wait;return {id:'operation',boot_id:oldBoot,phase:'accepted'};};
 f.flow.restart();f.flow.reviewRestart();const first=f.flow.confirmRestart();await new Promise(resolve=>setImmediate(resolve));await f.flow.confirmRestart();assert.equal(posts,1);release();await first;assert.equal(posts,1);
});

test('shared disclosure opens and closes actual details with accessible state',async()=>{
 const box={setup:()=>()=>vue.h('span')};
 async function component(file,bindings){const {descriptor}=parse(await readFile(new URL(`../src/components/${file}.vue`,import.meta.url),'utf8'));return (await load(compileScript(descriptor,{id:file,inlineTemplate:true}).content,bindings)).default;}
 const Notice=await component('NoticeBanner',{'@heroicons/vue/24/outline':{ArrowUpTrayIcon:box,XCircleIcon:box,ExclamationTriangleIcon:box}});
 const Status=await component('StatusNotice',{'./NoticeBanner.vue':Notice});
 const node=(type,text='')=>({type,text,children:[],props:{},parent:null});
 const renderer=vue.createRenderer({createElement:type=>node(type),createText:t=>node('#text',t),createComment:t=>node('#comment',t),setText:(n,t)=>n.text=t,setElementText:(n,t)=>{n.text=t;n.children=[]},parentNode:n=>n?.parent,nextSibling:n=>n?.parent?.children[n.parent.children.indexOf(n)+1]||null,patchProp:(n,k,_old,v)=>n.props[k]=v,insert:(n,p,anchor=null)=>{if(n.parent)n.parent.children.splice(n.parent.children.indexOf(n),1);n.parent=p;const at=anchor?p.children.indexOf(anchor):-1;p.children.splice(at<0?p.children.length:at,0,n)},remove:n=>{if(n?.parent){n.parent.children=n.parent.children.filter(x=>x!==n);n.parent=null}}});
 const root=node('root');const app=renderer.createApp(Status,{message:'Server restart required',details:'Ubuntu package libc6',tone:'warning'});app.mount(root);
 const all=n=>[n,...n.children.flatMap(all)];const text=n=>(n.type==='#comment'?'':n.text)+n.children.map(text).join('');
 const button=()=>all(root).find(n=>n.type==='button');assert.equal(button().props['aria-expanded'],false);button().props.onClick();await vue.nextTick();assert.equal(button().props['aria-expanded'],true);assert.match(text(root),/Hide details/);assert.match(text(root),/Ubuntu package libc6/);button().props.onClick();await vue.nextTick();assert.equal(button().props['aria-expanded'],false);assert.doesNotMatch(text(root),/Ubuntu package libc6/);app.unmount();
});

test('actual demo routes simulate settings, reject malformed policy and never pretend to reboot',async()=>{
 const source=await readFile(new URL('../../ui-site/src/api/mock.ts',import.meta.url),'utf8');
 const {demoGitHubSiteAccess}=await import('../../ui-site/src/api/githubAccessDemo.ts');
 const {trafficAPISettings}=await import('../src/utils/trafficAnalytics.ts');
 const raw=stripTypeScriptTypes(`const demoGitHubSiteAccess=${demoGitHubSiteAccess.toString()};const trafficAPISettings=${trafficAPISettings.toString()};const appVersion='demo';const createRequestID=()=> 'demo-id';\n`+source.replace(/^import .*\r?\n/gm,''));
 const {MockApiClient}=await import('data:text/javascript;base64,'+Buffer.from(raw).toString('base64'));
 const api=new MockApiClient();const first=await api.get('/api/v1/server/system');assert.equal(first.restart_required.state,'not_required');assert.equal(first.updates.last_result,'unknown');
 assert.equal((await api.put('/api/v1/server/system/security-updates',{enabled:false})).state,'disabled');assert.equal((await api.get('/api/v1/server/system')).updates.state,'disabled');await assert.rejects(api.put('/api/v1/server/system/security-updates',{enabled:'true'}),/boolean/);
 await assert.rejects(api.post('/api/v1/server/system/reboots',{boot_id:first.boot_id,confirm:true}),/No server was restarted/);
 assert.equal((await api.get('/api/v1/server/system')).reboot,null);
});

test('System retains Enabled, date and failures without routine success or unknown chips; kernel refreshes from actual status or unavailable',async()=>{
 const box={setup:(_,ctx)=>()=>vue.h('div',ctx.slots.default?.())};
 const badge=(await import(await (await import('./helpers/loadVue.mjs')).loadVue(new URL('../src/components/StatusBadge.vue',import.meta.url)))).default;
 const {descriptor}=parse(await readFile(new URL('../src/views/SettingsSystem.vue',import.meta.url),'utf8'));
 const live=vue.ref({...status(),running_kernel:'6.8.0-88-generic',updates:{...status().updates,last_result:'successful',last_attempt:'23 Nov2026 02:00 UTC'}});
 const view=(await load(compileScript(descriptor,{id:'system-display',inlineTemplate:true}).content,{'../components/Card.vue':box,'../components/ToggleSwitch.vue':box,'../components/StatusBadge.vue':badge,'../components/AppButton.vue':box,'../components/StatusNotice.vue':box,'../composables/useServerSystem':{useServerSystem:()=>({status:live,busy:vue.ref(false),pollError:vue.ref(''),restartPending:vue.ref(false),refresh(){},restart(){},setUpdates(){}})}})).default;
 let html=await renderToString(vue.createSSRApp(view));assert.match(html,/Enabled/);assert.match(html,/23 Nov2026 02:00 UTC/);assert.match(html,/6.8.0-88-generic/);assert.doesNotMatch(html,/Successful/);
 for(const last_attempt of ['23 Nov2026 02:00 UTC','']) {
  live.value={...live.value,updates:{...live.value.updates,last_result:'unknown',last_attempt}};
  html=await renderToString(vue.createSSRApp(view));assert.match(html,/Enabled/);assert.ok(html.includes(last_attempt || 'Unavailable'));assert.doesNotMatch(html,/>Unknown<|>Successful</);
 }
 live.value={...live.value,running_kernel:null,updates:{...live.value.updates,last_result:'failed'}};html=await renderToString(vue.createSSRApp(view));assert.match(html,/Unavailable/);assert.match(html,/Failed/);assert.match(html,/Enabled/);
});

test('System reconnect after a changed boot publishes the new kernel and never substitutes an installed candidate',async()=>{
 const f=await fixture();f.data.running_kernel='6.8.0-old';await f.flow.refresh();assert.equal(f.flow.status.value.running_kernel,'6.8.0-old');
 await submit(f);f.data.boot_id=newBoot;f.data.reboot={id:'operation',boot_id:oldBoot,phase:'accepted'};f.data.running_kernel='6.8.0-new';await f.flow.refresh();assert.equal(f.flow.status.value.running_kernel,'6.8.0-new');
 f.data.running_kernel=null;await f.flow.refresh();assert.equal(f.flow.status.value.running_kernel,null);
});

async function confirmedReturn() {
 const f=await fixture();await submit(f);
 f.data.boot_id=newBoot;f.data.reboot={id:'operation',boot_id:oldBoot,phase:'accepted',requested_at:new Date().toISOString()};
 await f.flow.refresh();return f;
}
test('completed restart dismissal survives polling and reload without clearing the required warning or recovery evidence',async()=>{
 const pending=await fixture();await submit(pending);pending.flow.dismissResult();assert.equal(pending.flow.canDismissResult.value,false);assert.equal(pending.store.has('fluxo_server_restart_dismissed'),false);
 const f=await confirmedReturn();assert.equal(f.flow.canDismissResult.value,true);f.flow.dismissResult();assert.equal(f.flow.resultDismissed.value,true);
 assert.equal(f.flow.restartRequired.value.state,'required');assert.ok(f.store.has('fluxo_server_restart'));
 let finish;f.api.getServerRebootServices=()=>new Promise(resolve=>{finish=resolve});const refreshing=f.flow.refresh();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(f.flow.resultDismissed.value,true,'routine polling must not flash the dismissed result');
 finish([{unit:'nginx.service',state:'running',detail:'active/running'}]);await refreshing;assert.equal(f.flow.resultDismissed.value,true);
 const restored=await fixture({storage:Object.fromEntries(f.store)});restored.data.boot_id=newBoot;restored.data.reboot={...f.data.reboot};await restored.flow.refresh();assert.equal(restored.flow.resultDismissed.value,true);assert.equal(restored.flow.restartRequired.value.state,'required');assert.equal(restored.posts(),0);
});
test('new failures and unavailable or unauthenticated observations are not hidden by a prior success dismissal',async t=>{
 const f=await confirmedReturn();f.flow.dismissResult();
 const originalNow=Date.now;Date.now=()=>originalNow()+100_000;t.after(()=>{Date.now=originalNow});
 f.api.getServerRebootServices=async()=>[{unit:'nginx.service',state:'failed',detail:'exit-code'}];await f.flow.refresh();assert.match(f.flow.stage.value,/need attention/);assert.equal(f.flow.resultDismissed.value,false);assert.equal(f.flow.canDismissResult.value,true);
 f.api.getServerRebootServices=async()=>{throw Error('Service inspection unavailable')};await f.flow.refresh();assert.equal(f.flow.resultDismissed.value,false);assert.equal(f.flow.canDismissResult.value,false);assert.match(f.flow.stage.value,/unavailable/);
 f.api.getServerSystem=async()=>{throw Error('Unauthorized')};await f.flow.refresh();assert.equal(f.flow.resultDismissed.value,false);assert.equal(f.flow.canDismissResult.value,false);assert.match(f.flow.stage.value,/Sign in again/);
});
test('a new restart has a new dismissal identity; empty expected-service inventory does not claim site health',async()=>{
 const f=await confirmedReturn();f.flow.dismissResult();
 f.api.restartServer=async()=>{f.data.reboot={id:'second',boot_id:newBoot,phase:'accepted',requested_at:new Date().toISOString()};return f.data.reboot};
 await submit(f);assert.equal(f.flow.resultDismissed.value,false);assert.equal(f.flow.canDismissResult.value,false);
 f.data.boot_id='00000000-0000-4000-8000-000000000003';f.api.getServerRebootServices=async()=>[];await f.flow.refresh();assert.match(f.flow.stage.value,/No managed services were expected/);assert.equal(f.flow.resultDismissed.value,false);assert.equal(f.flow.canDismissResult.value,true);
});
test('actual restart notice opens a shared modal, keeps status checking inside it, and exposes dismissal only after return',async t=>{
 const f=await confirmedReturn();f.data.restart_required.state='not_required';await f.flow.refresh();f.flow.modalOpen.value=false;
 const modal={props:['modelValue','title'],setup:(props,{slots})=>()=>props.modelValue?vue.h('div',{role:'dialog'},[vue.h('h2',props.title),slots.default?.(),slots.footer?.()]):null};
 const Resize=globalThis.ResizeObserver;globalThis.ResizeObserver=class{observe(){}disconnect(){}};t.after(()=>{globalThis.ResizeObserver=Resize});
 const view=await noticeView({...f.flow,start(){},stop(){}},{'./BaseModal.vue':modal});
 const node=(type,text='')=>({type,text,children:[],props:{},parent:null});
 const renderer=vue.createRenderer({createElement:type=>node(type),createText:text=>node('#text',text),createComment:text=>node('#comment',text),setText:(n,text)=>n.text=text,setElementText:(n,text)=>{n.text=text;n.children=[]},parentNode:n=>n?.parent,nextSibling:n=>n?.parent?.children[n.parent.children.indexOf(n)+1]||null,patchProp:(n,key,_,value)=>n.props[key]=value,insert:(n,parent,anchor=null)=>{if(n.parent)n.parent.children.splice(n.parent.children.indexOf(n),1);n.parent=parent;const at=anchor?parent.children.indexOf(anchor):-1;parent.children.splice(at<0?parent.children.length:at,0,n)},remove:n=>{if(n?.parent)n.parent.children=n.parent.children.filter(child=>child!==n)}});
 const root=node('root'),app=renderer.createApp(view);app.mount(root);t.after(()=>app.unmount());
 const all=n=>[n,...n.children.flatMap(all)],text=n=>(n.type==='#comment'?'':n.text)+n.children.map(text).join('');
 const button=label=>all(root).find(n=>n.type==='button'&&text(n).trim()===label);
 assert.ok(button('Show details'));assert.equal(button('Check status'),undefined);assert.equal(all(root).some(n=>n.props.role==='dialog'),false);assert.doesNotMatch(text(root),/active\/running/);
 button('Show details').props.onClick();await vue.nextTick();assert.ok(all(root).some(n=>n.props.role==='dialog'));assert.match(text(root),/Nginx/);assert.match(text(root),/Running/);assert.ok(button('Check status'));
 button('Close').props.onClick();await vue.nextTick();assert.doesNotMatch(text(root),/Services running/);assert.equal(f.flow.resultDismissed.value,true);
});

test('only authoritative failed scheduling permits another explicit confirmation; ambiguous attempts never resubmit',async()=>{
 const f=await fixture();await submit(f);f.data.reboot={id:'operation',boot_id:oldBoot,phase:'failed',detail:'Scheduling failed'};await f.flow.refresh();
 f.flow.restart();assert.equal(f.flow.restartView.value,'details');f.flow.reviewRestart();await f.flow.confirmRestart();assert.equal(f.posts(),2);
 const g=await fixture();let attempts=0;g.api.restartServer=async()=>{attempts++;throw Error('Disconnected')};await submit(g);g.flow.restart();g.flow.reviewRestart();await g.flow.confirmRestart();assert.equal(attempts,1);assert.equal(g.flow.restartView.value,'progress');
});

test('completed service results and actual reboot requirements open their own view of the same flow',async()=>{
 const f=await confirmedReturn();assert.ok(f.flow.restartRequired.value);assert.equal(f.flow.canDismissResult.value,true);
 f.flow.showRestartDetails();assert.equal(f.flow.restartView.value,'details');assert.equal(f.flow.modalOpen.value,true);
 f.flow.showRestartStatus();assert.equal(f.flow.restartView.value,'progress');assert.equal(f.posts(),1);
 f.flow.dismissResult();assert.ok(f.flow.restartRequired.value);f.flow.restart();assert.equal(f.flow.restartView.value,'details');
});
