import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {stripTypeScriptTypes} from 'node:module';
import {createSSRApp} from 'vue';
import {renderToString} from '@vue/server-renderer';
import {loadVue} from './helpers/loadVue.mjs';
import {APIError,apiErrorMessage} from '../src/utils/apiError.ts';
import {stickyGeometry} from '../src/utils/stickyLayout.ts';

const source = await readFile(new URL('../src/api/client.ts',import.meta.url),'utf8');
const dependencies=`const calls=[];const router={push:path=>calls.push(path)};const useToast=()=>({addToast:message=>calls.push(message)});const useAuthStore=()=>({token:'test',clearToken:()=>calls.push('cleared')});const clearLogViewSnapshots=()=>{};`;
const isolated=stripTypeScriptTypes(dependencies+source.replace(/^import .*;\r?\n/gm,line=>line.includes('utils/apiError')?line.replace(/['"][^'"]+['"]/,JSON.stringify(new URL('../src/utils/apiError.ts',import.meta.url).href)):'')+'\nexport {calls};');
const {apiClient,calls}=await import('data:text/javascript;base64,'+Buffer.from(isolated).toString('base64'));

test('cross-feature HTTP errors remain structured for predicates but display only readable public fields',async t=>{
 const old=globalThis.fetch;t.after(()=>globalThis.fetch=old);
 for(const path of ['/sites/1/domains/1','/sites/1/network','/firewall','/server/php/updates','/server/node/updates','/databases/1/exports/example/download']){
  const payload={code:'maintenance_busy',message:'Wildcard routing cannot include the reserved panel hostname',details:{password:'DO-NOT-DISPLAY',generation:'exact'}};
  const raw=JSON.stringify(payload);globalThis.fetch=async()=>new Response(raw,{status:409});
  await assert.rejects(apiClient.get('/api/v1'+path,{useCache:false}),e=>{
   assert.ok(e instanceof APIError);assert.equal(e.status,409);assert.equal(e.message,raw);assert.equal(e.rawMessage,raw);assert.deepEqual(e.payload,payload);
   assert.equal(apiErrorMessage(e),payload.message);assert.ok(!apiErrorMessage(e).includes('DO-NOT-DISPLAY'));return true;
  });
 }
});
test('validation, plain, malformed, HTML, empty and network failures use safe useful presentation',()=>{
 for(const [input,wanted] of [[Error('Already configured'),'Already configured'],[Error('{"errors":{"domain":["Enter a valid domain."]}}'),'Enter a valid domain.'],[Error('{"message":{"bad":"secret"},"details":"secret"}'),'Fallback'],[Error('{bad json secret'),'Fallback'],[Error('<html>proxy error secret</html>'),'Fallback'],[Error(''),'Fallback'],[{details:{password:'secret'}},'Fallback'],[new TypeError('Failed to fetch'),'Could not reach the server. Check your connection and try again.']]) assert.equal(apiErrorMessage(input,'Fallback'),wanted);
});
test('successful JSON/text/204 responses stay unchanged and 401 still clears auth and navigates once per response',async t=>{
 const oldFetch=globalThis.fetch,oldWindow=globalThis.window;t.after(()=>{globalThis.fetch=oldFetch;globalThis.window=oldWindow});
 for(const [body,status,wanted] of [['{"ok":true}',200,{ok:true}],['plain',200,'plain'],[null,204,null]]){globalThis.fetch=async()=>new Response(body,{status});assert.deepEqual(await apiClient.get('/api/v1/probe',{useCache:false}),wanted)}
 globalThis.window={location:{pathname:'/sites/1/settings/network'}};globalThis.fetch=async()=>new Response('Unauthorized',{status:401});
 await assert.rejects(apiClient.get('/api/v1/sites/1',{useCache:false}),/Unauthorized/);assert.deepEqual(calls.slice(-3),['cleared','Session expired or unauthorized. Please sign in again.','/login']);
});
test('binary download HTTP errors use the same readable and authenticated path',async t=>{
 const old=globalThis.fetch;t.after(()=>globalThis.fetch=old);
 globalThis.fetch=async()=>new Response('{"message":"Export is not ready","code":"not_ready"}',{status:409});
 await assert.rejects(apiClient.downloadDatabaseExport(1,'example'),e=>apiErrorMessage(e)==='Export is not ready'&&e.payload.code==='not_ready');
});
test('real shared error/form/notice frames format JSON and Vue escapes message text; raw detail stays available',async()=>{
 for(const [file,props] of [['ErrorAlert',{message:'{"message":"Readable domain error","details":"PRIVATE"}'}],['FormGroup',{label:'Domain',error:'{"errors":{"domain":["Readable domain error"]}}'}],['NoticeBanner',{message:'{"message":"Readable domain error"}',tone:'error'}]]) {
  const c=(await import(await loadVue(new URL(`../src/components/${file}.vue`,import.meta.url)))).default;
  const html=await renderToString(createSSRApp(c,props));assert.match(html,/Readable domain error/);assert.doesNotMatch(html,/PRIVATE|&quot;message&quot;|\[object Object\]/);
  if(file==='NoticeBanner') {const escaped=await renderToString(createSSRApp(c,{message:'{"message":"<script>alert(1)</script>"}'}));assert.doesNotMatch(escaped,/<script>/);assert.match(escaped,/&lt;script&gt;/)}
 }
});
test('sticky geometry clears the measured header, drops to natural flow on narrow/tall info pages, and keeps long navigation reachable',()=>{
 assert.deepEqual(stickyGeometry(1200,800,96,200),{top:112,available:672,active:true});
 assert.equal(stickyGeometry(375,800,96,200).active,false);
 assert.equal(stickyGeometry(1200,480,120,500).active,false);
 assert.equal(stickyGeometry(1200,480,120,500,768,true).active,true);
 assert.equal(stickyGeometry(1200,240,120,500,768,true).active,false);
 assert.equal(stickyGeometry(900,800,90,200,1024).active,false);
});
test('actual router redirects old Network links preserving query/hash and selects nested Settings + Network',async()=>{
 let raw=await readFile(new URL('../src/router/index.ts',import.meta.url),'utf8');
 raw=raw.replace(/^import (\w+) from ['"][^'"]+\.vue['"];?$/gm,'const $1 = {render(){return null}};').replace("import { createRouter, createWebHistory } from 'vue-router';",`import {createRouter,createMemoryHistory as createWebHistory} from '${import.meta.resolve('vue-router')}';`).replace("import { useAuthStore } from '../stores/auth';","const useAuthStore=()=>({syncFromStorage(){},isAuthenticated:true});");
 const oldDoc=globalThis.document,oldHistory=globalThis.history;globalThis.history={state:{}};globalThis.document={title:'',querySelector:()=>null};
 try {const {router}=await import('data:text/javascript;base64,'+Buffer.from(stripTypeScriptTypes(raw)).toString('base64'));await router.push('/sites/17/network?review=1#redirects');assert.equal(router.currentRoute.value.fullPath,'/sites/17/settings/network?review=1#redirects');assert.equal(router.currentRoute.value.name,'site-settings-network');assert.equal(router.currentRoute.value.matched.length,3);assert.equal(globalThis.document.title,'Network — fluxo')}
 finally{globalThis.document=oldDoc;globalThis.history=oldHistory}
});

