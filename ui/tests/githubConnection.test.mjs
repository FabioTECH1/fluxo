import test from 'node:test';
import assert from 'node:assert/strict';
import { tokenPresentation, deploymentAccessLabel, formatGitHubDate } from '../src/utils/githubConnection.ts';
const now = Date.parse('2026-11-18T12:00:00Z');
const account = { token_status: 'valid', token_expires_at: null, linked_sites: 2, deployment_access: { checked_at: null, sites: [] } };
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
