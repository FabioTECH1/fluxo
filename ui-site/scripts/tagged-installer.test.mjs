import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('../../functions/releases/[tag]/install.sh.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
});
const { onRequest } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

function request(tag, method = 'GET', query = '') {
  return onRequest({
    request: new Request(`https://fluxo.fabiotech.dev/releases/example/install.sh${query}`, { method }),
    params: { tag },
  });
}

test('stable, bridge and prerelease URLs redirect to their exact original assets', async () => {
  for (const tag of ['v0.4.56', 'v0.4.55', 'v0.4.55-base-test.3', 'v0.4.48-bridge.2', 'v1.2.3+build.4']) {
    const response = await request(tag);
    assert.equal(response.status, 302);
    assert.equal(response.headers.get('Location'),
      `https://github.com/FabioTECH1/fluxo/releases/download/${encodeURIComponent(tag)}/install.sh`);
    assert.equal(await response.text(), '');
    assert.equal(response.headers.get('X-Content-Type-Options'), 'nosniff');
  }
});

test('HEAD returns the same redirect without a response body', async () => {
  const response = await request('v0.4.56', 'HEAD');
  assert.equal(response.status, 302);
  assert.equal(response.headers.get('Location'), (await request('v0.4.56')).headers.get('Location'));
  assert.equal(await response.text(), '');
});

test('invalid, injected and oversized tags never produce a download redirect', async () => {
  for (const tag of [undefined, [], ['v0.4.56'], '', 'latest', '0.4.56', '../v0.4.56',
    'v0.4.56/../../other', 'v0.4.56%2Fother', 'v0.4.56?other', 'v0.4.56#other',
    'v0.4.56\r\nLocation: https://example.invalid', 'v0.4.56 ', 'v0.4.56\n', 'v0.4.56-☃',
    `v1.2.3-${'x'.repeat(100)}`]) {
    const response = await request(tag);
    assert.equal(response.status, 404, String(tag));
    assert.equal(response.headers.get('Location'), null);
    assert.equal(response.headers.get('Cache-Control'), 'no-store');
  }
  assert.equal(await (await request('invalid', 'HEAD')).text(), '');
});

test('a syntactically valid unknown tag never falls back to latest', async () => {
  const response = await request('v999999.0.0');
  assert.equal(response.status, 302);
  assert.equal(response.headers.get('Location'),
    'https://github.com/FabioTECH1/fluxo/releases/download/v999999.0.0/install.sh');
});

test('query values cannot override the tag, repository or redirect target', async () => {
  const response = await request('v0.4.55', 'GET', '?tag=v0.4.56&url=https://example.invalid&token=not-forwarded');
  assert.equal(response.status, 302);
  assert.equal(response.headers.get('Location'),
    'https://github.com/FabioTECH1/fluxo/releases/download/v0.4.55/install.sh');
});

test('mutating methods fail without a redirect', async () => {
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS']) {
    const response = await request('v0.4.56', method);
    assert.equal(response.status, 405);
    assert.equal(response.headers.get('Allow'), 'GET, HEAD');
    assert.equal(response.headers.get('Location'), null);
    assert.equal(response.headers.get('Cache-Control'), 'no-store');
  }
});

test('invocation rules cover tagged installers while ordinary installs stay static', async () => {
  const routes = JSON.parse(await readFile(new URL('../public/_routes.json', import.meta.url), 'utf8'));
  const matches = (pattern, path) => new RegExp(`^${pattern.split('*').map(part =>
    part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('.*')}$`).test(path);
  const invokes = path => routes.include.some(pattern => matches(pattern, path))
    && !routes.exclude.some(pattern => matches(pattern, path));
  assert.equal(invokes('/api/v1/releases/latest'), true);
  assert.equal(invokes('/releases/v0.4.55/install.sh'), true);
  assert.equal(invokes('/releases/v0.4.55-base-test.3/install.sh'), true);
  assert.equal(invokes('/install.sh'), false);
  assert.equal(invokes('/'), false);
  assert.equal(invokes('/docs/getting-started/installation'), false);
});
