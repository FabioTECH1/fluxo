<template>
  <div class="space-y-6">
    <div class="rounded-lg border border-gray-100 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">SSH access security</h2>
            <StatusBadge
              v-if="sshSecurity.available"
              :label="sshSecurity.password_login_enabled ? 'Password login enabled' : sshSecurity.hardened && sshSecurity.authorized_key_count > 0 ? 'Key-only login' : 'SSH access needs attention'"
              :variant="sshSecurity.password_login_enabled ? 'yellow' : sshSecurity.hardened && sshSecurity.authorized_key_count > 0 ? 'green' : 'red'"
            />
          </div>
          <p class="mt-1 max-w-3xl text-sm text-gray-600 dark:text-gray-400">
            Fluxo reads the effective OpenSSH policy for the <code class="font-mono text-xs">fluxo</code> system user. Adding a key does not silently change server authentication.
          </p>
        </div>
        <SkeletonLoader v-if="loadingSecurity" type="line" width="w-36" />
        <div v-else class="flex flex-wrap gap-2">
          <AppButton
            v-if="sshSecurity.password_login_enabled"
            :disabled="!sshSecurity.can_harden"
            @click="openHardeningModal"
          >
            Disable password login
          </AppButton>
          <AppButton
            v-else-if="sshSecurity.managed"
            variant="secondary"
            :loading="policyLoading"
            @click="restoreServerPolicy"
          >
            Restore server SSH policy
          </AppButton>
        </div>
      </div>

      <div v-if="!loadingSecurity" class="mt-5 grid gap-3 sm:grid-cols-3">
        <div class="rounded-lg bg-gray-50 p-3 dark:bg-gray-800/70">
          <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Public keys</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ sshSecurity.authorized_key_count }} installed for fluxo
          </p>
        </div>
        <div class="rounded-lg bg-gray-50 p-3 dark:bg-gray-800/70">
          <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Public-key authentication</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ settingLabel(sshSecurity.public_key_authentication) }}
          </p>
        </div>
        <div class="rounded-lg bg-gray-50 p-3 dark:bg-gray-800/70">
          <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Policy owner</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ sshSecurity.managed ? 'Managed by Fluxo' : 'Server or VPS provider' }}
          </p>
        </div>
      </div>

      <ErrorAlert v-if="!loadingSecurity && sshSecurity.error" class="mt-4" :message="sshSecurity.error" />
      <p
        v-else-if="!loadingSecurity && !sshSecurity.password_login_enabled && sshSecurity.authorized_key_count < 1"
        class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-900/20 dark:text-red-300"
      >
        Password login is disabled but no valid key is installed for the <code class="font-mono text-xs">fluxo</code> user. Use your existing root key or provider console, then add and test a Fluxo key before relying on this account.
      </p>
      <p
        v-else-if="!loadingSecurity && sshSecurity.password_login_enabled && !sshSecurity.can_harden"
        class="mt-4 rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
      >
        Add at least one valid key for the <code class="font-mono text-xs">fluxo</code> user, then test it from a separate terminal before disabling password login.
      </p>
      <p
        v-else-if="!loadingSecurity && !sshSecurity.password_login_enabled && !sshSecurity.managed"
        class="mt-4 rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:bg-blue-900/20 dark:text-blue-300"
      >
        Key-only access is enforced outside Fluxo. Change it through your VPS provider or the server's OpenSSH configuration.
      </p>
    </div>

    <SkeletonLoader v-if="loadingKeys" type="card" />
    <div v-else class="rounded-lg border border-gray-100 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div class="mb-4 flex items-center justify-between gap-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">SSH keys</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400">Keys listed here authorize terminal access as the <code class="font-mono text-xs">fluxo</code> user.</p>
        </div>
        <AppButton @click="showSSHModal = true">Add key</AppButton>
      </div>

      <DataTable :columns="columns" :items="sshKeys" empty-text="No SSH keys found. Add one to access your server securely.">
        <template #name="{ item }">
          <span class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</span>
        </template>
        <template #created_at="{ item }">
          <span class="text-gray-500 dark:text-gray-400">{{ new Date(item.created_at).toLocaleDateString() }}</span>
        </template>
        <template #actions="{ item }">
          <button class="font-semibold text-red-600 hover:text-red-900 dark:text-red-400 dark:hover:text-red-300" @click="deleteSSHKey(item.id)">Delete</button>
        </template>
      </DataTable>
    </div>

    <BaseModal v-model="showSSHModal" title="Add SSH Key" :loading="sshLoading" confirm-text="Add Key" max-width="max-w-2xl" @submit="formRef?.requestSubmit()">
      <form ref="formRef" class="space-y-4" @submit.prevent="addSSHKey">
        <FormGroup label="Name">
          <input v-model="newSSHKey.name" type="text" required class="w-full rounded-lg border border-gray-200 px-3 py-2 transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" placeholder="e.g. MacBook Pro">
        </FormGroup>
        <FormGroup label="Public Key" hint="Paste one public key line. Generate an Ed25519 key with: ssh-keygen -t ed25519 -C &quot;your@email.com&quot; — then copy ~/.ssh/id_ed25519.pub">
          <textarea v-model="newSSHKey.public_key" required class="h-32 w-full rounded-lg border border-gray-200 px-3 py-2 font-mono text-sm transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... user@machine"></textarea>
        </FormGroup>
        <p class="rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:bg-blue-900/20 dark:text-blue-300">
          The key will be installed for <code class="font-mono text-xs">fluxo</code>. Password login remains unchanged until you complete the separate hardening step.
        </p>
      </form>
    </BaseModal>

    <BaseModal
      v-model="showHardeningModal"
      title="Disable SSH password login"
      confirm-text="Disable password login"
      :loading="policyLoading"
      :confirm-disabled="!keyAccessConfirmed || !recoveryAccessConfirmed"
      max-width="max-w-2xl"
      @submit="enableHardening"
    >
      <div class="space-y-5">
        <div class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          <p class="font-semibold">Prevent an SSH lockout</p>
          <p class="mt-1">Keep your current session open. In a separate terminal, successfully connect with:</p>
          <code class="mt-3 block rounded bg-white/70 px-3 py-2 font-mono text-xs dark:bg-gray-950/50">ssh fluxo@YOUR_SERVER_IP</code>
        </div>

        <p class="text-sm text-gray-600 dark:text-gray-400">
          Fluxo will stage a global OpenSSH drop-in, verify its effective policy, validate the complete SSH configuration, reload OpenSSH, and restore the previous policy automatically if validation or reload fails.
        </p>

        <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
          <input v-model="keyAccessConfirmed" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500">
          <span class="text-sm text-gray-700 dark:text-gray-300">I successfully connected as <code class="font-mono text-xs">fluxo</code> using one of these keys in a separate terminal.</span>
        </label>
        <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
          <input v-model="recoveryAccessConfirmed" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500">
          <span class="text-sm text-gray-700 dark:text-gray-300">I have VPS provider-console or equivalent recovery access if SSH becomes unavailable.</span>
        </label>
      </div>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { onActivated, onMounted, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import AppButton from '../components/AppButton.vue';
import BaseModal from '../components/BaseModal.vue';
import DataTable from '../components/DataTable.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';

interface SSHSecurityStatus {
  available: boolean;
  password_authentication: string;
  keyboard_interactive_authentication: string;
  public_key_authentication: string;
  permit_root_login: string;
  password_login_enabled: boolean;
  hardened: boolean;
  managed: boolean;
  authorized_key_count: number;
  authorized_keys_valid: boolean;
  can_harden: boolean;
  error?: string;
}

const emptySecurityStatus = (): SSHSecurityStatus => ({
  available: false,
  password_authentication: '',
  keyboard_interactive_authentication: '',
  public_key_authentication: '',
  permit_root_login: '',
  password_login_enabled: false,
  hardened: false,
  managed: false,
  authorized_key_count: 0,
  authorized_keys_valid: false,
  can_harden: false,
});

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'created_at', label: 'Date Added' },
];

