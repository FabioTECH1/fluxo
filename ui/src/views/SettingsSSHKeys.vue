<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
    <div class="flex justify-between items-center mb-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900">SSH Keys</h2>
        <p class="text-sm text-gray-600">Manage SSH keys to access this server securely via terminal.</p>
      </div>
      <button @click="showSSHModal = true" class="bg-blue-600 text-white px-4 py-2 rounded-lg shadow hover:bg-blue-700 font-semibold text-sm transition-colors">
        Add Key
      </button>
    </div>

    <div class="overflow-x-auto border rounded-lg">
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Date Added</th>
            <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="key in sshKeys" :key="key.id" class="hover:bg-gray-50 transition-colors">
            <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{{ key.name }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ new Date(key.created_at).toLocaleDateString() }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
              <button @click="deleteSSHKey(key.id)" class="text-red-600 hover:text-red-900 ml-4 font-semibold">Delete</button>
            </td>
          </tr>
          <tr v-if="sshKeys.length === 0">
            <td colspan="3" class="px-6 py-8 text-center text-gray-500 text-sm">No SSH keys found. Add one to access your server securely.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- SSH Key Modal -->
    <div v-if="showSSHModal" class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-xl shadow-2xl w-full max-w-2xl overflow-hidden transform transition-all">
        <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
          <h3 class="text-lg font-bold text-gray-900">Add SSH Key</h3>
          <button @click="showSSHModal = false" class="text-gray-400 hover:text-gray-600 transition-colors">
            <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <form @submit.prevent="addSSHKey" class="p-6">
          <div class="mb-5">
            <label class="block text-gray-700 text-sm font-bold mb-2">Name</label>
            <input v-model="newSSHKey.name" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. MacBook Pro">
          </div>
          <div class="mb-6">
            <label class="block text-gray-700 text-sm font-bold mb-2">Public Key</label>
            <textarea v-model="newSSHKey.public_key" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm h-32" placeholder="ssh-rsa AAAAB3NzaC1... user@machine"></textarea>
            <p class="text-xs text-gray-500 mt-2">Starts with ssh-rsa, ssh-ed25519, ecdsa-sha2-nistp256, etc.</p>
          </div>

          <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100 mt-6">
            <button type="button" @click="showSSHModal = false" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">
              Cancel
            </button>
            <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="sshLoading">
              {{ sshLoading ? 'Adding...' : 'Add Key' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';

const { addToast } = useToast();
const { confirm } = useConfirm();

const sshKeys = ref<any[]>([]);
const showSSHModal = ref(false);
const newSSHKey = ref({ name: '', public_key: '' });
const sshLoading = ref(false);

const fetchSSHKeys = async () => {
  try {
    sshKeys.value = await apiClient.getSSHKeys();
  } catch (e) {
    console.error('Failed to load SSH keys:', e);
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
</script>