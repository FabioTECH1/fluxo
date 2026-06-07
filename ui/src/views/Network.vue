<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <h1 class="text-2xl font-bold mb-6">Network Firewall</h1>

    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Active Firewall Rules</h2>
          <p class="text-sm text-gray-650">Manage open ports and allowed IP addresses on your server.</p>
        </div>
        <button @click="showRuleModal = true" class="bg-blue-600 text-white px-4 py-2 rounded-lg shadow hover:bg-blue-700 font-semibold text-sm transition-colors">
          Add Rule
        </button>
      </div>

      <div class="overflow-x-auto border rounded-lg">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Port</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">From IP</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
              <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-200">
            <tr v-for="rule in rules" :key="rule.id" class="hover:bg-gray-50 transition-colors">
              <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{{ rule.name }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 font-mono">{{ rule.port }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 font-mono">{{ rule.from_ip }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm">
                <span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-green-100 text-green-800">
                  Allow
                </span>
              </td>
              <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                <button @click="deleteRule(rule.id)" class="text-red-600 hover:text-red-900 ml-4 font-semibold">Delete</button>
              </td>
            </tr>
            <tr v-if="rules.length === 0">
              <td colspan="5" class="px-6 py-8 text-center text-gray-500 text-sm">No custom firewall rules configured.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Add Rule Modal -->
    <div v-if="showRuleModal" class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden transform transition-all">
        <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
          <h3 class="text-lg font-bold text-gray-900">Add Firewall Rule</h3>
          <button @click="showRuleModal = false" class="text-gray-400 hover:text-gray-600 transition-colors">
            <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        
        <form @submit.prevent="addRule" class="p-6">
          <div class="mb-5">
            <label class="block text-gray-700 text-sm font-bold mb-2">Name</label>
            <input v-model="newRule.name" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. MySQL">
          </div>
          <div class="mb-5">
            <label class="block text-gray-700 text-sm font-bold mb-2">Port</label>
            <input v-model="newRule.port" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="e.g. 3306">
          </div>
          <div class="mb-6">
            <label class="block text-gray-700 text-sm font-bold mb-2">IP Address</label>
            <input v-model="newRule.from_ip" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="Leave blank for 'Any IP address'">
          </div>
          
          <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100 mt-6">
            <button type="button" @click="showRuleModal = false" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">
              Cancel
            </button>
            <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">
              {{ loading ? 'Adding...' : 'Add Rule' }}
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

const rules = ref<any[]>([]);
const showRuleModal = ref(false);
const newRule = ref({ name: '', port: '', from_ip: '' });
const loading = ref(false);

const fetchRules = async () => {
  try {
    rules.value = await apiClient.getFirewallRules();
  } catch (e) {
    console.error('Failed to load firewall rules:', e);
  }
};

const addRule = async () => {
  loading.value = true;
  try {
    await apiClient.addFirewallRule(newRule.value.name, newRule.value.port, newRule.value.from_ip);
    addToast('Firewall rule added successfully', 'success');
    showRuleModal.value = false;
    newRule.value = { name: '', port: '', from_ip: '' };
    fetchRules();
  } catch (e: any) {
    addToast(e.message || 'Failed to add firewall rule', 'error');
  } finally {
    loading.value = false;
  }
};

const deleteRule = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete Firewall Rule',
    message: 'Are you sure you want to delete this firewall rule? This port will be closed immediately.',
    confirmText: 'Delete Rule',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;

  try {
    await apiClient.deleteFirewallRule(id);
    addToast('Firewall rule deleted successfully', 'success');
    fetchRules();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete firewall rule', 'error');
  }
};

onMounted(() => {
  fetchRules();
});
</script>
