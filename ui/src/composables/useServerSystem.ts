import { apiErrorMessage } from '../utils/apiError';
import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from './useConfirm';

export interface SystemReboot { id: string; boot_id: string; phase: string; detail: string; requested_at: string; expected_services: string[] }
export interface SystemStatus {
 boot_id: string;
 running_kernel: string | null;
 updates: { state: string; editable: boolean; detail: string; automatic_reboot: string; last_attempt?: string; last_result: string };
 restart_required: { state: string; packages: string[]; detail: string };
 reboot: SystemReboot | null;
}
export interface SystemService { unit: string; state: string; detail: string }
interface Flow { id: string; boot: string; requested: number; returned?: number; returnedBoot?: string }
const key = 'fluxo_server_restart';
function restore(): Flow | null {
 try { const v = JSON.parse(localStorage.getItem(key) || 'null'); return v && typeof v.id === 'string' && typeof v.boot === 'string' && Number.isFinite(v.requested) ? v : null; } catch { return null; }
}
const status = ref<SystemStatus | null>(null);
const requiredKey = 'fluxo_restart_required';
function savedRequirement(): SystemStatus['restart_required'] | null {
 try { const v = JSON.parse(localStorage.getItem(requiredKey) || 'null'); return v?.state === 'required' && typeof v.detail === 'string' && Array.isArray(v.packages) ? v : null; } catch { return null; }
}
const restartRequired = ref(savedRequirement());
const flow = ref<Flow | null>(restore());
const services = ref<SystemService[]>([]);
const stage = ref('');
const error = ref('');
const pollError = ref('');
const busy = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
let polling = false;
let consumers = 0;
let requestInProgress = false;
let failures = 0;
const save = () => { try { if (flow.value) localStorage.setItem(key, JSON.stringify(flow.value)); } catch { /* Restricted browser storage cannot affect a server operation. */ } };
function message(e: unknown): string { return apiErrorMessage(e); }
export function returnedStage(currentBoot: string, f: Flow, s: SystemService[], elapsed: number): string {
 if (currentBoot === f.boot) return Date.now() - f.requested < 20_000 ? 'Restarting server…' : 'Waiting for server';
 if (!s.length) return 'Server is back · No managed services were expected to start';
 if (s.every(v => v.state === 'running')) return 'Services running';
 return elapsed < 90_000 ? 'Server is back · Checking services' : 'Server is back · Some services need attention';
}
async function refresh() {
 if (polling) return;
 polling = true;
 try {
  const next: SystemStatus = await apiClient.getServerSystem();
  // Unknown state never clears the last confirmed required notice.
  if (next.restart_required.state === 'unknown' && status.value?.restart_required.state === 'required') next.restart_required = status.value.restart_required;
  status.value = next; pollError.value = ''; failures = 0;
  if (next.restart_required.state !== 'unknown') {
   restartRequired.value = next.restart_required.state === 'required' ? next.restart_required : null;
   try { if (restartRequired.value) localStorage.setItem(requiredKey, JSON.stringify(restartRequired.value)); else localStorage.removeItem(requiredKey); } catch { /* no storage permission required */ }
  }
  if (next.reboot && ['accepted', 'unconfirmed'].includes(next.reboot.phase) && !flow.value) {
   flow.value = { id: next.reboot.id, boot: next.reboot.boot_id, requested: Date.parse(next.reboot.requested_at) }; save();
  }
  const f = flow.value;
  if (f) {
   if (!f.id && next.reboot?.boot_id === f.boot) { f.id = next.reboot.id; save(); }
   if (next.reboot?.id === f.id && ['failed', 'unconfirmed'].includes(next.reboot.phase)) { stage.value = 'Restart not confirmed'; error.value = next.reboot.detail; }
   else if (f.id && next.reboot?.id === f.id && next.reboot.phase === 'accepted' && next.boot_id !== f.boot) {
    f.returned ||= Date.now();
    if (f.returnedBoot !== next.boot_id) {
     f.returnedBoot = next.boot_id; apiClient.invalidate('/api/v1/');
     if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent('fluxo:server-restarted'));
    }
    save(); stage.value = 'Server is back · Checking services';
    services.value = await apiClient.getServerRebootServices(f.id);
    stage.value = returnedStage(next.boot_id, f, services.value, Date.now() - f.returned);
    if (services.value.every(s => s.state === 'running')) error.value = ''; 
   } else if (f.id && next.boot_id === f.boot) stage.value = returnedStage(next.boot_id, f, [], 0);
   else stage.value = 'Restart scheduling is unconfirmed';
  }
 } catch (e) {
  failures++;
  const text = message(e);
  if (text === 'Unauthorized') { stage.value = flow.value ? 'Sign in again to verify the restart' : ''; pollError.value = 'Your session expired. Sign in again; this does not confirm a restart.'; }
  else { pollError.value = text; if (flow.value) stage.value = status.value?.boot_id !== flow.value.boot && flow.value.returned ? 'Server is back · Service status unavailable' : Date.now() - flow.value.requested > 120_000 ? 'Still waiting for the server' : 'Waiting for server'; }
 } finally { polling = false; }
}
function tick() {
 void refresh().finally(() => { if (consumers) timer = setTimeout(tick, flow.value && !['Services running', 'Restart not confirmed', 'Server is back · No managed services were expected to start'].includes(stage.value) ? Math.min(15_000, 3000 * Math.max(1, failures)) : 30_000); });
}
async function restart() {
 if (requestInProgress || busy.value) return;
 requestInProgress = true;
 try {
  const yes = await useConfirm().confirm({ title: 'Restart server?', message: 'All hosted sites and Fluxo will be temporarily unavailable. Wait for the server to restart before continuing.', confirmText: 'Restart server', cancelText: 'Cancel', variant: 'danger' });
  if (!yes) return;
  busy.value = true; error.value = '';
  await refresh();
  if (!status.value || pollError.value) throw new Error('Refresh server status before restarting.');
  flow.value = { id: '', boot: status.value.boot_id, requested: Date.now() }; save(); stage.value = 'Restarting server…';
  try {
   const r: SystemReboot = await apiClient.restartServer(status.value.boot_id);
   flow.value.id = r.id; save(); stage.value = 'Restarting server…';
  } catch (e) { error.value = message(e); stage.value = 'Restart scheduling is unconfirmed'; /* Never automatically resubmit. */ }
  await refresh();
 } catch (e) { error.value = message(e); }
 finally { busy.value = false; requestInProgress = false; }
}
async function setUpdates(enabled: boolean) {
 if (busy.value) return;
 busy.value = true; error.value = '';
 try { const updates = await apiClient.setSystemSecurityUpdates(enabled); if (status.value) status.value.updates = updates; } catch (e) { error.value = message(e); } finally { busy.value = false; }
}
export function useServerSystem() {
 return { status, restartRequired, services, stage, error, pollError, busy, refresh, restart, setUpdates,
  restartPending: computed(() => busy.value || !!status.value?.reboot && status.value.reboot.boot_id === status.value.boot_id && status.value.reboot.phase !== 'failed'),
  start() { consumers++; if (consumers === 1) tick(); },
  stop() { consumers = Math.max(0, consumers - 1); if (!consumers) clearTimeout(timer); },
 };
}
