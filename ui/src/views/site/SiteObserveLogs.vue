<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6 dark:bg-gray-900 dark:border-gray-800">
    <div class="flex flex-col sm:flex-row justify-between sm:items-center gap-4 mb-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Site Logs</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">View recent log entries for this site.</p>
      </div>
      <div class="flex flex-wrap gap-3 items-center">
        <select v-model="selectedLog" @change="() => fetchLogs()" class="w-full sm:w-auto border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
          <option v-for="src in logSources" :key="src.id" :value="src.id" :disabled="!src.exists">
            {{ src.label }}{{ !src.exists ? ' (unavailable)' : '' }}
          </option>
        </select>
        <button @click="() => fetchLogs()" class="p-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </button>
        <div class="relative">
          <button @click="showActions = !showActions" class="px-3 py-2 text-gray-600 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors text-sm flex items-center gap-1 dark:text-gray-300 dark:bg-gray-800 dark:border-gray-600 dark:hover:bg-gray-700">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 5v.01M12 12v.01M12 19v.01M12 6a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2z" />
            </svg>
          </button>
          <div v-if="showActions" class="absolute right-0 mt-1 w-48 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10 dark:bg-gray-800 dark:border-gray-700">
            <button @click="downloadLog(); showActions = false" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 text-left dark:text-gray-300 dark:hover:bg-gray-700">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 10v6m0 0l-3-3m3 3l3-3m2 8H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
              </svg>
              Download
            </button>
            <button @click="clearLog(); showActions = false" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left dark:text-red-400 dark:hover:bg-red-900/30">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              Clear
            </button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="error" class="mb-4 text-red-700 bg-red-50 border border-red-200 p-3 rounded-lg text-sm dark:text-red-300 dark:bg-red-900/30 dark:border-red-800">
      {{ error }}
    </div>

    <div class="bg-gray-900 rounded-lg font-mono text-xs text-green-400 h-[600px] overflow-y-auto">
      <div v-if="logLines.length === 0 && !error" class="text-gray-500 italic p-4 dark:text-gray-400">No log entries found.</div>
      <div v-for="(line, idx) in logLines" :key="idx" class="flex leading-relaxed hover:bg-gray-800">
        <span class="text-gray-600 select-none px-3 py-0.5 text-right w-12 shrink-0 border-r border-gray-700">{{ idx + 1 }}</span>
        <span class="px-3 py-0.5 whitespace-pre-wrap flex-1">{{ line }}</span>
      </div>
    </div>

    <div class="flex justify-between items-center mt-3">
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ logLines.length }} lines from {{ currentLabel }}</span>
      <button @click="logLines = []" class="text-xs text-red-600 hover:text-red-900 font-semibold dark:text-red-400 dark:hover:text-red-300">Clear View</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { useConfirm } from '../../composables/useConfirm';
import { apiClient } from '../../api/client';

const route = useRoute();
const siteId = route.params.id as string;

const { addToast } = useToast();
const { confirm } = useConfirm();

interface LogSource {
  id: string;
  label: string;
  path: string;
  exists: boolean;
}

const logSources = ref<LogSource[]>([]);
const selectedLog = ref('');
const logLines = ref<string[]>([]);
const error = ref('');
const showActions = ref(false);

const currentLabel = computed(() => {
  const src = logSources.value.find(s => s.id === selectedLog.value);
  return src ? src.label : '';
});

const currentPath = computed(() => {
  const src = logSources.value.find(s => s.id === selectedLog.value);
  return src ? src.path : '';
});

const fetchLogSources = async () => {
  try {
    logSources.value = await apiClient.getSiteLogsList(siteId) || [];
    const first = logSources.value.find(s => s.exists);
    if (first) {
      selectedLog.value = first.id;
      fetchLogs(true);
    }
  } catch (e: any) {
    error.value = 'Failed to load log sources: ' + e.message;
  }
};

const fetchLogs = async (silent = false) => {
  error.value = '';
  if (!currentPath.value) return;
  try {
    const data = await apiClient.getSystemLogs(currentPath.value, 100);
    logLines.value = data.lines || [];
    if (!silent) addToast('Log refreshed', 'success');
  } catch (e: any) {
    error.value = e.message || 'Failed to load logs';
    logLines.value = [];
  }
};

const downloadLog = async () => {
  if (!currentPath.value) return;
  try {
    const blob = await apiClient.downloadSystemLog(currentPath.value);
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = currentPath.value.split('/').pop() || 'log.txt';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  } catch (e: any) {
    addToast(e.message || 'Failed to download log', 'error');
  }
};

const clearLog = async () => {
  if (!currentPath.value) return;
  const confirmed = await confirm({
    title: 'Clear Log File',
    message: `Are you sure you want to clear the ${currentLabel.value}? This will delete all content in the file.`,
    confirmText: 'Clear',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await apiClient.clearSystemLog(currentPath.value);
    addToast('Log cleared successfully', 'success');
    logLines.value = [];
  } catch (e: any) {
    addToast(e.message || 'Failed to clear log', 'error');
  }
};

const handleClickOutside = (e: MouseEvent) => {
  if (!(e.target as HTMLElement).closest('.relative')) showActions.value = false;
};

onMounted(() => {
  fetchLogSources();
  window.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  window.removeEventListener('click', handleClickOutside);
});
</script>
