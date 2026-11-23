import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {stripTypeScriptTypes} from 'node:module';
import {parse,compileScript} from '@vue/compiler-sfc';
import * as vue from 'vue';
import {renderToString} from '@vue/server-renderer';
import {loadVue} from './helpers/loadVue.mjs';
import * as utils from '../src/utils/trafficAnalytics.ts';
import {demoGitHubSiteAccess} from '../../ui-site/src/api/githubAccessDemo.ts';

test('range options follow every finite retention policy; all retained appears only for manual retention',()=>{
 for(const days of [30,60,90,180,365]) {
  const options=utils.trafficRangeOptions(days);
  assert.deepEqual(options.map(o=>o.value),['today','7d',...[30,60,90,180,365].filter(n=>n<=days).map(n=>`${n}d`)]);
  assert.ok(!options.some(o=>o.value==='all'));
 }
 assert.equal(utils.trafficRangeOptions(0).at(-1).value,'all');
 assert.deepEqual(utils.trafficRangeOptions(NaN),utils.trafficRangeOptions(30));
 assert.deepEqual(utils.trafficRangeOptions(999),utils.trafficRangeOptions(30));
});

test('daily/weekly series use the reported width, split gaps, preserve quiet zeros and show year for long history',()=>{
 const from='2025-11-23T00:00:00Z',to='2026-11-23T00:00:00Z';
 for(const seconds of [86400,604800,1814400]) {
  const at=n=>new Date(Date.parse(from)+n*seconds*1000).toISOString();
  const b=(n,value,collection='enabled')=>({hour:at(n),metrics:value===null?null:{requests:value,page_views:value/2},collection,incomplete:false});
  const result=utils.trafficSeries([b(0,10),b(1,0),b(2,null,'unknown'),b(3,20,'partial'),b(4,30)],'requests',from,to,undefined,seconds);
  assert.equal(result.runs.length,3);assert.deepEqual(result.points.map(p=>p.value),[10,0,20,30]);
  assert.ok(Math.abs((result.points[1].x-result.points[0].x)-660*seconds*1000/(Date.parse(to)-Date.parse(from)))<1e-10);
  assert.equal(result.points[2].partial,true);
 }
 assert.match(utils.trafficDate(from,true),/2025/);assert.match(utils.trafficDate(to,true),/2026/);
});

test('actual chart announces daily/weekly widths instead of labelling aggregates as hourly values',async()=>{
 const chart=(await import(await loadVue(new URL('../src/components/TrafficHourlyChart.vue',import.meta.url)))).default;
 for(const [seconds,label]of [[3600,'Hourly'],[86400,'Daily'],[604800,'Weekly'],[1209600,'14-day']]) {
  const html=await renderToString(vue.createSSRApp(chart,{from:'2025-11-23T00:00:00Z',to:'2026-11-23T00:00:00Z',buckets:[],gaps:[],bucketSeconds:seconds}));
  assert.match(html,new RegExp(label+' requests and estimated page views, UTC'));
  assert.match(html,/2025/);assert.match(html,/2026/);
 }
});

