import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import * as vue from 'vue';
import * as vueRouter from 'vue-router';
import { parse, compileScript } from '@vue/compiler-sfc';
import { renderToString } from '@vue/server-renderer';
import { loadVue } from './helpers/loadVue.mjs';
let sequence = 0;
async function load(source, bindings = {}) {
  const key = `wordPressCronTest${++sequence}`;
  globalThis[key] = { vue, ...bindings };
  const js = stripTypeScriptTypes(source).replace(/^import ([^;\n]+?) from ['"]([^'"]+)['"];?$/gm, (_, names, path) => {
    if (path.endsWith('/apiError')) return `import ${names} from ${JSON.stringify(new URL('../src/utils/apiError.ts', import.meta.url).href)};`;
    assert.ok(path in globalThis[key], path);
    return `const ${names.startsWith('{') ? names.replace(/\bas\b/g, ':') : names} = globalThis.${key}[${JSON.stringify(path)}];`;
  });
  return import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
}
const source = await readFile(new URL('../src/composables/useWordPressCron.ts', import.meta.url), 'utf8');
async function fixture() {
  let approved = true, resolveConfirmation, site = '5';
  const calls = [], confirmations = [];
  const api = {
    getWordPressCron: async id => { calls.push(['get', id]); return { enabled: false, phase: 'off' }; },
    setWordPressCron: async (id, enabled) => { calls.push(['set', id, enabled]); return { enabled, phase: enabled ? 'enabled' : 'off', cron_id: enabled ? 12 : undefined }; },
  };
  const mod = await load(source, { '../api/client': { apiClient: api }, './useConfirm': { useConfirm: () => ({ confirm: async options => {
    confirmations.push(options);
    return approved === 'waiting' ? new Promise(resolve => { resolveConfirmation = resolve; }) : approved;
  } }) } });
  return { flow: mod.useWordPressCron(() => site), api, calls, confirmations, approve: value => approved = value, accept: () => resolveConfirmation?.(true), site: value => site = value };
}

test('WordPress status is read-only and off by default; cancelled confirmation does not mutate', async () => {
  const f = await fixture();
  await f.flow.refresh();
  assert.equal(f.flow.state.value.enabled, false);
  assert.deepEqual(f.calls, [['get', '5']]);
  f.approve(false);
  assert.equal(await f.flow.setEnabled(true), false);
  assert.deepEqual(f.calls, [['get', '5']]);
  assert.equal(f.flow.busy.value, false);
});

test('busy confirmation prevents duplicate requests; enable and disable show actual server state', async () => {
  const f = await fixture(); await f.flow.refresh(); f.approve('waiting');
  const pending = f.flow.setEnabled(true);
  assert.equal(f.flow.busy.value, true);
  assert.equal(await f.flow.setEnabled(true), false);
  f.accept(); assert.equal(await pending, true);
  assert.equal(f.flow.state.value.phase, 'enabled');
  assert.deepEqual(f.calls.filter(call => call[0] === 'set'), [['set', '5', true]]);
  f.approve(true); await f.flow.setEnabled(false);
  assert.equal(f.flow.state.value.phase, 'off');
  assert.equal(f.confirmations.at(-1).variant, 'danger');
});

test('failed publication retains diagnostics and reads recoverable pending state', async () => {
  const f = await fixture(); await f.flow.refresh();
  f.api.setWordPressCron = async () => { throw Error('{"message":"Runtime maintenance is busy"}'); };
  f.api.getWordPressCron = async () => ({ enabled: false, phase: 'enabling', cron_id: 12, message: 'Retry setup or turn it off.' });
  assert.equal(await f.flow.setEnabled(true), false);
  assert.equal(f.flow.state.value.phase, 'enabling');
  assert.equal(f.flow.error.value, 'Runtime maintenance is busy');
  assert.equal(f.flow.busy.value, false);
});

test('failed status read does not invent off state or leave mutation enabled', async () => {
  const f = await fixture(); await f.flow.refresh();
  f.api.getWordPressCron = async () => { throw Error('Status unavailable'); };
  await f.flow.refresh();
  assert.equal(f.flow.state.value, null);
  assert.equal(await f.flow.setEnabled(true), false);
  assert.match(f.flow.error.value, /Status unavailable/);
});

