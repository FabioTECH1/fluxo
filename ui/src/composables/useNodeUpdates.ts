import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
import { createRequestID } from '../utils/requestID';
import { useToast } from './useToast';

export interface NodeRelease { version: string; npm: string; architecture: string; filename: string; sha256: string; manifest_sha256: string; signer: string; trust_revision: string; tree_sha256?: string }
export interface NodeUnit { id: number; site_id: number; domain: string; name: string; active: boolean; bun_only: boolean; state?: string; sub_state?: string; listener_present?: boolean }
export interface NodeOperation { id: string; kind: string; plan_id: string; status: string; phase: string; message: string; created_at: number; updated_at: number; generation_id?: string }
export interface NodePlan { id: string; kind: string; discovery_id: string; created_at: number; generation_id?: string; head?: string; before: { release: NodeRelease }; after: NodeRelease; retained?: { before: NodeRelease; after: NodeRelease }; impact: { sites: { id: number; domain: string; mode: string; app_type: string }[]; units: NodeUnit[]; blocked_reasons: string[] } }
export interface NodeGeneration { id: string; head: string; state: string; plan: NodePlan; retained?: { before: NodeRelease; after: NodeRelease } }
export interface NodeUpdateStatus { snapshot: { state: string; checked_at: number; attempted_at: number; error?: string; discovery_id: string; candidate_id: string; candidate: NodeRelease | null; installed?: { release?: NodeRelease }; update_available: boolean; notice: boolean }; active_operation: NodeOperation | null; operations: NodeOperation[]; recoveries: NodeGeneration[]; locked: boolean }
const state = ref<NodeUpdateStatus>({ snapshot: { state: 'unchecked', checked_at: 0, attempted_at: 0, discovery_id: '', candidate_id: '', candidate: null, update_available: false, notice: false }, active_operation: null, operations: [], recoveries: [], locked: false });
const { showToast, updateToast } = useToast();
const tracked = new Map<string, number>();
let users = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
let refreshing: Promise<void> | null = null;
export function nodeUpdateError(error: unknown): string {
  const message = error instanceof Error ? error.message : 'Node.js maintenance could not be completed.';
  try { const detail = JSON.parse(message); return detail.message || message; } catch { return message; }
}
async function refresh() {
  if (refreshing) return refreshing;
  refreshing = (async () => {
    state.value = await apiClient.get('/api/v1/server/node/updates', { useCache: false });
    if (state.value.active_operation && !tracked.has(state.value.active_operation.id)) track(state.value.active_operation);
    for (const [id, toast] of tracked) {
      const op: NodeOperation = state.value.operations.find(item => item.id === id) || await apiClient.get(`/api/v1/server/node/updates/operations/${id}`, { useCache: false });
      if (['queued', 'running'].includes(op.status)) { updateToast(toast, { description: op.message || 'Preparing Node.js maintenance…' }); continue; }
      if (op.kind !== 'check') apiClient.invalidate('/api/v1/server/node/info');
      updateToast(toast, { title: op.kind === 'check' ? op.status === 'succeeded' ? state.value.snapshot.update_available ? 'A Node.js update is available' : 'Node.js check completed' : 'Couldn’t check Node.js updates' : op.status === 'succeeded' ? 'Node.js update completed' : op.status === 'rolled_back' ? 'Previous Node.js runtime restored' : 'Node.js maintenance needs attention', description: op.message, type: op.status === 'succeeded' ? 'success' : op.status === 'rolled_back' ? 'warning' : 'error', action: op.kind === 'check' ? null : { label: 'Review Node.js', to: '/runtime/node' } });
      tracked.delete(id);
    }
  })().finally(() => { refreshing = null; });
  return refreshing;
}
function track(op: NodeOperation) { if (!tracked.has(op.id)) tracked.set(op.id, showToast({ type: 'loading', title: op.kind === 'check' ? 'Checking Node.js updates' : op.kind === 'rollback' ? 'Restoring Node.js' : 'Maintaining Node.js', description: op.message || 'Preparing…' })); }
async function tick() { try { await refresh(); } catch { for (const toast of tracked.values()) updateToast(toast, { description: 'Connection interrupted. Maintenance may still be running; reconnecting…' }); } if (users) timer = setTimeout(tick, state.value.active_operation || tracked.size ? 2000 : 60_000); }
async function startOperation(path: string, body: object) { const op = await apiClient.post(path, body); track(op); await refresh(); if (users) { clearTimeout(timer); timer = setTimeout(tick, 2000); } }
export function useNodeUpdates() {
  return { state, busy: computed(() => !!state.value.active_operation), locked: computed(() => state.value.locked), refresh,
    check: () => startOperation('/api/v1/server/node/updates/check', { request_id: createRequestID() }),
    review: (): Promise<NodePlan> => apiClient.post(`/api/v1/server/node/updates/plans/${state.value.snapshot.discovery_id}/review`, {}),
    reviewRecovery: (g: NodeGeneration, kind: 'repair' | 'rollback'): Promise<NodePlan> => apiClient.post(`/api/v1/server/node/updates/operations/${g.head}/recovery-plan`, { kind }),
    apply: (plan: NodePlan, requestID: string) => startOperation(plan.kind === 'apply' ? '/api/v1/server/node/updates/apply' : '/api/v1/server/node/updates/recovery/apply', { plan_id: plan.id, request_id: requestID }),
    async dismiss() { await apiClient.post('/api/v1/server/node/updates/notices/dismiss', { candidate_id: state.value.snapshot.candidate_id }); await refresh(); },
    start() { users++; if (users === 1) void tick(); }, stop() { users = Math.max(0, users - 1); if (!users) clearTimeout(timer); },
  };
}
