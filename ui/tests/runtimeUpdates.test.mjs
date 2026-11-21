import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { parse, compileScript } from '@vue/compiler-sfc';
import { createRenderer, h, KeepAlive, nextTick, ref } from 'vue';

let moduleID = 0;
async function loadModule(source, bindings) {
  const key = `runtimeUpdateTest${++moduleID}`;
  globalThis[key] = bindings;
  const javascript = stripTypeScriptTypes(source).replace(/^import (.+?) from ['"]([^'"]+)['"];?$/gm, (line, imported, path) => {
    if (path === 'vue') return line.replace(/['"]vue['"]/, JSON.stringify(import.meta.resolve('vue')));
    assert.ok(path in bindings, `Missing test binding: ${path}`);
    if (imported.startsWith('{')) return `const ${imported.replace(/\bas\b/g, ':')} = globalThis.${key}[${JSON.stringify(path)}];`;
    return `const ${imported} = globalThis.${key}[${JSON.stringify(path)}];`;
  });
  return import('data:text/javascript;base64,' + Buffer.from(javascript + '\n// fixture: ' + key).toString('base64'));
}
async function component(path, bindings = {}) {
  const { descriptor } = parse(await readFile(new URL(`../src/${path}`, import.meta.url), 'utf8'));
  const compiled = compileScript(descriptor, { id: path, inlineTemplate: true });
  return (await loadModule(compiled.content, bindings)).default;
}
const box = { setup: (_, { slots }) => () => h('div', slots.default?.()) };
const button = { props: ['to', 'disabled'], setup: (props, { slots, attrs }) => () => h(props.to ? 'a' : 'button', { ...attrs, href: props.to, disabled: props.disabled }, slots.default?.()) };
const modal = {
  props: ['modelValue', 'title', 'confirmDisabled', 'loading'], emits: ['submit'],
  setup: (props, { slots, emit }) => () => props.modelValue ? h('section', { 'data-modal': props.title }, [slots.default?.(), slots.footer?.(), h('button', { 'data-confirm': true, disabled: props.confirmDisabled || props.loading, onClick: () => emit('submit') }, 'Confirm')]) : null,
};
const table = { props: ['items'], setup: (props, { slots }) => () => h('div', props.items.map(item => h('div', slots.actions?.({ item })))) };
const menu = { props: ['items'], emits: ['select'], setup: (props, { emit }) => () => h('div', props.items.map(item => h('button', { disabled: item.disabled, onClick: () => emit('select', item.id) }, item.label))) };
function node(type, text = '') {
  return { type, tagName: type.toUpperCase(), text, children: [], props: {}, parent: null,
    addEventListener() {}, removeEventListener() {}, get options() { return this.children; },
  };
}
const renderer = createRenderer({
  createElement: type => node(type), createText: text => node('#text', text), createComment: text => node('#comment', text),
  setText: (target, text) => { target.text = text; },
  setElementText: (target, text) => { target.text = text; target.children = []; },
  parentNode: target => target.parent,
  nextSibling: target => target.parent?.children[target.parent.children.indexOf(target) + 1] || null,
  patchProp: (target, key, _old, value) => { target.props[key] = value; },
  insert(target, parent, anchor = null) {
    if (target.parent) target.parent.children.splice(target.parent.children.indexOf(target), 1);
    target.parent = parent;
    const position = anchor ? parent.children.indexOf(anchor) : -1;
    parent.children.splice(position < 0 ? parent.children.length : position, 0, target);
  },
  remove(target) { target.parent?.children.splice(target.parent.children.indexOf(target), 1); target.parent = null; },
});
function mount(view) { const root = node('root'); const app = renderer.createApp(view); app.mount(root); return { root, app }; }
function nodes(root, match) { return [...(match(root) ? [root] : []), ...root.children.flatMap(child => nodes(child, match))]; }
function text(root) { return (root.type === '#comment' ? '' : root.text) + root.children.map(text).join(''); }
function findButton(root, label) { return nodes(root, item => item.type === 'button' && text(item) === label)[0]; }
async function settle() { await new Promise(resolve => setImmediate(resolve)); await nextTick(); }
const toast = { showToast: () => 1, addToast() {}, updateToast() {}, removeToast() {} };
const noticeBanner = await component('components/NoticeBanner.vue', { '@heroicons/vue/24/outline': { ArrowUpTrayIcon: box, XCircleIcon: box } });
const errorAlert = await component('components/ErrorAlert.vue', { './NoticeBanner.vue': noticeBanner });

async function phpFixture(options = {}) {
  const requests = [];
  let acceptedOperation;
  const discovery = { id: 'saved-discovery', version: '8.4', candidate_id: 'candidate', inventory: { changes: [{ name: 'php8.4-fpm', installed: '8.4.26', candidate: '8.4.27' }] }, impact: { sites: [], workers: [], blocked_reasons: [] } };
  const response = { branches: [{ version: '8.4', state: options.stale ? 'stale' : 'available', notice: true, update_available: !options.stale, plan: discovery, blocked_reasons: [] }], active_operation: null, failures: [], recoveries: [] };
  if (options.recovery) response.recoveries.push({ operation_id: 'failed-operation', generation_id: 'generation', version: '8.4', state: 'recovery_pending', can_repair: true });
  const client = {
    async get(path, config) {
      requests.push({ method: 'GET', path, config });
      if (path === '/api/v1/server/php/updates') {
        if (options.readFailure || acceptedOperation && options.statusReadFailure) throw new Error('Saved update status unavailable');
        return structuredClone(response);
      }
      if (path.includes('/updates/operations/')) return { ...acceptedOperation, status: options.operationStatus || 'running' };
      if (path === '/api/v1/server/node/updates') {
        if (acceptedOperation && options.statusReadFailure) throw new Error('Node status unavailable');
        return { snapshot: { update_available: false }, active_operation: null, operations: [], recoveries: [], locked: false };
      }
      if (path === '/api/v1/server/php/versions/available') {
        // The cached inventory predates a repaired installation.
        return [{ version: '8.4', installed: true, status: config?.useCache === false ? 'running' : 'unavailable' }];
      }
      if (path === '/api/v1/server/php') return ['8.4'];
      if (path.endsWith('/repository')) return { state: 'current' };
      return {};
    },
    async post(path, body) {
      requests.push({ method: 'POST', path, body });
      if (path.endsWith('/recovery-plan')) return { id: 'recovery-plan', version: '8.4', kind: options.rollbackRecovery ? 'rollback' : 'repair', packages: [], impact: discovery.impact };
      if (path.endsWith('/recovery/apply') || path === '/api/v1/server/php/updates/apply' || path === '/api/v1/server/node/updates/apply') {
        if (options.acceptGate) await options.acceptGate;
        if (options.rejectStart) throw new Error('Recovery review expired');
        acceptedOperation = { id: 'accepted-operation', version: '8.4', kind: options.rollbackRecovery ? 'rollback' : path.endsWith('/recovery/apply') ? 'repair' : 'apply', status: 'queued', message: 'Accepted' };
        if (!options.staleSnapshot) response.active_operation = acceptedOperation;
        return acceptedOperation;
      }
      assert.equal(path, '/api/v1/server/php/updates/plans/saved-discovery/review');
      return { ...discovery, id: 'reviewed-plan', parent_plan_id: discovery.id, reviewed: options.reviewed !== false, rollback: { available: options.rollback !== false }, impact: { ...discovery.impact, blocked_reasons: options.blockers || [], scheduled_tasks: [{ name: 'Renew SSL Certificates', identity: 'ssl-job', notice: 'Older certificate hooks require separate SSH review.' }] } };
    },
    invalidate() {},
  };
  const maintenance = await loadModule(await readFile(new URL('../src/composables/runtimeMaintenance.ts', import.meta.url), 'utf8'), {});
  const updatesModule = await loadModule(await readFile(new URL('../src/composables/usePHPUpdates.ts', import.meta.url), 'utf8'), {
    '../api/client': { apiClient: client }, './useToast': { useToast: () => toast }, '../utils/requestID': { createRequestID: () => 'request-id' },
    './runtimeMaintenance': maintenance,
  });
  const nodeModule = await loadModule(await readFile(new URL('../src/composables/useNodeUpdates.ts', import.meta.url), 'utf8'), {
    '../api/client': { apiClient: client }, './useToast': { useToast: () => toast }, '../utils/requestID': { createRequestID: () => 'request-id' },
    './runtimeMaintenance': maintenance, '../utils/nodeMaintenancePresentation': await import('../src/utils/nodeMaintenancePresentation.ts'),
  });
  const php = await component('views/RuntimePHP.vue', {
    '../composables/useToast': { useToast: () => toast }, '../composables/useConfirm': { useConfirm: () => ({ confirm: async () => false }) },
    '../api/client': { apiClient: client }, '../utils/requestID': { createRequestID: () => 'request-id' }, '../composables/usePHPUpdates': updatesModule,
    '../components/Card.vue': box, '../components/DataTable.vue': table, '../components/StatusBadge.vue': box, '../components/SkeletonLoader.vue': box,
    '../components/ToggleSwitch.vue': box, '../components/TableActionMenu.vue': menu, '../components/BaseModal.vue': modal,
    '../components/ErrorAlert.vue': errorAlert, '../components/AppButton.vue': button,
  });
  return { php, requests, updates: updatesModule.usePHPUpdates(), nodeUpdates: nodeModule.useNodeUpdates(), response };
}

test('accepted PHP repair closes the modal and locks both runtimes through status read failures', async t => {
  const options = { recovery: true, statusReadFailure: true };
  const { php, requests, updates, nodeUpdates, response } = await phpFixture(options);
  const { root, app } = mount(php); t.after(() => app.unmount()); await settle();
  await findButton(root, 'Repair').props.onClick(); await settle();
  const modal = nodes(root, item => item.props['data-modal'] === 'Repair PHP 8.4')[0];
  assert.ok(modal);
  await nodes(modal, item => item.props['data-confirm'])[0].props.onClick(); await settle();
  assert.equal(nodes(root, item => item.props['data-modal'] === 'Repair PHP 8.4').length, 0, 'acceptance closes the confirmation without waiting for GET');
  assert.equal(updates.busy.value, true);
  assert.equal(nodeUpdates.busy.value, true);
  assert.equal(findButton(root, 'Update').props.disabled, true);
  await assert.rejects(() => nodeUpdates.check(), /operation is in progress/);
  assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/recovery/apply')).length, 1);
  options.statusReadFailure = false;
  options.operationStatus = 'succeeded';
  response.active_operation = null; response.recoveries = [];
  await updates.refresh(); await settle();
  assert.equal(updates.busy.value, false);
  assert.equal(nodeUpdates.busy.value, false);
});

test('repair submission disables controls immediately and refuses a second click before acceptance', async t => {
  let accept;
  const options = { recovery: true, acceptGate: new Promise(resolve => { accept = resolve; }) };
  const { php, requests, updates, nodeUpdates } = await phpFixture(options);
  const { root, app } = mount(php); t.after(() => app.unmount()); await settle();
  await findButton(root, 'Repair').props.onClick(); await settle();
  const confirm = nodes(root, item => item.props['data-confirm'])[0];
  const first = confirm.props.onClick(); await nextTick();
  assert.equal(nodes(root, item => item.props['data-confirm'])[0].props.disabled, true);
  assert.equal(nodeUpdates.busy.value, true);
  await confirm.props.onClick();
  assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/recovery/apply')).length, 1);
  accept(); await first; await settle();
  assert.equal(updates.busy.value, true);
});

test('stale empty snapshots cannot clear an accepted apply before its operation is terminal', async () => {
  const options = { staleSnapshot: true, operationStatus: 'running' };
  const { updates, nodeUpdates } = await phpFixture(options);
  await updates.apply({ id: 'reviewed', version: '8.4' }, 'same-request'); await settle();
  assert.equal(updates.state.value.active_operation, null, 'the fixture returned an older empty snapshot');
  assert.equal(updates.busy.value, true, 'the accepted claim survives the stale snapshot');
  assert.equal(nodeUpdates.locked.value, true);
  options.operationStatus = 'succeeded';
  await updates.refresh();
  assert.equal(updates.busy.value, false);
});

test('Node apply acknowledgement survives refresh failure and locks PHP actions', async () => {
  const options = { statusReadFailure: true, staleSnapshot: true };
  const { updates, nodeUpdates } = await phpFixture(options);
  await nodeUpdates.apply({ id: 'node-plan', kind: 'apply' }, 'node-request'); await settle();
  assert.equal(nodeUpdates.busy.value, true);
  assert.equal(updates.locked.value, true);
  await assert.rejects(() => updates.check('8.4'), /operation is in progress/);
  options.statusReadFailure = false;
  options.operationStatus = 'succeeded';
  await nodeUpdates.refresh();
  assert.equal(nodeUpdates.busy.value, false);
  assert.equal(updates.busy.value, false);
});

test('a rejected repair keeps its modal and request identity; accepted rollback closes despite GET failure', async t => {
  const options = { recovery: true, rejectStart: true };
  const { php, requests, updates, nodeUpdates } = await phpFixture(options);
  const { root, app } = mount(php); t.after(() => app.unmount()); await settle();
  await findButton(root, 'Repair').props.onClick(); await settle();
  await nodes(root, item => item.props['data-confirm'])[0].props.onClick(); await settle();
  assert.equal(nodes(root, item => item.props['data-modal'] === 'Repair PHP 8.4').length, 1);
  assert.equal(updates.busy.value, false);
  assert.equal(nodeUpdates.busy.value, false);
  options.rejectStart = false; options.statusReadFailure = true;
  await nodes(root, item => item.props['data-confirm'])[0].props.onClick(); await settle();
  const posts = requests.filter(r => r.method === 'POST' && r.path.endsWith('/recovery/apply'));
  assert.equal(posts.length, 2);
  assert.equal(posts[0].body.request_id, posts[1].body.request_id, 'an uncertain retry retains the idempotency key');
  assert.equal(nodes(root, item => item.props['data-modal'] === 'Repair PHP 8.4').length, 0);
  const rollback = await phpFixture({ recovery: true, rollbackRecovery: true, statusReadFailure: true });
  await rollback.updates.recover({ id: 'rollback', version: '8.4', kind: 'rollback' }, 'rollback-request'); await settle();
  assert.equal(rollback.updates.busy.value, true);
  assert.equal(rollback.nodeUpdates.busy.value, true);
});

test('navigation hydrates fresh PHP discovery and current inventory without a package check', async t => {
  const { php, requests, updates, response } = await phpFixture();
  const current = ref(true);
  const { root, app } = mount({ setup: () => () => h(KeepAlive, null, { default: () => current.value ? h(php) : h(box) }) });
  t.after(() => app.unmount());
  await settle();
  assert.equal(requests.filter(request => request.path === '/api/v1/server/php/updates').length, 1, 'mount and KeepAlive activation share hydration');
  assert.equal(findButton(root, 'Update').props.disabled, false, 'fresh status replaces cached unavailable inventory');
  await findButton(root, 'Update').props.onClick(); await settle();
  assert.ok(nodes(root, item => item.props['data-modal'] === 'Update PHP 8.4').length);
  assert.equal(nodes(root, item => item.props['data-confirm'])[0].props.disabled, false);
  assert.ok(text(root).includes('Older certificate hooks require separate SSH review.'), 'receipt notice stays visible and nonblocking');
  assert.deepEqual(requests.filter(request => request.method === 'POST').map(request => request.path), ['/api/v1/server/php/updates/plans/saved-discovery/review']);
  current.value = false; await settle();
  response.branches[0].update_available = false; response.branches[0].state = 'stale';
  current.value = true; await settle();
  assert.equal(findButton(root, 'Update').props.disabled, true, 'cached page activation reads current saved readiness');
  assert.equal(nodes(root, item => item.props['data-confirm']).length, 0, 'activation discards an earlier installation review');
  assert.equal(updates.state.value.branches[0].notice, true, 'staleness does not mutate dismissal identity');
});

test('stale notices and failed status reads cannot grant PHP review', async () => {
  for (const options of [{ stale: true }, { readFailure: true }]) {
    const { php, requests } = await phpFixture(options);
    const { root, app } = mount(php); await settle();
    assert.equal(findButton(root, 'Update').props.disabled, true);
    await findButton(root, 'Update').props.onClick();
    assert.equal(requests.filter(request => request.method === 'POST').length, 0);
    if (options.readFailure) assert.ok(text(root).includes('Saved update status unavailable'));
    app.unmount();
  }
});

test('PHP blockers are compact with complete expandable text diagnostics and a jobs link', async t => {
  const blockers = ['Scheduled task Renew SSL Certificates: <script>original diagnostic</script>\n' + 'full detail '.repeat(120), 'Other original failure'];
  const { php, requests } = await phpFixture({ blockers });
  const { root, app } = mount(php); t.after(() => app.unmount()); await settle();
  await findButton(root, 'Update').props.onClick(); await settle();
  assert.ok(text(root).includes('Resolve 2 issues to continue.'));
  assert.ok(!text(root).includes(blockers[0]), 'diagnostics start collapsed');
  assert.ok(nodes(root, item => item.props.href === '/processes/scheduler' && text(item) === 'Review jobs').length);
  assert.equal(nodes(root, item => item.props['data-confirm'])[0].props.disabled, true);
  findButton(root, 'Show details').props.onClick(); await nextTick();
  assert.ok(text(root).includes(blockers.join('\n\n')), 'original diagnostic has no truncation or rewriting');
  assert.equal(nodes(root, item => item.type === 'script').length, 0, 'diagnostic is text, not interpreted HTML');
  await nodes(root, item => item.props['data-confirm'])[0].props.onClick();
  assert.equal(requests.filter(request => request.path.endsWith('/apply')).length, 0);
});

test('PHP preserves unreviewed, rollback acknowledgement and busy/recovery gates', async () => {
  for (const options of [{ reviewed: false }, { rollback: false }, { locked: true }, { busy: true }]) {
    const { php, updates, requests } = await phpFixture(options);
    const { root, app } = mount(php); await settle();
    if (options.busy) updates.state.value.active_operation = { id: 'running-check', kind: 'check', status: 'running' };
    if (options.locked) updates.state.value.recoveries.push({ generation_id: 'g', operation_id: 'old', version: '8.4', state: 'recovery_required' });
    await nextTick();
    if (options.busy || options.locked) {
      assert.equal(findButton(root, 'Update').props.disabled, true);
      await findButton(root, 'Update').props.onClick();
      assert.equal(requests.filter(request => request.method === 'POST').length, 0);
    } else {
      await findButton(root, 'Update').props.onClick(); await settle();
      assert.equal(nodes(root, item => item.props['data-confirm'])[0].props.disabled, true);
      await nodes(root, item => item.props['data-confirm'])[0].props.onClick();
      assert.equal(requests.filter(request => request.path.endsWith('/apply')).length, 0);
    }
    app.unmount();
  }
});