test('navigation while confirmation is open cannot mutate another site', async () => {
  const f = await fixture(); await f.flow.refresh(); f.approve('waiting');
  const pending = f.flow.setEnabled(true); f.site('6'); f.accept();
  assert.equal(await pending, false);
  assert.equal(f.calls.filter(call => call[0] === 'set').length, 0);
});

test('WordPress card reuses accessible shared switch, card, error and button components', async () => {
  const { descriptor } = parse(await readFile(new URL('../src/components/WordPressCronCard.vue', import.meta.url), 'utf8'));
  const state = vue.ref({ enabled: false, phase: 'off' });
  const bindings = { '../composables/useWordPressCron': { useWordPressCron: () => ({ state, busy: vue.ref(false), error: vue.ref(''), refresh: async () => {}, setEnabled: async () => false }) } };
  for (const name of ['Card', 'ToggleSwitch', 'ErrorAlert', 'AppButton']) bindings[`./${name}.vue`] = (await import(await loadVue(new URL(`../src/components/${name}.vue`, import.meta.url)))).default;
  const component = (await load(compileScript(descriptor, { id: 'wordpress-cron-card', inlineTemplate: true }).content, bindings)).default;
  let html = await renderToString(vue.createSSRApp(component, { siteId: '5' }));
  assert.match(html, /role="switch"/);
  assert.match(html, /aria-checked="false"/);
  assert.match(html, /uppercase tracking-wider[^>]*>WordPress Features<\/h3>/);
  assert.match(html, /p-5 space-y-3/);
  assert.doesNotMatch(html, /Run due tasks every minute, even without visitors/);
  state.value = { enabled: false, phase: 'enabling', cron_id: 12, message: 'Retry the guarded setup.' };
  html = await renderToString(vue.createSSRApp(component, { siteId: '5' }));
  assert.match(html, /Retry setup/); assert.match(html, /Turn off/); assert.match(html, /Retry the guarded setup/);
});

async function clientFixture(t) {
  const { apiClient } = await load(await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8'), {
    '../router': { router: { push() {} } },
    '../composables/useToast': { useToast: () => ({ addToast() {} }) },
    '../stores/auth': { useAuthStore: () => ({ token: 'test' }) },
    '../utils/logViewCache': { clearLogViewSnapshots() {} },
  });
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  const f = { apiClient, contents: new Map([['5', '<?php\n// original\n']]), reads: [], failMutation: false, nextRead: null };
  globalThis.fetch = async (url, options = {}) => {
    const [, id, resource] = url.match(/^\/api\/v1\/sites\/(\d+)(?:\/(wordpress-config|wordpress-cron))?$/) || [];
    assert.ok(id, url);
    const method = options.method || 'GET';
    if (method === 'GET' && !resource) return Response.json({ id, domain: 'pressroom.test', ssl_active: true });
    if (method === 'GET' && resource === 'wordpress-cron') return Response.json({ enabled: false, phase: 'off' });
    if (method === 'GET' && resource === 'wordpress-config') {
      f.reads.push(id);
      if (f.nextRead) { const read = f.nextRead; f.nextRead = null; return read; }
      return Response.json({ content: f.contents.get(id) || '<?php\n' });
    }
    if (method === 'PUT' && resource === 'wordpress-cron') {
      if (f.failMutation) throw Error('Response interrupted');
      const { enabled } = JSON.parse(options.body);
      f.contents.set(id, enabled ? "<?php\n// BEGIN FLUXO WORDPRESS CRON\ndefine('DISABLE_WP_CRON', true);\n// END FLUXO WORDPRESS CRON\n" : '<?php\n// original\n');
      return Response.json({ enabled, phase: enabled ? 'enabled' : 'off' });
    }
    if (method === 'POST' && resource === 'wordpress-config') {
      if (f.failMutation) throw Error('Response interrupted');
      f.contents.set(id, JSON.parse(options.body).content);
      return Response.json({ success: true });
    }
    throw Error('Unexpected request: ' + method + ' ' + url);
  };
  return f;
}

