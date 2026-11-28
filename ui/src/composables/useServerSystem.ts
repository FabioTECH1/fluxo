import { APIError, apiErrorMessage } from '../utils/apiError';
import { computed, ref } from 'vue';
import { apiClient } from '../api/client';

export interface SystemReboot { id: string; boot_id: string; phase: string; detail: string; requested_at: string; expected_services: string[] }
export interface SystemStatus {
 boot_id: string;
 running_kernel: string | null;
 updates: { state: string; editable: boolean; detail: string; automatic_reboot: string; last_attempt?: string; last_result: string };
 restart_required: { state: string; packages: string[]; detail: string };
 reboot: SystemReboot | null;
}
export interface SystemService { unit: string; state: string; detail: string }
interface Flow { id: string; boot: string; requested: number; accepted?: boolean; returned?: number; returnedBoot?: string; checkedBoot?: string }
interface DismissedResult { id: string; boot: string; result: string }
const key = 'fluxo_server_restart';
const dismissalKey = 'fluxo_server_restart_dismissed';
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
function restoreDismissal(): DismissedResult | null {
 try {
  const value = JSON.parse(localStorage.getItem(dismissalKey) || 'null');
  return value && typeof value.id === 'string' && typeof value.boot === 'string' && typeof value.result === 'string' ? value : null;
 } catch { return null; }
}
const dismissedResult = ref(restoreDismissal());
const modalOpen = ref(false);
const restartView = ref<'details' | 'confirm' | 'progress'>(flow.value ? 'progress' : 'details');
const services = ref<SystemService[]>([]);
const stage = ref('');
const error = ref('');
const pollError = ref('');
const reconnecting = ref(false);
const busy = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
const refreshing = ref(false);
let refreshPromise: Promise<void> | null = null;
let observedResult = '';
let consumers = 0;
let requestInProgress = false;
let failures = 0;
let schedulingError = '';
const save = () => { try { if (flow.value) localStorage.setItem(key, JSON.stringify(flow.value)); } catch { /* Restricted browser storage cannot affect a server operation. */ } };
function message(e: unknown): string { return apiErrorMessage(e); }
function temporaryDisconnect(e: unknown): boolean {
 if (e instanceof APIError) return [502, 503, 504].includes(e.status) && e.payload === null
  && (!e.rawMessage.trim() || /^\s*</.test(e.rawMessage) || /^(?:Bad Gateway|Service Unavailable|Gateway Timeout)$/i.test(e.rawMessage.trim()));
 if (!(e instanceof Error)) return false;
 return ['TimeoutError', 'AbortError'].includes(e.name)
  || /^(?:Failed to fetch|NetworkError when attempting to fetch resource\.?|Load failed|fetch failed|network disconnected|offline)$/i.test(e.message);
}
export function returnedStage(currentBoot: string, f: Flow, s: SystemService[], elapsed: number): string {
 if (currentBoot === f.boot) return Date.now() - f.requested < 20_000 ? 'Restarting server…' : Date.now()-f.requested>120_000 ? 'Still waiting for the server' : 'Waiting for server';
 if (!s.length) return 'Server is back · No managed services were expected to start';
 if (s.every(v => v.state === 'running')) return 'Services running';
 return elapsed < 90_000 ? 'Server is back · Checking services' : 'Server is back · Some services need attention';
}
const canDismissResult = computed(() => !!flow.value?.id && !!status.value
 && status.value.reboot?.id === flow.value.id && status.value.reboot.phase === 'accepted'
 && status.value.boot_id !== flow.value.boot && flow.value.returnedBoot === status.value.boot_id
 && !pollError.value && ['Services running', 'Server is back · No managed services were expected to start', 'Server is back · Some services need attention'].includes(stage.value));
const recordedSchedulingFailure = computed(()=>!!flow.value?.id && status.value?.reboot?.id===flow.value.id && status.value.reboot.phase==='failed' && status.value.boot_id===flow.value.boot);
const failedScheduling = computed(()=>!pollError.value && recordedSchedulingFailure.value);
const resultIdentity = computed(() => JSON.stringify([stage.value, services.value.map(service => [service.unit, service.state]).sort((a,b) => a[0]!.localeCompare(b[0]!))]));
const resultDismissed = computed(() => canDismissResult.value && dismissedResult.value?.id === flow.value?.id
 && dismissedResult.value?.boot === status.value?.boot_id && dismissedResult.value?.result === resultIdentity.value);
