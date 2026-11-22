import type { NodeOperation, NodeUpdateStatus } from '../composables/useNodeUpdates';

const terminalSuccess = new Set(['succeeded', 'rolled_back']);
const failureStatuses = new Set(['failed', 'interrupted', 'recovery_required']);

// Prefer the pending generation's head, including after a reload or later checks.
// A new repair's running state is progress, not another failure notification.
export function nodeAttentionOperation(status: NodeUpdateStatus): NodeOperation | null {
  if (status.active_operation && status.active_operation.kind !== 'check') return null;
  const pending = status.recoveries.find(generation => !terminalSuccess.has(generation.state));
  const operation = pending
    ? status.operations.find(item => item.id === pending.head)
    : status.operations.find(item => item.kind !== 'check');
  if (!operation) return null;
  return failureStatuses.has(operation.status) || operation.status === 'rolled_back' && operation.kind !== 'rollback'
    ? operation : null;
}

// Older audit rows retain their original diagnostic text in storage. Present
// only their outcome in Activity; full operation messages belong to runtime UI.
export function activitySummary(item: { type: string; summary: string }): string {
  if (item.type !== 'node_update') return item.summary;
  const match = /^Node\.js (apply|migrate|repair|rollback): (succeeded|rolled_back|failed|interrupted|recovery_required)(?: —|$)/.exec(item.summary);
  if (!match) return item.summary;
  const action = ({ apply: 'update', migrate: 'migration', repair: 'repair', rollback: 'rollback' } as Record<string, string>)[match[1]!];
  switch (match[2]) {
    case 'succeeded': return `Node.js ${action} completed.`;
    case 'rolled_back': return match[1] === 'rollback' ? 'Node.js rollback completed.' : `Node.js ${action} failed; previous runtime restored.`;
    case 'failed': return `Node.js ${action} failed.`;
    case 'interrupted': return `Node.js ${action} was interrupted.`;
    case 'recovery_required': return `Node.js ${action} needs recovery.`;
    default: return item.summary;
  }
}

// A completed worker alone cannot turn an older/failed discovery into current.
// Use the authoritative saved snapshot already returned with its operation.
export function nodeCheckResult(operation: NodeOperation, snapshot: NodeUpdateStatus['snapshot']) {
  if (operation.status !== 'succeeded') return { title: 'Couldn’t check Node.js updates', description: operation.message, type: 'error' as const };
  const fresh = !snapshot.error && snapshot.checked_at > 0 && snapshot.checked_at >= operation.created_at
    && snapshot.attempted_at <= snapshot.checked_at;
  if (fresh && snapshot.state === 'available' && snapshot.update_available && snapshot.candidate) {
    return { title: 'A Node.js update is available', description: `Node.js ${snapshot.candidate.version} is available. Review the update before installing.`, type: 'success' as const };
  }
  if (fresh && snapshot.state === 'current' && !snapshot.update_available && !snapshot.migration_available) {
    return { title: 'No Node.js updates available', description: null, type: 'success' as const };
  }
  if (fresh && snapshot.state === 'migration_available' && snapshot.migration_available && snapshot.candidate) {
    return { title: 'Node.js migration is available', description: `Review migration to Node.js ${snapshot.candidate.version} before managed updates are enabled.`, type: 'warning' as const };
  }
  const reason = snapshot.state === 'stale' || !fresh
    ? 'The stored check is out of date. Check again before maintenance.'
    : 'The installation could not be checked safely. Review its configuration or check again.';
  return { title: 'Node.js update check needs attention', description: snapshot.error || reason, type: 'warning' as const };
}