const node=(type,text='')=>({type,text,children:[],props:{},style:{},parent:null,tagName:type.toUpperCase(),addEventListener(){},removeEventListener(){},get options(){return this.children}});
const renderer=vue.createRenderer({
 createElement:type=>node(type),createText:text=>node('#text',text),createComment:text=>node('#comment',text),
 setText:(n,text)=>n.text=text,setElementText:(n,text)=>{n.text=text;n.children=[]},parentNode:n=>n.parent,nextSibling:n=>n.parent?.children[n.parent.children.indexOf(n)+1]||null,
 patchProp:(n,key,_old,value)=>n.props[key]=value,
 insert(n,parent,anchor=null){if(n.parent)n.parent.children.splice(n.parent.children.indexOf(n),1);n.parent=parent;const i=anchor?parent.children.indexOf(anchor):-1;parent.children.splice(i<0?parent.children.length:i,0,n)},
 remove(n){n.parent?.children.splice(n.parent.children.indexOf(n),1);n.parent=null},
});
const nodes=(root,predicate)=>[...(predicate(root)?[root]:[]),...root.children.flatMap(n=>nodes(n,predicate))];
const text=root=>(root.type==='#comment'?'':root.text)+root.children.map(text).join('');
const settle=async()=>{await new Promise(r=>setImmediate(r));await vue.nextTick()};
const box={setup:(_,ctx)=>()=>vue.h('div',ctx.slots.default?.())};
let instance=0;
async function viewFixture(t,days=90) {
 const control={days,fail:false,failRange:null,reload:null,calls:[]};
 const metrics={requests:10,page_views:6,not_found:0,other_4xx:0,server_errors:0,response_bytes:100,avg_request_time_ms:10};
 const client={async getSiteTrafficAnalytics(_id,range){control.calls.push(range);if(control.fail || control.failRange===range)throw Error('History unavailable');return {range,retention_days:control.days,bucket_seconds:range==='90d'?86400:3600,available_from:'2026-09-01T00:00:00Z',from:'2026-08-26T00:00:00Z',to:'2026-11-23T12:00:00Z',site:{domain:'example.test',status:'disabled',enabled:false,effective_enabled:false,pending:false,backlog_bytes:0,dropped_bytes:0},master_enabled:false,totals:metrics,buckets:[],gaps:[],top_paths:[],referrers:[],notes:[],api:{buckets:[],gaps:[],endpoints:[],notes:[],totals:{},settings:{prefixes:['/api'],routes:[]}}}}};
 const bindings={'vue':vue,'vue-router':{useRoute:()=>({params:{id:'1'}})},'../../api/client':{apiClient:client},'../../utils/trafficAnalytics':utils,'../../composables/useConfirm':{useConfirm:()=>({confirm:async()=>false})},'../../composables/useTrafficPolling':{useTrafficPolling:load=>{control.reload=load;vue.onMounted(load);return{refresh:load}}}};
 const source=await readFile(new URL('../src/views/site/SiteObserveTraffic.vue',import.meta.url),'utf8');
 const {descriptor}=parse(source);let js=stripTypeScriptTypes(compileScript(descriptor,{id:'history-mounted',inlineTemplate:true}).content);
 const key=`trafficHistoryTest${++instance}`;globalThis[key]=bindings;
 js=js.replace(/import ([^;\n]+?) from ['"]([^'"]+)['"];?/g,(_line,names,path)=>{
  bindings[path]??=path.endsWith('ErrorAlert.vue')?{props:['message'],setup:p=>()=>vue.h('p',p.message)}:box;
  return `const ${names.startsWith('{')?names.replace(/\bas\b/g,':'):names}=globalThis.${key}[${JSON.stringify(path)}];`;
 });
 const view=(await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'))).default;
 const root=node('root');const app=renderer.createApp(view);app.component('router-link',box);app.mount(root);t.after(()=>{app.unmount();delete globalThis[key]});await settle();
 const select=()=>nodes(root,n=>n.type==='select')[0];
 return {control,root,select,setRange:async value=>{select().props['onUpdate:modelValue'](value);await settle()}};
}

test('mounted range menu adapts on polling, falls back after retention reduction and keeps failures visible',async t=>{
 const f=await viewFixture(t);const options=()=>nodes(f.select(),n=>n.type==='option').map(n=>n.props.value);
 assert.deepEqual(options(),['today','7d','30d','60d','90d']);
 await f.setRange('90d');assert.equal(f.control.calls.at(-1),'90d');assert.match(text(f.root),/Daily totals/);
 f.control.days=30;await f.control.reload();await settle();
 assert.deepEqual(options(),['today','7d','30d']);assert.equal(f.control.calls.at(-1),'30d');
 f.control.days=0;await f.control.reload();await settle();assert.equal(options().at(-1),'all');
 f.control.fail=true;await f.control.reload();await settle();assert.match(text(f.root),/History unavailable/);
 assert.match(text(f.root),/Earlier dates are unavailable/);
});

test('demo longer ranges are bounded, retain forward-only API rules and do not resurrect deleted history',async t=>{
 const old=Date.now;let now=Date.parse('2020-01-01T12:00:00Z');Date.now=()=>now;t.after(()=>Date.now=old);
 const source=await readFile(new URL('../../ui-site/src/api/mock.ts',import.meta.url),'utf8');
 const js=stripTypeScriptTypes(`const demoGitHubSiteAccess=${demoGitHubSiteAccess.toString()};const appVersion='test';const createRequestID=()=> 'demo';const trafficAPISettings=${utils.trafficAPISettings.toString()};\n`+source.replace(/^import .*\r?\n/gm,''));
 const {MockApiClient}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
 const client=new MockApiClient();const path='/api/v1/sites/1/traffic-analytics';
 client._handle('/api/v1/traffic-analytics','PUT',{retention_days:0});
 client._handle('/api/v1/traffic-analytics','PUT',{enabled:true});now+=2000;client._handle('/api/v1/traffic-analytics','GET');
 client._handle(path,'PUT',{enabled:true});now+=2000;client._handle('/api/v1/traffic-analytics','GET');
 now=Date.parse('2040-11-23T12:00:00Z');
 const report=client._handle(path+'?range=all','GET');
 assert.ok(report.buckets.length<=720);assert.ok(report.api.buckets.length<=720);assert.ok(report.bucket_seconds>=604800);assert.equal(report.retention_days,0);assert.ok(report.totals.requests>0);
 client._handle(path+'/api-settings','PUT',{prefixes:[],routes:[]});
 const before=client._handle(path+'?range=all','GET');assert.deepEqual(before.api.totals,report.api.totals);
 client._handle(path,'DELETE');const after=client._handle(path+'?range=all','GET');assert.equal(after.totals.requests,0);assert.ok(after.buckets.every(b=>b.metrics===null));
 assert.throws(()=>client._handle(path+'?range=50000000d','GET'),/supported traffic history range/);
});

test('retention reduction cannot leave an old report under a new range when the fallback fetch fails',async t=>{
 const f=await viewFixture(t);await f.setRange('90d');
 assert.match(text(f.root),/Daily totals/);
 f.control.days=30;f.control.failRange='30d';await f.control.reload();await settle();
 assert.equal(f.control.calls.at(-1),'30d');assert.match(text(f.root),/History unavailable/);
 assert.doesNotMatch(text(f.root),/Daily totals/);
});