const { addToast } = useToast();
const { confirm } = useConfirm();
const sshKeys = ref<any[]>([]);
const sshSecurity = ref<SSHSecurityStatus>(emptySecurityStatus());
const showSSHModal = ref(false);
const showHardeningModal = ref(false);
const newSSHKey = ref({ name: '', public_key: '' });
const keyAccessConfirmed = ref(false);
const recoveryAccessConfirmed = ref(false);
const sshLoading = ref(false);
const policyLoading = ref(false);
const loadingKeys = ref(true);
const loadingSecurity = ref(true);
const formRef = ref<HTMLFormElement | null>(null);

const settingLabel = (value: string) => value === 'yes' ? 'Enabled' : value === 'no' ? 'Disabled' : 'Unknown';

const fetchSSHKeys = async () => {
  try {
    sshKeys.value = await apiClient.getSSHKeys();
  } catch (error) {
    console.error('Failed to load SSH keys:', error);
  } finally {
    loadingKeys.value = false;
  }
};

const fetchSSHSecurity = async () => {
  try {
    sshSecurity.value = await apiClient.getSSHSecurity();
  } catch (error: any) {
    sshSecurity.value = { ...emptySecurityStatus(), error: error.message || 'Failed to inspect SSH security' };
  } finally {
    loadingSecurity.value = false;
  }
};

