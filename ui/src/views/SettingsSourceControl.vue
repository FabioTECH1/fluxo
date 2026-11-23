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

    <BaseModal v-model="showTokenModal" :title="legacyRecovery ? 'Recover GitHub Connection' : editing ? (tokenPresentation(editing, now).reconnect ? 'Reconnect GitHub Account' : 'Update GitHub Token') : 'Connect GitHub Account'" :loading="saving" :confirm-text="legacyRecovery ? 'Approve & reconnect' : editing ? (editing.github_user_id ? 'Save token & verify' : 'Continue') : 'Connect'" @submit="formRef?.requestSubmit()">
      <form ref="formRef" class="space-y-4" @submit.prevent="saveToken">
        <ErrorAlert :message="error" />
        <p v-if="editing && !legacyRecovery" class="text-sm text-gray-600 dark:text-gray-400">Update GitHub access for <strong>{{ editing.name }}</strong>. Your sites and deploy keys stay linked.</p>
        <FormGroup v-if="!editing" label="Account Label (Optional)" for-attr="github-account-name" hint="Give this account a nickname. Defaults to your GitHub username.">
          <input id="github-account-name" v-model="form.name" maxlength="100" type="text" class="w-full rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 focus:ring-2 focus:ring-blue-500" placeholder="e.g. Work GitHub">
        </FormGroup>
        <FormGroup label="Personal Access Token" for-attr="github-account-token" :hint="legacyRecovery ? '' : 'Classic: repo and admin:public_key. Fine-grained: repository access and Administration write.'">
          <input id="github-account-token" v-model="form.token" :readonly="saving" type="password" required maxlength="4096" autocomplete="new-password" spellcheck="false" class="w-full rounded-lg border border-gray-200 px-3 py-2 font-mono dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 focus:ring-2 focus:ring-blue-500" placeholder="ghp_xxxxxxxxxxxxxxx">
        </FormGroup>
        <p v-if="editing && !legacyRecovery" class="text-xs text-gray-500 dark:text-gray-400">Saving retries existing-key registration and checks access. Missing private keys still need server recovery.</p>
        <fieldset v-if="legacyRecovery && recoveryReview" class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-gray-700" :disabled="saving">
          <legend class="px-1 text-sm font-semibold text-gray-900 dark:text-gray-100">Confirm connection</legend>
          <dl class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div class="min-w-0 rounded-lg bg-gray-50 p-3 dark:bg-gray-800">
              <dt class="text-xs text-gray-500 dark:text-gray-400">Stored connection</dt>
              <dd class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-gray-100">{{ recoveryReview.stored.username ? `@${recoveryReview.stored.username}` : recoveryReview.stored.name || 'Unknown' }}</dd>
              <dd class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ recoveryReview.stored.github_user_id ? `GitHub ID ${recoveryReview.stored.github_user_id}` : 'GitHub ID not recorded' }}</dd>
            </div>
            <div class="min-w-0 rounded-lg bg-blue-50 p-3 dark:bg-blue-950/30">
              <dt class="text-xs text-gray-500 dark:text-gray-400">New token · verified</dt>
              <dd class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-gray-100">@{{ recoveryReview.proposed.username }}</dd>
              <dd class="mt-1 text-xs text-gray-500 dark:text-gray-400">GitHub ID {{ recoveryReview.proposed.github_user_id }}</dd>
            </div>
          </dl>
          <p class="text-sm text-gray-600 dark:text-gray-400">Your sites and existing deploy keys stay linked.</p>
          <ul v-if="recoveryReview.sites.length" class="max-h-36 space-y-2 overflow-auto text-xs text-gray-700 dark:text-gray-300" aria-label="Sites kept linked">
            <li v-for="site in recoveryReview.sites" :key="site.site_id" class="break-words"><strong>{{ site.domain }}</strong> · {{ site.repository }}</li>
          </ul>
          <p v-else class="text-sm text-gray-600 dark:text-gray-400">No linked repositories.</p>
          <FormGroup label="Fluxo panel password" for-attr="github-recovery-password">
            <input id="github-recovery-password" v-model="recovery.password" type="password" required maxlength="1024" autocomplete="current-password" class="w-full rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" />
          </FormGroup>
          <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="recovery.confirm" type="checkbox" required class="mt-1" />
            <span>I approve using @{{ recoveryReview.proposed.username }} for these sites.</span>
          </label>
        </fieldset>
        <div v-if="!legacyRecovery" class="rounded-lg border border-blue-100 bg-blue-50 p-4 dark:border-blue-900/50 dark:bg-blue-950/20">
          <a href="https://github.com/settings/tokens/new?scopes=repo,admin:public_key&description=Fluxo%20Server" target="_blank" rel="noopener noreferrer" class="text-xs font-bold text-blue-600 underline dark:text-blue-400">Generate pre-configured token on GitHub ↗</a>
        </div>
      </form>
    </BaseModal>

    <GitHubAccessModal v-if="accessAccount" v-model="showAccessModal" :account="accessAccount" :now="now" :busy="busy" :checking="checkingId === accessAccount.id" @retry="checkAccount(accessAccount)" @update="updateFromAccess" />
  </div>
</template>

