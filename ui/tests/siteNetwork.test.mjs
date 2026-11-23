import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import * as vue from 'vue';
import { parse, compileScript } from '@vue/compiler-sfc';
import * as validation from '../src/types/siteNetwork.ts';
import { certificateHostnames, normalizeDomainInput } from '../src/types/domain.ts';
let sequence = 0;
async function load(source, bindings = {}) {
  const key = `networkTest${++sequence}`;
  globalThis[key] = { vue, ...bindings };
  const js = stripTypeScriptTypes(source).replace(/^import ([^;\n]+?) from ['"]([^'"]+)['"];?$/gm, (_, names, path) => {
    if (path.endsWith('/apiError')) return `import ${names} from ${JSON.stringify(new URL('../src/utils/apiError.ts', import.meta.url).href)};`;
    assert.ok(path in globalThis[key], path);
    return `const ${names.startsWith('{') ? names.replace(/\bas\b/g, ':') : names} = globalThis.${key}[${JSON.stringify(path)}];`;
  });
  return import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
}
async function component(path, bindings = {}) {
  const { descriptor } = parse(await readFile(new URL(`../src/${path}.vue`, import.meta.url), 'utf8'));
  return (await load(compileScript(descriptor, { id: path, inlineTemplate: true }).content, bindings)).default;
}
const state = () => ({ managed: true, revision: 'revision-1', pending: false, protection: { enabled: false, username: '' }, redirects: [{ id: 'r1', source: '/old', destination: '/new', status: 301 }], wildcards: [] });
const composable = await readFile(new URL('../src/composables/useSiteNetwork.ts', import.meta.url), 'utf8');
async function fixture() {
  let saved = state(), approved = true, pendingConfirmation;
  const calls = [], confirmations = [];
  const api = {
    getSiteNetwork: async () => structuredClone(saved),
    updateSiteProtection: async (id, data) => { calls.push(['protection', id, data]); saved.protection = { enabled: data.enabled, username: data.username || saved.protection.username }; saved.revision += '-next'; return structuredClone(saved); },
    saveSiteRedirect: async (id, redirectId, data) => { calls.push(['redirect', id, redirectId, data]); return structuredClone(saved); },
    deleteSiteRedirect: async (...args) => { calls.push(['delete', ...args]); return structuredClone(saved); },
    recoverSiteNetwork: async (...args) => { calls.push(['recover', ...args]); saved.pending = false; return structuredClone(saved); },
  };
  const mod = await load(composable, { '../api/client': { apiClient: api }, './useConfirm': { useConfirm: () => ({ confirm: async options => { confirmations.push(options); return approved === 'wait' ? new Promise(resolve => { pendingConfirmation = resolve; }) : approved; } }) }, '../types/siteNetwork': validation });
  let siteId = '1';
  const flow = mod.useSiteNetwork(() => siteId);
  await flow.refresh();
  return { flow, api, calls, confirmations, saved, approve: value => { approved = value; }, accept: () => pendingConfirmation?.(true), site: value => { siteId = value; } };
}
function host() {
  globalThis.Document ||= class {};
  globalThis.ShadowRoot ||= class {};
  const node = (type, text = '') => ({ type, tagName: type.toUpperCase(), text, children: [], props: {}, parent: null, listeners: {}, value: '', getRootNode: () => ({}), addEventListener(name, callback) { this.listeners[name] = callback; }, removeEventListener(name) { delete this.listeners[name]; } });
  const renderer = vue.createRenderer({ createElement: type => node(type), createText: text => node('#text', text), createComment: text => node('#comment', text), setText: (n, text) => n.text = text, setElementText: (n, text) => { n.text = text; n.children = []; }, parentNode: n => n?.parent, nextSibling: n => n?.parent?.children[n.parent.children.indexOf(n) + 1] || null, patchProp: (n, key, _, value) => { n.props[key] = value; if (key === 'value') n.value = value; }, insert: (n, parent, anchor = null) => { if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1); n.parent = parent; const at = anchor ? parent.children.indexOf(anchor) : -1; parent.children.splice(at < 0 ? parent.children.length : at, 0, n); }, remove: n => { if (n?.parent) n.parent.children = n.parent.children.filter(child => child !== n); } });
  const root = node('root');
  const all = n => [n, ...n.children.flatMap(all)];
  const text = n => (n.type === '#comment' ? '' : n.text) + n.children.map(text).join('');
  return { renderer, root, all: () => all(root), text: () => text(root) };
}

