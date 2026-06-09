<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <div class="flex justify-between items-center">
      <PageHeader title="Sites" />
      <AppButton variant="primary" @click="showModal = true">Add Site</AppButton>
    </div>

    <DataTable :columns="columns" :items="sites" empty-text="No sites found.">
      <template #domain="{ item }">
        <span class="font-medium text-gray-900 dark:text-gray-100">{{ item.domain }}</span>
      </template>
      <template #php_version="{ item }">
        <span class="text-gray-500 dark:text-gray-400">{{ item.php_version }}</span>
      </template>
      <template #path="{ item }">
        <span class="text-gray-500 dark:text-gray-400">{{ item.path }}</span>
      </template>
      <template #actions="{ item }">
        <router-link :to="`/sites/${item.id}`" class="text-blue-600 dark:text-blue-400 hover:text-blue-900 mr-4 font-semibold">Manage</router-link>
        <button @click="deleteSite(item.id)" class="text-red-600 dark:text-red-400 hover:text-red-900 font-semibold">Delete</button>
      </template>
    </DataTable>

    <CreateSiteModal v-model="showModal" @created="fetchSites" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';
import CreateSiteModal from '../components/CreateSiteModal.vue';
import PageHeader from '../components/PageHeader.vue';
import AppButton from '../components/AppButton.vue';
import DataTable from '../components/DataTable.vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';

const columns = [
  { key: 'domain', label: 'Domain' },
  { key: 'php_version', label: 'PHP Version' },
  { key: 'path', label: 'Path' },
];

const { confirm } = useConfirm();
const { addToast } = useToast();

const sites = ref<any[]>([]);
const showModal = ref(false);

const fetchSites = async () => {
  try {
    sites.value = await apiClient.getSites();
    showModal.value = false;
  } catch (e) {
    console.error(e);
  }
};

const deleteSite = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete Site',
    message: 'Are you sure you want to delete this site? This will remove all associated configurations and files.',
    confirmText: 'Delete Site',
    cancelText: 'Cancel',
    variant: 'danger'
  });

  if (confirmed) {
    try {
      await apiClient.deleteSite(id);
      addToast('Site deleted successfully', 'success');
      await fetchSites();
    } catch (e: any) {
      addToast(e.message || 'Failed to delete site', 'error');
    }
  }
};

onMounted(fetchSites);
</script>