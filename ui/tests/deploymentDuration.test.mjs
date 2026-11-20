import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { createSSRApp, createRenderer, h, nextTick, ref } from 'vue';
import { renderToString } from '@vue/server-renderer';
import { loadVue } from './helpers/loadVue.mjs';
import { deploymentDuration } from '../src/utils/deploymentPresentation.ts';
import { demoGitHubSiteAccess } from '../../ui-site/src/api/githubAccessDemo.ts';
import { trafficAPISettings } from '../src/utils/trafficAnalytics.ts';

const start = '2026-11-20T12:00:00Z';
const at = seconds => Date.parse(start) + seconds * 1000;
const { default: Duration } = await import(await loadVue(new URL('../src/components/DeploymentDuration.vue', import.meta.url)));

test('duration uses observed start/finish, excluding queue wait and later edits', () => {
  const deployment = { status: 'success', created_at: '2026-11-19T00:00:00Z', updated_at: '2026-11-25T00:00:00Z', started_at: start, finished_at: new Date(at(84)).toISOString() };
  assert.equal(deploymentDuration(deployment, at(1000)), '1m 24s');
  assert.equal(deploymentDuration({ ...deployment, status: 'failed' }, at(1000)), '1m 24s');
  assert.equal(deploymentDuration({ ...deployment, finished_at: new Date(at(3723)).toISOString() }), '1h 2m 3s');
  assert.equal(deploymentDuration({ ...deployment, finished_at: start }), '0s');
});

test('historical, interrupted, pending and malformed timing never invent a duration', () => {
  for (const deployment of [
    { status: 'success', created_at: start, updated_at: new Date(at(84)).toISOString() },
    { status: 'failed', started_at: start, finished_at: null },
    { status: 'pending', started_at: start },
    { status: 'success', started_at: 'invalid', finished_at: start },
    { status: 'success', started_at: start, finished_at: 'invalid' },
    { status: 'success', started_at: new Date(at(1)).toISOString(), finished_at: start },
    { status: 'running', started_at: start, finished_at: new Date(at(84)).toISOString() },
    { status: 'running', started_at: new Date(at(100)).toISOString() },
  ]) assert.equal(deploymentDuration(deployment, at(84)), '—');
});

test('running value increases without extra wording and terminal duration stays fixed', () => {
  const running = { status: 'running', started_at: start, finished_at: null };
  assert.equal(deploymentDuration(running, at(46)), '46s');
  assert.equal(deploymentDuration(running, at(60)), '1m 0s');
  assert.equal(deploymentDuration(running, at(67)), '1m 7s');
  const completed = { ...running, status: 'success', finished_at: new Date(at(67)).toISOString() };
  assert.equal(deploymentDuration(completed, at(500)), '1m 7s');
});

test('actual duration component renders accessible unavailable state and reacts to clock/result changes', async () => {
  const html = await renderToString(createSSRApp(Duration, { deployment: { status: 'failed', started_at: start }, now: at(84) }));
  assert.match(html, /Duration unavailable/);
  assert.match(html, /gap-1/);
  const node = (tag, text = '') => ({ tag, text, children: [], props: {}, parent: null });
  const renderer = createRenderer({ createElement: node, createText: text => node('#text', text), createComment: text => node('#comment', text),
    insert(child, parent) { child.parent = parent; parent.children.push(child); }, remove() {}, setText(n, t) { n.text = t; }, setElementText(n, t) { n.text = t; },
    parentNode: n => n.parent, nextSibling: () => null, patchProp(n, k, _old, v) { n.props[k] = v; } });
  const now = ref(at(46));
  const deployment = ref({ status: 'running', started_at: start, finished_at: null });
  const root = node('root');
  const app = renderer.createApp({ render: () => h(Duration, { deployment: deployment.value, now: now.value }) });
  const text = n => n.text + n.children.map(text).join('');
  app.mount(root);
  assert.equal(text(root), 'Duration46s');
  now.value = at(67); await nextTick();
  assert.equal(text(root), 'Duration1m 7s');
  deployment.value = { ...deployment.value, status: 'failed', finished_at: new Date(at(67)).toISOString() }; await nextTick();
  now.value = at(100); await nextTick();
  assert.equal(text(root), 'Duration1m 7s');
  assert.doesNotMatch(text(root), /so far/);
  app.unmount();
});

test('demo captures distinct pending, running and completed boundaries while old fixtures remain unavailable', async t => {
  const source = await readFile(new URL('../../ui-site/src/api/mock.ts', import.meta.url), 'utf8');
  const isolated = stripTypeScriptTypes(`const demoGitHubSiteAccess=${demoGitHubSiteAccess.toString()};const appVersion='test';const createRequestID=()=> 'demo-id';const trafficAPISettings=${trafficAPISettings.toString()};\n` + source.replace(/^import .*\r?\n/gm, ''));
  const { MockApiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const timers = [];
  const previousWindow = globalThis.window;
  globalThis.window = { location: { search: '' }, setTimeout(fn, ms) { timers.push({ fn, ms }); }, dispatchEvent() {} };
  t.after(() => { globalThis.window = previousWindow; });
  const client = new MockApiClient();
  const list = () => client._handle('/api/v1/sites/1/deployments', 'GET').data;
  assert.equal(deploymentDuration(list()[0]), '1m 24s');
  assert.equal(deploymentDuration(list()[1]), '—');
  client._handle('/api/v1/sites/1/deploy', 'POST');
  const deployment = list()[0];
  assert.equal(deployment.status, 'pending');
  assert.equal(deployment.started_at, null); assert.equal(deployment.finished_at, null);
  timers.find(timer => timer.ms === 500).fn();
  assert.equal(deployment.status, 'running'); assert.ok(deployment.started_at); assert.equal(deployment.finished_at, null);
  timers.find(timer => timer.ms === 2500).fn();
  assert.equal(deployment.status, 'success'); assert.ok(deployment.finished_at);
  const failed = client._handle('/api/v1/sites/4/deployments', 'GET').data[0];
  const previous = [failed.started_at, failed.finished_at];
  client._handle(`/api/v1/sites/4/deployments/${failed.id}/dismiss`, 'POST');
  assert.deepEqual([failed.started_at, failed.finished_at], previous);
});
