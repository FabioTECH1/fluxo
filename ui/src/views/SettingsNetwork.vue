<template>
  <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
    <div class="flex justify-between items-center mb-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Active Firewall Rules</h2>
        <p class="text-sm text-gray-600 dark:text-gray-400">Manage open ports and allowed IP addresses on your server.</p>
      </div>
      <button @click="showRuleModal = true" class="bg-blue-600 text-white px-4 py-2 rounded-lg shadow hover:bg-blue-700 font-semibold text-sm transition-colors">
        Add Rule
      </button>
    </div>

    <DataTable :columns="columns" :items="rules" empty-text="No custom firewall rules configured.">
      <template #name="{ item }">
        <span class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</span>
      </template>
      <template #type="{ item }">
        <StatusBadge :label="item.type === 'deny' ? 'Deny' : 'Allow'" :variant="item.type === 'deny' ? 'red' : 'green'" />
      </template>
      <template #port="{ item }">
        <span class="text-gray-500 dark:text-gray-400 font-mono">{{ item.port }}</span>
      </template>
      <template #from_ip="{ item }">
        <span class="text-gray-500 dark:text-gray-400 font-mono">{{ item.from_ip }}</span>
      </template>
      <template #actions="{ item }">
        <button @click="deleteRule(item.id)" class="text-red-600 dark:text-red-400 hover:text-red-900 dark:hover:text-red-300 font-semibold">Delete</button>
      </template>
    </DataTable>

    <!-- Add Rule Modal -->
    <BaseModal v-model="showRuleModal" title="Add Firewall Rule" :loading="loading" confirm-text="Add Rule" @submit="formRef?.requestSubmit()">
      <form ref="formRef" @submit.prevent="addRule">
        <FormGroup label="Name">
          <input v-model="newRule.name" type="text" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. MySQL">
        </FormGroup>
        <div class="mb-5">
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Type</label>
          <div class="flex gap-4">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="newRule.type" value="allow" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Allow</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">Allow requests to this port.</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="newRule.type" value="deny" class="text-red-600 dark:text-red-400 focus:ring-red-500">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Deny</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">Deny requests to this port.</span>
            </label>
          </div>
        </div>
        <FormGroup label="Port">
          <input v-model="newRule.port" type="text" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="e.g. 3306">
        </FormGroup>
        <div class="mb-6">
          <FormGroup label="IP Address">
            <input v-model="newRule.from_ip" type="text" class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="Leave blank for 'Any IP address'">
          </FormGroup>
        </div>
      </form>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import DataTable from '../components/DataTable.vue';
import StatusBadge from '../components/StatusBadge.vue';
import BaseModal from '../components/BaseModal.vue';
import FormGroup from '../components/FormGroup.vue';

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'type', label: 'Type' },
  { key: 'port', label: 'Port' },
  { key: 'from_ip', label: 'From IP' },
];

const { addToast } = useToast();
const { confirm } = useConfirm();

const rules = ref<any[]>([]);
const showRuleModal = ref(false);
const newRule = ref({ name: '', type: 'allow', port: '', from_ip: '' });
const loading = ref(false);
const formRef = ref<HTMLFormElement | null>(null);

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
    await apiClient.addFirewallRule(newRule.value.name, newRule.value.port, newRule.value.from_ip, newRule.value.type);
    addToast('Firewall rule added successfully', 'success');
    showRuleModal.value = false;
    newRule.value = { name: '', type: 'allow', port: '', from_ip: '' };
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