test('validation uses bcrypt byte boundaries and exact safe sources while allowing destination queries', () => {
  assert.equal(validation.protectionPasswordError('é'.repeat(36)), '');
  assert.ok(validation.protectionPasswordError('é'.repeat(37)));
  assert.ok(validation.protectionPasswordError('valid\npassword'));
  assert.equal(validation.protectionUsernameError('staging.user@team-1'), '');
  assert.ok(validation.protectionUsernameError('a b'));
  for (const source of ['/a?b=1', '/a%20b', '/a/../b', '//b', '/a$', '/a.*']) assert.ok(validation.redirectSourceError(source), source);
  assert.equal(validation.redirectSourceError('/pricing-v2/a_1~test+'), '');
  for (const destination of ['//evil.test', 'https://user:pass@example.com/', '/a$b', '/a\\b', '/a;b', '/{a}']) assert.ok(validation.redirectDestinationError(destination), destination);
  assert.equal(validation.redirectDestinationError('https://example.com/pricing?plan=team&from=old'), '');
});

test('enable form never loads a saved password, rejects invalid credentials and clears plaintext after submission', async () => {
  const f = await fixture();
  f.flow.openCredentials();
  assert.equal(f.flow.password.value, '');
  assert.equal(f.flow.showPassword.value, true);
  f.flow.username.value = 'staging'; f.flow.password.value = 'short';
  await f.flow.saveCredentials(); assert.equal(f.calls.length, 0);
  f.flow.password.value = 'a-secure-password'; await f.flow.saveCredentials();
  assert.equal(f.calls[0][2].expected_revision, 'revision-1');
  assert.equal(f.calls[0][2].password, 'a-secure-password');
  assert.equal(f.flow.password.value, ''); assert.equal(f.flow.showPassword.value, false);
  f.flow.openCredentials(); assert.equal(f.flow.password.value, '');
});

test('custom and pending states disable controls, retain saved redirect rows, and recovery requires explicit confirmation', async () => {
  const f = await fixture(); f.saved.managed = false; f.saved.protection.enabled = true; await f.flow.refresh();
  assert.equal(f.flow.editable.value, false); assert.equal(f.flow.protectionEnabled.value, false);
  f.flow.openCredentials(); f.flow.openRedirect(); await f.flow.toggleProtection(false);
  assert.equal(f.flow.showPassword.value, false); assert.equal(f.flow.showRedirect.value, false); assert.equal(f.calls.length, 0);
  assert.equal(f.flow.state.value.redirects[0].source, '/old');
  f.saved.managed = true; f.saved.pending = true; await f.flow.refresh();
  f.approve(false); await f.flow.recover(); assert.equal(f.calls.length, 0); assert.equal(f.flow.editable.value, false);
  f.approve(true); await f.flow.recover(); assert.deepEqual(f.calls[0], ['recover', '1', 'revision-1']); assert.equal(f.flow.editable.value, true);
});

test('disable/delete confirmations cancel mutations and duplicate clicks share one pending confirmation', async () => {
  const f = await fixture(); f.saved.protection.enabled = true; await f.flow.refresh();
  f.approve(false); await f.flow.toggleProtection(false); await f.flow.deleteRedirect(f.saved.redirects[0]); assert.equal(f.calls.length, 0);
  f.approve('wait'); const pending = f.flow.toggleProtection(false); await f.flow.toggleProtection(false);
  assert.equal(f.confirmations.length, 3); f.accept(); await pending; assert.equal(f.calls.length, 1);
  assert.equal(f.confirmations[0].variant, 'danger');
});

