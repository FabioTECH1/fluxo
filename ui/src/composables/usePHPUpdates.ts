import { computed, ref } from 'vue';
import type { ScheduledTaskReceipt } from './useNodeUpdates';
import { apiClient } from '../api/client';
import { useToast } from './useToast';
import { createRequestID } from '../utils/requestID';
import { runtimeMaintenanceBusy, runtimeMaintenanceLocked, beginRuntimeSubmission, acceptRuntimeOperation, observeRuntimeMaintenance, finishRuntimeOperation } from './runtimeMaintenance';

export interface PHPUpdatePlan {
  id: string; version: string; candidate_id: string; reviewed?: boolean; parent_plan_id?: string;
  inventory: { changes: { name: string; installed: string; candidate: string }[] };
  impact: { scheduled_tasks?: ScheduledTaskReceipt[]; sites: { id: number; domain: string }[]; workers: { id: number; name: string; kind?: string; protocol?: string }[]; schedulers?: { id: number; site_id: number; version: string }[]; phpmyadmin: boolean; notes?: string[]; blocked_reasons?: string[] };
  rollback?: { available: boolean; reason?: string };
}
export interface PHPRepositoryReview {
 id: string; created_at: number; codename: string; architecture: string; files: string[];
 disabled_ppa_entries: number; key_fingerprint: string; rollback_unavailable: string[]; package_blockers?: Record<string, string[]>; retained_extensions?: string[]; migration_blockers?: string[];
}
export interface PHPUpdateBranch {
  version: string; update_available: boolean; notice: boolean; state: string;
  plan?: PHPUpdatePlan; blocked_reasons: string[];
}
export interface PHPUpdateOperation {
  id: string; kind: string; version: string; status: string; phase: string;
  message: string; output: string; username: string;
  result?: Record<string, unknown>;
}
export interface PHPRecoverySummary {
 generation_id: string; operation_id: string; version: string; state: string;
 can_repair: boolean; can_rollback: boolean; rollback_reason?: string;
}
export interface PHPRecoveryPlan {
 id: string; version: string; kind: 'repair' | 'rollback'; goal: 'before' | 'after';
 packages: { name: string; version: string; architecture: string }[];
 retry_fpm_reload?: boolean;
 impact: PHPUpdatePlan['impact'];
}
interface UpdateState { branches: PHPUpdateBranch[]; active_operation: PHPUpdateOperation | null; failures: PHPUpdateOperation[]; recoveries: PHPRecoverySummary[] }
const state = ref<UpdateState>({ branches: [], active_operation: null, failures: [], recoveries: [] });
const isRepository = (kind: string) => kind.startsWith('repository_');
const operationTitle = (o: PHPUpdateOperation) => o.kind === 'repository_review' ? 'Reviewing PHP repository' : o.kind === 'repository_apply' ? 'Switching PHP repository' : `${operationVerb(o.kind)} PHP ${o.version}`;
const operationVerb = (kind: string) => kind === 'repair' ? 'Repairing' : kind === 'rollback' ? 'Rolling back' : 'Updating';
const completedVerb = (kind: string) => kind === 'repair' ? 'repair completed' : kind === 'rollback' ? 'package rollback completed' : 'updated';
const tracked = new Map<string, { toast: number; version: string }>();
let timer: ReturnType<typeof setTimeout> | undefined;
let users = 0;
let refreshing: Promise<void> | null = null;
const { showToast, updateToast } = useToast();

export function phpUpdateError(error: any): string {
  const fallback = 'PHP maintenance could not be started.';
  const message = typeof error?.message === 'string' && error.message.trim() ? error.message.trim() : fallback;
  try {
    const detail = JSON.parse(message);
    for (const value of [detail?.message, detail?.error]) {
      if (typeof value === 'string' && value.trim()) return value.trim();
    }
    return fallback;
  } catch { return message; }
}
const active = (o: PHPUpdateOperation) => ['queued', 'running'].includes(o.status);

