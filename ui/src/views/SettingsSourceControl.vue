<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Source Control Accounts</h2>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Manage GitHub connections and deployment access.</p>
      </div>
      <AppButton :disabled="busy" @click="openConnect">Add Account</AppButton>
    </div>
    <ErrorAlert :message="loadError" />
    <SkeletonLoader v-if="loading && !accounts.length" type="table" />
    <Card v-else-if="!accounts.length">
      <EmptyState message="No GitHub accounts connected" />
      <div class="flex justify-center pb-4"><AppButton variant="secondary" size="sm" :disabled="busy" @click="openConnect">Connect GitHub Account</AppButton></div>
    </Card>
    <div v-else class="space-y-4">
      <SourceControlAccountCard v-for="account in accounts" :key="account.id" :account="account" :now="now" :busy="busy" :checking="checkingId === account.id" @check="checkAccount(account)" @update="openUpdate(account)" @disconnect="disconnectAccount(account)" />
      <p class="text-xs text-gray-500 dark:text-gray-400">Updating a token keeps your linked sites. Deployment access is checked separately. Expiry is unavailable when GitHub does not provide a date.</p>
    </div>

    <BaseModal v-model="showTokenModal" :title="legacyRecovery ? 'Recover Legacy GitHub Connection' : editing ? (tokenPresentation(editing, now).reconnect ? 'Reconnect GitHub Account' : 'Update GitHub Token') : 'Connect GitHub Account'" :loading="saving" :confirm-text="legacyRecovery ? 'Recover connection & verify' : editing ? 'Save token & verify' : 'Connect'" @submit="formRef?.requestSubmit()">
      <form ref="formRef" class="space-y-4" @submit.prevent="saveToken">
        <ErrorAlert :message="error" />
        <p v-if="editing" class="text-sm text-gray-600 dark:text-gray-400">Replace the token for <strong>{{ editing.name }}</strong> (@{{ editing.username || 'unknown' }}). Your linked sites stay connected. Fluxo will verify deployment access and restore missing deploy-key registrations where permitted.</p>
        <FormGroup v-if="!editing" label="Account Label (Optional)" for-attr="github-account-name" hint="Give this account a nickname. Defaults to your GitHub username.">
          <input id="github-account-name" v-model="form.name" maxlength="100" type="text" class="w-full rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 focus:ring-2 focus:ring-blue-500" placeholder="e.g. Work GitHub">
        </FormGroup>
        <FormGroup label="Personal Access Token" for-attr="github-account-token" hint="Use a classic token with repo and admin:public_key scopes. Fine-grained tokens need access to the selected repositories and Administration write permission to repair deploy keys.">
          <input id="github-account-token" v-model="form.token" type="password" required maxlength="4096" autocomplete="new-password" spellcheck="false" class="w-full rounded-lg border border-gray-200 px-3 py-2 font-mono dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 focus:ring-2 focus:ring-blue-500" placeholder="ghp_xxxxxxxxxxxxxxx">
        </FormGroup>
        <fieldset v-if="legacyRecovery && editing" class="space-y-4 rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-900 dark:bg-amber-950/20" :disabled="saving">
          <legend class="px-1 text-sm font-semibold text-amber-900 dark:text-amber-200">Administrator recovery</legend>
          <p class="text-sm text-gray-700 dark:text-gray-300">The old token cannot verify this legacy account. Review the sites below, then authorize the account authenticated by your new token to manage this connection. This establishes its verified identity from now on; it does not prove historical ownership. Site links and existing private keys stay in place.</p>
          <ul v-if="editing.deployment_access.sites.length" class="max-h-36 space-y-2 overflow-auto text-xs text-gray-700 dark:text-gray-300">
            <li v-for="site in editing.deployment_access.sites" :key="site.site_id" class="break-words"><strong>{{ site.domain }}</strong> · {{ site.repository }}</li>
          </ul>
          <p v-else class="text-sm text-gray-600 dark:text-gray-400">No linked repositories.</p>
          <FormGroup label="GitHub username for the new token" for-attr="github-recovery-username" hint="Type the username of the GitHub account whose new token you entered.">
            <input id="github-recovery-username" v-model.trim="recovery.username" required maxlength="100" autocomplete="off" spellcheck="false" class="w-full rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" />
          </FormGroup>
          <FormGroup label="Your Fluxo panel password" for-attr="github-recovery-password">
            <input id="github-recovery-password" v-model="recovery.password" type="password" required maxlength="1024" autocomplete="current-password" class="w-full rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" />
          </FormGroup>
          <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="recovery.confirm" type="checkbox" required class="mt-1" />
            <span>I authorize this GitHub account to manage this legacy connection and its listed site repositories.</span>
          </label>
        </fieldset>
        <div class="rounded-lg border border-blue-100 bg-blue-50 p-4 dark:border-blue-900/50 dark:bg-blue-950/20">
          <a href="https://github.com/settings/tokens/new?scopes=repo,admin:public_key&description=Fluxo%20Server" target="_blank" rel="noopener noreferrer" class="text-xs font-bold text-blue-600 underline dark:text-blue-400">Generate pre-configured token on GitHub ↗</a>
        </div>
      </form>
    </BaseModal>

    <BaseModal v-model="showAccessModal" title="Deployment access" max-width="max-w-2xl" hide-footer>
      <div v-if="accessAccount" class="space-y-4">
        <div class="flex flex-wrap items-center gap-2"><h3 class="text-sm font-semibold">{{ accessAccount.name }}</h3><StatusBadge :label="tokenPresentation(accessAccount, now).label" :variant="tokenPresentation(accessAccount, now).variant" /></div>
        <p v-if="accessAccount.token_status === 'invalid'" class="text-sm text-red-700 dark:text-red-300">GitHub rejected this token. Reconnect with a new token for the same account. SSH deployment access is checked independently.</p>
        <p v-else-if="accessAccount.token_status === 'error'" class="text-sm text-yellow-700 dark:text-yellow-300">GitHub validation is unavailable. Retry the check and review token permissions, organization policy, API limits and connectivity.</p>
        <p class="text-sm text-gray-600 dark:text-gray-400">{{ deploymentAccessLabel(accessAccount) }}<span v-if="accessAccount.deployment_access.checked_at"> · Checked {{ formatGitHubDate(accessAccount.deployment_access.checked_at) }}</span></p>
        <div v-for="site in accessAccount.deployment_access.sites" :key="site.site_id" class="border-t border-gray-100 pt-4 dark:border-gray-800">
          <div class="flex flex-wrap items-center gap-2"><span class="text-sm font-semibold">{{ site.domain }}</span><StatusBadge :label="site.status === 'verified' ? 'Verified' : site.status === 'error' ? 'Needs attention' : 'Not checked'" :variant="site.status === 'verified' ? 'green' : site.status === 'error' ? 'yellow' : 'gray'" /></div>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 break-words">{{ site.repository }}</p>
          <p v-if="site.message" class="mt-2 text-sm text-gray-600 dark:text-gray-400">{{ site.message }}</p>
        </div>
        <p v-if="accessAccount.deployment_access.sites.some(site => site.status === 'error')" class="text-sm text-gray-600 dark:text-gray-400">Update the token to retry deploy-key registration. The token needs permission to manage deploy keys. Missing or invalid private keys must be restored on the server.</p>
        <p class="text-xs text-gray-500 dark:text-gray-400">Check access does not change keys or start a deployment.</p>
        <div class="flex justify-end"><AppButton variant="secondary" @click="showAccessModal = false">Close</AppButton></div>
      </div>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onMounted, onUnmounted, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import Card from '../components/Card.vue';
