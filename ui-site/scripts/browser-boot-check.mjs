// Real ESM browser boot regression. Uses a new private Chrome profile and only
// loopback pages; never attaches to a user's browser or starts a server.
// Run against existing dev/prod fixture servers, with explicit opt-in:
// FLUXO_BROWSER_BOOT_TEST=1 node scripts/browser-boot-check.mjs --chrome /usr/bin/google-chrome --url http://127.0.0.1:PORT/demo/settings/system --url http://127.0.0.1:PORT/demo/sites/1/settings/network
import {spawn} from 'node:child_process';
import {access,mkdtemp,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join,basename} from 'node:path';
import {once} from 'node:events';
import {constants} from 'node:fs';

if(process.env.FLUXO_BROWSER_BOOT_TEST!=='1')throw Error('Explicit FLUXO_BROWSER_BOOT_TEST=1 is required.');
const args=process.argv.slice(2),urls=[];let chrome;
while(args.length){const option=args.shift(),value=args.shift();if(option==='--chrome')chrome=value;else if(option==='--url')urls.push(value);else throw Error('Use --chrome PATH --url LOOPBACK_URL (repeat --url).');}
if(!chrome||!urls.length)throw Error('Installed Chrome path and loopback URLs required; no browser download.');
for(const raw of urls){const u=new URL(raw);if(u.protocol!=='http:'||!['127.0.0.1','[::1]'].includes(u.hostname)||u.username||u.password||u.search||u.hash)throw Error('Only credential-free loopback HTTP fixture pages allowed.');}
await access(chrome,constants.X_OK);
const dir=await mkdtemp(join(tmpdir(),'fluxo-browser-boot-'));
let child,ws,launchError;const pending=new Map();let seq=0,events=[];
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
function call(method,params={},sessionId){const id=++seq;return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{pending.delete(id);reject(Error('Browser command timed out: '+method));},15000);pending.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})}));});}
try{
 child=spawn(chrome,['--headless=new','--no-sandbox','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0',`--user-data-dir=${dir}`,'about:blank'],{stdio:'ignore',detached:true,env:{PATH:process.env.PATH,LANG:process.env.LANG||'C.UTF-8',HOME:dir,XDG_CONFIG_HOME:join(dir,'config'),XDG_CACHE_HOME:join(dir,'cache')}});
 child.on('error',e=>{launchError=e});
 let port;for(let i=0;i<100;i++){if(launchError)throw launchError;if(child.exitCode!==null)throw Error('Private Chrome exited before debugging became ready.');try{port=(await readFile(join(dir,'DevToolsActivePort'),'utf8')).split('\n')[0];break;}catch{await pause(100);}}
 if(!port)throw Error('Private Chrome did not become ready.');
 const meta=await(await fetch(`http://127.0.0.1:${port}/json/version`)).json();ws=new WebSocket(meta.webSocketDebuggerUrl);await once(ws,'open');
 ws.addEventListener('message',event=>{const item=JSON.parse(event.data);if(item.id){const p=pending.get(item.id);if(p){pending.delete(item.id);clearTimeout(p.timer);item.error?p.reject(Error(item.error.message)):p.resolve(item.result);}}else events.push(item);});
 const result=[];
 for(const url of urls){
  const {targetId}=await call('Target.createTarget',{url:'about:blank'});const {sessionId}=await call('Target.attachToTarget',{targetId,flatten:true});
  try{
   await call('Runtime.enable',{},sessionId);await call('Page.enable',{},sessionId);events=[];await call('Page.navigate',{url},sessionId);
   let status;for(let i=0;i<150;i++){
    if(events.some(e=>e.sessionId===sessionId&&e.method==='Runtime.exceptionThrown'))throw Error('Uncaught browser exception during '+new URL(url).pathname+' (details remain in local fixture logs).');
    const response=await call('Runtime.evaluate',{expression:'JSON.stringify({ready:document.readyState,text:document.body?.innerText||""})',returnByValue:true},sessionId);status=JSON.parse(response.result.value);
    if(status.ready==='complete'&&status.text.includes('Automatic security updates')&&new URL(url).pathname.endsWith('/settings/system'))break;
    if(status.ready==='complete'&&status.text.includes('Protect this site')&&new URL(url).pathname.endsWith('/settings/network'))break;
    await pause(100);
   }
   await pause(300);
   if(events.some(e=>e.sessionId===sessionId&&e.method==='Runtime.exceptionThrown'))throw Error('Late browser boot exception.');
   const expected=new URL(url).pathname.endsWith('/settings/system')?'Automatic security updates':'Protect this site';
   if(!status?.text.includes(expected))throw Error('Actual page did not boot: '+new URL(url).pathname);
   result.push({path:new URL(url).pathname,body_ready:true,uncaught_exception:false});
  }finally{await call('Target.closeTarget',{targetId});}
 }
 console.log(JSON.stringify({passed:true,browser:basename(chrome),browser_version:meta.Browser,private_profile:true,actual_browser_boot:true,pages:result,no_native_service_or_installer_claim:true}));
}finally{
 for(const p of pending.values()){clearTimeout(p.timer);p.reject(Error('Browser closing.'));}pending.clear();
 if(ws){try{await call('Browser.close');}catch{}ws.close();}
 for(const p of pending.values())clearTimeout(p.timer);pending.clear();
 if(child&&child.exitCode===null){try{process.kill(-child.pid,'SIGTERM')}catch{};await Promise.race([once(child,'exit'),pause(3000)]);if(child.exitCode===null){try{process.kill(-child.pid,'SIGKILL')}catch{};await once(child,'exit');}}
 await rm(dir,{recursive:true,force:true});
}