async function refresh() {
  if (refreshing) return refreshing;
  refreshing = (async () => {
    const result = await apiClient.get('/api/v1/server/php/updates', { useCache: false });
    state.value = { ...result, recoveries: result.recoveries || [] };
    const running = state.value.active_operation;
    observeRuntimeMaintenance('php', running, state.value.recoveries.some(r => !['succeeded', 'rolled_back'].includes(r.state)));
    if (running && running.kind !== 'check' && !tracked.has(running.id)) track(running, running.version);
    for (const [id, entry] of tracked) {
      const operation: PHPUpdateOperation = running?.id === id ? running
        : await apiClient.get(`/api/v1/server/php/updates/operations/${id}`, { useCache: false });
      if (active(operation)) {
        updateToast(entry.toast, { description: operation.message || 'Preparing PHP maintenance…' });
        continue;
      }
      finishRuntimeOperation('php', operation.id);
      if (operation.kind === 'check' && operation.status === 'succeeded') {
        const branch = state.value.branches.find(b => b.version === entry.version);
        const current = branch?.state === 'current' && !branch.blocked_reasons.length;
        const found = branch?.update_available === true;
        updateToast(entry.toast, {
          title: found ? `An update is available for PHP ${entry.version}` : current ? `No PHP ${entry.version} updates available` : `PHP ${entry.version} update check needs attention`,
          description: found || current ? null : branch?.blocked_reasons.join('. ') || 'The installed PHP branch could not be checked safely. Try again or review its configuration.',
          type: found || current ? 'success' : 'warning',
        });
      } else {
        if (operation.kind !== 'check') apiClient.invalidate('/api/v1/server/php');
        updateToast(entry.toast, {
          title: isRepository(operation.kind) ? (operation.status === 'succeeded' ? operation.kind === 'repository_review' ? 'PHP repository review ready' : 'PHP repository switched' : 'PHP repository setup needs attention') : operation.status === 'succeeded' ? `PHP ${operation.version} ${completedVerb(operation.kind)}`
            : operation.kind === 'check' ? 'Couldn’t check for PHP updates'
              : operation.status === 'restart_required' ? `PHP ${operation.version} needs service attention` : `PHP ${operation.version} update needs attention`,
          description: operation.message, type: operation.status === 'succeeded' ? 'success' : operation.status === 'restart_required' ? 'warning' : 'error',
          action: operation.kind !== 'check' ? { label: 'Review PHP', to: '/runtime/php' } : null,
        });
      }
      tracked.delete(id);
    }
  })().finally(() => { refreshing = null; });
  return refreshing;
}
function track(o: PHPUpdateOperation, version: string) {
  acceptRuntimeOperation('php', o);
  if (active(o)) state.value.active_operation = o;
  if (tracked.has(o.id)) return;
  tracked.set(o.id, { version, toast: showToast({ type: 'loading', title: o.kind === 'check' ? `Checking PHP ${version} updates` : operationTitle(o), description: o.message || 'Preparing…' }) });
  if (users > 0) { clearTimeout(timer); timer = setTimeout(tick, 2000); }
}
async function submitOperation(path: string, body: object, version: string): Promise<PHPUpdateOperation> {
  const release = beginRuntimeSubmission();
  try {
    const operation: PHPUpdateOperation = await apiClient.post(path, body);
    track(operation, version);
    // A failed status read cannot turn an acknowledged start into a rejected
    // submission. Retain the operation and let the existing poller reconcile it.
    void refresh().catch(() => {
      const entry = tracked.get(operation.id);
      if (entry) updateToast(entry.toast, { description: 'Maintenance was accepted. Reconnecting to its status…' });
    });
    return operation;
  } finally { release(); }
}
async function tick() {
  try { await refresh(); } catch {
    for (const entry of tracked.values()) updateToast(entry.toast, { description: 'Connection interrupted. The operation may still be running; reconnecting…' });
  }
  if (users > 0) timer = setTimeout(tick, state.value.active_operation || tracked.size ? 2000 : 60_000);
}
export function usePHPUpdates() {
  return {
    state,
    busy: computed(() => !!state.value.active_operation || runtimeMaintenanceBusy.value),
    locked: computed(() => !!state.value.active_operation || runtimeMaintenanceLocked.value || state.value.recoveries.some(r => !['succeeded', 'rolled_back'].includes(r.state))),
    async reviewRecovery(operationID: string, kind: 'repair' | 'rollback'): Promise<PHPRecoveryPlan> {
      return apiClient.post(`/api/v1/server/php/updates/operations/${operationID}/recovery-plan`, { kind });
    },
    async recover(plan: PHPRecoveryPlan, requestID: string) {
      return submitOperation('/api/v1/server/php/updates/recovery/apply', { plan_id: plan.id, request_id: requestID }, plan.version);
    },
    refresh,
    async reviewRepository(): Promise<PHPRepositoryReview> {
      const operation = await submitOperation('/api/v1/server/php/updates/repository/review', { request_id: createRequestID() }, '');
      for (let i = 0; i < 600; i++) {
        const current: PHPUpdateOperation = await apiClient.get(`/api/v1/server/php/updates/operations/${operation.id}`, { useCache: false });
        if (!active(current)) {
          if (current.status !== 'succeeded') throw new Error(current.message);
          return current.result as unknown as PHPRepositoryReview;
        }
        await new Promise(resolve => setTimeout(resolve, 1000));
      }
      throw new Error('Repository review is still running. Reopen Runtime to inspect its progress.');
    },
    async applyRepository(planID: string, requestID: string) {
      return submitOperation('/api/v1/server/php/updates/repository/apply', { plan_id: planID, request_id: requestID }, '');
    },
    async check(version: string) {
      return submitOperation('/api/v1/server/php/updates/check', { version }, version);
    },
    async apply(plan: PHPUpdatePlan, requestID: string, acknowledgeNoRollback = false) {
      return submitOperation('/api/v1/server/php/updates/apply', { version: plan.version, plan_id: plan.id, request_id: requestID, acknowledge_no_rollback: acknowledgeNoRollback }, plan.version);
    },
    async dismiss(branch: PHPUpdateBranch) {
      await apiClient.post('/api/v1/server/php/updates/notices/dismiss', { version: branch.version, candidate_id: branch.plan?.candidate_id });
      await refresh();
    },
    async verify(operation: PHPUpdateOperation) {
      return submitOperation(`/api/v1/server/php/updates/operations/${operation.id}/verify`, {}, operation.version);
    },
    start() { users++; if (users === 1) { clearTimeout(timer); void tick(); } },
    stop() { users = Math.max(0, users - 1); if (!users) clearTimeout(timer); },
  };
}
