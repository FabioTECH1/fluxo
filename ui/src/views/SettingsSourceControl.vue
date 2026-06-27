<template>
  <div class="space-y-6">
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Source Control Accounts</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Connect one or more GitHub accounts to allow Fluxo to list your repositories, register webhooks, and inject SSH deploy keys.
          </p>
        </div>
        <AppButton variant="primary" @click="showAddModal = true">Add Account</AppButton>
      </div>

      <SkeletonLoader v-if="loading" type="table" />
      
      <div v-else-if="accounts.length === 0" class="flex flex-col items-center justify-center py-12 border-2 border-dashed border-gray-200 dark:border-gray-800 rounded-lg">
        <svg class="w-12 h-12 text-gray-400 dark:text-gray-600 mb-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-.778.099-1.533.284-2.253" />
        </svg>
        <p class="text-sm font-medium text-gray-900 dark:text-gray-100">No accounts connected</p>
        <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 mb-4">Get started by connecting your first GitHub account.</p>
        <AppButton variant="secondary" size="sm" @click="showAddModal = true">Connect GitHub Account</AppButton>
      </div>

      <div v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <div v-for="account in accounts" :key="account.id" class="flex items-center justify-between py-4 first:pt-0 last:pb-0">
          <div class="flex items-center gap-3">
            <div class="p-2 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <!-- GitHub Icon -->
              <svg class="w-6 h-6 text-gray-700 dark:text-gray-300" fill="currentColor" viewBox="0 0 24 24">
                <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.603-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.579.688.481C19.138 20.162 22 16.418 22 12c0-5.523-4.477-10-10-10z" />
              </svg>
            </div>
            <div>
              <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                Connected on {{ formatDate(account.created_at) }}
              </p>
            </div>
          </div>
          <AppButton variant="danger" size="sm" :loading="disconnectingId === account.id" @click="disconnectAccount(account)">
            Disconnect
          </AppButton>
        </div>
      </div>
    </div>

    <!-- Connect Account Modal -->
    <BaseModal v-model="showAddModal" title="Connect GitHub Account" :loading="connecting" confirm-text="Connect" @submit="formRef?.requestSubmit()">
      <form ref="formRef" @submit.prevent="connectAccount" class="space-y-4">
        <ErrorAlert :message="error" />

        <FormGroup label="Account Label (Optional)" hint="Give this account a nickname to identify it (e.g. Work GitHub). Defaults to your GitHub username.">
          <input v-model="form.name" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. Work GitHub">
        </FormGroup>

        <FormGroup label="Personal Access Token" hint="Requires a Classic Personal Access Token with repo and admin:public_key scopes.">
          <input v-model="form.token" type="password" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="ghp_xxxxxxxxxxxxxxx">
        </FormGroup>

        <div class="p-4 bg-blue-50 dark:bg-blue-950/20 border border-blue-100 dark:border-blue-900/50 rounded-lg">
          <a 
            href="https://github.com/settings/tokens/new?scopes=repo,admin:public_key&description=Fluxo%20Server" 
            target="_blank" 
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-xs font-bold text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 transition-colors underline"
          >
            Generate pre-configured token on GitHub ↗
          </a>
        </div>
      </form>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import BaseModal from '../components/BaseModal.vue';
import FormGroup from '../components/FormGroup.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';

const { addToast } = useToast();
const { confirm } = useConfirm();

const accounts = ref<any[]>([]);
const loading = ref(true);
const connecting = ref(false);
const disconnectingId = ref<number | null>(null);
const showAddModal = ref(false);
const error = ref('');
const formRef = ref<HTMLFormElement | null>(null);

const form = ref({
  name: '',
  token: ''
});

const fetchAccounts = async (bypassCache = false) => {
  try {
    loading.value = true;
    accounts.value = await apiClient.getGithubAccounts(bypassCache) || [];
  } catch (e: any) {
    console.error('Failed to load accounts:', e);
    addToast(e.message || 'Failed to load source control accounts', 'error');
  } finally {
    loading.value = false;
  }
};

const connectAccount = async () => {
  error.value = '';
  connecting.value = true;
  try {
    await apiClient.connectGithubAccount(form.value);
    addToast('GitHub account connected successfully', 'success');
    showAddModal.value = false;
    form.value = { name: '', token: '' };
    await fetchAccounts(true);
  } catch (e: any) {
    error.value = e.message || 'Failed to connect account';
  } finally {
    connecting.value = false;
  }
};

const disconnectAccount = async (account: any) => {
  const isConfirmed = await confirm({
    title: 'Disconnect GitHub Account',
    message: `Are you sure you want to disconnect "${account.name}"? Existing sites using this account will lose their deployment webhook capabilities.`,
    confirmText: 'Disconnect',
    variant: 'danger'
  });

  if (!isConfirmed) return;

  disconnectingId.value = account.id;
  try {
    await apiClient.disconnectGithubAccount(account.id);
    addToast('GitHub account disconnected successfully', 'success');
    await fetchAccounts(true);
  } catch (e: any) {
    addToast(e.message || 'Failed to disconnect account', 'error');
  } finally {
    disconnectingId.value = null;
  }
};

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
};

onMounted(() => {
  fetchAccounts();
});

onActivated(() => {
  fetchAccounts();
});
</script>