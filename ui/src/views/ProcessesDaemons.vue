<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" />
    <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Background Processes</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Background processes are managed via systemd, which monitors and restarts them automatically.
            <a href="https://systemd.io" class="text-blue-600 dark:text-blue-400 hover:underline">Learn more</a>
          </p>
        </div>
        <div class="flex gap-3">
          <button @click="showAddModal = true" class="w-8 h-8 flex items-center justify-center text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-bold shadow-sm transition-colors text-lg leading-none" title="Add process">+</button>
          <button @click="() => fetchDaemons()" class="p-2 text-gray-600 dark:text-gray-400 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors" title="Refresh">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          </button>
        </div>
      </div>

      <div v-if="daemons.length === 0" class="text-center text-gray-500 dark:text-gray-400 text-sm py-12">
        No background processes configured.
      </div>

      <div class="space-y-2">
        <div v-for="d in daemons" :key="d.id" class="flex items-center gap-3 px-4 py-3 border border-gray-200 dark:border-gray-700 rounded-lg hover:border-gray-300 dark:hover:bg-gray-800/20 dark:hover:border-gray-600 transition-colors">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ d.name || d.command.split(' ').slice(0, 2).join(' ') }}</span>
              <span v-if="d.site_domain" class="px-1.5 py-0.5 text-[10px] font-semibold rounded bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-100 dark:border-blue-900/50 uppercase tracking-wider">{{ d.site_domain }}</span>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-1 truncate">
              {{ d.command }} &middot; {{ d.directory }}
            </div>
          </div>
          <div class="flex items-center gap-4 shrink-0">
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ d.instances || 1 }} {{ (d.instances || 1) > 1 ? 'Processes' : 'Process' }}</span>
            <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border"
                  :class="d.status === 'active' || d.status === 'running'
                    ? 'bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40'
                    : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-700'">
              <span class="h-1.5 w-1.5 rounded-full" :class="d.status === 'active' || d.status === 'running' ? 'bg-green-500' : 'bg-gray-400'"></span>
              {{ d.status === 'active' || d.status === 'running' ? 'Running' : 'Stopped' }}
            </span>
          </div>
          <div class="relative shrink-0">
            <button @click="toggleMenu(d.id)" class="px-2.5 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 font-medium transition-colors">···</button>
            <div v-if="openMenu === d.id" class="absolute right-0 mt-1 w-36 bg-white dark:bg-gray-900 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 py-1 z-10">
              <button v-if="d.status !== 'active'" @click="startDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/30">Start</button>
              <button v-if="d.status === 'active'" @click="stopDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-yellow-600 dark:text-yellow-400 hover:bg-yellow-50 dark:hover:bg-yellow-900/30">Stop</button>
              <button v-if="d.status === 'active'" @click="restartDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800">Restart</button>
              <button @click="viewLogs(d); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800">Logs</button>
              <button @click="deleteDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30">Delete</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <AddDaemonModal v-model="showAddModal" @created="onCreated" />

    <BaseModal v-model="showLogs" :title="'Logs: ' + logTitle" max-width="max-w-3xl">
      <template #footer>
        <AppButton variant="secondary" @click="showLogs = false">Close</AppButton>
      </template>
      <div v-if="logLines.length === 0" class="text-sm text-gray-400 dark:text-gray-500 italic py-8 text-center">No log entries.</div>
      <pre v-else class="bg-gray-900 rounded-lg p-4 text-xs text-green-400 font-mono max-h-96 overflow-y-auto whitespace-pre-wrap">{{ logLines.join('\n') }}</pre>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated, onDeactivated, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import { apiClient } from '../api/client';
import AddDaemonModal from './AddDaemonModal.vue';
import BaseModal from '../components/BaseModal.vue';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';

const { confirm } = useConfirm();
const { addToast } = useToast();

const daemons = ref<any[]>([]);
const openMenu = ref<number | null>(null);
const showAddModal = ref(false);
const showLogs = ref(false);
const logTitle = ref('');
const logLines = ref<string[]>([]);
const loading = ref(true);

const fetchDaemons = async (silent = false) => {
  try {
    if (!silent) loading.value = true;
    daemons.value = await apiClient.get('/api/v1/daemons');
    if (!silent) addToast('Daemons refreshed', 'success');
  } catch (e: any) { 
    if (!silent) addToast(e.message || 'Failed', 'error'); 
  } finally {
    if (!silent) loading.value = false;
  }
};

const startDaemon = async (id: number) => {
  try {
    await apiClient.post(`/api/v1/daemons/${id}/start`);
    apiClient.invalidate('/api/v1/daemons');
    addToast('Started', 'success'); setTimeout(fetchDaemons, 1000);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const stopDaemon = async (id: number) => {
  try {
    await apiClient.post(`/api/v1/daemons/${id}/stop`);
    apiClient.invalidate('/api/v1/daemons');
    addToast('Stopped', 'success'); setTimeout(fetchDaemons, 1000);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const restartDaemon = async (id: number) => {
  try {
    await apiClient.post(`/api/v1/daemons/${id}/restart`);
    apiClient.invalidate('/api/v1/daemons');
    addToast('Restarted', 'success'); setTimeout(fetchDaemons, 1000);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const viewLogs = async (d: any) => {
  logTitle.value = d.name || d.command.split(' ').slice(0, 2).join(' ');
  showLogs.value = true;
  try {
    const data = await apiClient.get(`/api/v1/daemons/${d.id}/logs`);
    logLines.value = data.lines || [];
  } catch (e) { logLines.value = []; }
};

const deleteDaemon = async (id: number) => {
  const confirmed = await confirm({ title: 'Delete Daemon', message: 'Stop and remove this daemon?', confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!confirmed) return;
  try {
    await apiClient.delete(`/api/v1/daemons/${id}`);
    apiClient.invalidate('/api/v1/daemons');
    addToast('Deleted', 'success'); fetchDaemons();
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const onCreated = () => { showAddModal.value = false; fetchDaemons(); };
const toggleMenu = (id: number) => { openMenu.value = openMenu.value === id ? null : id; };
const handleClickOutside = (e: MouseEvent) => { if (!(e.target as HTMLElement).closest('.relative')) openMenu.value = null; };

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

onMounted(async () => { 
  loading.value = true;
  await fetchDaemons(true); 
  loading.value = false;
  addClickListener();
});
onActivated(() => { fetchDaemons(true); addClickListener(); });
onDeactivated(removeClickListener);
onUnmounted(removeClickListener);
</script>
