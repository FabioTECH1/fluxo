import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { parse, compileScript } from '@vue/compiler-sfc';
import * as vue from 'vue';
import * as vueRouter from 'vue-router';

const { createRenderer, h, nextTick, ref } = vue;
const { createRouter, createMemoryHistory } = vueRouter;
let moduleID = 0;
const box = { setup: (_, { slots }) => () => h('div', slots.default?.()) };
// Browser layout/transitions and modal focus management are outside this suite.
// All notification components, AppButton, RouterLink and router matching are real.
const modal = {
  props: ['modelValue', 'title'],
  setup: (props, { slots }) => () => props.modelValue
    ? h('section', { role: 'dialog', 'aria-label': props.title }, [slots.default?.(), slots.footer?.()]) : null,
};
async function loadModule(source, bindings = {}) {
  const key = `notificationTest${++moduleID}`;
  globalThis[key] = { vue: { ...vue, TransitionGroup: box }, 'vue-router': vueRouter, ...bindings };
  const javascript = stripTypeScriptTypes(source).replace(/^import ([\s\S]+?) from ['"]([^'"]+)['"];?$/gm, (_, imported, path) => {
    assert.ok(path in globalThis[key], `Missing test binding: ${path}`);
    if (imported.startsWith('{')) return `const ${imported.replace(/\bas\b/g, ':')} = globalThis.${key}[${JSON.stringify(path)}];`;
    return `const ${imported} = globalThis.${key}[${JSON.stringify(path)}];`;
  });
  try {
    return await import('data:text/javascript;base64,' + Buffer.from(javascript).toString('base64'));
  } finally {
    delete globalThis[key];
  }
}
async function component(path, bindings = {}) {
  const { descriptor } = parse(await readFile(new URL(`../src/components/${path}.vue`, import.meta.url), 'utf8'));
  const compiled = compileScript(descriptor, { id: path, inlineTemplate: true });
  return (await loadModule(compiled.content, bindings)).default;
}
function node(type, text = '') { return { type, text, children: [], props: {}, parent: null }; }
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
function nodes(root, match) { return [...(match(root) ? [root] : []), ...root.children.flatMap(child => nodes(child, match))]; }
function text(root) { return (root.type === '#comment' ? '' : root.text) + root.children.map(text).join(''); }
const buttons = (root, label) => nodes(root, item => item.type === 'button' && text(item).trim() === label);
const links = (root, label) => nodes(root, item => item.type === 'a' && text(item).trim() === label);
const labelled = (root, label) => nodes(root, item => item.props['aria-label'] === label);
async function settle() { await new Promise(resolve => setImmediate(resolve)); await nextTick(); }
async function navigate(router, to) { await router.push(to); await settle(); }
async function mount(view, path = '/overview', base = '/') {
  const router = createRouter({ history: createMemoryHistory(base), routes: [
    { path: '/overview', component: box },
    { path: '/runtime', component: box, children: [
      { path: '', redirect: '/runtime/php' },
      { path: 'php', name: 'php', alias: '/php-alias', component: box, children: [{ path: 'details', component: box }] },
      { path: 'node', component: box, children: [{ path: 'details', component: box }] },
      { path: 'php-other', component: box }, { path: 'node-other', component: box },
    ] },
    { path: '/sites/:id/deployments', name: 'deployments', alias: '/projects/:id/deployments', component: box },
    { path: '/settings/source-control', component: box },
    { path: '/:pathMatch(.*)*', component: box },
  ] });
  await navigate(router, path);
  const root = node('root');
  const app = renderer.createApp(view);
  app.use(router);
  app.mount(root);
  await settle();
  return { root, app, router };
}
const navigation = await loadModule(await readFile(new URL('../src/composables/useNotificationNavigation.ts', import.meta.url), 'utf8'));
const toast = await loadModule(await readFile(new URL('../src/composables/useToast.ts', import.meta.url), 'utf8'));
const icons = Object.fromEntries(['ArrowUpTrayIcon', 'XCircleIcon', 'XMarkIcon', 'ArrowPathIcon', 'CheckCircleIcon', 'ExclamationTriangleIcon', 'InformationCircleIcon'].map(name => [name, box]));
const common = {
  '@heroicons/vue/24/outline': icons,
  '../composables/useNotificationNavigation': navigation,
  '../composables/useToast': toast,
  './BaseModal.vue': modal,
};
const appButton = await component('AppButton', common);
common['./AppButton.vue'] = appButton;
common['./NoticeBanner.vue'] = await component('NoticeBanner', common);
common['./UpdateNotice.vue'] = await component('UpdateNotice', common);
common['./PersistentFailureBanner.vue'] = await component('PersistentFailureBanner', common);
const toastContainer = await component('ToastContainer', common);
const errorAlert = await component('ErrorAlert', common);

