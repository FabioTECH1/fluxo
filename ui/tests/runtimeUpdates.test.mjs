import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { parse, compileScript } from '@vue/compiler-sfc';
import { createRenderer, h, KeepAlive, nextTick, ref } from 'vue';
import * as vueRouter from 'vue-router';

let moduleID = 0;
async function loadModule(source, bindings) {
  const key = `runtimeUpdateTest${++moduleID}`;
  globalThis[key] = bindings;
  const javascript = stripTypeScriptTypes(source).replace(/\bimport ([^;\n]+?) from ['"]([^'"]+)['"];?/g, (line, imported, path) => {
    if (path.endsWith('/apiError')) return `import ${imported} from ${JSON.stringify(new URL('../src/utils/apiError.ts', import.meta.url).href)};`;
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
const button = { props: ['to', 'disabled', 'loading'], setup: (props, { slots, attrs }) => () => h(props.to ? 'a' : 'button', { ...attrs, href: props.to, disabled: props.disabled || props.loading }, slots.default?.()) };
const modal = {
  props: ['modelValue', 'title', 'confirmDisabled', 'loading'], emits: ['submit'],
  setup: (props, { slots, emit }) => () => props.modelValue ? h('section', { 'data-modal': props.title }, [slots.default?.(), slots.footer?.(), h('button', { 'data-confirm': true, disabled: props.confirmDisabled || props.loading, onClick: () => emit('submit') }, 'Confirm')]) : null,
};
const table = { props: ['items'], setup: (props, { slots }) => () => h('div', props.items.map(item => h('div', slots.actions?.({ item })))) };
const menu = { props: ['items'], emits: ['select'], setup: (props, { emit }) => () => h('div', props.items.map(item => h('button', { disabled: item.disabled, onClick: () => emit('select', item.id) }, item.label))) };
function node(type, text = '') {
  return { type, tagName: type.toUpperCase(), text, children: [], props: {}, parent: null,
    addEventListener() {}, removeEventListener() {}, getRootNode() { return { activeElement: null }; }, get options() { return this.children; },
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
function mount(view, router) { const root = node('root'); const app = renderer.createApp(view); if (router) app.use(router); app.mount(root); return { root, app }; }
function nodes(root, match) { return [...(match(root) ? [root] : []), ...root.children.flatMap(child => nodes(child, match))]; }
function text(root) { return (root.type === '#comment' ? '' : root.text) + root.children.map(text).join(''); }
function findButton(root, label) { return nodes(root, item => item.type === 'button' && text(item) === label)[0]; }
async function settle() { await new Promise(resolve => setImmediate(resolve)); await nextTick(); }
const toast = { showToast: () => 1, addToast() {}, updateToast() {}, removeToast() {} };
const noticeBanner = await component('components/NoticeBanner.vue', { '@heroicons/vue/24/outline': { ArrowUpTrayIcon: box, XCircleIcon: box } });
const errorAlert = await component('components/ErrorAlert.vue', { './NoticeBanner.vue': noticeBanner });

async function phpFixture(options = {}) {
  const requests = [];
  const toastUpdates = [];
  const fixtureToast = { ...toast, updateToast(_id, result) { toastUpdates.push(result); } };
  let acceptedOperation;
  const discovery = { id: 'saved-discovery', version: '8.4', candidate_id: 'candidate', inventory: { changes: [{ name: 'php8.4-fpm', installed: '8.4.26', candidate: '8.4.27' }] }, impact: { sites: [], workers: [], blocked_reasons: [] } };
  const response = { branches: [{ version: '8.4', state: options.stale ? 'stale' : 'available', notice: true, update_available: !options.stale, plan: discovery, blocked_reasons: [] }], active_operation: null, failures: [], recoveries: [] };
  if (options.recovery) response.recoveries.push({ operation_id: 'failed-operation', generation_id: 'generation', version: '8.4', state: 'recovery_pending', can_repair: true });
  const nodePlan = { id: 'node-recovery-plan', kind: 'repair', before: { release: { version: '22.23.1', npm: '10.9.8' } }, after: { version: '22.23.5', npm: '10.9.9' }, impact: { sites: [], units: [], blocked_reasons: [] } };
  const nodeResponse = { snapshot: { update_available: false }, active_operation: null, operations: [], recoveries: [], locked: false };
  if (options.nodeRecovery) nodeResponse.recoveries.push({ id: 'node-generation', head: 'node-failure', state: 'recovery_required', plan: nodePlan });
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
        return structuredClone(nodeResponse);
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
      if (path.endsWith('/recovery-plan')) {
        if (options.rejectReview) throw new Error('Recovery review is unavailable');
        return path.includes('/server/node/') ? structuredClone(nodePlan) : { id: 'recovery-plan', version: '8.4', kind: options.rollbackRecovery ? 'rollback' : 'repair', packages: [], impact: discovery.impact };
      }
      if (path === '/api/v1/server/php/updates/check' || path === '/api/v1/server/node/updates/check') {
        if (options.acceptGate) await options.acceptGate;
        if (options.rejectStart) throw new Error('Check could not be started');
        acceptedOperation = { id: 'accepted-check', version: body.version, kind: 'check', status: 'queued', message: 'Check accepted' };
        if (path.includes('/server/node/')) nodeResponse.active_operation = acceptedOperation;
        else response.active_operation = acceptedOperation;
        return acceptedOperation;
      }
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
    '../api/client': { apiClient: client }, './useToast': { useToast: () => fixtureToast }, '../utils/requestID': { createRequestID: () => 'request-id' },
    './runtimeMaintenance': maintenance,
  });
  const nodeModule = await loadModule(await readFile(new URL('../src/composables/useNodeUpdates.ts', import.meta.url), 'utf8'), {
    '../api/client': { apiClient: client }, './useToast': { useToast: () => fixtureToast }, '../utils/requestID': { createRequestID: () => 'request-id' },
    './runtimeMaintenance': maintenance, '../utils/nodeMaintenancePresentation': await import('../src/utils/nodeMaintenancePresentation.ts'),
  });
  const php = await component('views/RuntimePHP.vue', {
    '../composables/useToast': { useToast: () => fixtureToast }, '../composables/useConfirm': { useConfirm: () => ({ confirm: async () => false }) },
    '../api/client': { apiClient: client }, '../utils/requestID': { createRequestID: () => 'request-id' }, '../composables/usePHPUpdates': updatesModule,
    '../components/Card.vue': box, '../components/DataTable.vue': table, '../components/StatusBadge.vue': box, '../components/SkeletonLoader.vue': box,
    '../components/ToggleSwitch.vue': box, '../components/TableActionMenu.vue': menu, '../components/BaseModal.vue': modal,
    '../components/ErrorAlert.vue': errorAlert, '../components/AppButton.vue': button,
  });
  return { php, requests, toastUpdates, updates: updatesModule.usePHPUpdates(), nodeUpdates: nodeModule.useNodeUpdates(), response, nodeResponse, updatesModule, nodeModule };
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

async function failureJourney(runtime, options = {}) {
  const fixture = await phpFixture(options);
  const failure = { id: runtime === 'php' ? 'failed-operation' : 'node-failure', kind: options.failureKind || 'apply', status: 'failed', version: '8.4', message: 'Check for updates again or review the recorded recovery.', output: 'Full original diagnostic' };
  if (runtime === 'php') fixture.response.failures.push(failure);
  else fixture.nodeResponse.operations.push(failure);
  const navigation = await loadModule(await readFile(new URL('../src/composables/useNotificationNavigation.ts', import.meta.url), 'utf8'), { 'vue-router': vueRouter });
  const persistent = await component('components/PersistentFailureBanner.vue', { './AppButton.vue': button, './BaseModal.vue': modal, '../composables/useToast': { useToast: () => toast } });
  const updateNotice = box;
  const notice = await component(`components/${runtime === 'php' ? 'PHP' : 'Node'}UpdateNotice.vue`, {
    'vue-router': vueRouter, '../composables/useNotificationNavigation': navigation,
    '../composables/useToast': { useToast: () => toast },
    [`../composables/use${runtime === 'php' ? 'PHP' : 'Node'}Updates`]: runtime === 'php' ? fixture.updatesModule : fixture.nodeModule,
    './PersistentFailureBanner.vue': persistent, './AppButton.vue': button, './BaseModal.vue': modal, './UpdateNotice.vue': updateNotice,
  });
  const panel = runtime === 'php' ? fixture.php : await component('components/NodeUpdatePanel.vue', {
    '../composables/useNodeUpdates': fixture.nodeModule, '../utils/requestID': { createRequestID: () => 'node-request' },
    './Card.vue': box, './AppButton.vue': button, './BaseModal.vue': modal, './ErrorAlert.vue': errorAlert,
  });
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [
    { path: '/overview', component: box }, { path: '/runtime/php', component: box }, { path: '/runtime/node', component: box },
  ] });
  await router.push(options.startPath || `/runtime/${runtime}`);
  const wrapper = { setup: () => () => h('div', [h(notice), router.currentRoute.value.path === `/runtime/${runtime}` ? h(panel) : null]) };
  const mounted = mount(wrapper, router); await settle(); await settle();
  const dialog = title => nodes(mounted.root, item => item.props['data-modal'] === title)[0];
  return { ...fixture, ...mounted, router, dialog, failure };
}

for (const runtime of ['php', 'node']) {
  test(`${runtime} View error opens the existing repair confirmation from the destination and another page`, async t => {
    for (const startPath of [`/runtime/${runtime}`, '/overview']) {
      const fixture = await failureJourney(runtime, { recovery: runtime === 'php', nodeRecovery: runtime === 'node', startPath });
      const { root, app, requests, router, dialog } = fixture;
      try {
        await findButton(root, 'View error').props.onClick(); await settle();
        const details = dialog(`${runtime === 'php' ? 'PHP' : 'Node.js'} maintenance details`);
        assert.ok(details);
        assert.equal(findButton(details, 'Check for updates'), undefined, 'pending recovery must not be replaced with another package check');
        await findButton(details, 'Review repair').props.onClick(); await settle(); await settle();
        assert.equal(router.currentRoute.value.path, `/runtime/${runtime}`);
        assert.equal(dialog(`${runtime === 'php' ? 'PHP' : 'Node.js'} maintenance details`), undefined, 'error details close before confirmation');
        assert.ok(dialog(runtime === 'php' ? 'Repair PHP 8.4' : 'Repair Node.js'));
        const reviews = requests.filter(r => r.method === 'POST' && r.path.endsWith('/recovery-plan'));
        assert.equal(reviews.length, 1);
        assert.ok(reviews[0].path.includes(runtime === 'php' ? 'failed-operation' : 'node-failure'));
        assert.equal(reviews[0].body.kind, 'repair');
        assert.equal(requests.filter(r => r.method === 'POST' && /\/recovery\/apply$/.test(r.path)).length, 0, 'review must never start repair without confirmation');
      } finally { app.unmount(); }
    }
  });

  test(`${runtime} a failed preflight can start a check directly from its error details`, async t => {
    let accept;
    const options = { acceptGate: new Promise(resolve => { accept = resolve; }) };
    const { root, app, requests, dialog } = await failureJourney(runtime, options);
    t.after(() => app.unmount());
    await findButton(root, 'View error').props.onClick(); await settle();
    const title = `${runtime === 'php' ? 'PHP' : 'Node.js'} maintenance details`;
    const details = dialog(title);
    assert.equal(findButton(details, 'Review repair'), undefined);
    const action = findButton(details, 'Check for updates');
    const pending = action.props.onClick(); await settle();
    assert.equal(findButton(dialog(title), 'Check for updates').props.disabled, true);
    await action.props.onClick();
    assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/updates/check')).length, 1, 'double-click cannot enqueue a second check');
    accept(); await pending; await settle();
    assert.equal(dialog(title), undefined);
    const post = requests.find(r => r.method === 'POST' && r.path.endsWith('/updates/check'));
    assert.equal(post.path, `/api/v1/server/${runtime}/updates/check`);
    if (runtime === 'php') assert.equal(post.body.version, '8.4');
    assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/apply')).length, 0);
    assert.ok(text(root).includes('needs attention.'), 'closing details does not dismiss the persistent error');
  });

  test(`${runtime} rejected checks keep original diagnostics open and allow retry`, async t => {
    const options = { rejectStart: true };
    const { root, app, requests, dialog } = await failureJourney(runtime, options);
    t.after(() => app.unmount());
    await findButton(root, 'View error').props.onClick(); await settle();
    const title = `${runtime === 'php' ? 'PHP' : 'Node.js'} maintenance details`;
    await findButton(dialog(title), 'Check for updates').props.onClick(); await settle();
    assert.ok(dialog(title));
    assert.ok(text(dialog(title)).includes(runtime === 'php' ? 'Full original diagnostic' : 'Check for updates again'));
    options.rejectStart = false;
    await findButton(dialog(title), 'Check for updates').props.onClick(); await settle();
    assert.equal(dialog(title), undefined);
    assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/updates/check')).length, 2);
  });

  test(`${runtime} a failed repair review preserves the error until a confirmation is available`, async t => {
    const options = { recovery: runtime === 'php', nodeRecovery: runtime === 'node', rejectReview: true };
    const { root, app, requests, dialog } = await failureJourney(runtime, options);
    t.after(() => app.unmount());
    await findButton(root, 'View error').props.onClick(); await settle();
    const title = `${runtime === 'php' ? 'PHP' : 'Node.js'} maintenance details`;
    await findButton(dialog(title), 'Review repair').props.onClick(); await settle(); await settle();
    assert.ok(dialog(title), 'a failed read must not close the original diagnostic');
    assert.equal(dialog(runtime === 'php' ? 'Repair PHP 8.4' : 'Repair Node.js'), undefined);
    options.rejectReview = false;
    await findButton(dialog(title), 'Review repair').props.onClick(); await settle(); await settle();
    assert.equal(dialog(title), undefined);
    assert.ok(dialog(runtime === 'php' ? 'Repair PHP 8.4' : 'Repair Node.js'));
    assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/recovery/apply')).length, 0);
  });
}

test('Node toast details offer the same guarded repair review without starting repair', async t => {
  const { root, app, nodeUpdates, failure, requests, dialog, router } = await failureJourney('node', { nodeRecovery: true, startPath: '/overview' });
  t.after(() => app.unmount());
  nodeUpdates.detailsOperation.value = failure; await settle();
  const details = dialog('Node.js maintenance details');
  assert.ok(details);
  await findButton(details, 'Review repair').props.onClick(); await settle(); await settle();
  assert.equal(nodeUpdates.detailsOperation.value, null);
  assert.equal(router.currentRoute.value.path, '/runtime/node');
  assert.ok(dialog('Repair Node.js'));
  assert.equal(requests.filter(r => r.method === 'POST' && r.path.endsWith('/recovery/apply')).length, 0);
});

test('context actions use current recovery identity and do not guess from diagnostic wording', async () => {
  const fixture = await phpFixture({ recovery: true, nodeRecovery: true });
  const phpFailure = { id: 'failed-operation', kind: 'apply', status: 'failed', version: '8.4', message: 'Please check again' };
  fixture.updates.state.value = structuredClone(fixture.response);
  assert.equal(fixture.updates.failureAction(phpFailure), 'repair');
  assert.equal(fixture.updates.failureAction({ ...phpFailure, id: 'unrelated-head' }), null);
  assert.equal(fixture.updates.requestRepairReview('unrelated-head'), false);
  assert.equal(fixture.updates.requestRepairReview(phpFailure.id), true);
  assert.equal(fixture.updates.requestRepairReview(phpFailure.id), false);
  fixture.updates.finishRepairReview(phpFailure.id);
  fixture.updates.state.value.recoveries = [];
  assert.equal(fixture.updates.failureAction(phpFailure), 'check');
  assert.equal(fixture.updates.failureAction({ ...phpFailure, kind: 'repository_apply' }), null);
  assert.equal(fixture.updates.failureAction({ ...phpFailure, status: 'succeeded' }), null);
  fixture.nodeUpdates.state.value = structuredClone(fixture.nodeResponse);
  assert.equal(fixture.nodeUpdates.failureAction({ id: 'node-failure', kind: 'apply', status: 'failed' }), 'repair');
  assert.equal(fixture.nodeUpdates.failureAction({ id: 'unrelated-head', kind: 'apply', status: 'failed' }), null);
  assert.equal(fixture.nodeUpdates.requestRepairReview('unrelated-head'), false);
});

async function siteFeatureFixture(options = {}) {
  const requests = [], toasts = [];
  const features = { laravel_detected: true, laravel_version: '12', scheduler_enabled: false, scheduler_available: true,
    nightwatch_enabled: !!options.nightwatchEnabled, nightwatch_available: true, nightwatch_installed: true,
    queue_worker_enabled: false, queue_worker_available: true, horizon_enabled: false, horizon_available: false,
    octane_enabled: false, octane_available: false, maintenance_available: true, in_maintenance: false };
  const client = {
    async getSite() { return { id: 1, app_type: 'laravel', domain: 'example.test', deployment_strategy: 'standard' }; },
    async getSiteFeatures() { return structuredClone(features); },
    async getSiteDeployments() { return { items: [] }; }, async getSiteDaemons() { return []; },
    async getSiteCrons() { return []; }, async getSiteActivity() { return { items: [] }; }, async getMetrics() { return {}; },
    async toggleSiteScheduler(id, enabled) {
      requests.push({ feature: 'scheduler', id, enabled });
      if (options.requestGate) await options.requestGate;
      if (options.failure) throw new Error(options.failure);
      features.scheduler_enabled = enabled;
    },
    async toggleSiteNightwatch(id, enabled, token) {
      requests.push({ feature: 'nightwatch', id, enabled, token });
      if (options.requestGate) await options.requestGate;
      if (options.failure) throw new Error(options.failure);
      features.nightwatch_enabled = enabled;
    },
  };
  const toggle = await component('components/ToggleSwitch.vue');
  const overview = await component('views/site/SiteOverview.vue', {
    '../../components/StickySection.vue': box,
    '../../components/WordPressCronCard.vue': { render: () => null },
    'vue-router': { useRoute: () => ({ params: { id: '1' } }) },
    '../../composables/useToast': { useToast: () => ({ addToast: (...args) => toasts.push(args) }) },
    '../../composables/useConfirm': { useConfirm: () => ({ confirm: () => options.confirmGate || Promise.resolve(true) }) },
    '../../composables/useWebSocket': { useWebSocket: () => ({ logs: ref([]), connect() {}, disconnect() {} }) },
    '../../api/client': { apiClient: client }, '../../components/SkeletonLoader.vue': box,
    '../../components/StatusBadge.vue': box, '../../components/ToggleSwitch.vue': toggle,
    '../../components/QueueWorkerModal.vue': { render: () => null }, '../AddDaemonModal.vue': { render: () => null }, '../AddCronModal.vue': { render: () => null },
    '../../utils/sitePresentation': { siteTypeLabel: () => 'Laravel' }, '../../utils/deploymentPresentation': { deploymentTitle: () => 'Deployment' },
    '../../utils/apiError': await import('../src/utils/apiError.ts'),
  });
  return { overview, requests, toasts };
}
const featureSwitch = (root, label) => nodes(root, item => item.type === 'button' && item.props['aria-label'] === label)[0];

test('Scheduler toggle blocks duplicate confirmation/submission and refreshes confirmed state', async t => {
  let confirm, complete;
  const f = await siteFeatureFixture({ confirmGate: new Promise(resolve => { confirm = resolve; }), requestGate: new Promise(resolve => { complete = resolve; }) });
  const { root, app } = mount(f.overview); t.after(() => app.unmount()); await settle();
  const control = featureSwitch(root, 'Scheduler');
  const first = control.props.onClick(); await settle();
  assert.equal(control.props.disabled, true);
  control.props.onClick(); await settle(); assert.equal(f.requests.length, 0);
  confirm(true); await settle(); assert.equal(f.requests.length, 1);
  control.props.onClick(); await settle(); assert.equal(f.requests.length, 1);
  complete(); await first; await settle();
  assert.equal(control.props.disabled, false); assert.equal(control.props['aria-checked'], true);
});

test('Scheduler failed toggle preserves state, renders message rather than JSON, and permits retry', async t => {
  const f = await siteFeatureFixture({ failure: JSON.stringify({ code: 'maintenance_busy', message: 'Runtime maintenance access is busy. Try again shortly.' }) });
  const { root, app } = mount(f.overview); t.after(() => app.unmount()); await settle();
  const control = featureSwitch(root, 'Scheduler');
  await control.props.onClick(); await settle();
  assert.equal(control.props['aria-checked'], false); assert.equal(control.props.disabled, false);
  assert.deepEqual(f.toasts, [['Runtime maintenance access is busy. Try again shortly.', 'error']]);
  await control.props.onClick(); await settle(); assert.equal(f.requests.length, 2);
});

test('canceling Scheduler confirmation unlocks its control without sending a mutation', async t => {
  const f = await siteFeatureFixture({ confirmGate: Promise.resolve(false) });
  const { root, app } = mount(f.overview); t.after(() => app.unmount()); await settle();
  const control = featureSwitch(root, 'Scheduler'); await control.props.onClick(); await settle();
  assert.equal(f.requests.length, 0); assert.equal(control.props.disabled, false);
});

test('Nightwatch disable locks confirmation and request, then displays structured errors without JSON', async t => {
  let complete;
  const f = await siteFeatureFixture({ nightwatchEnabled: true, requestGate: new Promise(resolve => { complete = resolve; }), failure: '{"code":"maintenance_unavailable","message":"Runtime access could not be verified."}' });
  const { root, app } = mount(f.overview); t.after(() => app.unmount()); await settle();
  const control = featureSwitch(root, 'Nightwatch'); control.props.onClick(); await settle();
  assert.equal(control.props.disabled, true); control.props.onClick(); await settle(); assert.equal(f.requests.length, 1);
  complete(); await settle();
  assert.equal(control.props.disabled, false); assert.equal(control.props['aria-checked'], true);
  assert.deepEqual(f.toasts, [['Runtime access could not be verified.', 'error']]);
});

test('feature error presentation preserves text errors and rejects malformed message fields', async () => {
  const { apiErrorMessage } = await import('../src/utils/apiError.ts');
  assert.equal(apiErrorMessage(new Error('Site not found')), 'Site not found');
  assert.equal(apiErrorMessage(new Error('{"message":{"unsafe":"object"}}'), 'Failed'), 'Failed');
  assert.equal(apiErrorMessage(null, 'Failed'), 'Failed');
});

test('Nightwatch enable prevents repeated submits and refreshes state after acceptance', async t => {
  // Vue's text-input directive checks browser root classes during updates;
  // this renderer supplies inert roots because focus behavior is out of scope.
  const oldDocument = globalThis.Document, oldShadowRoot = globalThis.ShadowRoot;
  globalThis.Document = class {};
  globalThis.ShadowRoot = class {};
  t.after(() => { globalThis.Document = oldDocument; globalThis.ShadowRoot = oldShadowRoot; });
  let complete;
  const f = await siteFeatureFixture({ requestGate: new Promise(resolve => { complete = resolve; }) });
  const { root, app } = mount(f.overview); t.after(() => app.unmount()); await settle();
  const control = featureSwitch(root, 'Nightwatch'); await control.props.onClick(); await settle();
  const input = nodes(root, item => item.type === 'input')[0];
  input.props['onUpdate:modelValue']('nw-fixture'); await settle();
  const submit = findButton(root, 'Enable'); submit.props.onClick(); await settle();
  assert.equal(submit.props.disabled, true); assert.equal(control.props.disabled, true);
  submit.props.onClick(); await settle(); assert.equal(f.requests.length, 1);
  complete(); await settle();
  assert.equal(control.props['aria-checked'], true); assert.equal(control.props.disabled, false);
  assert.equal(findButton(root, 'Enable'), undefined);
});


test('Node check toast reports saved discovery outcomes without another request', async () => {
  for (const scenario of [
    { state: 'current', available: false, status: 'succeeded', title: 'No Node.js updates available', type: 'success' },
    { state: 'available', available: true, status: 'succeeded', title: 'A Node.js update is available', type: 'success' },
    { state: 'stale', available: true, status: 'succeeded', title: 'Node.js update check needs attention', type: 'warning' },
    { state: 'check_failed', available: false, status: 'failed', title: 'Couldn’t check Node.js updates', type: 'error' },
    { state: 'unsupported', available: false, status: 'succeeded', title: 'Node.js update check needs attention', type: 'warning' },
    { state: 'current', available: false, status: 'failed', title: 'Couldn’t check Node.js updates', type: 'error' },
    { state: 'current', available: false, status: 'succeeded', error: 'Configuration is blocked', title: 'Node.js update check needs attention', type: 'warning' },
    { state: 'current', available: false, status: 'succeeded', checked: 900, title: 'Node.js update check needs attention', type: 'warning' },
  ]) {
    const f = await phpFixture();
    const accepted = await f.nodeUpdates.check();
    await settle();
    f.nodeResponse.active_operation = null;
    f.nodeResponse.operations = [{ ...accepted, status: scenario.status, created_at: 1000, message: 'Original diagnostic' }];
    f.nodeResponse.snapshot = { state: scenario.state, update_available: scenario.available, migration_available: false,
      checked_at: scenario.checked ?? 1001, attempted_at: scenario.checked ?? 1001, error: scenario.error,
      candidate: scenario.available ? { version: '22.23.5' } : null };
    const before = f.requests.length;
    await f.nodeUpdates.refresh();
    assert.deepEqual(f.requests.slice(before).map(r => r.path), ['/api/v1/server/node/updates']);
    const result = f.toastUpdates.at(-1);
    assert.equal(result.title, scenario.title, JSON.stringify(scenario));
    assert.equal(result.type, scenario.type);
    assert.equal(result.action, null);
    if (scenario.status === 'failed') assert.equal(result.description, 'Original diagnostic');
    if (scenario.state === 'available') assert.match(result.description, /22\.23\.5/);
  }
});


test('Node panel preserves stale and failed states ahead of retained availability flags', async () => {
  for (const state of ['stale', 'check_failed']) {
    const f = await phpFixture();
    f.nodeResponse.snapshot = { state, checked_at: 1001, attempted_at: 1001, update_available: true,
      candidate: { version: '22.23.5' }, installed: { release: { version: '22.23.1' } } };
    const panel = await component('components/NodeUpdatePanel.vue', {
      '../composables/useNodeUpdates': f.nodeModule, '../utils/requestID': { createRequestID: () => 'node-request' },
      './Card.vue': box, './AppButton.vue': button, './BaseModal.vue': modal, './ErrorAlert.vue': errorAlert,
    });
    const { root, app } = mount(panel);
    try {
      await settle();
      assert.match(text(root), state === 'stale' ? /stored check is out of date/ : /last check failed/);
      assert.doesNotMatch(text(root), /No Node.js updates available|22\.23\.5 is available/);
    } finally { app.unmount(); }
  }
});
