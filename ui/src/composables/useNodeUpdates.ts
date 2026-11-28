import { apiErrorMessage } from '../utils/apiError';
import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
import { createRequestID } from '../utils/requestID';
import { useToast } from './useToast';
import { nodeAttentionOperation, nodeCheckResult } from '../utils/nodeMaintenancePresentation';
import { runtimeMaintenanceBusy, runtimeMaintenanceLocked, beginRuntimeSubmission, acceptRuntimeOperation, observeRuntimeMaintenance, finishRuntimeOperation } from './runtimeMaintenance';

export interface NodeRelease { version: string; npm: string; architecture: string; filename: string; sha256: string; manifest_sha256: string; signer: string; trust_revision: string; tree_sha256?: string }
export interface NodeLegacySource { schema: number; kind: string; version: string; npm: string; package_version: string; architecture: string; node: { path: string; sha256: string }; managers: Record<string, string> }
export interface NodeUnit { id: number; site_id: number; domain: string; name: string; active: boolean; bun_only: boolean; bun?: { path: string; version: string; sha256: string }; state?: string; sub_state?: string; listener_present?: boolean }
export interface NodePHPConsumer { branch: string; domain: string; consumer: { id: number; site_id: number; name: string; kind: string; protocol?: string; command: string; instances: number }; php: { path: string; version: string; sha256: string }; units: { enabled: string; runtime: { name: string; active: boolean; natural?: boolean; state: string; sub_state: string } }[] }
export interface ScheduledTaskReceipt { name: string; domain?: string; notice?: string; contract: { id: number; kind: string; expression: string; command: string; user: string; directory: string }; identity: string }
export interface NodeOperation { id: string; kind: string; plan_id: string; status: string; phase: string; message: string; created_at: number; updated_at: number; generation_id?: string }
export interface NodePlan { migration?: NodeLegacySource; consumer_policy: number; id: string; kind: string; discovery_id: string; created_at: number; generation_id?: string; head?: string; before: { release: NodeRelease }; after: NodeRelease; retained?: { before: NodeRelease; after: NodeRelease }; impact: { scheduled_tasks?: ScheduledTaskReceipt[]; sites: { id: number; domain: string; mode: string; app_type: string }[]; units: NodeUnit[]; blocked_reasons: string[]; php_policy?: number; php_consumers?: NodePHPConsumer[]; php_resumes?: Record<string, { completed?: boolean; invocation?: string }> } }
export interface NodeGeneration { id: string; head: string; state: string; plan: NodePlan; coordination?: NodePlan['impact']; retained?: { before: NodeRelease; after: NodeRelease } }
export interface NodeUpdateStatus { snapshot: { migration_available?: boolean; legacy_source?: NodeLegacySource; state: string; checked_at: number; attempted_at: number; error?: string; discovery_id: string; candidate_id: string; candidate: NodeRelease | null; installed?: { release?: NodeRelease }; update_available: boolean; notice: boolean }; active_operation: NodeOperation | null; operations: NodeOperation[]; recoveries: NodeGeneration[]; locked: boolean }
const state = ref<NodeUpdateStatus>({ snapshot: { state: 'unchecked', checked_at: 0, attempted_at: 0, discovery_id: '', candidate_id: '', candidate: null, update_available: false, notice: false }, active_operation: null, operations: [], recoveries: [], locked: false });
const { showToast, updateToast } = useToast();
const detailsOperation = ref<NodeOperation | null>(null);
const recoveryDetailsError = ref('');
const repairReviewRequest = ref<string | null>(null);
let beforeRepairConfirmation: (() => void) | null = null;
const updateReviewRequest=ref<string|null>(null);
let beforeUpdateConfirmation:(()=>void)|null=null;
const repairForOperation = (id: string) => state.value.recoveries.find(g => g.head === id && !['succeeded', 'rolled_back'].includes(g.state));
function failureAction(operation: { id: string; kind?: string; status?: string }): 'repair' | 'check' | null {
  if (repairForOperation(operation.id)) return 'repair';
  if (!['failed', 'interrupted', 'recovery_required', 'rolled_back'].includes(operation.status || '')
    || !['apply', 'migrate', 'check'].includes(operation.kind || '')
    || state.value.recoveries.some(g => !['succeeded', 'rolled_back'].includes(g.state))) return null;
  return 'check';
}
const tracked = new Map<string, number>();
let users = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
let refreshing: Promise<void> | null = null;
export function nodeUpdateError(error: unknown): string {
  return apiErrorMessage(error, 'Node.js maintenance could not be completed.');
}
async function refresh() {
  if (refreshing) return refreshing;
  refreshing = (async () => {
    const next: NodeUpdateStatus = await apiClient.get('/api/v1/server/node/updates', { useCache: false });
    let detailError = '';
    for (const generation of next.recoveries) {
      if (!['succeeded', 'rolled_back'].includes(generation.state) && !next.operations.some(item => item.id === generation.head)) {
        try {
          const operation: NodeOperation = await apiClient.get(`/api/v1/server/node/updates/operations/${encodeURIComponent(generation.head)}`, { useCache: false });
          next.operations.push(operation);
        } catch (error) {
          detailError = `Couldn’t load the recovery details. Refresh to retry. ${nodeUpdateError(error)}`;
        }
      }
    }
    // Publish complete snapshots together so refreshing an older recovery does
    // not briefly remove its alert or collapse the user's open details.
    state.value = next;
    observeRuntimeMaintenance('node', next.active_operation, next.recoveries.some(g => !['succeeded', 'rolled_back'].includes(g.state)));
    recoveryDetailsError.value = detailError;
    if (state.value.active_operation && !tracked.has(state.value.active_operation.id)) track(state.value.active_operation);
    for (const [id, toast] of tracked) {
      const op: NodeOperation = state.value.operations.find(item => item.id === id) || await apiClient.get(`/api/v1/server/node/updates/operations/${id}`, { useCache: false });
      if (['queued', 'running'].includes(op.status)) { updateToast(toast, { description: op.message || 'Preparing Node.js maintenance…' }); continue; }
      finishRuntimeOperation('node', op.id);
      if (op.kind !== 'check') apiClient.invalidate('/api/v1/server/node/info');
      const result = op.kind === 'check' ? nodeCheckResult(op, state.value.snapshot) : {
        title: op.status === 'succeeded' ? op.kind === 'migrate' ? 'Node.js migration completed' : 'Node.js update completed' : op.status === 'rolled_back' ? 'Previous Node.js runtime restored' : 'Node.js maintenance needs attention',
        description: op.message, type: op.status === 'succeeded' ? 'success' as const : op.status === 'rolled_back' ? 'warning' as const : 'error' as const,
      };
      updateToast(toast, { ...result, action: op.kind === 'check' ? null : { label: 'Review details', onClick: () => { detailsOperation.value = op; } } });
      tracked.delete(id);
    }
  })().finally(() => { refreshing = null; });
  return refreshing;
}
function track(op: NodeOperation) {
  acceptRuntimeOperation('node', op);
  if (['queued', 'running'].includes(op.status)) state.value.active_operation = op;
  if (!tracked.has(op.id)) tracked.set(op.id, showToast({ type: 'loading', title: op.kind === 'check' ? 'Checking Node.js updates' : op.kind === 'rollback' ? 'Restoring Node.js' : 'Maintaining Node.js', description: op.message || 'Preparing…' }));
}
async function tick() { try { await refresh(); } catch { for (const toast of tracked.values()) updateToast(toast, { description: 'Connection interrupted. Maintenance may still be running; reconnecting…' }); } if (users) timer = setTimeout(tick, state.value.active_operation || tracked.size ? 2000 : 60_000); }
async function startOperation(path: string, body: object) {
  const release = beginRuntimeSubmission();
  try {
    const op: NodeOperation = await apiClient.post(path, body);
    track(op);
    void refresh().catch(() => {
      const toast = tracked.get(op.id);
      if (toast !== undefined) updateToast(toast, { description: 'Maintenance was accepted. Reconnecting to its status…' });
    });
    if (users) { clearTimeout(timer); timer = setTimeout(tick, 2000); }
    return op;
  } finally { release(); }
}
export function useNodeUpdates() {
  return { state, updateReviewRequest,
    requestUpdateReview(discoveryID:string,beforeOpen:()=>void):boolean {
      if(runtimeMaintenanceLocked.value||state.value.locked||state.value.active_operation||updateReviewRequest.value||repairReviewRequest.value||!state.value.snapshot.update_available||state.value.snapshot.discovery_id!==discoveryID)return false;
      beforeUpdateConfirmation=beforeOpen;updateReviewRequest.value=discoveryID;return true;
    },
    updateReviewReady(){beforeUpdateConfirmation?.();beforeUpdateConfirmation=null;},
    finishUpdateReview(){beforeUpdateConfirmation=null;updateReviewRequest.value=null;},
    failureAction, repairReviewRequest, repairForOperation,
    requestRepairReview(operationID: string, beforeOpen: () => void = () => {}): boolean {
      if (runtimeMaintenanceBusy.value || state.value.active_operation || updateReviewRequest.value || repairReviewRequest.value || !repairForOperation(operationID)) return false;
      beforeRepairConfirmation = beforeOpen;
      repairReviewRequest.value = operationID;
      return true;
    },
    repairReviewReady(operationID: string) { if (repairReviewRequest.value === operationID) { beforeRepairConfirmation?.(); beforeRepairConfirmation = null; } },
    finishRepairReview(operationID: string) { if (repairReviewRequest.value === operationID) { beforeRepairConfirmation = null; repairReviewRequest.value = null; } },
    attention: computed(() => {
      const operation = nodeAttentionOperation(state.value);
      if (operation) return operation;
      const pending = state.value.recoveries.find(g => !['succeeded', 'rolled_back'].includes(g.state));
      return pending && recoveryDetailsError.value && !state.value.active_operation
        ? { id: pending.head, message: recoveryDetailsError.value } : null;
    }), detailsOperation, closeDetails() { detailsOperation.value = null; }, busy: computed(() => !!state.value.active_operation || runtimeMaintenanceBusy.value), locked: computed(() => state.value.locked || runtimeMaintenanceLocked.value), refresh,
    check: () => startOperation('/api/v1/server/node/updates/check', { request_id: createRequestID() }),
    review: (): Promise<NodePlan> => apiClient.post(`/api/v1/server/node/updates/plans/${state.value.snapshot.discovery_id}/review`, {}),
    reviewRecovery: (g: NodeGeneration, kind: 'repair' | 'rollback'): Promise<NodePlan> => apiClient.post(`/api/v1/server/node/updates/operations/${g.head}/recovery-plan`, { kind }),
    apply: (plan: NodePlan, requestID: string) => startOperation(plan.kind === 'migrate' ? '/api/v1/server/node/updates/migrate' : plan.kind === 'apply' ? '/api/v1/server/node/updates/apply' : '/api/v1/server/node/updates/recovery/apply', { plan_id: plan.id, request_id: requestID }),
    async dismiss() { await apiClient.post('/api/v1/server/node/updates/notices/dismiss', { candidate_id: state.value.snapshot.candidate_id }); await refresh(); },
    start() { users++; if (users === 1) void tick(); }, stop() { users = Math.max(0, users - 1); if (!users) clearTimeout(timer); },
  };
}