async function runtimeFixture(runtime) {
  let starts = 0; let stops = 0; let dismissals = 0; let closes = 0;
  const state = ref(runtime === 'php'
    ? { branches: [{ version: '8.4', notice: true, update_available: true }], failures: [] }
    : { snapshot: { notice: true, update_available: true, candidate: { version: '24.16.0' } } });
  const attention = ref(null);
  const detailsOperation = ref(null);
  const updates = { state, attention, detailsOperation, failureAction: () => null,
    repairReviewRequest: ref(null), busy: ref(false), locked: ref(false),
    start: () => starts++, stop: () => stops++,
    dismiss: async () => { dismissals++; }, closeDetails() { closes++; detailsOperation.value = null; } };
  const name = runtime === 'php' ? 'PHP' : 'Node';
  const view = await component(`${name}UpdateNotice`, { ...common,
    [`../composables/use${name}Updates`]: { [`use${name}Updates`]: () => updates, [`${runtime}UpdateError`]: String },
  });
  return { view, state, attention, detailsOperation, updates,
    counts: () => ({ starts, stops, dismissals, closes }),
    availability: runtime === 'php' ? 'PHP updates' : 'Node.js updates',
    dismissLabel: runtime === 'php' ? 'Dismiss PHP update notifications' : 'Dismiss Node.js update notification',
    actionLabel: runtime === 'php' ? 'Review updates' : 'Review update',
  };
}

