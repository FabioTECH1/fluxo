<template>
  <SkeletonLoader v-if="loadingKeys" type="card" />
  <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
    <div class="flex justify-between items-center mb-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">SSH Keys</h2>
        <p class="text-sm text-gray-600 dark:text-gray-400">Manage SSH keys to access this server securely via terminal.</p>
      </div>
      <button @click="showSSHModal = true" class="bg-blue-600 text-white px-4 py-2 rounded-lg shadow hover:bg-blue-700 font-semibold text-sm transition-colors">
        Add Key
      </button>
    </div>

    <DataTable :columns="columns" :items="sshKeys" empty-text="No SSH keys found. Add one to access your server securely.">
      <template #name="{ item }">
        <span class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</span>
      </template>
      <template #created_at="{ item }">
        <span class="text-gray-500 dark:text-gray-400">{{ new Date(item.created_at).toLocaleDateString() }}</span>
      </template>
      <template #actions="{ item }">
        <button @click="deleteSSHKey(item.id)" class="text-red-600 dark:text-red-400 hover:text-red-900 dark:hover:text-red-300 font-semibold">Delete</button>
      </template>
    </DataTable>

    <!-- SSH Key Modal -->
    <BaseModal v-model="showSSHModal" title="Add SSH Key" :loading="sshLoading" confirm-text="Add Key" max-width="max-w-2xl" @submit="formRef?.requestSubmit()">
      <form ref="formRef" @submit.prevent="addSSHKey" class="space-y-4">
        <FormGroup label="Name">
          <input v-model="newSSHKey.name" type="text" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. MacBook Pro">
        </FormGroup>
        <FormGroup label="Public Key" hint="Paste the contents of your public key file (e.g. ~/.ssh/id_ed25519.pub). Generate one with: ssh-keygen -t ed25519 -C &quot;your@email.com&quot; — then run: cat ~/.ssh/id_ed25519.pub">
          <textarea v-model="newSSHKey.public_key" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm h-32" placeholder="ssh-rsa AAAAB3NzaC1... user@machine"></textarea>
        </FormGroup>
      </form>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import DataTable from '../components/DataTable.vue';
import BaseModal from '../components/BaseModal.vue';
import FormGroup from '../components/FormGroup.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'created_at', label: 'Date Added' },
];

const { addToast } = useToast();
const { confirm } = useConfirm();

const sshKeys = ref<any[]>([]);
const showSSHModal = ref(false);
const newSSHKey = ref({ name: '', public_key: '' });
const sshLoading = ref(false);
const loadingKeys = ref(true);
const formRef = ref<HTMLFormElement | null>(null);

const fetchSSHKeys = async () => {
  try {
    loadingKeys.value = true;
    sshKeys.value = await apiClient.getSSHKeys();
  } catch (e) {
    console.error('Failed to load SSH keys:', e);
  } finally {
    loadingKeys.value = false;
  }
};

const addSSHKey = async () => {
  sshLoading.value = true;
  try {
    await apiClient.addSSHKey(newSSHKey.value.name, newSSHKey.value.public_key);
    addToast('SSH key added successfully', 'success');
    showSSHModal.value = false;
    newSSHKey.value = { name: '', public_key: '' };
    fetchSSHKeys();
  } catch (e: any) {
    addToast(e.message || 'Failed to add SSH key', 'error');
  } finally {
    sshLoading.value = false;
  }
};

const deleteSSHKey = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete SSH Key',
    message: 'Are you sure you want to delete this SSH key? You will lose access from associated devices.',
    confirmText: 'Delete Key',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;

  try {
    await apiClient.deleteSSHKey(id);
    addToast('SSH key deleted successfully', 'success');
    fetchSSHKeys();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete SSH key', 'error');
  }
};

onMounted(() => {
  fetchSSHKeys();
});

onActivated(fetchSSHKeys);
</script>