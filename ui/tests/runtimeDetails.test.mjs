import test from 'node:test';import assert from 'node:assert/strict';
import * as vue from 'vue';import {component,host,modal,box,settle} from './helpers/componentHarness.mjs';
import {loadVue} from './helpers/loadVue.mjs';
import {phpSummary,nodeSummary,summaryLabel,upstreamVersion} from '../src/utils/runtimeUpdateDetails.ts';
const branch=(changes)=>({version:'8.4',update_available:true,plan:{id:'plan',inventory:{changes}}});
const core=(installed,candidate)=>({name:'php8.4-fpm',installed,candidate,candidate_source:'php8.4',origins:[{site:'packages.example.test',origin:'Example repository',trusted:true}]});
test('exact upstream patch, Debian epoch/revision and extension-only changes remain distinct',()=>{
 const patch=phpSummary(branch([core('1:8.4.27-2+vendor','1:8.4.28-3+vendor')]));assert.match(patch.changes[0].source,/php8.4.*packages.example.test/);assert.equal(patch.before,'8.4.27');assert.equal(patch.target,'8.4.28');assert.equal(summaryLabel(patch),'PHP 8.4.28 update available');
 const revision=phpSummary(branch([core('8.4.28-1','8.4.28-2')]));assert.equal(revision.kind,'Package revision update');assert.equal(revision.target,null);assert.equal(revision.changes[0].candidate,'8.4.28-2');
 const extension=phpSummary(branch([{name:'php8.4-redis',installed:'6.1.0-1',candidate:'6.2.0-1',candidate_source:'php-redis'}]));assert.equal(extension.target,null);assert.equal(extension.kind,'Extension updates');
 assert.equal(upstreamVersion('8.4.28~rc1-1'),null);assert.equal(upstreamVersion('invalid'),null);
 const mixed=phpSummary(branch([core('8.4.27-1','8.4.28-1'),{...core('8.4.27-1','8.4.29-1'),name:'php8.4-cli'}]));assert.equal(mixed.target,null);
});
test('Node runtime and bundled npm use real snapshot values including legacy migration',()=>{
 const s=nodeSummary({discovery_id:'fresh',candidate:{version:'22.23.5',npm:'10.9.9'},legacy_source:{version:'22.23.1',npm:'10.9.8'},migration_available:true});assert.equal(s.before,'22.23.1');assert.equal(s.after,'22.23.5');assert.equal(s.changes[1].installed,'10.9.8');assert.equal(s.changes[1].candidate,'10.9.9');assert.equal(s.kind,'Node.js migration');
 assert.equal(nodeSummary({candidate:null}).target,null);
});
async function fixture(t,summary){
 const open=vue.ref(false),summaries=vue.ref([summary]),calls=[],reviews=[],notes=vue.ref({state:'available',date:'2026-11-19',security:false,highlights:['<script>text is escaped</script>']});
 const button=(await import(await loadVue(new URL('../src/components/AppButton.vue',import.meta.url)))).default;
 const Details=await component('components/RuntimeUpdateDetailsModal.vue',{'@heroicons/vue/24/outline':{ArrowLongRightIcon:box},'../api/client':{apiClient:{get:async(path)=>{calls.push(path);return notes.value}}},'./BaseModal.vue':modal,'./AppButton.vue':button,'./AppTypeIcon.vue':box,'./DataTable.vue':{props:{scrollClass:String,stickyHeader:Boolean,items:Array},setup:p=>()=>vue.h('div',{'data-scroll':p.scrollClass,'data-sticky':p.stickyHeader},p.items.map(i=>vue.h('span',i.installed+' → '+i.candidate)))},'./StatusBadge.vue':{props:['label'],setup:p=>()=>vue.h('span',p.label)},'./FormGroup.vue':box});
 const h=host();const app=h.renderer.createApp({setup:()=>()=>vue.h(Details,{modelValue:open.value,summaries:summaries.value,'onUpdate:modelValue':v=>open.value=v,onReview:v=>reviews.push(v)})});app.mount(h.root);t.after(()=>app.unmount());await settle();
 return {...h,open,summaries,calls,reviews,notes};
}
test('opening details fetches optional notes only, escapes them and retains bounded exact package changes',async t=>{
 const f=await fixture(t,phpSummary(branch([core('8.4.27-1','8.4.28-3')])));assert.equal(f.calls.length,0);f.open.value=true;await settle();assert.deepEqual(f.calls,['/api/v1/server/runtime-release-notes?runtime=php&version=8.4.28']);
 assert.match(f.text(),/8.4.27.*8.4.28/);assert.match(f.text(),/Target-release highlights/);assert.doesNotMatch(f.text(),/Security fixes/);assert.equal(f.all().some(n=>n.type==='script'),false);
 f.button('Show package changes (1)').props.onClick();await settle();assert.ok(f.find(n=>n.props['data-scroll']==='max-h-56 overflow-y-auto'&&n.props['data-sticky']===true));assert.match(f.text(),/8.4.27-1 → 8.4.28-3/);assert.equal(f.calls.length,1);
 f.button('Review update').props.onClick();assert.equal(f.reviews[0].id,'plan');assert.equal(f.calls.length,1,'review action does not apply or refresh APT');
});
test('revision-only details do not fetch irrelevant upstream notes; unknown notes never become security claims',async t=>{
 const f=await fixture(t,phpSummary(branch([core('8.4.28-1','8.4.28-2')])));f.open.value=true;await settle();assert.equal(f.calls.length,0);assert.match(f.text(),/Package revision update/);assert.match(f.text(),/upstream version is unchanged/);
 f.notes.value={state:'unavailable',security:false,highlights:[]};f.summaries.value=[phpSummary(branch([core('8.4.27-1','8.4.28-2')]))];await settle();assert.match(f.text(),/Release highlights are unavailable/);assert.doesNotMatch(f.text(),/Security fixes/);
});