<script setup lang="ts">
import { apiErrorMessage } from '../utils/apiError';
import { computed, onActivated, onMounted, onUnmounted, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import Card from '../components/Card.vue';
import EmptyState from '../components/EmptyState.vue';
import SourceControlAccountCard from '../components/SourceControlAccountCard.vue';
import GitHubAccessModal from '../components/GitHubAccessModal.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import BaseModal from '../components/BaseModal.vue';
import FormGroup from '../components/FormGroup.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import { type GitHubAccount, type GitHubRecoveryReview, tokenPresentation, requiresLegacyGitHubRecovery } from '../utils/githubConnection';

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
const recoveryReview = ref<GitHubRecoveryReview | null>(null);
const reviewedToken = ref('');
const recovery = ref({ username: '', password: '', confirm: false });
const now = ref(Date.now());
let timer: ReturnType<typeof setInterval> | undefined;
let loadVersion = 0;
watch(showTokenModal, open => {
  if (!open) {
    form.value.token = '';
    recovery.value = { username: '', password: '', confirm: false };
    legacyRecovery.value = false;
    recoveryReview.value = null;
    reviewedToken.value = '';
  }
});
watch(() => form.value.token, token => { if (reviewedToken.value && token !== reviewedToken.value) resetRecovery(); });

async function fetchAccounts(bypassCache = false) {
  const version = ++loadVersion;
  loading.value = true;
  loadError.value = '';
  try {
    const result: GitHubAccount[] = await apiClient.getGithubAccounts(bypassCache) || [];
    if (version === loadVersion) {
      accounts.value = result;
      if (accessAccount.value) accessAccount.value = result.find(account => account.id === accessAccount.value!.id) || null;
    }
  } catch (e: any) {
    if (version === loadVersion) loadError.value = apiErrorMessage(e, 'Failed to load source control accounts');
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}
function resetRecovery() { legacyRecovery.value = false; recoveryReview.value = null; reviewedToken.value = ''; recovery.value = { username: '', password: '', confirm: false }; }
function openConnect() { resetRecovery(); editing.value = null; form.value = { name: '', token: '' }; error.value = ''; showTokenModal.value = true; }
function openUpdate(account: GitHubAccount) { if (busy.value) return; resetRecovery(); editing.value = account; form.value = { name: '', token: '' }; error.value = ''; showTokenModal.value = true; }
function updateFromAccess() {
  if (busy.value || !accessAccount.value) return;
  const account = accessAccount.value;
  showAccessModal.value = false;
  openUpdate(account);
}
async function saveToken() {
  if (busy.value) return;
  saving.value = true;
  error.value = '';
  try {
    if (editing.value && !editing.value.github_user_id && !recoveryReview.value) {
      const accountID = editing.value.id;
      const token = form.value.token;
      const review = await apiClient.reviewGithubRecovery(accountID, { token });
      if (!showTokenModal.value || editing.value?.id !== accountID || form.value.token !== token) return;
      if (review.needs_recovery) {
        recoveryReview.value = review;
        reviewedToken.value = token;
        recovery.value = { username: review.proposed.username, password: '', confirm: false };
        legacyRecovery.value = true;
        return;
      }
    }
    const result: GitHubAccount = editing.value
      ? await apiClient.updateGithubAccount(editing.value.id, {
        token: form.value.token,
        ...(legacyRecovery.value ? { legacy_recovery: {
          ...recovery.value,
          github_user_id: recoveryReview.value!.proposed.github_user_id,
          sites: recoveryReview.value!.sites.map(site => ({ site_id: site.site_id, repository: site.repository })),
        } } : {}),
      })
      : await apiClient.connectGithubAccount(form.value);
    showTokenModal.value = false;
    form.value.token = '';
    addToast(editing.value ? 'GitHub token updated' : 'GitHub account connected', 'success');
    if (result.deployment_access?.sites.some(site => site.status !== 'verified')) { accessAccount.value = result; showAccessModal.value = true; }
  } catch (e: any) {
    error.value = apiErrorMessage(e, 'Failed to save GitHub token');
    if (requiresLegacyGitHubRecovery(editing.value, error.value)) resetRecovery();
  }
  finally { recovery.value.password = ''; saving.value = false; await fetchAccounts(true); }
}
async function checkAccount(account: GitHubAccount) {
  if (busy.value) return;
  checkingId.value = account.id;
  try { accessAccount.value = await apiClient.checkGithubAccount(account.id); showAccessModal.value = true; }
  catch (e: any) { addToast(apiErrorMessage(e, 'Failed to check GitHub access'), 'error'); }
  finally { checkingId.value = null; await fetchAccounts(true); }
}
async function disconnectAccount(account: GitHubAccount) {
  if (busy.value) return;
  const accepted = await confirm({ title: 'Disconnect GitHub Account', message: `Disconnect "${account.name}"? The saved token will be removed and ${account.linked_sites} linked site(s) will lose their account connection. Existing applications keep running. Use Update token to replace credentials while keeping sites connected.`, confirmText: 'Disconnect', variant: 'danger' });
  if (!accepted || busy.value) return;
  disconnectingId.value = account.id;
  try { await apiClient.disconnectGithubAccount(account.id); addToast('GitHub account disconnected', 'success'); }
  catch (e: any) { addToast(apiErrorMessage(e, 'Failed to disconnect account'), 'error'); }
  finally { disconnectingId.value = null; await fetchAccounts(true); }
}
onMounted(() => { void fetchAccounts(); timer = setInterval(() => { now.value = Date.now(); }, 60_000); });
onActivated(() => { now.value = Date.now(); void fetchAccounts(); });
onUnmounted(() => { if (timer) clearInterval(timer); });
</script>
