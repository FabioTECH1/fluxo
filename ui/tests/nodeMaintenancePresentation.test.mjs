import test from 'node:test';
import assert from 'node:assert/strict';
import { activitySummary, nodeAttentionOperation } from '../src/utils/nodeMaintenancePresentation.ts';
const op = (id, kind, status, message = 'full diagnostic') => ({ id, kind, status, message });
const state = (operations, recoveries = [], active_operation = null) => ({ operations, recoveries, active_operation });

test('a failed update stays visible after a later successful daily check', () => {
  const failed = op('update', 'apply', 'recovery_required', 'complete, untruncated error');
  assert.equal(nodeAttentionOperation(state([op('check', 'check', 'succeeded'), failed])).message, failed.message);
});
test('the current recovery head supplies details rather than a stale attempt', () => {
  const failure = op('repair', 'repair', 'recovery_required', 'new repair error');
  assert.equal(nodeAttentionOperation(state([op('old', 'apply', 'failed'), failure], [{ head: 'repair', state: 'recovery_required' }])), failure);
});
test('successful recovery removes stale failure attention', () => {
  assert.equal(nodeAttentionOperation(state([op('repair', 'repair', 'succeeded'), op('old', 'apply', 'failed')], [{ head: 'repair', state: 'succeeded' }])), null);
});
test('a running repair shows progress instead of the previous failure banner', () => {
  const repair = op('repair', 'repair', 'running');
  assert.equal(nodeAttentionOperation(state([repair, op('old', 'apply', 'failed')], [{ head: 'repair', state: 'starting' }], repair)), null);
});
test('automatic rollback needs attention while an explicit successful rollback does not', () => {
  assert.ok(nodeAttentionOperation(state([op('a', 'apply', 'rolled_back')])));
  assert.equal(nodeAttentionOperation(state([op('b', 'rollback', 'rolled_back')])), null);
});
test('historical Activity messages show a human outcome without mutating the record', () => {
  const item = { type: 'node_update', summary: 'Node.js apply: recovery_required — Update failed: lengthy error; rollback needs recovery: same error' };
  const original = item.summary;
  assert.equal(activitySummary(item), 'Node.js update needs recovery.');
  assert.equal(item.summary, original);
});
test('Activity preserves other event text and already concise outcomes', () => {
  for (const item of [{ type: 'deployment', summary: 'Deploy failed: useful existing summary' }, { type: 'node_update', summary: 'Node.js repair completed.' }]) {
    assert.equal(activitySummary(item), item.summary);
  }
});
test('Activity distinguishes automatic restoration from requested rollback', () => {
  assert.equal(activitySummary({ type: 'node_update', summary: 'Node.js apply: rolled_back — diagnostic' }), 'Node.js update failed; previous runtime restored.');
  assert.equal(activitySummary({ type: 'node_update', summary: 'Node.js rollback: rolled_back — diagnostic' }), 'Node.js rollback completed.');
});