test('configuration revisions change only after relevant mutations and survive an emptied HTTP cache', async t => {
  const f = await clientFixture(t), api = f.apiClient;
  assert.equal(api.getWordPressConfigRevision('5'), 0);
  await api.getWordPressConfig('5');
  await api.getWordPressConfig('5');
  await api.getWordPressConfig('5', true);
  await api.getWordPressCron('5');
  assert.equal(api.getWordPressConfigRevision('5'), 0, 'reads do not mark the editor stale');
  await api.setWordPressCron('6', true);
  assert.equal(api.getWordPressConfigRevision('5'), 0, 'another site does not invalidate this editor');
  await api.setWordPressCron('5', true);
  assert.equal(api.getWordPressConfigRevision(5), 1);
  await api.setWordPressCron('5', false);
  assert.equal(api.getWordPressConfigRevision('5'), 2, 'another mutation is observed even with no cached entry');
  await api.saveWordPressConfig('5', '<?php\n// saved\n');
  assert.equal(api.getWordPressConfigRevision('5'), 3);
  f.failMutation = true;
  await assert.rejects(api.setWordPressCron('5', true), /interrupted/);
  await assert.rejects(api.saveWordPressConfig('5', '<?php'), /interrupted/);
  assert.equal(api.getWordPressConfigRevision('5'), 5, 'ambiguous mutation failures also invalidate');
});

function editorHost() {
  const node = (type, text = '') => ({ type, text, children: [], props: {}, parent: null });
  const renderer = vue.createRenderer({
    createElement: type => node(type), createText: text => node('#text', text), createComment: text => node('#comment', text),
    setText: (n, text) => { n.text = text; }, setElementText: (n, text) => { n.text = text; n.children = []; },
    parentNode: n => n.parent, nextSibling: n => n.parent?.children[n.parent.children.indexOf(n) + 1] || null,
    patchProp: (n, key, _old, value) => { n.props[key] = value; },
    insert(n, parent, anchor = null) {
      if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1);
      n.parent = parent;
      const at = anchor ? parent.children.indexOf(anchor) : -1;
      parent.children.splice(at < 0 ? parent.children.length : at, 0, n);
    },
    remove(n) { n.parent?.children.splice(n.parent.children.indexOf(n), 1); n.parent = null; },
  });
  const root = node('root');
  const all = n => [n, ...n.children.flatMap(all)];
  return { renderer, root, find: predicate => all(root).find(predicate) };
}

async function editorFixture(t) {
  const f = await clientFixture(t), errors = [];
  const { descriptor } = parse(await readFile(new URL('../src/views/site/SiteSettingsWordPress.vue', import.meta.url), 'utf8'));
  const blank = { render: () => null };
  const editor = (await load(compileScript(descriptor, { id: 'wordpress-config-test', inlineTemplate: true }).content, {
    'vue-router': vueRouter,
    '@heroicons/vue/24/outline': { EyeIcon: blank, EyeSlashIcon: blank },
    '../../api/client': { apiClient: f.apiClient },
    '../../composables/useConfirm': { useConfirm: () => ({ confirm: async () => false }) },
    '../../composables/useToast': { useToast: () => ({ addToast: (...args) => errors.push(args) }) },
    '../../components/ScriptEditor.vue': {
      props: ['modelValue', 'masked', 'readonly'], emits: ['update:modelValue', 'reveal'],
      setup: (props, { emit }) => () => vue.h('textarea', { value: props.modelValue, 'data-masked': props.masked, readonly: props.readonly,
        onInput: value => emit('update:modelValue', value), onReveal: () => emit('reveal') }),
    },
  })).default;
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [
    { path: '/sites/:id/settings/wordpress', component: editor }, { path: '/away', component: blank },
  ] });
  await router.push('/sites/5/settings/wordpress');
  const host = editorHost();
  const app = host.renderer.createApp({ setup: () => () => vue.h(vueRouter.RouterView, null, {
    default: ({ Component }) => vue.h(vue.KeepAlive, null, { default: () => Component }),
  }) });
  app.use(router); app.mount(host.root); t.after(() => app.unmount());
  const settle = async () => { await new Promise(resolve => setImmediate(resolve)); await vue.nextTick(); };
  await settle();
  const visit = async path => { await router.push(path); await settle(); };
  return Object.assign(f, host, { router, errors, settle, visit, field: () => host.find(n => n.type === 'textarea') });
}

