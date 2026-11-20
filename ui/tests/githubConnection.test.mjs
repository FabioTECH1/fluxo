import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { tokenPresentation, deploymentAccessLabel, formatGitHubDate, savedGitOptions, siteGitOptions, requiresLegacyGitHubRecovery } from '../src/utils/githubConnection.ts';
const now = Date.parse('2026-11-18T12:00:00Z');
const account = { token_status: 'valid', token_expires_at: null, linked_sites: 2, deployment_access: { checked_at: null, sites: [] } };
test('administrator recovery is offered only for the legacy ownership refusal', () => {
  const message = 'The saved credential could not verify this legacy GitHub account. Its site links have been preserved.';
  assert.equal(requiresLegacyGitHubRecovery({ ...account, github_user_id: 0 }, message), true);
  assert.equal(requiresLegacyGitHubRecovery({ ...account, github_user_id: 123 }, message), false);
  assert.equal(requiresLegacyGitHubRecovery(null, message), false);
  for (const error of ['Incorrect panel password', 'Could not validate GitHub token', 'Saved-token verification is temporarily unavailable']) {
    assert.equal(requiresLegacyGitHubRecovery({ ...account, github_user_id: 0 }, error), false);
  }
});
test('past and exact expiry say Expired, never yesterday or a negative duration', () => {
  for (const date of ['2026-11-17T12:00:00Z', '2026-11-18T12:00:00Z']) {
    const result = tokenPresentation({ ...account, token_expires_at: date }, now);
    assert.equal(result.label, 'Expired'); assert.equal(result.remaining, 'Expired'); assert.equal(result.reconnect, true);
  }
});
test('upcoming expiry shows rounded remaining days and warnings', () => {
  assert.equal(tokenPresentation({ ...account, token_expires_at: '2026-11-30T12:00:00Z' }, now).remaining, '12 days left');
  assert.equal(tokenPresentation({ ...account, token_expires_at: '2026-11-18T13:00:00Z' }, now).remaining, '1 day left');
});
test('unknown expiry and transient errors never imply an expired or permanent token', () => {
  assert.equal(formatGitHubDate(null), 'Unavailable');
  assert.equal(tokenPresentation(account, now).remaining, '');
  assert.equal(tokenPresentation({ ...account, token_status: 'error' }, now).reconnect, false);
  assert.equal(tokenPresentation({ ...account, token_status: 'invalid' }, now).reconnect, true);
});
test('deployment summaries distinguish unknown, partial and successful checks', () => {
  assert.equal(deploymentAccessLabel(account), 'Not checked');
  const checked = { ...account, deployment_access: { checked_at: '2026-11-18T12:00:00Z', sites: [{ status: 'verified' }, { status: 'error' }] } };
  assert.equal(deploymentAccessLabel(checked), '1 of 2 sites verified');
  assert.equal(deploymentAccessLabel({ ...account, linked_sites: 0 }), 'No linked sites');
});

test('site Git options use the linked account on load and refresh, and skip disconnected sites', async () => {
  const calls = [];
  const client = {
    async getGithubRepos(...args) { calls.push(args); return ['second/repo']; },
    async getGithubBranches(...args) { calls.push(args); return ['main']; },
  };
  assert.deepEqual(await siteGitOptions(client, 0, 'old/repo'), { repos: [], branches: [], errors: [] });
  for (const refresh of [false, true]) {
    assert.deepEqual(await siteGitOptions(client, 2, 'second/repo', refresh), { repos: ['second/repo'], branches: ['main'], errors: [] });
  }
  assert.deepEqual(calls, [[2, false], ['second/repo', 2, false], [2, true], ['second/repo', 2, true]]);
});