const refresh = async () => {
  await Promise.all([fetchSSHKeys(), fetchSSHSecurity()]);
};

const addSSHKey = async () => {
  sshLoading.value = true;
  try {
    await apiClient.addSSHKey(newSSHKey.value.name, newSSHKey.value.public_key);
    addToast('SSH key added. Test ssh fluxo@YOUR_SERVER_IP from a separate terminal before disabling password login.', 'success');
    showSSHModal.value = false;
    await refresh();
  } catch (error: any) {
    addToast(error.message || 'Failed to add SSH key', 'error');
  } finally {
    sshLoading.value = false;
  }
};

const deleteSSHKey = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete SSH Key',
    message: 'Remove this key from the fluxo user? Fluxo will block the operation if it would remove the final key while password login is disabled.',
    confirmText: 'Delete Key',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!confirmed) return;

  try {
    await apiClient.deleteSSHKey(id);
    addToast('SSH key deleted successfully', 'success');
    await refresh();
  } catch (error: any) {
    addToast(error.message || 'Failed to delete SSH key', 'error');
  }
};

const openHardeningModal = () => {
  keyAccessConfirmed.value = false;
  recoveryAccessConfirmed.value = false;
  showHardeningModal.value = true;
};

const enableHardening = async () => {
  if (!keyAccessConfirmed.value || !recoveryAccessConfirmed.value) return;
  policyLoading.value = true;
  try {
    sshSecurity.value = await apiClient.enableSSHHardening();
    showHardeningModal.value = false;
    addToast('SSH password login disabled after successful validation', 'success');
    await refresh();
  } catch (error: any) {
    addToast(error.message || 'Failed to disable SSH password login', 'error');
  } finally {
    policyLoading.value = false;
  }
};

const restoreServerPolicy = async () => {
  const confirmed = await confirm({
    title: 'Restore Server SSH Policy',
    message: 'Remove Fluxo\'s managed hardening policy? The underlying VPS or OpenSSH policy will take effect and may allow password login again.',
    confirmText: 'Restore Policy',
    cancelText: 'Keep Key-only Login',
    variant: 'danger',
  });
  if (!confirmed) return;

  policyLoading.value = true;
  try {
    sshSecurity.value = await apiClient.disableSSHHardening();
    addToast('Fluxo hardening removed; the underlying server SSH policy is now active', 'success');
    await refresh();
  } catch (error: any) {
    addToast(error.message || 'Failed to restore the server SSH policy', 'error');
  } finally {
    policyLoading.value = false;
  }
};

watch(showSSHModal, isOpen => {
  if (!isOpen) newSSHKey.value = { name: '', public_key: '' };
});
watch(showHardeningModal, isOpen => {
  if (!isOpen) {
    keyAccessConfirmed.value = false;
    recoveryAccessConfirmed.value = false;
  }
});

onMounted(refresh);
onActivated(refresh);
</script>
