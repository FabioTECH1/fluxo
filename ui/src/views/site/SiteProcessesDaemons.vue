<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6 dark:bg-gray-900 dark:border-gray-800">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Background Processes</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">
            Background processes are managed via systemd, which monitors and restarts them automatically.
          </p>
        </div>
        <div class="flex gap-3">
          <button @click="showAddModal = true" class="w-8 h-8 flex items-center justify-center text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-bold shadow-sm transition-colors text-lg leading-none" title="Add process">+</button>
          <button @click="() => fetchDaemons(false, true)" class="p-2 text-gray-600 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors dark:text-gray-400 dark:bg-gray-800 dark:border-gray-600 dark:hover:bg-gray-800" title="Refresh">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          </button>
        </div>
      </div>

      <div v-if="daemons.length === 0" class="text-center text-gray-500 text-sm py-12 dark:text-gray-400">
        No background processes configured.
      </div>

      <div class="space-y-2">
        <div v-for="d in daemons" :key="d.id" class="flex items-center gap-3 px-4 py-3 border border-gray-200 dark:border-gray-700 rounded-lg hover:border-gray-300 dark:hover:bg-gray-800/20 dark:hover:border-gray-600 transition-colors">
          <div class="flex-1 min-w-0">
            <span class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ d.name || d.command.split(' ').slice(0, 2).join(' ') }}</span>
            <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-1 truncate">
              {{ d.command }} &middot; {{ d.directory }}
            </div>
          </div>
          <div class="flex items-center gap-4 shrink-0">
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ d.instances || 1 }} {{ (d.instances || 1) > 1 ? 'Processes' : 'Process' }}</span>
            <ToggleSwitch v-if="supportsDeployRestart(d)" :model-value="!!d.restart_on_deploy" label="Deploy restart"
              :disabled="updatingPolicy === d.id" @update:model-value="updateDeploymentPolicy(d, $event)" />
            <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border"
                  :class="isDaemonRunning(d)
                    ? 'bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40'
                    : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-700'">
              <span class="h-1.5 w-1.5 rounded-full" :class="isDaemonRunning(d) ? 'bg-green-500' : 'bg-gray-400'"></span>
              {{ isDaemonRunning(d) ? 'Running' : 'Stopped' }}
            </span>
          </div>
          <div class="relative shrink-0">
            <button @click="toggleMenu(d.id)" class="px-2.5 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 font-medium transition-colors">···</button>
            <div v-if="openMenu === d.id" class="absolute right-0 mt-1 w-36 bg-white dark:bg-gray-900 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 py-1 z-10">
              <button v-if="!isDaemonRunning(d)" @click="startDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/30">Start</button>
              <button v-if="isDaemonRunning(d)" @click="stopDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-yellow-600 dark:text-yellow-400 hover:bg-yellow-50 dark:hover:bg-yellow-900/30">Stop</button>
              <button v-if="isDaemonRunning(d)" @click="restartDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800">Restart</button>
              <button @click="viewLogs(d); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800">Logs</button>
              <button @click="deleteDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30">Delete</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <AddDaemonModal v-model="showAddModal" :site-id="siteId" @created="onCreated" />

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
import { ref, onMounted, onActivated, onDeactivated, onUnmounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import AddDaemonModal from '../AddDaemonModal.vue';
import BaseModal from '../../components/BaseModal.vue';
import AppButton from '../../components/AppButton.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';

const route = useRoute();
let siteId = route.params.id as string;

const { confirm } = useConfirm();
const { addToast } = useToast();

const daemons = ref<any[]>([]);
const openMenu = ref<number | null>(null);
const showAddModal = ref(false);
const showLogs = ref(false);
const logTitle = ref('');
const logLines = ref<string[]>([]);
const updatingPolicy = ref<number | null>(null);

const isDaemonRunning = (daemon: any) => daemon.status === 'active' || daemon.status === 'running';

const supportsDeployRestart = (daemon: any) => {
  const name = daemon.name || '';
  const command = daemon.command || '';
  return !['Node.js', 'Laravel Horizon', 'Laravel Octane', 'Nightwatch'].includes(name) &&
    !command.includes('artisan horizon') && !command.includes('artisan octane:start') && !command.includes('nightwatch:agent');
};

const fetchDaemons = async (silent = false, bypassCache = false) => {
  try {
    daemons.value = await apiClient.getSiteDaemons(siteId, bypassCache) || [];
    if (!silent) addToast('Daemons refreshed', 'success');
  } catch (e: any) { if (!silent) addToast(e.message || 'Failed', 'error'); }
};

const startDaemon = async (id: number) => {
  try {
    await apiClient.startSiteDaemon(siteId, id);
    addToast('Started', 'success'); setTimeout(fetchDaemons, 1000);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const stopDaemon = async (id: number) => {
  try {
    await apiClient.stopSiteDaemon(siteId, id);
    addToast('Stopped', 'success'); setTimeout(fetchDaemons, 1000);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const restartDaemon = async (id: number) => {
  try {
    await apiClient.restartSiteDaemon(siteId, id);
    addToast('Restarted', 'success'); setTimeout(fetchDaemons, 1000);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const updateDeploymentPolicy = async (daemon: any, enabled: boolean) => {
  updatingPolicy.value = daemon.id;
  try {
    await apiClient.updateSiteDaemonDeploymentPolicy(siteId, daemon.id, enabled);
    daemon.restart_on_deploy = enabled;
    addToast(enabled ? 'Process will restart after deployments' : 'Automatic deployment restart disabled', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to update deployment policy', 'error');
  } finally {
    updatingPolicy.value = null;
  }
};

const viewLogs = async (d: any) => {
  logTitle.value = d.name || d.command.split(' ').slice(0, 2).join(' ');
  showLogs.value = true;
  try {
    const data = await apiClient.getSiteDaemonLogs(siteId, d.id);
    logLines.value = data.lines || [];
  } catch (e) { logLines.value = []; }
};

const deleteDaemon = async (id: number) => {
  const confirmed = await confirm({ title: 'Delete Daemon', message: 'Stop and remove this daemon?', confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!confirmed) return;
  try {
    await apiClient.deleteSiteDaemon(siteId, id);
    addToast('Deleted', 'success'); fetchDaemons(true);
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const onCreated = () => { showAddModal.value = false; fetchDaemons(true); };
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

onMounted(() => { fetchDaemons(true); addClickListener(); });
onActivated(() => { fetchDaemons(true); addClickListener(); });
onDeactivated(removeClickListener);
onUnmounted(removeClickListener);

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  fetchDaemons(true);
});
</script>