test('toast presentation never dumps raw object fields and lifecycle updates keep the same readable boundary',async()=>{
 let s=await readFile(new URL('../src/composables/useToast.ts',import.meta.url),'utf8');
 s=s.replace("from 'vue'",`from '${import.meta.resolve('vue')}'`).replace("from '../utils/apiError'",`from '${new URL('../src/utils/apiError.ts',import.meta.url).href}'`);
 const {useToast}=await import('data:text/javascript;base64,'+Buffer.from(stripTypeScriptTypes(s)).toString('base64'));const toast=useToast();
 const raw='{"message":"Readable firewall error","details":{"password":"PRIVATE"}}';
 try{for(const type of ['info','success','warning','loading']){const id=toast.showToast({title:'123',description:'[1,2]',type});assert.equal(toast.toasts.value.at(-1).title,'123');assert.equal(toast.toasts.value.at(-1).description,'[1,2]');toast.removeToast(id)}const id=toast.showToast({title:raw,type:'error'});assert.equal(toast.toasts.value.at(-1).title,'Readable firewall error');toast.updateToast(id,{description:'{"errors":{"domain":["Enter a domain"]}}'});assert.equal(toast.toasts.value.at(-1).description,'Enter a domain');assert.doesNotMatch(JSON.stringify(toast.toasts.value),/PRIVATE/)}finally{for(const item of [...toast.toasts.value])toast.removeToast(item.id)}
});

test('bracket diagnostic prefixes remain plain; recognizable broken JSON arrays and HTML fail safely',()=>{
 for(const text of ['[Errno 13] Permission denied','[::1]:443 connection refused','[404] Not found'])assert.equal(apiErrorMessage(Error(text),'Safe fallback'),text);
 for(const text of ['["broken array"','[{"message":','<html>secret</html>'])assert.equal(apiErrorMessage(Error(text),'Safe fallback'),'Safe fallback');
 assert.equal(apiErrorMessage(Error('"JSON string message"')),'JSON string message');
});