test('failed ambiguous mutations are observed once and never silently retried or reported successful', async () => {
  const f = await fixture(); let attempts = 0;
  f.api.saveSiteRedirect = async () => { attempts++; f.saved.pending = true; throw Error('Change interrupted'); };
  f.flow.openRedirect(); f.flow.source.value = '/old'; f.flow.destination.value = '/new'; await f.flow.saveRedirect();
  assert.equal(attempts, 1); assert.equal(f.flow.state.value.pending, true); assert.equal(f.flow.showRedirect.value, true); assert.match(f.flow.error.value, /interrupted/);
  await f.flow.saveRedirect(); assert.equal(attempts, 1);
  f.api.getSiteNetwork = async () => { throw Error('offline'); }; await f.flow.refresh(); assert.equal(f.flow.editable.value, false);
});

test('changing sites while confirmation is open prevents a stale mutation', async () => {
  const f = await fixture(); f.approve('wait'); const pending = f.flow.deleteRedirect(f.saved.redirects[0]);
  f.site('2'); f.flow.reset(); await f.flow.refresh(); f.accept(); await pending; assert.equal(f.calls.length, 0);
});

test('actual PasswordInput masks, shows, generates and remasks cleared drafts', async () => {
  const Password = await component('components/PasswordInput'); const h = host(); const draft = vue.ref('new-password');
  const app = h.renderer.createApp({ setup: () => () => vue.h(Password, { modelValue: draft.value, 'onUpdate:modelValue': value => draft.value = value, maxlength: 72 }) }); app.mount(h.root);
  const input = () => h.all().find(n => n.type === 'input'); const toggle = () => h.all().find(n => n.props['aria-label'] === 'Show password' || n.props['aria-label'] === 'Hide password');
  assert.equal(input().props.type, 'password'); toggle().props.onClick(); await vue.nextTick(); assert.equal(input().props.type, 'text');
  const generate = h.all().find(n => n.type === 'button' && n.text.trim() === 'Generate'); generate.props.onClick(); await vue.nextTick();
  assert.equal(draft.value.length, 20); assert.notEqual(draft.value, 'new-password'); assert.equal(input().props.type, 'text');
  draft.value = ''; await vue.nextTick(); assert.equal(input().props.type, 'password');
  app.unmount();
});

