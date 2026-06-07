<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-2xl font-bold">Sites</h1>
      <button @click="showModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">Add Site</button>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Domain</th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">PHP Version</th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Path</th>
            <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="site in sites" :key="site.id" class="hover:bg-gray-50 transition-colors">
            <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{{ site.domain }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ site.php_version }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ site.path }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
              <router-link :to="`/sites/${site.id}`" class="text-blue-600 hover:text-blue-900 mr-4 font-semibold">Manage</router-link>
              <button @click="deleteSite(site.id)" class="text-red-600 hover:text-red-900 font-semibold">Delete</button>
            </td>
          </tr>
          <tr v-if="sites.length === 0">
            <td colspan="4" class="px-6 py-8 text-center text-gray-500 text-sm">No sites found.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <CreateSiteModal v-if="showModal" @close="showModal = false" @created="fetchSites" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';
import CreateSiteModal from '../components/CreateSiteModal.vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';

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