const restartSteps = computed(() => {
 const f = flow.value;
 const returned = !!f?.accepted && !!f.returnedBoot && f.returnedBoot !== f.boot;
 const checked = returned && f?.checkedBoot === f?.returnedBoot;
 return [
  { label: 'Restart requested', state: recordedSchedulingFailure.value ? 'failed' : f?.accepted ? 'complete' : 'current' },
  { label: 'New boot confirmed', state: returned ? 'complete' : f?.accepted && !recordedSchedulingFailure.value ? 'current' : 'pending' },
  { label: 'Services checked', state: checked ? 'complete' : returned ? 'current' : 'pending' },
 ];
});
const restartDescription = computed(() => {
 if (reconnecting.value) return 'Fluxo is reconnecting automatically.';
 if (pollError.value) return '';
 if (stage.value === 'Services running') return 'Expected services are running.';
 if (stage.value.includes('No managed services')) return 'No managed services were expected to start.';
 if (stage.value.includes('need attention')) return 'Review the service status below.';
 if (stage.value === 'Server is back · Checking services') return 'Restart confirmed. Checking expected services.';
 if (stage.value.toLowerCase().includes('unconfirmed') || stage.value === 'Restart not confirmed') return 'The restart is not confirmed. Check status before requesting another.';
 if (flow.value?.accepted) return stage.value === 'Restarting server…' ? 'Restart request accepted.' : 'Fluxo is reconnecting automatically.';
 return 'Confirming the restart request.';
});
function dismissResult() {
 if (!canDismissResult.value || !flow.value || !status.value) return;
 dismissedResult.value = { id: flow.value.id, boot: status.value.boot_id, result: resultIdentity.value };
 try { localStorage.setItem(dismissalKey, JSON.stringify(dismissedResult.value)); } catch { /* Keep dismissal for this session when storage is unavailable. */ }
}
function refresh(): Promise<void> {
 if (refreshPromise) return refreshPromise;
 refreshPromise = readStatus().finally(() => { refreshPromise = null; });
 return refreshPromise;
}
async function readStatus() {
 refreshing.value = true;
 let readingServices = false;
 try {
  const next: SystemStatus = await apiClient.getServerSystem();
  // Unknown state never clears the last confirmed required notice.
  if (next.restart_required.state === 'unknown' && status.value?.restart_required.state === 'required') next.restart_required = status.value.restart_required;
  status.value = next; pollError.value = ''; reconnecting.value = false; failures = 0;
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
   if (f.id && next.reboot?.id === f.id && next.reboot.boot_id === f.boot && next.reboot.phase === 'accepted') {
    f.accepted = true; save();
    if (schedulingError && error.value === schedulingError) error.value = '';
    schedulingError = '';
   }
   if (next.reboot?.id === f.id && ['failed', 'unconfirmed'].includes(next.reboot.phase)) { stage.value = 'Restart not confirmed'; error.value = next.reboot.detail; }
   else if (f.id && next.reboot?.id === f.id && next.reboot.phase === 'accepted' && next.boot_id !== f.boot) {
    f.returned ||= Date.now();
    if (f.returnedBoot !== next.boot_id) {
     f.returned = Date.now(); f.returnedBoot = next.boot_id; services.value = []; observedResult = ''; apiClient.invalidate('/api/v1/');
     if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent('fluxo:server-restarted'));
    }
    save();
    const context = `${f.id}:${next.boot_id}`;
    const wasDismissed = dismissedResult.value?.id === f.id && dismissedResult.value?.boot === next.boot_id;
    // Retain a completed result during polling instead of flashing the notice.
    // A changed outcome or failed inspection below still becomes visible.
    if (observedResult !== context && !wasDismissed) stage.value = 'Server is back · Checking services';
    readingServices = true;
    const inspected = await apiClient.getServerRebootServices(f.id);
    if (flow.value !== f || status.value?.boot_id !== next.boot_id) return;
    services.value = inspected;
    observedResult = context;
    stage.value = returnedStage(next.boot_id, f, services.value, Date.now() - f.returned);
    if (['Services running', 'Server is back · No managed services were expected to start', 'Server is back · Some services need attention'].includes(stage.value)) { f.checkedBoot = next.boot_id; save(); }
    if (services.value.every(s => s.state === 'running')) error.value = ''; 
   } else if (f.accepted && next.boot_id === f.boot) stage.value = returnedStage(next.boot_id, f, [], 0);
   else stage.value = 'Restart scheduling is unconfirmed';
  }
 } catch (e) {
  failures++;
  const text = message(e);
  const diagnostic = text === 'Request failed' ? readingServices ? 'Could not check service status. Use Check status to try again.' : 'Could not read server status. Use Check status to try again.' : text;
  reconnecting.value = false;
  if (text === 'Unauthorized' || e instanceof APIError && e.status === 401) { stage.value = flow.value ? 'Sign in again to verify the restart' : ''; pollError.value = 'Your session expired. Sign in again; this does not confirm a restart.'; }
  else {
   pollError.value = diagnostic;
   const f = flow.value;
   if (f) {
    reconnecting.value = !!f.accepted && !recordedSchedulingFailure.value && !readingServices && temporaryDisconnect(e);
    stage.value = readingServices ? 'Server is back · Service status unavailable'
     : recordedSchedulingFailure.value ? 'Restart not confirmed'
     : !f.accepted ? 'Restart scheduling is unconfirmed'
     : f.returnedBoot ? 'Reconnecting to server'
     : Date.now() - f.requested > 120_000 ? 'Still waiting for the server' : 'Waiting for server';
   }
  }
 } finally { refreshing.value = false; }
}
function tick() {
 void refresh().finally(() => { if (consumers) timer = setTimeout(tick, flow.value && !['Services running', 'Restart not confirmed', 'Server is back · No managed services were expected to start'].includes(stage.value) ? Math.min(15_000, 3000 * Math.max(1, failures, flow.value && Date.now()-flow.value.requested>120_000 ? 5 : 1)) : 30_000); });
}
function restart() {
 restartView.value = flow.value && !canDismissResult.value && !failedScheduling.value ? 'progress' : 'details';
 modalOpen.value = true;
}
function showRestartDetails(){restartView.value='details';modalOpen.value=true;}
function showRestartStatus(){restartView.value='progress';modalOpen.value=true;}
function reviewRestart() { if (modalOpen.value && !requestInProgress && !busy.value && (!flow.value || canDismissResult.value || failedScheduling.value)) restartView.value = 'confirm'; }
async function confirmRestart() {
 if (requestInProgress || busy.value || !modalOpen.value || restartView.value !== 'confirm' || flow.value && !canDismissResult.value && !failedScheduling.value) return;
 requestInProgress = true;
 try {
  busy.value = true; error.value = ''; schedulingError = '';
  await refresh();
  if (status.value?.reboot && status.value.reboot.boot_id === status.value.boot_id && status.value.reboot.phase !== 'failed') throw new Error('A restart is already pending. Check its status before requesting another.');
  if (!status.value || pollError.value) throw new Error('Refresh server status before restarting.');
  flow.value = { id: '', boot: status.value.boot_id, requested: Date.now() }; save(); stage.value = 'Restarting server…';
  services.value = []; observedResult = ''; restartView.value = 'progress'; modalOpen.value = true;
  try {
   const r: SystemReboot = await apiClient.restartServer(status.value.boot_id);
   flow.value.id = r.id; flow.value.accepted = r.phase === 'accepted' && r.boot_id === flow.value.boot; save(); stage.value = 'Restarting server…';
  } catch (e) { schedulingError = message(e); error.value = schedulingError; stage.value = 'Restart scheduling is unconfirmed'; /* Never automatically resubmit. */ }
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
 return { showRestartDetails,showRestartStatus,modalOpen, restartView, reviewRestart, confirmRestart, status, restartRequired, services, stage, restartSteps, restartDescription, reconnecting, error, pollError, busy, refreshing, refresh, restart, setUpdates, canDismissResult, resultDismissed, dismissResult,
  restartPending: computed(() => busy.value || !!status.value?.reboot && status.value.reboot.boot_id === status.value.boot_id && status.value.reboot.phase !== 'failed'),
  start() { consumers++; if (consumers === 1) tick(); },
  stop() { consumers = Math.max(0, consumers - 1); if (!consumers) clearTimeout(timer); },
 };
}
