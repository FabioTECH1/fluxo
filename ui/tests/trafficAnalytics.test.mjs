import test from 'node:test';
import { demoGitHubSiteAccess } from '../../ui-site/src/api/githubAccessDemo.ts';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { trafficAPISettings, trafficBytes, trafficCollected, trafficCoverageNote, trafficDate, trafficError, trafficGapBands, trafficIsDelayed, trafficMilliseconds, trafficPollDelay, trafficRetentionLabel, trafficRetentionOptions, trafficSeries, trafficStatus } from '../src/utils/trafficAnalytics.ts';

const start = '2026-11-19T00:00:00Z';
const end = '2026-11-19T05:00:00Z';
const bucket = (hour, requests, collection = 'enabled') => ({ hour: `2026-11-19T0${hour}:00:00Z`, metrics: requests === null ? null : { requests, page_views: requests / 2 }, collection, incomplete: false });
const site = { site_id: 1, enabled: true, effective_enabled: true, pending: false, status: 'collecting', backlog_bytes: 0, incomplete: false, dropped_bytes: 0 };

test('disabled, unavailable and missing hours split the series without inventing zero requests', () => {
  const result = trafficSeries([bucket(0, 10), bucket(1, 20), bucket(2, 900, 'disabled'), bucket(3, null, 'unknown'), bucket(4, 0)], 'requests', start, end);
  assert.equal(result.max, 20);
  assert.deepEqual(result.points.map(point => point.value), [10, 20, 0]);
  assert.equal(result.runs.length, 2);
  assert.equal(result.points.at(-1).y, 172);
  assert.equal(trafficSeries([bucket(0, 1), bucket(2, 3)], 'requests', start, end).runs.length, 2);
});

test('partial hours are standalone points so a line cannot cross an uncollected span', () => {
  const result = trafficSeries([bucket(0, 10), bucket(1, 15, 'partial'), bucket(2, 20)], 'requests', start, end);
  assert.equal(result.runs.length, 3);
  assert.equal(result.points[1].partial, true);
  assert.deepEqual(trafficSeries([bucket(0, null, 'unknown')], 'requests', start, end).points, []);
});

test('gap bands clamp to the displayed UTC range and discard invalid or external spans', () => {
  const bands = trafficGapBands([
    { from: '2026-11-18T20:00:00Z', to: '2026-11-19T01:00:00Z', reason: 'disabled' },
    { from: '2026-11-20T00:00:00Z', to: '2026-11-20T01:00:00Z', reason: 'outside' },
    { from: 'invalid', to: end, reason: 'invalid' },
  ], start, end);
  assert.equal(bands.length, 1);
  assert.equal(bands[0].x, 44);
  assert.equal(bands[0].width, 132);
  assert.deepEqual(trafficGapBands([], end, start), []);
});

test('pending and draining cannot be presented as successfully disabled or collecting', () => {
  assert.deepEqual(trafficStatus({ ...site, pending: true, enabled: false, status: 'draining' }), { label: 'Draining', variant: 'yellow' });
  assert.equal(trafficStatus({ ...site, effective_enabled: false, pending: true }).label, 'Applying change');
  assert.equal(trafficStatus({ ...site, error: 'Nginx reload failed' }).variant, 'red');
  assert.equal(trafficStatus({ ...site, effective_enabled: false }).label, 'Server collection off');
  assert.equal(trafficStatus({ ...site, incomplete: true }).label, 'Incomplete data');
});

test('formatting uses UTC and delayed status uses collector checks, including quiet sites', () => {
  assert.equal(trafficDate('2026-11-19T01:05:00+01:00'), '19 Nov, 00:05 UTC');
  assert.equal(trafficDate(null), 'Not yet processed');
  assert.equal(trafficBytes(1536), '1.5 KiB');
  assert.equal(trafficMilliseconds(1234.56), '1,234.6 ms');
  assert.equal(trafficCoverageNote, 'Based on requests reaching this server. Visits served entirely from a CDN or browser cache are not included.');
  assert.equal(trafficBytes(-1), '0 B');
  assert.equal(trafficError(new Error('{"message":"Nginx validation failed"}')), 'Nginx validation failed');
  const now = Date.parse('2026-11-19T12:00:00Z');
  assert.equal(trafficIsDelayed({ ...site, last_checked_at: '2026-11-19T11:59:50Z', last_processed_at: start }, now), false);
  assert.equal(trafficIsDelayed({ ...site, last_checked_at: '2026-11-19T11:58:00Z' }, now), true);
  assert.equal(trafficIsDelayed({ ...site, effective_enabled: false, backlog_bytes: 200 }, now), false);
  assert.equal(trafficPollDelay(false, false), null);
  assert.equal(trafficPollDelay(true, false), 15000);
  assert.equal(trafficPollDelay(false, true), 30000);
});