for (const runtime of ['php', 'node']) {
  test(`${runtime} availability survives destination and route changes, preserving dismiss, polling and eligibility`, async t => {
    const fixture = await runtimeFixture(runtime);
    const { root, app, router } = await mount(fixture.view, '/overview', '/demo');
    t.after(() => app.unmount());
    assert.equal(labelled(root, fixture.availability).length, 1);
    assert.equal(links(root, fixture.actionLabel).length, 1);
    const message = text(root);
    for (const path of [`/runtime/${runtime}`, `/runtime/${runtime}/`]) {
      await navigate(router, path);
      assert.equal(labelled(root, fixture.availability).length, 1);
      assert.equal(text(root), message.replace(fixture.actionLabel, ''));
      assert.equal(links(root, fixture.actionLabel).length, 0);
      assert.equal(labelled(root, fixture.dismissLabel).length, 1);
    }
    await labelled(root, fixture.dismissLabel)[0].props.onClick(); await settle();
    assert.equal(fixture.counts().dismissals, 1);
    // Descendants are distinct pages, never hidden by a loose path prefix.
    for (const path of [`/runtime/${runtime}/details`, `/runtime/${runtime}-other`, '/overview', runtime === 'php' ? '/runtime/node' : '/runtime/php']) {
      await navigate(router, path);
      assert.equal(labelled(root, fixture.availability).length, 1);
      assert.equal(links(root, fixture.actionLabel).length, 1);
    }
    const saved = runtime === 'php' ? fixture.state.value.branches[0] : fixture.state.value.snapshot;
    saved.update_available = false; await nextTick();
    assert.equal(labelled(root, fixture.availability).length, 0, 'stale availability is still ineligible');
    saved.update_available = true; saved.notice = false; await nextTick();
    assert.equal(labelled(root, fixture.availability).length, 0, 'dismissed availability is still ineligible');
    assert.deepEqual(fixture.counts(), { starts: 1, stops: 0, dismissals: 1, closes: 0 });
    app.unmount();
    assert.equal(fixture.counts().stops, 1);
  });

  test(`${runtime} persistent errors retain details and controls while only destination links disappear`, async t => {
    const fixture = await runtimeFixture(runtime);
    const failure = { id: 'failed-apply', kind: 'apply', version: '8.4', status: 'recovery_required', message: 'Recovery is required after the failed update.', output: 'Original package failure' };
    if (runtime === 'php') fixture.state.value.failures.push(failure);
    else fixture.attention.value = failure;
    const { root, app, router } = await mount(fixture.view, `/runtime/${runtime}/`);
    t.after(() => app.unmount());
    const openLabel = runtime === 'php' ? 'Open PHP' : 'Open Node.js';
    assert.ok(text(root).includes('needs attention.'));
    assert.ok(text(root).includes(failure.message));
    assert.equal(links(root, openLabel).length, 0);
    assert.equal(labelled(root, fixture.availability).length, runtime === 'php' ? 1 : 0, 'existing Node attention precedence remains');
    buttons(root, 'View error')[0].props.onClick(); await nextTick();
    assert.equal(nodes(root, item => item.props.role === 'dialog').length, 1);
    assert.ok(text(root).includes(runtime === 'php' ? failure.output : failure.message));
    assert.equal(labelled(root, 'Copy error output').length, 1);
    assert.equal(buttons(root, 'Close').length, 1);
    assert.equal(buttons(root, 'Dismiss').length, 0, 'runtime failure remains nondismissible');
    assert.equal(links(root, openLabel).length, 0, 'modal destination action is also redundant');
    await navigate(router, '/overview');
    assert.equal(links(root, openLabel).length, 2, 'banner and open modal both regain navigation');
    await navigate(router, `/runtime/${runtime}`);
    assert.equal(links(root, openLabel).length, 0);
    buttons(root, 'Close')[0].props.onClick(); await nextTick();
    assert.equal(nodes(root, item => item.props.role === 'dialog').length, 0);
    assert.ok(text(root).includes(failure.message), 'closing details keeps the error banner');
  });

  test(`${runtime} destination notice keeps pending dismiss state and reports dismissal errors`, async t => {
    const fixture = await runtimeFixture(runtime);
    const api = toast.useToast();
    let reject; let calls = 0;
    fixture.updates.dismiss = () => { calls++; return new Promise((_, fail) => { reject = fail; }); };
    const view = { setup: () => () => h('div', [h(fixture.view), h(toastContainer)]) };
    const { root, app } = await mount(view, `/runtime/${runtime}`);
    t.after(() => { app.unmount(); for (const notice of [...api.toasts.value]) api.removeToast(notice.id); });
    const pending = labelled(root, fixture.dismissLabel)[0].props.onClick(); await nextTick();
    assert.equal(labelled(root, fixture.dismissLabel)[0].props.disabled, true);
    await labelled(root, fixture.dismissLabel)[0].props.onClick(); assert.equal(calls, 1, 'duplicate dismiss is guarded');
    reject(new Error('Dismissal failed')); await pending; await settle();
    assert.equal(labelled(root, fixture.dismissLabel)[0].props.disabled, false);
    assert.equal(labelled(root, fixture.availability).length, 1);
    assert.ok(text(root).includes('Dismissal failed'));
    assert.ok(nodes(root, item => item.props.role === 'alert').length, 'error toast stays visible on destination');
  });
}

test('Node recovery details preserve Close on destination and regain navigation elsewhere', async t => {
  const fixture = await runtimeFixture('node');
  fixture.detailsOperation.value = { id: 'restored', status: 'rolled_back', message: 'Previous Node.js runtime restored' };
  const { root, app, router } = await mount(fixture.view, '/runtime/node');
  t.after(() => app.unmount());
  assert.ok(text(root).includes('Previous Node.js runtime restored'));
  assert.equal(links(root, 'Open Node.js').length, 0);
  assert.equal(buttons(root, 'Close').length, 1);
  await navigate(router, '/runtime/php');
  assert.equal(links(root, 'Open Node.js').length, 1);
  await navigate(router, '/runtime/node/');
  assert.equal(links(root, 'Open Node.js').length, 0);
  buttons(root, 'Close')[0].props.onClick(); await nextTick();
  assert.equal(fixture.counts().closes, 1);
  assert.equal(nodes(root, item => item.props.role === 'dialog').length, 0);
});

