import test from 'node:test';import assert from 'node:assert/strict';
import * as vue from 'vue';import {createRouter,createMemoryHistory,onBeforeRouteUpdate,RouterView,useRoute} from 'vue-router';
import {component,host,box,settle} from './helpers/componentHarness.mjs';
import {siteSummary,switchedSitePath} from '../src/utils/siteSwitcher.ts';
const sites=[{id:1,domain:'laravel.test',app_type:'laravel',php_version:'8.4',repository:'org/api',branch:'main'},{id:2,domain:'wordpress.test',app_type:'wordpress',php_version:'8.3'}];
test('site metadata includes framework/PHP/repository/branch and switching strips resource-specific state',()=>{
 assert.equal(siteSummary(sites[0]),'Laravel (PHP 8.4) · org/api:main');assert.match(siteSummary(sites[1]),/WordPress \(PHP 8.3\)/);
 assert.equal(switchedSitePath('/sites/1/domains/ssl',sites[1]),'/sites/2/domains/ssl');
 assert.equal(switchedSitePath('/sites/1/commands/tinker',sites[1]),'/sites/2/overview');
 assert.equal(switchedSitePath('/sites/2/settings/wordpress',sites[0]),'/sites/1/overview');
 assert.equal(switchedSitePath('/sites/1/deployments/123',sites[1]),'/sites/2/overview');
});
test('searchable switcher uses cached list API, exposes current row and respects route guards',async t=>{
 const oldWindow=globalThis.window,oldDocument=globalThis.document;const listeners={};
 globalThis.window={innerWidth:390,innerHeight:560,addEventListener(){},removeEventListener(){}};
 globalThis.document={addEventListener:(n,f)=>listeners[n]=f,removeEventListener(){},getElementById:()=>null};
 let app;t.after(()=>{app?.unmount();globalThis.window=oldWindow;globalThis.document=oldDocument});
 let reads=0,allow=false;const icons={CheckIcon:box,ChevronDownIcon:box,MagnifyingGlassIcon:box};
 const Switcher=await component('components/SiteSwitcher.vue',{'@heroicons/vue/24/outline':icons,'./AppTypeIcon.vue':box,'../api/client':{apiClient:{getSites:async()=>{reads++;return sites}}}});
 const page={setup(){onBeforeRouteUpdate(()=>allow);return()=>vue.h(Switcher,{site:sites[0]})}};
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/sites/:id/domains/ssl',component:page}]});await router.push('/sites/1/domains/ssl?certificate=123#private');
 const h=host();app=h.renderer.createApp({setup:()=>()=>vue.h(RouterView)});app.use(router);app.mount(h.root);
 const trigger=()=>h.find(n=>n.props['aria-haspopup']==='listbox');await trigger().props.onClick();await settle();
 assert.equal(reads,1);assert.equal(h.all().filter(n=>n.props.role==='option').length,2);
 assert.ok(h.find(n=>n.props.role==='option'&&n.props['aria-selected']===true));
 const input=h.find(n=>n.type==='input');input.props['onUpdate:modelValue']('PHP 8.3');await settle();
 assert.match(h.text(),/wordpress.test/);assert.doesNotMatch(h.text(),/org\/api:main/);
 assert.equal(h.find(n=>n.props.role==='option').props.title,'wordpress.test\nWordPress (PHP 8.3)');
 await h.find(n=>n.props.role==='option').props.onClick();await settle();assert.equal(router.currentRoute.value.params.id,'1','cancelled unsaved guard retains current site');
 allow=true;await trigger().props.onClick();await settle();
 const options=h.all().filter(n=>n.props.role==='option');await options[1].props.onClick();await settle();
 assert.equal(router.currentRoute.value.fullPath,'/sites/2/domains/ssl','query/hash and old resource IDs are not carried over');
});

test('actual dashboard keeps section caches distinct and remounts only when changing sites',async t=>{
 const activeSite=vue.ref(null);let mounted=0;
 const store={activeSite,fetchSite:async id=>{activeSite.value={id,domain:`site${id}.test`,app_type:'php'}}};
 const deployments={deploying:vue.ref(false),latestStatus:vue.ref(''),deploySignal:vue.ref(0),unresolvedFailure:vue.ref(null)};
 const Dashboard=await component('views/SiteDashboard.vue',{'pinia':{storeToRefs:s=>s},'../stores/site':{useSiteStore:()=>store},'../stores/deployments':{useDeploymentsStore:()=>deployments},'../composables/useConfirm':{useConfirm:()=>({confirm:async()=>true})},'../api/client':{apiClient:{getSiteFeatures:async()=>({})}},'../components/AppButton.vue':box,'../components/PersistentFailureBanner.vue':box,'../components/SiteSwitcher.vue':box});
 const child=label=>({setup(){const site=useRoute().params.id;const instance=++mounted;return()=>vue.h('p',`${label}:${site}:${instance}`)}});
 const overview=child('overview'),settings=child('settings');
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/sites/:id',component:Dashboard,children:[{path:'overview',component:overview},{path:'settings',component:settings}]}]});
 await router.push('/sites/1/overview');const h=host();const app=h.renderer.createApp({setup:()=>()=>vue.h(RouterView)});app.use(router);app.mount(h.root);t.after(()=>app.unmount());await settle();assert.match(h.text(),/overview:1:1/);
 await router.push('/sites/1/settings');await settle();assert.match(h.text(),/settings:1:2/);
 await router.push('/sites/1/overview');await settle();assert.match(h.text(),/overview:1:1/);assert.equal(mounted,2,'ordinary tab visits reuse their own cached view');
 await router.push('/sites/2/overview');await settle();assert.match(h.text(),/overview:2:3/,'another site gets a fresh child view');
});