test('actual Network view connects the switch and validated credential form, and displays read-only custom/pending states', async () => {
  const f = await fixture();
  const Password = await component('components/PasswordInput');
  const Toggle = await component('components/ToggleSwitch');
  const shell = { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) };
  const Button = { props: ['disabled'], setup: (props, { slots, attrs }) => () => vue.h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) };
  const Modal = { props: ['modelValue', 'confirmDisabled', 'confirmText'], setup: (props, { slots, emit }) => () => props.modelValue ? vue.h('section', [slots.default?.(), vue.h('button', { disabled: props.confirmDisabled, onClick: () => emit('submit') }, props.confirmText)]) : null };
  const table = { props: ['items'], setup: props => () => vue.h('div', props.items.map(item => item.source)) };
  const badge = { props: ['label'], setup: props => () => vue.h('span', props.label) };
  const notice = { props: ['message'], setup: (props, { slots }) => () => vue.h('aside', [props.message, slots.default?.(), slots.actions?.()]) };
  const bindings = {
    'vue-router': { useRoute: () => ({ params: { id: '1' } }) },
    '../../components/AppButton.vue': Button, '../../components/BaseModal.vue': Modal, '../../components/Card.vue': shell,
    '../../components/DataTable.vue': table, '../../components/ErrorAlert.vue': shell, '../../components/FormGroup.vue': shell,
    '../../components/PasswordInput.vue': Password, '../../components/StatusBadge.vue': badge, '../../components/StatusNotice.vue': notice,
    '../../components/TableActionMenu.vue': shell, '../../components/ToggleSwitch.vue': Toggle,
    '../../composables/useSiteNetwork': { useSiteNetwork: () => f.flow },
  };
  const View = await component('views/site/SiteNetwork', bindings); const h = host(); const app = h.renderer.createApp(View);
  app.component('router-link', { setup: (_, { slots }) => () => vue.h('a', slots.default?.()) }); app.mount(h.root); await vue.nextTick(); await vue.nextTick();
  h.all().find(n => n.props.role === 'switch').props.onClick(); await vue.nextTick();
  const submit = () => h.all().find(n => n.type === 'button' && n.text === 'Enable protection');
  assert.equal(submit().props.disabled, true); f.flow.username.value = 'reviewer'; f.flow.password.value = 'valid-password'; await vue.nextTick(); assert.equal(submit().props.disabled, false);
  submit().props.onClick(); await vue.nextTick(); await vue.nextTick(); assert.equal(f.calls[0][0], 'protection'); assert.equal(f.flow.password.value, '');
  f.saved.managed = false; await f.flow.refresh(); await vue.nextTick(); assert.match(h.text(), /Custom vhost is active/); assert.match(h.text(), /Saved redirects/); assert.match(h.text(), /\/old/);
  assert.equal(h.all().find(n => n.props.role === 'switch').props.disabled, true);
  f.saved.pending = true; await f.flow.refresh(); await vue.nextTick(); assert.match(h.text(), /network change was interrupted/); assert.match(h.text(), /Restore configuration/);
  app.unmount();
});

test('actual compact domain modal shows wildcard coverage, forces custom toggle off, and emits chosen settings', async () => {
  const Base = { props: ['modelValue'], setup: (props, { slots, emit }) => () => props.modelValue ? vue.h('div', [slots.default?.(), vue.h('button', { onClick: () => emit('submit') }, 'Save')]) : null };
  const Toggle = await component('components/ToggleSwitch');
  const Modal = await component('components/DomainConfigurationModal', { '../types/domain': { classifyWWWDomain: async () => ({ isSubdomain: false }) }, './BaseModal.vue': Base, './ToggleSwitch.vue': Toggle });
  const h = host(); const managed = vue.ref(true); let saved;
  const app = h.renderer.createApp({ setup: () => () => vue.h(Modal, { modelValue: true, domain: 'example.com', behavior: 'from_www', wildcard: true, managed: managed.value, showWildcard: true, sslTo: '/sites/1/domains/ssl', onSave: (...args) => saved = args }) }); app.component('router-link', { setup: (_, { slots }) => () => vue.h('a', slots.default?.()) }); app.mount(h.root); await vue.nextTick();
  assert.match(h.text(), /WWW redirects/); assert.match(h.text(), /example.com · \*\.example.com/); assert.doesNotMatch(h.text(), /certificate selector|will be used to access/);
  h.all().find(n => n.type === 'button' && n.text === 'Save').props.onClick(); assert.deepEqual(saved, ['from_www', true]);
  managed.value = false; await vue.nextTick(); await vue.nextTick();
  const toggle = h.all().find(n => n.props.role === 'switch'); assert.equal(toggle.props.disabled, true); assert.equal(toggle.props['aria-checked'], false); assert.match(h.text(), /Restore the Fluxo default/);
  app.unmount();
});

test('wildcard input normalizes before save and combined certificate scope retains apex, WWW and wildcard', () => {
  assert.deepEqual(normalizeDomainInput(' *.Example.COM '), { domain: 'example.com', wildcard: true });
  assert.deepEqual(certificateHostnames({ domain: 'example.com', www_redirect: 'from_www', wildcard: true }), ['example.com', 'www.example.com', '*.example.com']);
  assert.deepEqual(certificateHostnames({ domain: 'example.com', www_redirect: 'none', wildcard: true }), ['example.com', '*.example.com']);
});

