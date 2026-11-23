import test from 'node:test';
import assert from 'node:assert/strict';
import { createSSRApp, createRenderer, h } from 'vue';
import { renderToString } from '@vue/server-renderer';
import { loadVue } from './helpers/loadVue.mjs';
import { githubAccessNotice, githubSiteAccessPresentation, githubTokenAction, githubAccessNeedsTokenUpdate } from '../src/utils/githubConnection.ts';
import { demoGitHubSiteAccess } from '../../ui-site/src/api/githubAccessDemo.ts';
const now = Date.parse('2026-11-20T12:00:00Z');
const account = { id:1, github_user_id:123, name:'Work GitHub', username:'example', token_status:'valid', token_expires_at:null, linked_sites:4,
  deployment_access:{checked_at:'2026-11-20T12:00:00Z', sites: demoGitHubSiteAccess([1,2,3,4].map(id=>({id,domain:`site-${id}.example`,repository:`example/repo-${id}`})))}};
const { default: Modal } = await import(await loadVue(new URL('../src/components/GitHubAccessModal.vue', import.meta.url)));
const { default: AccountCard } = await import(await loadVue(new URL('../src/components/SourceControlAccountCard.vue', import.meta.url)));

test('compact account actions distinguish known validity, expiry, rejection and unavailable checks', () => {
  assert.equal(githubTokenAction(account, now), 'Update token');
  for(const a of [{...account,token_status:'invalid'},{...account,token_expires_at:'2026-11-19T12:00:00Z'}]) {
    assert.equal(githubTokenAction(a,now),'Reconnect');
  }
  const unavailable={...account,token_status:'error'};
  assert.match(githubAccessNotice(unavailable,now),/validation is unavailable/);
  assert.doesNotMatch(githubAccessNotice(unavailable,now),/Token expired|Token rejected/);
});
test('site labels require structured evidence and never diagnose generic SSH failures as registration or connectivity faults', () => {
  const base={site_id:1,domain:'app',repository:'org/app',status:'error',message:'registration missing'};
  assert.equal(githubSiteAccessPresentation(base).label,'Needs attention');
  const ssh=githubSiteAccessPresentation({...base,reason:'ssh_access_failed'});
  assert.doesNotMatch(ssh.message,/registration missing|connectivity fault/);
  assert.match(ssh.message,/deploy key, repository access or connectivity/);
  assert.match(githubSiteAccessPresentation({...base,reason:'private_key_unavailable'}).message,/Reconnect cannot restore it/);
  assert.equal(githubSiteAccessPresentation({...base,reason:'check_timeout'}).label,'Check timed out');
  assert.equal(githubSiteAccessPresentation({...base,reason:'check_canceled'}).label,'Check canceled');
});
test('actual shared modal renders compact mixed results and the correct primary action', async () => {
  const html=await renderToString(createSSRApp(Modal,{modelValue:true,account,now,busy:false,checking:false}));
  for(const text of ['Update token','Retry check','Private key unavailable','Reconnect cannot restore it','Check timed out','Verified','1 of 4 sites verified']) assert.ok(html.includes(text),text);
  assert.equal(html.includes('Update token &amp; repair'),false);
  for(const token_status of ['invalid','error','unchecked']) {
    const html=await renderToString(createSSRApp(Modal,{modelValue:true,account:{...account,token_status},now,busy:false,checking:false}));
    assert.equal(html.includes('>Reconnect<'),token_status==='invalid');
    assert.equal(html.includes('Update token &amp; repair'),false);
  }
});
test('verified access keeps ordinary token management on the card without a repair prompt in the report', async () => {
  const healthy={...account,linked_sites:2,deployment_access:{checked_at:'2026-11-20T12:00:00Z',sites:[1,2].map(site_id=>({
    site_id,domain:`site-${site_id}.example`,repository:`example/repo-${site_id}`,status:'verified',message:'SSH repository access verified.',
  }))}};
  const props={account:healthy,now,busy:false,checking:false};
  const card=await renderToString(createSSRApp(AccountCard,props));
  assert.match(card,/2 sites verified/);
  assert.match(card,/>Update token</);
  assert.doesNotMatch(card,/repair/i);
  const modal=await renderToString(createSSRApp(Modal,{...props,modelValue:true}));
  assert.match(modal,/Token valid\. Deployment access verified\./);
  assert.match(modal,/>Retry check</);
  assert.match(modal,/>Close</);
  assert.doesNotMatch(modal,/Update token|repair|deploy-key permissions/);
});
test('repair wording requires a registration failure; retry-only and server-recovery states do not request a new token', async () => {
  for(const reason of ['registration_failed','ssh_access_failed','private_key_unavailable','check_timeout','check_canceled','site_changed','check_failed',undefined]) {
    const report={...account,linked_sites:1,deployment_access:{checked_at:'2026-11-20T12:00:00Z',sites:[{
      site_id:1,domain:'app.example',repository:'example/app',status:'error',reason,message:'Access needs attention.',
    }]}};
    const html=await renderToString(createSSRApp(Modal,{modelValue:true,account:report,now,busy:false,checking:false}));
    assert.equal(html.includes('Update token &amp; repair'),reason==='registration_failed',String(reason));
    assert.equal(html.includes('>Update token<'),reason==='ssh_access_failed',String(reason));
    assert.match(html,/>Retry check</);
  }
  for(const deployment_access of [
    {checked_at:null,sites:[]},
    {checked_at:null,sites:[{status:'error',reason:'registration_failed'}]},
    {checked_at:'2026-11-20T12:00:00Z',sites:[]},
    {checked_at:'2026-11-20T12:00:00Z',sites:[{status:'verified',reason:'registration_failed'}]},
  ]) {
    const unchecked={...account,deployment_access};
    assert.equal(githubAccessNeedsTokenUpdate(unchecked,now),false);
    assert.equal(githubTokenAction(unchecked,now),'Update token');
  }
  const expired={...account,token_expires_at:'2026-11-19T12:00:00Z'};
  const html=await renderToString(createSSRApp(Modal,{modelValue:true,account:expired,now,busy:false,checking:false}));
  assert.match(html,/>Reconnect</);
  assert.doesNotMatch(html,/Update token|repair/);
});
test('modal Retry and Update are distinct events; all footer actions are blocked while busy', () => {
  const previousWindow=globalThis.window, previousDocument=globalThis.document;
  globalThis.window={addEventListener(){},removeEventListener(){}};
  globalThis.document={body:{style:{}}};
  const node=(tag,text='')=>({tag,text,children:[],props:{},parent:null});
  const renderer=createRenderer({createElement:node,createText:text=>node('#text',text),createComment:text=>node('#comment',text),
    insert(child,parent){child.parent=parent;parent.children.push(child)},remove(){},setText(n,t){n.text=t},setElementText(n,t){n.text=t},
    parentNode:n=>n.parent,nextSibling:()=>null,patchProp(n,k,o,v){n.props[k]=v}});
  const text=n=>n.text+n.children.map(text).join('');
  const buttons=n=>[...(n.tag==='button'?[n]:[]),...n.children.flatMap(buttons)];
  try {
    for(const busy of [false,true]) {
      const events=[];const root=node('root');
      const app=renderer.createApp({render:()=>h(Modal,{modelValue:true,account,now,busy,checking:busy,onRetry:()=>events.push('retry'),onUpdate:()=>events.push('update'),'onUpdate:modelValue':v=>events.push(v)})});
      app.mount(root);
      for(const label of ['Retry check','Update token','Close']) {
        const button=buttons(root).find(b=>text(b).trim()===label);assert.ok(button,label);
        assert.equal(!!button.props.disabled,busy);
        if(!button.props.disabled) button.props.onClick({});
      }
      assert.deepEqual(events,busy?[]:['retry','update',false]);
      app.unmount();
    }
  } finally {globalThis.window=previousWindow;globalThis.document=previousDocument}
});
test('demo retry keeps failures and token repair cannot restore unavailable private keys', () => {
  const sites=[1,2,3,4].map(id=>({id,domain:`site-${id}.example`,repository:`example/repo-${id}`}));
  const checked=demoGitHubSiteAccess(sites);const repaired=demoGitHubSiteAccess(sites,true);
  assert.equal(checked[0].status,'error');assert.equal(repaired[0].status,'verified');
  assert.equal(repaired[1].reason,'private_key_unavailable');assert.equal(repaired[1].status,'error');
  assert.deepEqual(checked[2],repaired[2]);
});
