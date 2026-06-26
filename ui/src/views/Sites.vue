<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <div class="flex justify-between items-center">
      <PageHeader title="Sites" />
      <AppButton variant="primary" @click="showCreateModal = true">Add Site</AppButton>
    </div>

    <SkeletonLoader v-if="loading" type="table" />
    <DataTable v-else :columns="columns" :items="sites" empty-text="No sites found.">
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

    <CreateSiteModal v-model="showCreateModal" @submit-create="onCreateSubmit" />

    <SiteCreationProgress v-if="creatingPayload" :payload="creatingPayload" @created="onSiteCreated" @error="onSiteError" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated } from 'vue';
import { useRouter } from 'vue-router';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import CreateSiteModal from '../components/CreateSiteModal.vue';
import SiteCreationProgress from '../components/SiteCreationProgress.vue';
import PageHeader from '../components/PageHeader.vue';
import AppButton from '../components/AppButton.vue';
import DataTable from '../components/DataTable.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';

const router = useRouter();
const { addToast } = useToast();

const columns = [
  { key: 'domain', label: 'Domain' },
  { key: 'php_version', label: 'PHP Version' },
  { key: 'path', label: 'Path' },
];

const sites = ref<any[]>([]);
const showCreateModal = ref(false);
const loading = ref(true);
const creatingPayload = ref<any>(null);

const fetchSites = async () => {
  try {
    loading.value = true;
    sites.value = await apiClient.getSites();
    showCreateModal.value = false;
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const onCreateSubmit = (payload: any) => {
  creatingPayload.value = payload;
};

const onSiteCreated = (site: any) => {
  creatingPayload.value = null;
  router.push(`/sites/${site.id}/overview`);
};

const onSiteError = (message: string) => {
  creatingPayload.value = null;
  addToast(message, 'error');
  showCreateModal.value = true;
};

onMounted(fetchSites);
onActivated(fetchSites);
</script>
