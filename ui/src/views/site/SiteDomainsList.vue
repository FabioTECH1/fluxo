<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
    <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Domains</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage your site's domains and aliases.</p>
      </div>
    </div>

    <div class="px-6 pt-4">
      <div class="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg px-4 py-3 text-sm text-blue-800 dark:text-blue-300">
        <p>Point your domain to this server by adding an <strong>A record</strong> at your DNS provider to:</p>
        <div class="flex items-center gap-2 mt-1">
          <code class="bg-blue-100 dark:bg-blue-900/40 px-2 py-0.5 rounded font-mono text-blue-900 dark:text-blue-200">{{ hostAddress }}</code>
          <button @click="copyIp" class="text-blue-500 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-200 transition-colors" title="Copy IP">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
          </button>
        </div>
      </div>
    </div>

    <div class="p-6">
      <h3 class="text-sm font-semibold text-gray-500 uppercase tracking-wider mb-4 dark:text-gray-400">Custom domains</h3>
      <p class="text-sm text-gray-500 mb-4 dark:text-gray-400">Add custom domains and aliases that you own.</p>

      <ul class="divide-y divide-gray-100 border border-gray-200 rounded-lg overflow-hidden dark:divide-gray-800 dark:border-gray-700">
        <li v-for="d in domains" :key="d.id" class="px-4 py-3 flex items-center justify-between hover:bg-gray-50 transition-colors dark:hover:bg-gray-800">
          <div class="flex items-center gap-3">
            <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ d.domain }}</span>
            <span v-if="d.primary" class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">Primary</span>
          </div>
          <div class="flex items-center gap-2">
            <span v-if="d.primary" class="text-xs text-gray-400 mr-2 dark:text-gray-500">Redirect from www.</span>
            <div v-if="!d.primary" class="relative">
              <button @click="toggleMenu(d.id)" class="px-3 py-1.5 text-xs text-gray-600 bg-gray-50 border border-gray-200 rounded-lg hover:bg-gray-100 font-medium transition-colors dark:text-gray-300 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-700">Open menu</button>
              <div v-if="openMenu === d.id" class="absolute right-0 mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10 dark:bg-gray-800 dark:border-gray-700">
                <button @click="deleteDomain(d.id); openMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left dark:text-red-400 dark:hover:bg-red-900/30">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                  Delete
                </button>
              </div>
            </div>
          </div>
        </li>
      </ul>

      <div class="flex gap-3 mt-3">
        <input v-model="newDomain" type="text" class="flex-1 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="your-domain.com" @keyup.enter="addDomain" />
        <button @click="addDomain" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors">Add domain</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, onActivated, onDeactivated, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';

const route = useRoute();
let siteId = route.params.id as string;

const { confirm } = useConfirm();
const { addToast } = useToast();

const domains = ref<any[]>([]);
const newDomain = ref('');
const openMenu = ref<number | null>(null);
const hostAddress = ref('');

const fetchDomains = async () => {
  try {
    domains.value = await apiClient.getSiteDomains(siteId) || [];
  } catch (e) {}
};

const fetchMetrics = async () => {
  try {
    const m = await apiClient.getMetrics();
    hostAddress.value = m?.host_address || '';
  } catch (e) {}
};

const copyIp = async () => {
  try {
    await navigator.clipboard.writeText(hostAddress.value);
    addToast('IP copied to clipboard', 'success');
  } catch {
    addToast('Failed to copy', 'error');
  }
};

const addDomain = async () => {
  const domain = newDomain.value.trim();
  if (!domain) return;
  try {
    await apiClient.addSiteDomain(siteId, { domain });
    addToast('Domain added', 'success');
    newDomain.value = '';
    fetchDomains();
  } catch (e: any) {
    addToast(e.message || 'Failed to add domain', 'error');
  }
};

const deleteDomain = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete Domain',
    message: 'Are you sure you want to delete this domain alias?',
    confirmText: 'Delete',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await apiClient.deleteSiteDomain(siteId, id);
    addToast('Domain deleted', 'success');
    fetchDomains();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete', 'error');
  }
};

const toggleMenu = (id: number) => {
  openMenu.value = openMenu.value === id ? null : id;
};

const handleClickOutside = (e: MouseEvent) => {
  if (!(e.target as HTMLElement).closest('.relative')) openMenu.value = null;
};

let clickListenerActive = false;

const addClickListener = () => {
  if (clickListenerActive) return;
  window.addEventListener('click', handleClickOutside);
  clickListenerActive = true;
};

const removeClickListener = () => {
  if (!clickListenerActive) return;
  window.removeEventListener('click', handleClickOutside);
  clickListenerActive = false;
};

onMounted(() => {
  fetchDomains();
  fetchMetrics();
  addClickListener();
});

onActivated(() => {
  fetchDomains();
  fetchMetrics();
  addClickListener();
});

onDeactivated(() => {
  removeClickListener();
});

onUnmounted(() => {
  removeClickListener();
});

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  fetchDomains();
});
</script>