async function demo() {
  const source = await readFile(new URL('../../ui-site/src/api/mock.ts', import.meta.url), 'utf8');
  const { demoGitHubSiteAccess } = await import('../../ui-site/src/api/githubAccessDemo.ts');
  const { trafficAPISettings } = await import('../src/utils/trafficAnalytics.ts');
  const bindings = Object.entries({ ...validation, certificateHostnames, demoGitHubSiteAccess, trafficAPISettings }).map(([name, fn]) => `const ${name}=${fn.toString()};`).join('\n');
  const raw = stripTypeScriptTypes(bindings + "\nconst appVersion='demo';const createRequestID=()=> 'demo-id';\n" + source.replace(/^import .*\r?\n/gm, ''));
  return import('data:text/javascript;base64,' + Buffer.from(raw).toString('base64'));
}

test('actual demo URL handlers enforce revision, password secrecy, redirect CRUD and managed-vhost conflict', async () => {
  const { MockApiClient } = await demo(); const api = new MockApiClient(); let current = await api.get('/api/v1/sites/1/network');
  current = await api.put('/api/v1/sites/1/network/protection', { expected_revision: current.revision, enabled: true, username: 'staging', password: 'a-secure-password' });
  assert.equal(current.protection.enabled, true); assert.doesNotMatch(JSON.stringify(current), /a-secure-password|password_hash/);
  await assert.rejects(api.post('/api/v1/sites/1/network/redirects', { expected_revision: 'stale', source: '/old', destination: '/new', status: 301 }), /changed/);
  current = await api.post('/api/v1/sites/1/network/redirects', { expected_revision: current.revision, source: '/old', destination: '/new?q=1', status: 302 });
  const rule = current.redirects[0]; assert.equal(rule.status, 302);
  await assert.rejects(api.put('/api/v1/sites/1/vhost', { config: 'server {}' }), /409/);
  current = await api.delete(`/api/v1/sites/1/network/redirects/${rule.id}`, { expected_revision: current.revision }); assert.equal(current.redirects.length, 0);
  current = await api.put('/api/v1/sites/1/network/protection', { expected_revision: current.revision, enabled: false });
  await api.put('/api/v1/sites/1/vhost', { config: 'server {}' }); assert.equal((await api.get('/api/v1/sites/1/network')).managed, false);
  await assert.rejects(api.post('/api/v1/sites/1/network/redirects', { expected_revision: (await api.get('/api/v1/sites/1/network')).revision, source: '/a', destination: '/b', status: 301 }), /Restore/);
  await api.post('/api/v1/sites/1/vhost/restore', {});
});

test('demo domain wildcard changes retain managed status and ordinary certificate keeps wildcard pending', async () => {
  const { MockApiClient } = await demo(); const api = new MockApiClient();
  await api.put('/api/v1/sites/1/domains/0', { expected_revision: (await api.get('/api/v1/sites/1/domains'))[0].revision, wildcard: true, www_redirect: 'from_www' });
  let domain = (await api.get('/api/v1/sites/1/domains'))[0]; assert.equal(domain.wildcard_ssl_state, 'pending'); assert.equal(domain.ssl_active, true);
  assert.deepEqual((await api.get('/api/v1/sites/1/network')).wildcards, ['myapp.com']); assert.equal((await api.get('/api/v1/sites/1/network')).managed, true);
  await api.post('/api/v1/sites/1/ssl/letsencrypt', {}); domain = (await api.get('/api/v1/sites/1/domains'))[0]; assert.equal(domain.wildcard_ssl_state, 'pending');
  await api.put('/api/v1/sites/1/domains/0', { www_redirect: 'none' }); assert.equal((await api.get('/api/v1/sites/1/domains'))[0].wildcard, true);
  // A wildcard-only source cannot be invented to cover an unrelated target.
  assert.deepEqual(await api.get('/api/v1/sites/2/ssl/cloneable?domain_id=0'), []);
  await assert.rejects(api.post('/api/v1/sites/2/domains', { domain: 'myapp.com', www_redirect: 'from_www', wildcard: true }), /already attached/);
  const alias = await api.post('/api/v1/sites/2/domains', { domain: 'shop.myapp.com', www_redirect: 'none', wildcard: false });
  const candidates = await api.get(`/api/v1/sites/2/ssl/cloneable?domain_id=${alias.id}`); assert.equal(candidates[0].provider, 'custom'); assert.ok(candidates[0].domains.includes('*.myapp.com'));
});

