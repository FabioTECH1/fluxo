import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from './useConfirm';
import { protectionPasswordError, protectionUsernameError, redirectDestinationError, redirectSourceError } from '../types/siteNetwork';
import type { SiteNetworkState, SiteRedirect } from '../types/siteNetwork';

export function useSiteNetwork(siteId: () => string) {
  const state = ref<SiteNetworkState | null>(null);
  const loading = ref(false);
  const saving = ref(false);
  const confirming = ref(false);
  const error = ref('');
  const showPassword = ref(false);
  const username = ref('');
  const password = ref('');
  const showRedirect = ref(false);
  const editingRedirect = ref<string | null>(null);
  const source = ref('');
  const destination = ref('');
  const status = ref<301 | 302>(301);
  let requestVersion = 0;
  const { confirm } = useConfirm();
  const busy = computed(() => loading.value || saving.value || confirming.value);
  const editable = computed(() => Boolean(state.value?.managed && !state.value.pending) && !busy.value);
  const protectionEnabled = computed(() => Boolean(state.value?.managed && state.value.protection.enabled));
  const usernameError = computed(() => protectionUsernameError(username.value));
  const passwordError = computed(() => protectionPasswordError(password.value));
  const sourceError = computed(() => redirectSourceError(source.value));
  const destinationError = computed(() => redirectDestinationError(destination.value));
  const credentialsValid = computed(() => !usernameError.value && !passwordError.value);
  const redirectValid = computed(() => !sourceError.value && !destinationError.value && [301, 302].includes(status.value));

  async function refresh(preserveError = false) {
    const request = ++requestVersion;
    const id = siteId();
    loading.value = true;
    if (!preserveError) error.value = '';
    try {
      const next = await apiClient.getSiteNetwork(id);
      if (request === requestVersion && id === siteId()) state.value = next;
    } catch (failure: any) {
      if (request === requestVersion && id === siteId()) {
        // A failed observation must never leave stale controls editable.
        state.value = null;
        if (!preserveError) error.value = failure.message || 'Failed to load network settings.';
      }
    } finally {
      if (request === requestVersion && id === siteId()) loading.value = false;
    }
  }

  async function mutate(action: (id: string, revision: string) => Promise<SiteNetworkState>) {
    if (!state.value || saving.value) return false;
    const id = siteId();
    const revision = state.value.revision;
    saving.value = true;
    error.value = '';
    try {
      const next = await action(id, revision);
      if (id !== siteId()) return false;
      state.value = next;
      return true;
    } catch (failure: any) {
      if (id === siteId()) {
        error.value = failure.message || 'The network change could not be confirmed. Refresh the state before continuing.';
        // Read once to discover a conflict/interruption; never repeat a mutation.
        await refresh(true);
      }
      return false;
    } finally { saving.value = false; }
  }

  function openCredentials() {
    if (!editable.value) return;
    username.value = state.value?.protection.username || 'staging';
    password.value = '';
    showPassword.value = true;
  }

  async function saveCredentials() {
    if (!editable.value || !credentialsValid.value) return;
    // Clear plaintext even when the request fails. Saved credentials are never loaded.
    const nextPassword = password.value;
    password.value = '';
    if (await mutate((id, revision) => apiClient.updateSiteProtection(id, {
      expected_revision: revision, enabled: true, username: username.value, password: nextPassword,
    }))) showPassword.value = false;
  }

  async function toggleProtection(enabled: boolean) {
    if (!editable.value) return;
    if (enabled) { openCredentials(); return; }
    const id = siteId();
    confirming.value = true;
    const approved = await confirm({ title: 'Disable password protection?', message: 'Visitors will be able to access this site without the shared username and password.', confirmText: 'Disable protection', variant: 'danger' });
    confirming.value = false;
    if (!approved || id !== siteId() || !editable.value) return;
    await mutate((target, revision) => apiClient.updateSiteProtection(target, { expected_revision: revision, enabled: false }));
  }

  function openRedirect(redirect: SiteRedirect | null = null) {
    if (!editable.value) return;
    editingRedirect.value = redirect?.id ?? null;
    source.value = redirect?.source || '';
    destination.value = redirect?.destination || '';
    status.value = redirect?.status || 301;
    showRedirect.value = true;
  }

  async function saveRedirect() {
    if (!editable.value || !redirectValid.value) return;
    if (await mutate((id, revision) => apiClient.saveSiteRedirect(id, editingRedirect.value, {
      expected_revision: revision, source: source.value, destination: destination.value, status: status.value,
    }))) showRedirect.value = false;
  }

  async function deleteRedirect(redirect: SiteRedirect) {
    if (!editable.value) return;
    const id = siteId();
    confirming.value = true;
    const approved = await confirm({ title: 'Delete redirect?', message: `${redirect.source} will no longer redirect to ${redirect.destination}.`, confirmText: 'Delete redirect', variant: 'danger' });
    confirming.value = false;
    if (!approved || id !== siteId() || !editable.value) return;
    await mutate((target, revision) => apiClient.deleteSiteRedirect(target, redirect.id, revision));
  }

  async function recover() {
    if (!state.value?.pending || busy.value) return;
    const id = siteId();
    confirming.value = true;
    const approved = await confirm({ title: 'Restore last confirmed network configuration?', message: 'An interrupted network change has not been confirmed. Restore the last confirmed configuration before making further changes.', confirmText: 'Restore configuration', variant: 'danger' });
    confirming.value = false;
    if (!approved || id !== siteId() || !state.value?.pending) return;
    await mutate((target, revision) => apiClient.recoverSiteNetwork(target, revision));
  }

  function reset() {
    requestVersion++;
    state.value = null;
    error.value = '';
    showPassword.value = false;
    password.value = '';
    showRedirect.value = false;
  }

  return { state, loading, saving, busy, error, editable, protectionEnabled, refresh, reset,
    showPassword, username, password, usernameError, passwordError, credentialsValid, openCredentials, saveCredentials, toggleProtection,
    showRedirect, editingRedirect, source, destination, status, sourceError, destinationError, redirectValid, openRedirect, saveRedirect, deleteRedirect, recover };
}