test('Node card uses standard header actions and distinguishes fresh/current, available, stale and pending recovery',async t=>{
 const state=vue.ref({snapshot:{state:'current',checked_at:1001,update_available:false,installed:{release:{version:'24.19.0'}}},recoveries:[],active_operation:null});
 const updates={state,attention:vue.ref(null),busy:vue.ref(false),locked:vue.ref(false),repairReviewRequest:vue.ref(null),updateReviewRequest:vue.ref(null),refresh:async()=>{},check:async()=>{},review:async()=>{},finishUpdateReview(){},finishRepairReview(){}};
 const button=(await import(await loadVue(new URL('../src/components/AppButton.vue',import.meta.url)))).default;
 const badge=(await import(await loadVue(new URL('../src/components/StatusBadge.vue',import.meta.url)))).default;
 const panel=await component('components/NodeUpdatePanel.vue',{'../composables/useNodeUpdates':{useNodeUpdates:()=>updates,nodeUpdateError:String},'../utils/requestID':{createRequestID:()=> 'id'},'./Card.vue':box,'./AppButton.vue':button,'./BaseModal.vue':modal,'./ErrorAlert.vue':box,'./StatusBadge.vue':badge,'./RuntimeUpdateDetailsModal.vue':box});
 const h=host();const app=h.renderer.createApp(panel);app.mount(h.root);t.after(()=>app.unmount());await settle();
 assert.match(h.text(),/24.19.0Up to date/);assert.equal(h.button('Update').props.disabled,true);assert.match(h.button('Update').props.class,/bg-blue-600/);assert.match(h.button('Update').props.class,/px-4 py-2 text-sm/);assert.match(h.button('Check for updates').props.class,/px-4 py-2 text-sm/);assert.doesNotMatch(h.text(),/Maintain the installed|Checks run daily|No Node.js updates available/);
 state.value.snapshot={...state.value.snapshot,state:'available',candidate:{version:'24.20.0',npm:'11.20.0'},update_available:true};await settle();assert.match(h.text(),/Installed24.19.0.*Available24.20.0/);assert.ok(h.button('Show details'));assert.equal(h.button('Update').props.disabled,false);
 for(const status of ['stale','check_failed','unchecked']){state.value.snapshot.state=status;await settle();assert.doesNotMatch(h.text(),/Up to date/);assert.equal(h.button('Update').props.disabled,true);}
 state.value.snapshot.state='current';state.value.snapshot.update_available=false;state.value.recoveries=[{id:'past',state:'succeeded',plan:{after:{version:'24.19.0'},before:{release:{version:'24.18.0'}}}}];await settle();assert.match(h.text(),/Previous version24.18.0/);assert.ok(h.button('Review rollback'));assert.doesNotMatch(h.text(),/previous release remains/);
 state.value.recoveries=[{id:'pending',state:'recovery_required'}];updates.locked.value=true;await settle();assert.ok(h.button('Review repair'));assert.doesNotMatch(h.text(),/Previous version|Up to date/);
});

test('multiple PHP branches select their displayed branch and retain that choice when discovery IDs refresh',async t=>{
 const first=phpSummary(branch([core('8.4.27-1','8.4.28-1')]));
 const second=phpSummary({version:'8.5',plan:{id:'plan85',inventory:{changes:[{name:'php8.5-fpm',candidate_source:'php8.5',installed:'8.5.1-1',candidate:'8.5.2-1'}]}}});
 const f=await fixture(t,first);f.summaries.value=[first,second];f.open.value=true;await settle();
 const select=f.find(n=>n.type==='select');assert.equal(select.selectedIndex,0,'initial selector matches the displayed first branch');
 select.props['onUpdate:modelValue'](select.options[1].value);await settle();assert.match(f.text(),/8.5.1.*8.5.2/);
 f.summaries.value=[first,{...second,id:'fresh-plan85'}];await settle();assert.match(f.text(),/8.5.1.*8.5.2/);assert.equal(f.calls.length,2,'status polling does not fetch notes again for the same target');
 f.button('Review update').props.onClick();assert.equal(f.reviews[0].id,'fresh-plan85');assert.equal(f.reviews[0].branch,'8.5');
});