test('notification destination matching follows router aliases, params and exact query/hash views', async t => {
  const to = ref('/runtime/php');
  const view = { setup: () => () => h(common['./UpdateNotice.vue'], { message: 'Saved notice', to: to.value, dismissLabel: 'Dismiss notice' }) };
  const { root, app, router } = await mount(view, '/php-alias/');
  t.after(() => app.unmount());
  const shown = () => links(root, 'Review updates').length === 1;
  assert.equal(shown(), false, 'aliases and trailing slashes resolve to the same page');
  to.value = { name: 'php' }; await nextTick(); assert.equal(shown(), false);
  to.value = '/RUNTIME/PHP/'; await nextTick(); assert.equal(shown(), false, 'router default case-insensitive matching');
  to.value = '/runtime'; await nextTick(); assert.equal(shown(), true, 'redirect links are kept conservatively without evaluating redirect functions');
  to.value = '/runtime/php/details'; await nextTick(); assert.equal(shown(), true, 'a child page is different');
  await navigate(router, '/sites/7/deployments?deployment_id=11&tab=logs#output');
  for (const [destination, expected, reason] of [
    ['/sites/7/deployments?tab=logs&deployment_id=11#output', false, 'query key order is irrelevant'],
    [{ name: 'deployments', params: { id: 7 }, query: { deployment_id: 11, tab: 'logs' }, hash: '#output' }, false, 'named routes and numeric raw values normalize'],
    ['/projects/7/deployments/?tab=logs&deployment_id=11#output', false, 'alias with params'],
    ['/sites/8/deployments?tab=logs&deployment_id=11#output', true, 'different site param'],
    ['/sites/7/deployments?tab=logs&deployment_id=12#output', true, 'different deployment log'],
    ['/sites/7/deployments?tab=summary&deployment_id=11#output', true, 'different query tab'],
    ['/sites/7/deployments?tab=logs&deployment_id=11#summary', true, 'different hash section'],
    ['/sites/7/deployments', true, 'clearing query/hash can change the selected view'],
  ]) {
    to.value = destination; await nextTick(); assert.equal(shown(), expected, reason);
    assert.ok(text(root).includes('Saved notice')); assert.equal(labelled(root, 'Dismiss notice').length, 1);
  }
  await navigate(router, '/sites/7/deployments?filter=one&filter=two');
  to.value = '/sites/7/deployments?filter=one&filter=two'; await nextTick(); assert.equal(shown(), false);
  to.value = '/sites/7/deployments?filter=two&filter=one'; await nextTick(); assert.equal(shown(), true, 'repeated-value order may be meaningful');
});

for (const type of ['info', 'warning', 'error', 'success', 'loading']) {
  test(`${type} toast remains visible while both responsive navigation actions follow route changes`, async t => {
    const api = toast.useToast();
    const id = api.showToast({ type, title: `${type} notice`, description: 'Details '.repeat(40), duration: null,
      action: { label: 'View logs', to: '/sites/7/deployments?deployment_id=11', dismissOnClick: false } });
    const { root, app, router } = await mount(toastContainer, '/sites/7/deployments?deployment_id=11');
    t.after(() => { app.unmount(); api.removeToast(id); });
    assert.ok(text(root).includes(`${type} notice`));
    assert.equal(nodes(root, item => item.props.role === (type === 'error' ? 'alert' : 'status')).length, 1);
    assert.equal(links(root, 'View logs').length, 0);
    assert.equal(buttons(root, 'View logs').length, 0, 'hidden navigation must not fall through to a callback button');
    assert.equal(labelled(root, `Dismiss ${type} notice`).length, type === 'loading' ? 0 : 1, 'loading stays nondismissible by default');
    buttons(root, 'Show details')[0].props.onClick(); await nextTick();
    assert.equal(buttons(root, 'Hide details').length, 1, 'expansion stays available');
    for (const path of ['/sites/7/deployments?deployment_id=12', '/sites/8/deployments?deployment_id=11', '/overview']) {
      await navigate(router, path);
      assert.equal(links(root, 'View logs').length, 2, 'desktop and mobile navigation remain available for different views');
    }
    await navigate(router, '/sites/7/deployments/?deployment_id=11');
    assert.equal(links(root, 'View logs').length, 0);
    assert.ok(api.toasts.value.some(item => item.id === id), 'route changes never remove the toast');
    if (type === 'loading') api.removeToast(id);
    else labelled(root, `Dismiss ${type} notice`)[0].props.onClick();
    await nextTick();
    assert.equal(api.toasts.value.some(item => item.id === id), false);
  });
}

