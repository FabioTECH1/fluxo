<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" />
    <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Scheduled Jobs</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Recurring tasks that run on your server via cron.
            <a href="https://man7.org/linux/man-pages/man5/crontab.5.html" class="text-blue-600 dark:text-blue-400 hover:underline">Learn more</a>
          </p>
        </div>
        <div class="flex gap-3">
          <button @click="showAddModal = true" class="w-8 h-8 flex items-center justify-center text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-bold shadow-sm transition-colors text-lg leading-none" title="Add job">+</button>
          <button @click="() => fetchCrons()" class="p-2 text-gray-600 dark:text-gray-400 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors" title="Refresh">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          </button>
        </div>
      </div>

      <div v-if="crons.length === 0" class="text-center text-gray-500 dark:text-gray-400 text-sm py-12">
        No scheduled jobs configured.
      </div>

      <div class="space-y-2">
        <div v-for="c in crons" :key="c.id" class="flex items-center gap-3 px-4 py-3 border border-gray-200 dark:border-gray-700 rounded-lg hover:border-gray-300 dark:hover:bg-gray-800/20 dark:hover:border-gray-600 transition-colors">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</span>
              <span v-if="c.site_domain" class="px-1.5 py-0.5 text-[10px] font-semibold rounded bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-100 dark:border-blue-900/50 uppercase tracking-wider">{{ c.site_domain }}</span>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-1 truncate">
              {{ c.user || 'fluxo' }} &middot; {{ c.command }}
            </div>
          </div>
          <div class="flex items-center gap-4 shrink-0">
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ frequencyLabel(c.expression) || c.expression }}</span>
            <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40">
              <span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
              Installed
            </span>
          </div>
          <div class="relative shrink-0">
            <button @click="toggleMenu(c.id)" class="px-2.5 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 font-medium transition-colors">···</button>
            <div v-if="openMenu === c.id" class="absolute right-0 mt-1 w-36 bg-white dark:bg-gray-900 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 py-1 z-10">
              <button @click="runCron(c); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/30">Run Now</button>
              <button @click="viewLogs(c); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800">Logs</button>
              <button @click="deleteCron(c.id); openMenu = null" class="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30">Delete</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <AddCronModal v-model="showAddModal" @created="onCreated" />

    <BaseModal v-model="showRunModal" :title="'Output: ' + runTitle" max-width="max-w-3xl">
      <template #footer>
        <AppButton variant="secondary" @click="showRunModal = false">Close</AppButton>
      </template>
      <pre class="bg-gray-900 rounded-lg p-4 text-xs text-green-400 font-mono max-h-96 overflow-y-auto whitespace-pre-wrap">{{ runOutput || 'No output.' }}</pre>
    </BaseModal>

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
import { ref, onMounted, onActivated, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import { apiClient } from '../api/client';
import AddCronModal from './AddCronModal.vue';
import BaseModal from '../components/BaseModal.vue';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';

const { confirm } = useConfirm();
const { addToast } = useToast();

const crons = ref<any[]>([]);
const openMenu = ref<number | null>(null);
const showAddModal = ref(false);
const showLogs = ref(false);
const logTitle = ref('');
const logLines = ref<string[]>([]);
const showRunModal = ref(false);
const runTitle = ref('');
const runOutput = ref('');
const loading = ref(true);

const fetchCrons = async (silent = false) => {
  try {
    if (!silent) loading.value = true;
    crons.value = await apiClient.get('/api/v1/crons');
    if (!silent) addToast('Crons refreshed', 'success');
  } catch (e: any) { 
    if (!silent) addToast(e.message || 'Failed', 'error'); 
  } finally {
    if (!silent) loading.value = false;
  }
};

const runCron = async (c: any) => {
  try {
    const data = await apiClient.post(`/api/v1/crons/${c.id}/run`);
    runTitle.value = c.name || c.command.split(' ').slice(0, 3).join(' ');
    runOutput.value = data.output || 'Command executed with no output.';
    showRunModal.value = true;
    addToast('Executed', 'success');
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const viewLogs = async (c: any) => {
  logTitle.value = c.name || c.command.split(' ').slice(0, 3).join(' ');
  showLogs.value = true;
  try {
    const data = await apiClient.get(`/api/v1/crons/${c.id}/logs`);
    logLines.value = data.lines || [];
  } catch (e) { logLines.value = []; }
};

const deleteCron = async (id: number) => {
  const confirmed = await confirm({ title: 'Delete Job', message: 'Delete this scheduled job?', confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!confirmed) return;
  try {
    await apiClient.delete(`/api/v1/crons/${id}`);
    apiClient.invalidate('/api/v1/crons');
    addToast('Deleted', 'success'); fetchCrons();
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const frequencyLabel = (expr: string) => {
  const map: Record<string, string> = {
    '* * * * *': 'Every minute', '*/5 * * * *': 'Every 5 min',
    '0 * * * *': 'Hourly', '0 0 * * *': 'Daily',
    '0 0 * * 0': 'Weekly', '0 0 1 * *': 'Monthly',
  };
  return map[expr] || '';
};

const onCreated = () => { showAddModal.value = false; fetchCrons(); };
const toggleMenu = (id: number) => { openMenu.value = openMenu.value === id ? null : id; };
const handleClickOutside = (e: MouseEvent) => { if (!(e.target as HTMLElement).closest('.relative')) openMenu.value = null; };

onMounted(async () => { 
  loading.value = true;
  await fetchCrons(true); 
  loading.value = false;
  window.addEventListener('click', handleClickOutside); 
});
onActivated(() => { fetchCrons(true); });
onUnmounted(() => { window.removeEventListener('click', handleClickOutside); });
</script>