import EmptyState from '../components/EmptyState.vue';
import SourceControlAccountCard from '../components/SourceControlAccountCard.vue';
import StatusBadge from '../components/StatusBadge.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import BaseModal from '../components/BaseModal.vue';
import FormGroup from '../components/FormGroup.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import { type GitHubAccount, tokenPresentation, deploymentAccessLabel, formatGitHubDate, requiresLegacyGitHubRecovery } from '../utils/githubConnection';

const { addToast } = useToast();
const { confirm } = useConfirm();
const accounts = ref<GitHubAccount[]>([]);
const loading = ref(true);
const saving = ref(false);
const checkingId = ref<number | null>(null);
const disconnectingId = ref<number | null>(null);
const busy = computed(() => saving.value || checkingId.value !== null || disconnectingId.value !== null);
const showTokenModal = ref(false);
const showAccessModal = ref(false);
const editing = ref<GitHubAccount | null>(null);
const accessAccount = ref<GitHubAccount | null>(null);
const error = ref('');
const loadError = ref('');
const formRef = ref<HTMLFormElement | null>(null);
const form = ref({ name: '', token: '' });
const legacyRecovery = ref(false);
const recovery = ref({ username: '', password: '', confirm: false });
const now = ref(Date.now());
let timer: ReturnType<typeof setInterval> | undefined;
let loadVersion = 0;
watch(showTokenModal, open => {
  if (!open) {
    form.value.token = '';
    recovery.value = { username: '', password: '', confirm: false };
    legacyRecovery.value = false;
  }
});

