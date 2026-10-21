<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" />
    <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Scheduled Jobs</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Recurring tasks that run on your server via cron.
            <a href="https://man7.org/linux/man-pages/man5/crontab.5.html" target="_blank" rel="noopener noreferrer" class="text-blue-600 dark:text-blue-400 hover:underline">Learn more</a>
          </p>
        </div>
        <div class="flex gap-3">
          <button @click="showAddModal = true" class="w-8 h-8 flex items-center justify-center text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-bold shadow-sm transition-colors text-lg leading-none" title="Add job">+</button>
          <button @click="() => fetchCrons()" class="p-2 text-gray-600 dark:text-gray-400 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors" title="Refresh">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          </button>
        </div>
      </div>

      <div class="mb-4 space-y-2 text-sm" aria-live="polite">
        <StatusBadge :label="engineLabel" :variant="engineStatus?.backend.state === 'active' ? 'green' : 'yellow'" />
        <p v-if="statusMessage" class="text-gray-600 dark:text-gray-400">{{ statusMessage }}</p>
      </div>

      <ErrorAlert v-if="loadError" :message="loadError" />

      <div v-if="crons.length === 0 && !loadError" class="text-center text-gray-500 dark:text-gray-400 text-sm py-12">
        No scheduled jobs configured.
      </div>

      <div class="space-y-2">
        <div v-for="c in crons" :key="c.id" class="flex items-center gap-3 px-4 py-3 border border-gray-200 dark:border-gray-700 rounded-lg hover:border-gray-300 dark:hover:bg-gray-800/20 dark:hover:border-gray-600 transition-colors">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</span>
              <span v-if="c.site_domain" class="px-1.5 py-0.5 text-[10px] font-semibold rounded bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-100 dark:border-blue-900/50 uppercase tracking-wider">{{ c.site_domain }}</span>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-1 truncate" :title="`${c.user || 'fluxo'} · ${c.command}`">
              {{ c.user || 'fluxo' }} &middot; {{ c.command }}
            </div>
          </div>
          <div class="flex items-center gap-4 shrink-0">
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ frequencyLabel(c.expression) || c.expression }}</span>
            <StatusBadge :label="configurationLabel(c.configuration_state)" :variant="c.configuration_state === 'configured' ? 'blue' : 'yellow'" />
          </div>
          <TableActionMenu :aria-label="`${c.name || 'Scheduled job'} actions`" :items="cronActions" @select="id => selectAction(id, c)" />
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
import { computed, ref, onMounted, onActivated } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import { apiClient } from '../api/client';
import AddCronModal from './AddCronModal.vue';
import BaseModal from '../components/BaseModal.vue';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';
import TableActionMenu from '../components/TableActionMenu.vue';
import ErrorAlert from '../components/ErrorAlert.vue';

const { confirm } = useConfirm();
const { addToast } = useToast();

const crons = ref<any[]>([]);
interface EngineStatus { backend: { engine: string; state: string; qualification: string; message?: string }; branches: { state: string; message?: string }[]; state_error?: string }
const engineStatus = ref<EngineStatus | null>(null);
const engineLabel = computed(() => {
 const b = engineStatus.value?.backend;
 if (!b) return 'Scheduler status unavailable';
 const name = b.engine === 'cronie' ? 'Cronie' : b.engine === 'cron' ? 'Cron' : b.engine === 'none' ? 'Scheduler' : 'Scheduling engine';
 return `${name} ${b.state === 'missing' ? 'not found' : b.state}`;
});
const statusMessage = computed(() => engineStatus.value?.state_error || engineStatus.value?.backend.message || engineStatus.value?.branches.find(b => b.state !== 'ready')?.message || 'Configuration status does not confirm that application jobs succeeded.');
const configurationLabel = (state?: string) => ({ configured: 'Configured', missing: 'Missing', modified: 'Modified', unknown: 'Unknown' }[state || 'unknown'] || 'Unknown');
const cronActions = [{ id: 'run', label: 'Run Now' }, { id: 'logs', label: 'Logs' }, { id: 'delete', label: 'Delete', variant: 'danger' as const }];
const selectAction = (id: string, c: any) => { if (id === 'run') runCron(c); else if (id === 'logs') viewLogs(c); else if (id === 'delete') deleteCron(c.id); };
const showAddModal = ref(false);
const showLogs = ref(false);
const logTitle = ref('');
const logLines = ref<string[]>([]);
const showRunModal = ref(false);
const runTitle = ref('');
const runOutput = ref('');
const loading = ref(true);
const loadError = ref('');

const fetchCrons = async (silent = false) => {
  try {
    if (!silent) loading.value = true;
    const results = await Promise.allSettled([apiClient.get('/api/v1/crons', { useCache: false }), apiClient.get('/api/v1/scheduler/status', { useCache: false })]);
    if (results[0].status === 'rejected') throw results[0].reason;
    crons.value = results[0].value;
    loadError.value = '';
    engineStatus.value = results[1].status === 'fulfilled' ? results[1].value : null;
    if (!silent) addToast('Crons refreshed', 'success');
  } catch (e: any) { 
    loadError.value = e.message || 'Unable to load scheduled jobs. Use Refresh to try again.';
    if (!silent) addToast(e.message || 'Failed', 'error'); 
  } finally {
    loading.value = false;
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
onMounted(async () => { await fetchCrons(true); });
onActivated(() => { fetchCrons(true); });
</script>