test('actual API client reads fresh network state and sends revisioned JSON for every mutation including DELETE', async t => {
  const source = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8');
  const isolated = stripTypeScriptTypes("const router={push(){}};const useToast=()=>({addToast(){}});const useAuthStore=()=>({token:'network-token'});const clearLogViewSnapshots=()=>{};\n" + source.replace(/^import .*;\r?\n/gm, line => line.includes('utils/apiError') ? line.replace(/['"][^'"]+['"]/, JSON.stringify(new URL('../src/utils/apiError.ts', import.meta.url).href)) : ''));
  const { apiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const originalFetch = globalThis.fetch; t.after(() => globalThis.fetch = originalFetch);
  const calls = [];
  globalThis.fetch = async (url, options) => { calls.push([url, options]); return new Response(JSON.stringify(state())); };
  await apiClient.getSiteNetwork(1); await apiClient.getSiteNetwork(1); assert.equal(calls.length, 2); assert.equal(calls[0][1].cache, 'no-store'); assert.equal(calls[0][1].headers.Authorization, 'Bearer network-token');
  await apiClient.updateSiteProtection(1, { expected_revision: 'r2', enabled: false }); assert.equal(calls.at(-1)[1].method, 'PUT');
  await apiClient.saveSiteRedirect(1, null, { expected_revision: 'r3', source: '/a', destination: '/b', status: 301 }); assert.equal(calls.at(-1)[1].method, 'POST');
  await apiClient.saveSiteRedirect(1, 'redirect-id', { expected_revision: 'r4', source: '/a', destination: '/c', status: 302 }); assert.equal(calls.at(-1)[0], '/api/v1/sites/1/network/redirects/redirect-id'); assert.equal(calls.at(-1)[1].method, 'PUT');
  await apiClient.deleteSiteRedirect(1, 'redirect-id', 'r5'); assert.equal(calls.at(-1)[1].method, 'DELETE'); assert.deepEqual(JSON.parse(calls.at(-1)[1].body), { expected_revision: 'r5' });
  await apiClient.recoverSiteNetwork(1, 'r6'); assert.equal(calls.at(-1)[0], '/api/v1/sites/1/network/recover'); assert.deepEqual(JSON.parse(calls.at(-1)[1].body), { expected_revision: 'r6' });
});

test('actual demo fetch interceptor admits only scoped simulated mutations and forwards DELETE revision bodies', async () => {
  const { MockApiClient } = await demo(); const source = await readFile(new URL('../../ui-site/src/main.ts', import.meta.url), 'utf8');
  let blocked = 0, escaped = 0;
  const environment = { window: { fetch: async () => { escaped++; throw Error('Network escaped interceptor'); } }, mockApi: new MockApiClient(), showDemoModal: () => blocked++ };
  const key = `interceptorTest${++sequence}`; globalThis[key] = environment;
  const isolated = stripTypeScriptTypes(`const {window,mockApi,showDemoModal}=globalThis.${key};\n` + source.slice(source.indexOf('const originalFetch ='), source.indexOf('// Dispatch:')));
  await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const request = (url, method = 'GET', body) => environment.window.fetch(url, { method, body: body === undefined ? undefined : JSON.stringify(body) });
  let current = await (await request('/api/v1/sites/1/network')).json();
  current = await (await request('/api/v1/sites/1/network/redirects', 'POST', { expected_revision: current.revision, source: '/before', destination: '/after', status: 301 })).json();
  const result = await request(`/api/v1/sites/1/network/redirects/${current.redirects[0].id}`, 'DELETE', { expected_revision: current.revision }); assert.equal(result.status, 200); assert.deepEqual((await result.json()).redirects, []);
  const domain = await request('/api/v1/sites/1/domains/0', 'PUT', { expected_revision: (await (await request('/api/v1/sites/1/domains')).json())[0].revision, wildcard: true }); assert.equal(domain.status, 200);
  const conflict = await request('/api/v1/sites/1/vhost', 'PUT', { config: 'server {}' }); assert.equal(conflict.status, 409); assert.match(await conflict.text(), /wildcard subdomains/);
  const system = await request('/api/v1/server/system/security-updates', 'PUT', { enabled: false }); assert.equal(system.status, 200); assert.equal((await system.json()).state, 'disabled');
  const discovery = await request('/api/v1/server/nginx/updates/check', 'POST', {}); assert.equal(discovery.status, 200); assert.equal((await discovery.json()).snapshot.state, 'update_available');
  const wrongNotice = await request('/api/v1/server/nginx/updates/notices/dismiss', 'POST', { identity: 'old' }); assert.equal(wrongNotice.status, 409);
  const restricted = await request('/api/v1/sites/1', 'PUT', { domain: 'unapproved.example' }); assert.equal(restricted.status, 403); assert.equal(blocked, 1); assert.equal(escaped, 0);
});

test('Nginx demo uses explicit discovery and candidate-bound dismiss without any install action', async () => {
  const { MockApiClient } = await demo(); const api = new MockApiClient();
  let current = await api.get('/api/v1/server/nginx/updates'); assert.equal(current.snapshot, null); assert.equal(current.running_adoption, 'not_verified');
  current = await api.post('/api/v1/server/nginx/updates/check', {}); assert.equal(current.notice, true); assert.equal(current.snapshot.installed_version, '1.24.0-2ubuntu7.15'); assert.equal(current.snapshot.candidate_version, '1.24.0-2ubuntu7.21');
  await assert.rejects(api.post('/api/v1/server/nginx/updates/notices/dismiss', { identity: 'old' }), /changed/);
  current = await api.post('/api/v1/server/nginx/updates/notices/dismiss', { identity: current.snapshot.identity }); assert.equal(current.notice, false);
  current = await api.post('/api/v1/server/nginx/updates/check', {}); assert.equal(current.notice, false); assert.equal(current.running_adoption, 'not_verified');
  const disk = await api.get('/api/v1/server/nginx/info'); assert.equal(disk.version_scope, 'on_disk_binary');
});

test('demo exact panel coexists with managed wildcard and cannot silently fall through on move/remove',async()=>{
 const {MockApiClient,mockPanelDomain}=await demo();const api=new MockApiClient();
 const domain=(await api.get('/api/v1/sites/1/domains'))[0];
 await api.put('/api/v1/sites/1/domains/0',{expected_revision:domain.revision,wildcard:true,www_redirect:'from_www'});
 await assert.rejects(api.delete('/api/v1/settings/panel-domain'),/Disable wildcard/);
 await assert.rejects(api.post('/api/v1/settings/panel-domain/custom',{domain:'other.example.net',certificate:'demo',private_key:'demo'}),/Disable wildcard/);
 assert.equal(mockPanelDomain.domain,'admin.myapp.com');
 const updated=(await api.get('/api/v1/sites/1/domains'))[0];await api.put('/api/v1/sites/1/domains/0',{expected_revision:updated.revision,wildcard:false,www_redirect:'from_www'});
 await api.delete('/api/v1/settings/panel-domain');assert.equal(mockPanelDomain.domain,'');
 await api.post('/api/v1/settings/panel-domain/custom',{domain:'admin.myapp.com',certificate:'demo',private_key:'demo'});
});
