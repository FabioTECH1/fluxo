import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import * as vue from 'vue';
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
  assert.match(html, /Run due tasks every minute, even without visitors/);
  state.value = { enabled: false, phase: 'enabling', cron_id: 12, message: 'Retry the guarded setup.' };
  html = await renderToString(vue.createSSRApp(component, { siteId: '5' }));
  assert.match(html, /Retry setup/); assert.match(html, /Turn off/); assert.match(html, /Retry the guarded setup/);
});
