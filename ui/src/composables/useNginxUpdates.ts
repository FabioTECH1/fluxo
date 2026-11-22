import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
export interface NginxOrigin { origin: string; archive: string; site: string; component: string; trusted: boolean }
export interface NginxSnapshot { state: 'current' | 'update_available' | 'held' | 'unavailable' | 'unsupported'; reason: string; binary: string; package: string; architecture: string; installed_version: string; candidate_version: string; sources: NginxOrigin[]; held: boolean; source_identity: string; identity: string }
export interface NginxUpdateStatus { snapshot: NginxSnapshot | null; last_successful_check: number; attempted_at: number; error: string; stale: boolean; checking: boolean; notice: boolean; running_adoption: 'not_verified' }
const state = ref<NginxUpdateStatus | null>(null);
const busy = ref(false);
const error = ref('');
const instructionsOpen = ref(false);
let clients = 0, timer: ReturnType<typeof setInterval> | undefined;
let reading: Promise<void> | null = null;
let generation = 0;
const failureMessage = (failure: unknown) => {
  const message = failure instanceof Error ? failure.message : 'The Nginx package check could not be confirmed.';
  try { const parsed = JSON.parse(message); if (typeof parsed.message === 'string') return parsed.message; } catch { /* Plain errors already contain the useful diagnostic. */ }
  return message;
};
async function refresh() {
  if (busy.value) return;
  if (reading) return reading;
  const request = generation;
  reading = (async () => {
    try { const next = await apiClient.getNginxUpdates(); if (request === generation && !busy.value) { state.value = next; error.value = ''; } }
    catch (failure) { if (request !== generation) return; error.value = failureMessage(failure); if (state.value) state.value = { ...state.value, stale: true, notice: false, error: error.value }; }
  })();
  try { await reading; } finally { reading = null; }
}
async function check() {
  if (busy.value || state.value?.checking) return;
  const request = ++generation;
  busy.value = true; error.value = '';
  try {
    const next = await apiClient.checkNginxUpdates();
    if (request !== generation) return;
    state.value = next; error.value = next.error;
    return next;
  } catch (failure) {
    if (request !== generation) return;
    error.value = failureMessage(failure);
    if (state.value) state.value = { ...state.value, stale: true, notice: false, error: error.value };
  } finally { busy.value = false; }
}
async function dismiss() {
  if (busy.value || !state.value?.snapshot?.identity) return;
  const request = ++generation;
  busy.value = true;
  try { const next = await apiClient.dismissNginxUpdate(state.value.snapshot.identity); if (request === generation) state.value = next; }
  catch (failure) { error.value = failureMessage(failure); }
  finally { busy.value = false; }
}
export function nginxCheckMessage(status: NginxUpdateStatus | null) {
  if (!status) return 'Check configured APT repositories for an applicable package update.';
  if (status.checking) return 'Checking configured package repositories…';
  if (status.error || status.stale) return status.snapshot ? 'Previous result — a fresh check is needed.' : 'No successful package check is available.';
  switch (status.snapshot?.state) {
    case 'current': return 'No Nginx update is offered by the current APT policy.';
    case 'update_available': return 'A Nginx package update is available.';
    case 'held': return 'A newer candidate exists, but the package is held.';
    case 'unsupported': return 'Package update applicability is unsupported or unknown.';
    default: return 'No applicable package candidate could be confirmed.';
  }
}
// Text only, never executed. Quote metadata and pin the reviewed revision;
// operators must recheck current policy, holds and the simulation over SSH.
export function nginxSSHCommands(snapshot: NginxSnapshot | null): string {
  if (!snapshot || !['update_available', 'held'].includes(snapshot.state) || !/^nginx(?:-[a-z0-9][a-z0-9+.-]*)?$/.test(snapshot.package) || !/^[a-z0-9][a-z0-9-]*$/.test(snapshot.architecture) || !/^[0-9A-Za-z.+:~_-]+$/.test(snapshot.candidate_version)) return '';
  const quote = (s: string) => `'${s.replaceAll("'", "'\\''")}'`;
  const pkg = quote(`${snapshot.package}:${snapshot.architecture}`);
  const target = quote(`${snapshot.package}:${snapshot.architecture}=${snapshot.candidate_version}`);
  return `sudo apt-get update\napt-cache policy ${pkg}\napt-mark showhold\nsudo apt-get --simulate install --only-upgrade ${target}\n\n# Only after reviewing policy, holds and every proposed change:\nsudo apt-get install --only-upgrade ${target}\nsudo nginx -t\n# If the config test succeeds, in your chosen maintenance window:\nsudo systemctl restart nginx\nsudo systemctl status nginx --no-pager`;
}
export function useNginxUpdates() {
  return { state, busy, error, instructionsOpen, refresh, check, dismiss,
    available: computed(() => Boolean(state.value?.notice && !state.value.stale && !state.value.error)),
    openInstructions: () => { instructionsOpen.value = true; },
    start: () => { if (++clients === 1) { void refresh(); timer = setInterval(() => { if (!busy.value) void refresh(); }, 60000); } },
    stop: () => { if (clients > 0 && --clients === 0) { clearInterval(timer); timer = undefined; generation++; state.value = null; error.value = ''; instructionsOpen.value = false; } },
  };
}