test('traffic client bypasses cache, accepts 202 state and preserves structured failure details', async t => {
  const source = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8');
  const dependencies = `const router={push(){}};const useToast=()=>({addToast(){}});const useAuthStore=()=>({token:'fixture-token'});const clearLogViewSnapshots=()=>{};\n`;
  const isolated = stripTypeScriptTypes(dependencies + source.replace(/^import .*;\r?\n/gm, ''));
  const { apiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  const calls = [];
  globalThis.fetch = async (url, options) => {
    calls.push([url, options]);
    if (url.endsWith('/api-settings')) return new Response(JSON.stringify({ settings: options.method === 'PUT' ? JSON.parse(options.body) : { prefixes: ['/api'], routes: [] }, effective_from: start }));
    if (url.includes('/sites/2/') && options.method === 'PUT') return new Response('{"message":"Nginx reload failed"}', { status: 500 });
    if (options.method === 'PUT') return new Response(JSON.stringify({ ...site, enabled: false, pending: true, status: 'draining' }), { status: 202 });
    if (options.method === 'DELETE') return new Response(JSON.stringify({ deleted: true, site }));
    return new Response(JSON.stringify({ site, sequence: calls.length }));
  };
  assert.notEqual((await apiClient.getSiteTrafficAnalytics(1, '7d')).sequence, (await apiClient.getSiteTrafficAnalytics(1, '7d')).sequence);
  assert.equal(calls[0][0], '/api/v1/sites/1/traffic-analytics?range=7d');
  assert.equal(calls[0][1].cache, 'no-store');
  assert.equal(calls[0][1].headers.Authorization, 'Bearer fixture-token');
  const pending = await apiClient.setSiteTrafficAnalytics(1, false);
  assert.equal(pending.pending, true);
  assert.equal(pending.effective_enabled, true);
  assert.equal(calls.at(-1)[1].body, '{"enabled":false}');
  await apiClient.setTrafficRetention(0);
  assert.equal(calls.at(-1)[0], '/api/v1/traffic-analytics');
  assert.equal(calls.at(-1)[1].body, '{"retention_days":0}');
  await assert.rejects(apiClient.setSiteTrafficAnalytics(2, false), /Nginx reload failed/);
  assert.equal((await apiClient.deleteSiteTrafficAnalytics(1)).deleted, true);
  assert.equal(calls.at(-1)[1].method, 'DELETE');
  assert.deepEqual((await apiClient.getSiteTrafficAPISettings(1)).settings, { prefixes: ['/api'], routes: [] });
  assert.equal(calls.at(-1)[1].cache, 'no-store');
  const settings = { prefixes: ['/api', '/v1'], routes: ['/api/users/:id'] };
  assert.deepEqual((await apiClient.setSiteTrafficAPISettings(1, settings)).settings, settings);
  assert.equal(calls.at(-1)[0], '/api/v1/sites/1/traffic-analytics/api-settings');
  assert.equal(calls.at(-1)[1].body, JSON.stringify(settings));
});

test('polling stops when hidden, deactivated or unmounted, and disabled collection has no timer', async () => {
  const source = await readFile(new URL('../src/composables/useTrafficPolling.ts', import.meta.url), 'utf8');
  const dependencies = `const hooks={};const onMounted=fn=>hooks.mounted=fn;const onActivated=fn=>hooks.activated=fn;const onDeactivated=fn=>hooks.deactivated=fn;const onUnmounted=fn=>hooks.unmounted=fn;\n`;
  const isolated = stripTypeScriptTypes(dependencies + source.replace(/^import .*;\r?\n/gm, '') + '\nexport { hooks };');
  const { useTrafficPolling, hooks } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const originalDocument = globalThis.document;
  const originalSetTimeout = globalThis.setTimeout;
  const originalClearTimeout = globalThis.clearTimeout;
  const timers = new Map();
  let callback, loads = 0, interval = 30000;
  globalThis.document = { hidden: false, addEventListener(_event, fn) { callback = fn; }, removeEventListener() { callback = null; } };
  globalThis.setTimeout = (fn, ms) => { const id = timers.size + 1; timers.set(id, { fn, ms }); return id; };
  globalThis.clearTimeout = id => timers.delete(id);
  try {
    const polling = useTrafficPolling(async () => { loads++; }, () => interval);
    hooks.mounted(); await Promise.resolve();
    hooks.activated(); await Promise.resolve();
    assert.equal(loads, 1); assert.equal(timers.size, 1);
    globalThis.document.hidden = true; callback();
    assert.equal(timers.size, 0);
    await polling.refresh(); assert.equal(loads, 1);
    globalThis.document.hidden = false; callback(); await Promise.resolve();
    assert.equal(loads, 2);
    hooks.deactivated(); assert.equal(timers.size, 0);
    await polling.refresh(); assert.equal(loads, 2);
    interval = null;
    hooks.activated(); await Promise.resolve();
    assert.equal(loads, 3); assert.equal(timers.size, 0);
    hooks.unmounted(); assert.equal(callback, null);
  } finally {
    globalThis.document = originalDocument;
    globalThis.setTimeout = originalSetTimeout;
    globalThis.clearTimeout = originalClearTimeout;
  }
});

test('demo starts off, applies switches asynchronously, preserves history and uses the same switch to restart', async t => {
  const source = await readFile(new URL('../../ui-site/src/api/mock.ts', import.meta.url), 'utf8');
  const isolated = stripTypeScriptTypes(`const demoGitHubSiteAccess=${demoGitHubSiteAccess.toString()};const appVersion='test';const createRequestID=()=> 'demo-id';const trafficAPISettings=${trafficAPISettings.toString()};\n` + source.replace(/^import .*\r?\n/gm, ''));
  const { MockApiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const client = new MockApiClient();
  const originalNow = Date.now;
  let now = Date.parse('2026-11-19T12:00:00Z');
  Date.now = () => now;
  t.after(() => { Date.now = originalNow; });
  const state = () => client._handle('/api/v1/traffic-analytics', 'GET');
  const report = () => client._handle('/api/v1/sites/1/traffic-analytics?range=7d', 'GET');
  const toggle = enabled => client._handle('/api/v1/sites/1/traffic-analytics', 'PUT', { enabled });
  assert.equal(state().master_enabled, false);
  assert.equal(report().buckets.every(row => row.metrics === null), true);
  assert.equal(client._handle('/api/v1/traffic-analytics', 'PUT', { enabled: true }).pending, true);
  now += 2000; assert.equal(state().pending, false);
  assert.equal(toggle(true).effective_enabled, false);
  now += 2000; state(); now += 1000;
  const initial = report(); assert.ok(initial.totals.requests > 0);
  const stopping = toggle(false); assert.equal(stopping.pending, true); assert.equal(stopping.effective_enabled, true);
  now += 2000; state();
  const retained = report().totals.requests;
  now += 3_600_000;
  assert.equal(report().totals.requests, retained);
  assert.equal(report().site.effective_enabled, false);
  assert.equal(toggle(true).pending, true);
  now += 2000; state(); now += 1000;
  assert.ok(report().totals.requests > retained);
  assert.ok(report().gaps.length >= 2);
  assert.equal(client._handle('/api/v1/sites/1/traffic-analytics', 'DELETE').deleted, true);
  assert.equal(report().site.enabled, true);
  assert.equal(report().totals.requests, 0);
  assert.equal(report().buckets.every(row => row.metrics === null), true);
});


test('API chart errors combine only HTTP 4xx and 5xx on the requests scale and retain coverage gaps', () => {
  const apiBucket = (hour, requests, status4, status5, collection = 'enabled') => ({ ...bucket(hour, requests, collection), metrics: requests === null ? null : { requests, status_1xx: 7, status_2xx: requests - status4 - status5 - 16, status_3xx: 9, status_4xx: status4, status_5xx: status5 } });
  const buckets = [apiBucket(0, 100, 4, 6), apiBucket(1, null, 0, 0, 'unknown'), apiBucket(2, 50, 2, 3, 'partial'), apiBucket(3, 0, 0, 0)];
  const requests = trafficSeries(buckets, 'requests', start, end);
  const errors = trafficSeries(buckets, 'http_errors', start, end, requests.max);
  assert.deepEqual(errors.points.map(point => point.value), [10, 5, 0]);
  assert.equal(errors.max, 100);
  assert.equal(errors.points[0].y, 172 - 0.1 * 152);
  assert.equal(errors.points[0].x, requests.points[0].x);
  assert.equal(errors.runs.length, 3);
  assert.equal(trafficCollected(buckets[1]), false);
  assert.equal(trafficCollected(buckets[3]), true);
  const pages = trafficSeries([bucket(0, 100)], 'page_views', start, end);
  assert.equal(pages.max, 50);
  assert.equal(pages.points[0].value, 50);
});

test('API settings accept only bounded literal prefixes and explicit unambiguous route templates', () => {
  assert.deepEqual(trafficAPISettings(' /api\n/v1\n', '/api/users/:id').settings, { prefixes: ['/api', '/v1'], routes: ['/api/users/:id'] });
  assert.deepEqual(trafficAPISettings('/', '/api/users/new\n/api/users/:id').errors, { prefixes: '', routes: '' });
  assert.deepEqual(trafficAPISettings('', '').settings, { prefixes: [], routes: [] });
  for (const value of ['/api/', '/api//users', '/api?token=private', '/api#fragment', '/api/*', '/api/:id', '/api/..', '/api/[a-z]', '/api/{id}', '/api/with space', '/api/\u0000']) assert.ok(trafficAPISettings(value, '').errors.prefixes, value);
  for (const value of ['/api/:id/:id', '/api/user-:id', '/api/:1id', '/api/*', '/api/:id/', '/api/./users']) assert.ok(trafficAPISettings('/api', value).errors.routes, value);
  assert.ok(trafficAPISettings('/api', '/api/users/:id\n/api/users/:name').errors.routes);
  assert.ok(trafficAPISettings('/api', '/api/:id/orders\n/api/users/:kind').errors.routes);
  assert.equal(trafficAPISettings('/api', '/api/users/:id\n/api/orders/:id').errors.routes, '');
  assert.ok(trafficAPISettings('/api\n/api', '').errors.prefixes);
  assert.ok(trafficAPISettings(Array.from({ length: 17 }, (_, i) => '/v' + i).join('\n'), '').errors.prefixes);
  assert.ok(trafficAPISettings('/api', Array.from({ length: 33 }, (_, i) => '/api/v' + i).join('\n')).errors.routes);
  assert.ok(trafficAPISettings('/' + 'é'.repeat(256), '').errors.prefixes);
});

test('chart presentation keeps API status errors distinct and Pages uses only stored estimated page views', async () => {
  const { parse, compileScript } = await import('@vue/compiler-sfc');
  const { createSSRApp } = await import('vue');
  const { renderToString } = await import('@vue/server-renderer');
  const source = await readFile(new URL('../src/components/TrafficHourlyChart.vue', import.meta.url), 'utf8');
  const { descriptor } = parse(source);
  const compiled = compileScript(descriptor, { id: 'traffic-chart-test', inlineTemplate: true });
  const moduleSource = stripTypeScriptTypes(compiled.content.replace(/from ['"]vue['"]/g, `from '${import.meta.resolve('vue')}'`).replace("from '../utils/trafficAnalytics'", `from '${new URL('../src/utils/trafficAnalytics.ts', import.meta.url).href}'`).replace(/^import type .*;\r?\n/gm, ''));
  const { default: chart } = await import('data:text/javascript;base64,' + Buffer.from(moduleSource).toString('base64'));
  const props = { from: start, to: end, gaps: [], buckets: [{ ...bucket(0, 100), metrics: { requests: 100, status_1xx: 1, status_2xx: 80, status_3xx: 9, status_4xx: 6, status_5xx: 4 } }] };
  const api = await renderToString(createSSRApp(chart, { ...props, mode: 'api' }));
  assert.match(api, /Hourly API requests and HTTP 4xx \+ 5xx responses, UTC/);
  assert.match(api, /stroke="var\(--color-red-500\)"/);
  assert.doesNotMatch(api, /Estimated page views|successful requests/);
  const pages = await renderToString(createSSRApp(chart, { ...props, buckets: [bucket(0, 100)], mode: 'pages' }));
  assert.match(pages, /Hourly estimated page views, UTC/);
  assert.match(pages, /50 Estimated page views/);
  assert.doesNotMatch(pages, /100 Requests|API requests|HTTP 4xx/);
});

test('demo API settings work while off and apply forward without reclassifying retained endpoint history', async t => {
  const source = await readFile(new URL('../../ui-site/src/api/mock.ts', import.meta.url), 'utf8');
  const isolated = stripTypeScriptTypes(`const demoGitHubSiteAccess=${demoGitHubSiteAccess.toString()};const appVersion='test';const createRequestID=()=> 'demo-id';const trafficAPISettings=${trafficAPISettings.toString()};\n` + source.replace(/^import .*\r?\n/gm, ''));
  const { MockApiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const originalNow = Date.now;
  let now = Date.parse('2026-11-19T12:00:00Z');
  Date.now = () => now;
  t.after(() => { Date.now = originalNow; });
  const client = new MockApiClient();
  const path = '/api/v1/sites/1/traffic-analytics';
  const report = () => client._handle(path + '?range=7d', 'GET');
  const save = settings => client._handle(path + '/api-settings', 'PUT', settings);
  assert.deepEqual(client._handle(path + '/api-settings', 'GET').settings, { prefixes: ['/api'], routes: [] });
  assert.equal(report().api.available_from, null);
  assert.equal(report().api.buckets.every(bucket => bucket.metrics === null), true);
  save({ prefixes: ['/api'], routes: ['/api/users/:id'] });
  assert.equal(report().site.enabled, false);
  assert.equal(report().master_enabled, false);
  assert.equal(report().api.available_from, null);
  assert.throws(() => save({ prefixes: ['/api/*'], routes: [] }), /literal paths/);
  client._handle('/api/v1/traffic-analytics', 'PUT', { enabled: true });
  now += 2000; client._handle('/api/v1/traffic-analytics', 'GET');
  client._handle(path, 'PUT', { enabled: true });
  now += 2000; client._handle('/api/v1/traffic-analytics', 'GET');
  now = Date.parse('2026-11-19T13:00:00Z');
  const before = report();
  assert.ok(before.api.totals.requests > 0);
  assert.ok(before.api.endpoints.some(endpoint => endpoint.path === '/api/users/:id'));
  assert.equal(before.api.endpoints.some(endpoint => endpoint.path === '/api/users/42'), false);
  assert.equal(before.api.totals.requests, Object.keys(before.api.totals).filter(key => /^status_/.test(key)).reduce((sum, key) => sum + before.api.totals[key], 0));
  const weighted = before.api.endpoints.reduce((sum, endpoint) => sum + endpoint.metrics.avg_request_time_ms * endpoint.metrics.requests, 0) / before.api.totals.requests;
  assert.ok(Math.abs(weighted - before.api.totals.avg_request_time_ms) < 0.000001);
  save({ prefixes: [], routes: ['/webhooks/:event'] });
  assert.deepEqual(report().api.totals, before.api.totals);
  assert.deepEqual(report().api.endpoints, before.api.endpoints);
  now += 3_600_000;
  const after = report();
  assert.ok(after.api.totals.requests > before.api.totals.requests);
  assert.ok(after.api.endpoints.some(endpoint => endpoint.path === '/webhooks/:event'));
  assert.deepEqual(after.api.buckets.find(bucket => bucket.hour === '2026-11-19T12:00:00.000Z').metrics, before.api.buckets.find(bucket => bucket.hour === '2026-11-19T12:00:00.000Z').metrics);
  assert.ok(after.api.gaps.length > 0);
  save({ prefixes: [], routes: [] });
  const retained = report().api.totals.requests;
  now += 3_600_000;
  const noMatch = report();
  assert.equal(noMatch.api.totals.requests, retained);
  assert.equal(noMatch.api.buckets.at(-1).metrics.requests, 0);
  assert.notEqual(noMatch.api.buckets.at(-1).collection, 'unknown');
  assert.equal(noMatch.site.effective_enabled, true);
});

test('retention controls expose finite and manual policies and retain storage/gap explanations', async () => {
  assert.deepEqual(trafficRetentionOptions.map(option => option.value), [30, 60, 90, 180, 365, 0]);
  assert.equal(trafficRetentionLabel(0), 'Keep until manually deleted');
  assert.equal(trafficRetentionLabel(90), '90-day retention');
  const { parse, compileScript } = await import('@vue/compiler-sfc');
  const { createSSRApp } = await import('vue');
  const { renderToString } = await import('@vue/server-renderer');
  const source = await readFile(new URL('../src/views/ObserveTraffic.vue', import.meta.url), 'utf8');
  const fixture = { master_enabled: false, pending: false, status: 'disabled', retention_days: 0, freshness_seconds: 30, sites: [] };
  // Render the actual compiled view with a loaded server response. Polling and
  // component shells are stubbed so the control can be checked without a browser.
  const { descriptor } = parse(source.replace('ref<TrafficState | null>(null)', `ref<TrafficState | null>(${JSON.stringify(fixture)})`).replace('ref<TrafficRetentionDays>(30)', 'ref<TrafficRetentionDays>(0)'));
  const compiled = compileScript(descriptor, { id: 'traffic-retention-view-test', inlineTemplate: true });
  const shells = `const shell = {setup:(_,ctx)=>()=>h('div',ctx.slots.default?.())};\n`;
  let moduleSource = compiled.content.replace(/from ['"]vue['"]/g, `from '${import.meta.resolve('vue')}'`)
    .replace(/^import (\w+) from ['"][^'"]+\.vue['"];?$/gm, 'const $1 = shell;')
    .replace(/^import \{ apiClient \}[^\n]+\n/gm, 'const apiClient = {};\n')
    .replace(/^import \{ useTrafficPolling \}[^\n]+\n/gm, 'const useTrafficPolling = () => ({refresh:async()=>{}});\n')
    .replace("from '../utils/trafficAnalytics'", `from '${new URL('../src/utils/trafficAnalytics.ts', import.meta.url).href}'`)
    .replace(/^import type .*;\r?\n/gm, '');
  moduleSource = `import { h } from '${import.meta.resolve('vue')}';\n` + shells + moduleSource;
  const { default: view } = await import('data:text/javascript;base64,' + Buffer.from(stripTypeScriptTypes(moduleSource)).toString('base64'));
  const app = createSSRApp(view);
  app.component('router-link', { setup: () => () => null });
  const rendered = await renderToString(app);
  assert.match(rendered, /id="traffic-retention"/);
  assert.match(rendered, /value="0">Keep until manually deleted/);
  assert.match(rendered, /Longer retention cannot recover deleted history or fill intervals/);
  assert.match(rendered, /collection pauses when storage is full/);
  assert.doesNotMatch(rendered, /30-day retention/);
});

test('demo retention is server-wide, rejects malformed values, and cannot resurrect expired or deleted history', async t => {
  const source = await readFile(new URL('../../ui-site/src/api/mock.ts', import.meta.url), 'utf8');
  const isolated = stripTypeScriptTypes(`const demoGitHubSiteAccess=${demoGitHubSiteAccess.toString()};const appVersion='test';const createRequestID=()=> 'demo-id';const trafficAPISettings=${trafficAPISettings.toString()};\n` + source.replace(/^import .*\r?\n/gm, ''));
  const { MockApiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const originalNow = Date.now;
  let now = Date.parse('2026-11-19T12:00:00Z');
  Date.now = () => now;
  t.after(() => { Date.now = originalNow; });
  const client = new MockApiClient();
  const state = () => client._handle('/api/v1/traffic-analytics', 'GET');
  const save = body => client._handle('/api/v1/traffic-analytics', 'PUT', body);
  assert.equal(state().retention_days, 30);
  for (const value of [null, '60', true, -1, 31, 366, 30.5]) assert.throws(() => save({ retention_days: value }), /retention_days/);
  assert.throws(() => save({ retention_days: 60, enabled: true }), /only/);
  for (const days of [60, 90, 180, 365, 0, 30]) {
    assert.equal(save({ retention_days: days }).retention_days, days);
    assert.equal(state().master_enabled, false);
    assert.equal(state().pending, false);
  }
  save({ enabled: true });
  now += 2000; state();
  client._handle('/api/v1/sites/1/traffic-analytics', 'PUT', { enabled: true });
  now += 2000; state();
  now += 3_600_000;
  save({ enabled: false });
  now += 2000; state();
  const oldSessions = structuredClone(client.trafficSites.get(1).sessions);
  save({ retention_days: 0 });
  now += 500 * 86_400_000;
  assert.equal(state().retention_days, 0);
  assert.deepEqual(client.trafficSites.get(1).sessions, oldSessions);
  save({ retention_days: 30 });
  assert.equal(client.trafficSites.get(1).sessions.length, 0);
  save({ retention_days: 0 });
  assert.equal(client.trafficSites.get(1).sessions.length, 0);
  client._handle('/api/v1/sites/1/traffic-analytics', 'DELETE');
  now += 3_600_000;
  assert.equal(client._handle('/api/v1/sites/1/traffic-analytics?range=30d', 'GET').totals.requests, 0);
});