async function fetchAccounts(bypassCache = false) {
  const version = ++loadVersion;
  loading.value = true;
  loadError.value = '';
  try {
    const result = await apiClient.getGithubAccounts(bypassCache) || [];
    if (version === loadVersion) accounts.value = result;
  } catch (e: any) {
    if (version === loadVersion) loadError.value = e.message || 'Failed to load source control accounts';
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}
function resetRecovery() { legacyRecovery.value = false; recovery.value = { username: '', password: '', confirm: false }; }
function openConnect() { resetRecovery(); editing.value = null; form.value = { name: '', token: '' }; error.value = ''; showTokenModal.value = true; }
function openUpdate(account: GitHubAccount) { resetRecovery(); editing.value = account; form.value = { name: '', token: '' }; error.value = ''; showTokenModal.value = true; }
async function saveToken() {
  if (busy.value) return;
  saving.value = true;
  error.value = '';
  try {
    const result: GitHubAccount = editing.value
      ? await apiClient.updateGithubAccount(editing.value.id, {
        token: form.value.token,
        ...(legacyRecovery.value ? { legacy_recovery: {
          ...recovery.value,
          sites: editing.value.deployment_access.sites.map(site => ({ site_id: site.site_id, repository: site.repository })),
        } } : {}),
      })
      : await apiClient.connectGithubAccount(form.value);
    showTokenModal.value = false;
    form.value.token = '';
    addToast(editing.value ? 'GitHub token updated' : 'GitHub account connected', 'success');
    if (result.deployment_access?.sites.some(site => site.status !== 'verified')) { accessAccount.value = result; showAccessModal.value = true; }
  } catch (e: any) {
    error.value = e.message || 'Failed to save GitHub token';
    if (requiresLegacyGitHubRecovery(editing.value, error.value)) legacyRecovery.value = true;
  }
  finally { recovery.value.password = ''; saving.value = false; await fetchAccounts(true); }
}
async function checkAccount(account: GitHubAccount) {
  if (busy.value) return;
  checkingId.value = account.id;
  try { accessAccount.value = await apiClient.checkGithubAccount(account.id); showAccessModal.value = true; }
  catch (e: any) { addToast(e.message || 'Failed to check GitHub access', 'error'); }
  finally { checkingId.value = null; await fetchAccounts(true); }
}
async function disconnectAccount(account: GitHubAccount) {
  if (busy.value) return;
  const accepted = await confirm({ title: 'Disconnect GitHub Account', message: `Disconnect "${account.name}"? The saved token will be removed and ${account.linked_sites} linked site(s) will lose their account connection. Existing applications keep running. Use Update token to replace credentials while keeping sites connected.`, confirmText: 'Disconnect', variant: 'danger' });
  if (!accepted || busy.value) return;
  disconnectingId.value = account.id;
  try { await apiClient.disconnectGithubAccount(account.id); addToast('GitHub account disconnected', 'success'); }
  catch (e: any) { addToast(e.message || 'Failed to disconnect account', 'error'); }
  finally { disconnectingId.value = null; await fetchAccounts(true); }
}
onMounted(() => { void fetchAccounts(); timer = setInterval(() => { now.value = Date.now(); }, 60_000); });
onActivated(() => { now.value = Date.now(); void fetchAccounts(); });
onUnmounted(() => { if (timer) clearInterval(timer); });
</script>