test('Git option failures stay visible and do not discard successful independent results', async () => {
  for (const refresh of [false, true]) {
    const branchFailure = await siteGitOptions({
      async getGithubRepos() { return [{ full_name: 'company/project' }]; },
      async getGithubBranches() { throw new Error('GitHub returned status 403'); },
    }, 2, 'company/project', refresh);
    assert.deepEqual(branchFailure.repos, [{ full_name: 'company/project' }]);
    assert.deepEqual(branchFailure.branches, []);
    assert.deepEqual(branchFailure.errors, ['Branches: GitHub returned status 403']);
    const repoFailure = await siteGitOptions({
      async getGithubRepos() { throw new Error('GitHub could not be reached'); },
      async getGithubBranches() { return [{ name: 'master' }]; },
    }, 2, 'company/project', refresh);
    assert.deepEqual(repoFailure.repos, []);
    assert.deepEqual(repoFailure.branches, [{ name: 'master' }]);
    assert.deepEqual(repoFailure.errors, ['Repositories: GitHub could not be reached']);
  }
  const failed = await siteGitOptions({
    async getGithubRepos() { throw {}; },
    async getGithubBranches() { throw {}; },
  }, 2, 'company/project');
  assert.equal(failed.errors.length, 2);
  assert.deepEqual(await siteGitOptions({
    async getGithubRepos() { return null; },
    async getGithubBranches() { return null; },
  }, 2, 'company/project'), { repos: [], branches: [], errors: [] });
});

test('saved Git selections remain visible with unavailable options without changing the source list', async () => {
  const { parse, compileScript } = await import('@vue/compiler-sfc');
  const { createSSRApp } = await import('vue');
  const { renderToString } = await import('@vue/server-renderer');
  const source = await readFile(new URL('../src/components/SearchSelect.vue', import.meta.url), 'utf8');
  const { descriptor } = parse(source);
  const compiled = compileScript(descriptor, { id: 'saved-git-selection-test', inlineTemplate: true });
  const moduleSource = stripTypeScriptTypes(compiled.content.replace(/from ['"]vue['"]/g, `from '${import.meta.resolve('vue')}'`));
  const { default: select } = await import('data:text/javascript;base64,' + Buffer.from(moduleSource).toString('base64'));
  for (const value of ['company/project', 'master']) {
    const original = [{ label: 'Select a repository', value: '' }];
    const options = savedGitOptions(original, value);
    const html = await renderToString(createSSRApp(select, { modelValue: value, options, placeholder: 'Unavailable' }));
    assert.ok(html.includes(value));
    assert.ok(!html.includes('>Unavailable<'));
    assert.equal(original.length, 1);
    assert.equal(savedGitOptions(options, value).length, 2);
    assert.equal(savedGitOptions(original, '').length, 1);
  }
});

test('disconnect and reconnect invalidate actual cached site membership', async t => {
  const source = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8');
  const dependencies = `const router={push(){}};const useToast=()=>({addToast(){}});const useAuthStore=()=>({token:'fixture-token'});const clearLogViewSnapshots=()=>{};\n`;
  const isolated = stripTypeScriptTypes(dependencies + source.replace(/^import .*;\r?\n/gm, ''));
  const { apiClient } = await import('data:text/javascript;base64,' + Buffer.from(isolated).toString('base64'));
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let localAccount = 1;
  let siteReads = 0;
  globalThis.fetch = async (url, options) => {
    if (url === '/api/v1/sites/1') {
      siteReads++;
      return new Response(JSON.stringify({ id: 1, github_account_id: localAccount }), { status: 200 });
    }
    if (url === '/api/v1/github/accounts/1' && options.method === 'DELETE') {
      localAccount = 0;
      return new Response(null, { status: 204 });
    }
    if (url === '/api/v1/github/accounts' && options.method === 'POST') {
      localAccount = 2;
      return new Response(JSON.stringify({ id: 2 }), { status: 201 });
    }
    throw new Error('Unexpected request: ' + url);
  };
  assert.equal((await apiClient.getSite(1)).github_account_id, 1);
  assert.equal((await apiClient.getSite(1)).github_account_id, 1);
  assert.equal(siteReads, 1);
  await apiClient.disconnectGithubAccount(1);
  assert.equal((await apiClient.getSite(1)).github_account_id, 0);
  await apiClient.connectGithubAccount({ token: 'replacement' });
  assert.equal((await apiClient.getSite(1)).github_account_id, 2);
  assert.equal(siteReads, 3);
});
