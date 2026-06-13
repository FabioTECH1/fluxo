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
        <router-link :to="`/sites/${item.id}`" class="text-blue-600 dark:text-blue-400 hover:text-blue-900 font-semibold">Manage</router-link>
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

const columns = [
  { key: 'domain', label: 'Domain' },
  { key: 'php_version', label: 'PHP Version' },
  { key: 'path', label: 'Path' },
];

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

onMounted(fetchSites);
</script>