test('retained WordPress editor refreshes after Cron changes but not ordinary navigation or other sites', async t => {
  const f = await editorFixture(t);
  assert.deepEqual(f.reads, ['5'], 'mount and initial activation coalesce');
  await f.visit('/away'); await f.visit('/sites/5/settings/wordpress');
  assert.deepEqual(f.reads, ['5'], 'ordinary visits keep the loaded editor');
  await f.visit('/away'); await f.apiClient.setWordPressCron('6', true); await f.visit('/sites/5/settings/wordpress');
  assert.deepEqual(f.reads, ['5']);
  await f.visit('/away'); await f.apiClient.setWordPressCron('5', true);
  assert.deepEqual(f.reads, ['5'], 'mutation marks stale without fetching a hidden configuration');
  await f.visit('/sites/5/settings/wordpress');
  assert.match(f.field().props.value, /define\('DISABLE_WP_CRON', true\)/);
  assert.equal(f.field().props['data-masked'], true);
  await f.visit('/away'); await f.visit('/sites/5/settings/wordpress');
  assert.deepEqual(f.reads, ['5', '5'], 'successful refresh consumes the invalidation');
  await f.visit('/away'); await f.apiClient.setWordPressCron('5', false); await f.visit('/sites/5/settings/wordpress');
  assert.doesNotMatch(f.field().props.value, /DISABLE_WP_CRON/);
  assert.deepEqual(f.reads, ['5', '5', '5']);
});

test('a refresh cannot replace edits made while its response is in flight', async t => {
  const f = await editorFixture(t);
  await f.visit('/away'); await f.apiClient.setWordPressCron('5', true);
  let resolve;
  f.nextRead = new Promise(done => { resolve = done; });
  await f.visit('/sites/5/settings/wordpress');
  const draft = '<?php\n// unsaved local changes\n';
  f.field().props.onInput(draft); await vue.nextTick();
  resolve(Response.json({ content: f.contents.get('5') })); await f.settle();
  assert.equal(f.field().props.value, draft, 'late server content does not overwrite a draft');
  await f.visit('/away');
  assert.equal(f.router.currentRoute.value.path, '/sites/5/settings/wordpress', 'cancelled discard preserves the editor');
  assert.equal(f.field().props.value, draft);
});

test('saving configuration consumes its own revision and another save refreshes the retained editor once', async t => {
  const f = await editorFixture(t);
  const draft = '<?php\n// saved through editor\n';
  f.field().props.onReveal();
  f.field().props.onInput(draft); await vue.nextTick();
  const save = f.find(n => n.type === 'button' && n.text === 'Save configuration');
  assert.equal(save.props.disabled, false);
  await save.props.onClick(); await f.settle();
  assert.equal(f.contents.get('5'), draft);
  await f.visit('/away'); await f.visit('/sites/5/settings/wordpress');
  assert.deepEqual(f.reads, ['5'], 'the saving editor already holds the acknowledged content');
  await f.visit('/away'); await f.apiClient.saveWordPressConfig('5', '<?php\n// another save\n');
  await f.visit('/sites/5/settings/wordpress');
  assert.equal(f.field().props.value, '<?php\n// another save\n');
  assert.deepEqual(f.reads, ['5', '5']);
});

test('a failed refresh retains the invalidation for the next visit', async t => {
  const f = await editorFixture(t);
  await f.visit('/away'); await f.apiClient.setWordPressCron('5', true);
  f.nextRead = Promise.resolve(new Response('Configuration temporarily unavailable', { status: 503 }));
  await f.visit('/sites/5/settings/wordpress');
  assert.equal(f.field().props.value, '<?php\n// original\n');
  assert.deepEqual(f.errors, [['Configuration temporarily unavailable', 'error']]);
  await f.visit('/away'); await f.visit('/sites/5/settings/wordpress');
  assert.match(f.field().props.value, /DISABLE_WP_CRON/);
  assert.deepEqual(f.reads, ['5', '5', '5']);
});