test('callback toast actions, failure details and ordinary controls remain usable on destination', async t => {
  const api = toast.useToast(); let clicks = 0; let dismissals = 0;
  const id = api.showToast({ type: 'warning', title: 'Previous runtime restored', duration: null,
    action: { label: 'Review details', onClick: () => clicks++, dismissOnClick: false } });
  const view = { setup: () => () => h('div', [h(toastContainer),
    h(errorAlert, { message: 'Original failure', details: 'Complete diagnostic' }),
    h(common['./PersistentFailureBanner.vue'], { title: 'Other failure', output: 'Diagnostic', onDismiss: () => dismissals++ }),
    h(appButton, { to: '/runtime/php' }, () => 'Ordinary navigation'),
  ]) };
  const { root, app, router } = await mount(view, '/runtime/php');
  t.after(() => { app.unmount(); api.removeToast(id); });
  assert.equal(buttons(root, 'Review details').length, 2);
  buttons(root, 'Review details')[0].props.onClick(); await nextTick();
  assert.equal(clicks, 1); assert.ok(api.toasts.value.some(item => item.id === id));
  assert.equal(links(root, 'Ordinary navigation').length, 1, 'AppButton navigation outside notifications is unaffected');
  buttons(root, 'Show details')[0].props.onClick(); await nextTick();
  assert.ok(text(root).includes('Complete diagnostic'));
  buttons(root, 'View error')[0].props.onClick(); await nextTick();
  buttons(root, 'Dismiss')[0].props.onClick(); assert.equal(dismissals, 1);
  await navigate(router, '/runtime/php/');
  assert.equal(buttons(root, 'Review details').length, 2);
});

test('Fluxo release notice keeps external navigation and existing eligibility/dismissal', async t => {
  const storage = new Map();
  const original = globalThis.localStorage;
  globalThis.localStorage = { getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value) };
  t.after(() => { globalThis.localStorage = original; });
  const status = { check_available: true, update_available: true, current_version: '1.0.0', latest_version: '1.0.1', release_url: 'https://github.com/FabioTECH1/fluxo/releases/tag/v1.0.1' };
  const view = await component('UpdateAvailableBanner', { ...common, '../api/client': { apiClient: { getUpdateStatus: async () => status } } });
  const { root, app, router } = await mount(view, '/runtime/php');
  t.after(() => app.unmount());
  assert.ok(text(root).includes('Fluxo v1.0.1 is available.'));
  assert.equal(links(root, 'Release notes')[0].props.href, status.release_url);
  await navigate(router, '/runtime/node');
  assert.equal(links(root, 'Release notes').length, 1);
  labelled(root, 'Dismiss Fluxo 1.0.1 update notice')[0].props.onClick(); await nextTick();
  assert.equal(labelled(root, 'Fluxo update').length, 0);
  app.unmount();
  const dismissed = await mount(view); assert.equal(labelled(dismissed.root, 'Fluxo update').length, 0); dismissed.app.unmount();
  storage.clear(); status.check_available = false;
  const unavailable = await mount(view); assert.equal(labelled(unavailable.root, 'Fluxo update').length, 0); unavailable.app.unmount();
});
