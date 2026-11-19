import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { trafficBytes, trafficDate, trafficError, trafficGapBands, trafficIsDelayed, trafficPollDelay, trafficSeries, trafficStatus } from '../src/utils/trafficAnalytics.ts';

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
  await assert.rejects(apiClient.setSiteTrafficAnalytics(2, false), /Nginx reload failed/);
  assert.equal((await apiClient.deleteSiteTrafficAnalytics(1)).deleted, true);
  assert.equal(calls.at(-1)[1].method, 'DELETE');
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
  const isolated = stripTypeScriptTypes(`const appVersion='test';const createRequestID=()=> 'demo-id';\n` + source.replace(/^import .*\r?\n/gm, ''));
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
