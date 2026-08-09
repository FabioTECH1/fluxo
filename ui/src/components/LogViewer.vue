<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" />
    <div v-else class="rounded-lg border border-gray-100 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div class="mb-4 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ title }}</h2>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ description }}</p>
        </div>

        <div class="flex flex-wrap items-center gap-3">
          <select
            v-model="selectedLog"
            aria-label="Log source"
            :disabled="refreshing || loadingSources"
            class="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 sm:w-auto"
            @change="handleLogChange"
          >
            <option v-for="source in logSources" :key="source.id" :value="source.id" :disabled="!source.exists">
              {{ source.label }}{{ !source.exists ? ' (unavailable)' : '' }}
            </option>
          </select>

          <AppButton
            variant="secondary"
            size="sm"
            :loading="refreshing"
            :disabled="!currentPath || loadingSources"
            :title="refreshing ? 'Refreshing logs' : 'Refresh logs'"
            :aria-label="refreshing ? 'Refreshing logs' : 'Refresh logs'"
            :aria-busy="refreshing"
            @click="() => fetchLogs(false, true)"
          >
            <ArrowPathIcon class="h-4 w-4 motion-reduce:animate-none" :class="{ 'animate-spin': refreshing }" aria-hidden="true" />
          </AppButton>

          <span v-if="refreshStatus" class="text-xs text-gray-500 dark:text-gray-400" role="status" aria-live="polite">
            {{ refreshStatus }}
          </span>

          <div ref="actionMenu" class="relative">
            <button
              ref="actionButton"
              type="button"
              aria-label="Log actions"
              aria-haspopup="menu"
              :aria-expanded="showActions"
              aria-controls="log-actions-menu"
              :disabled="!currentPath || downloading || clearing"
              class="flex items-center rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-400 dark:hover:bg-gray-700"
              @click.stop="showActions = !showActions"
            >
              <EllipsisVerticalIcon class="h-4 w-4" aria-hidden="true" />
            </button>
            <div
              v-if="showActions"
              id="log-actions-menu"
              role="menu"
              class="absolute right-0 z-10 mt-1 w-48 rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-900"
            >
              <button role="menuitem" type="button" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800" @click="downloadLog">
                <ArrowDownTrayIcon class="h-4 w-4" aria-hidden="true" />
                {{ downloading ? 'Downloading…' : 'Download' }}
              </button>
              <button role="menuitem" type="button" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/30" @click="clearLog">
                <TrashIcon class="h-4 w-4" aria-hidden="true" />
                {{ clearing ? 'Clearing…' : 'Clear' }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="error" role="alert" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/30 dark:text-red-300">
        {{ error }}
      </div>

      <div
        class="h-[600px] overflow-y-auto rounded-lg bg-gray-900 font-mono text-xs text-green-400"
        tabindex="0"
        :aria-label="`${currentLabel || title} log entries`"
      >
        <div v-if="logLines.length === 0 && !error" class="p-4 italic text-gray-500 dark:text-gray-400">No log entries found.</div>
        <div v-for="(line, index) in logLines" :key="index" class="flex leading-relaxed hover:bg-gray-800">
          <span class="w-12 shrink-0 select-none border-r border-gray-700 px-3 py-0.5 text-right text-gray-600 dark:text-gray-500">{{ index + 1 }}</span>
          <span class="flex-1 whitespace-pre-wrap px-3 py-0.5">{{ line }}</span>
        </div>
      </div>

      <div class="mt-3 flex items-center justify-between">
        <span class="text-xs text-gray-500 dark:text-gray-400">
          {{ logLines.length }} {{ logLines.length === 1 ? 'line' : 'lines' }}<template v-if="currentLabel"> from {{ currentLabel }}</template>
        </span>
        <button type="button" :disabled="logLines.length === 0" class="text-xs font-semibold text-red-600 hover:text-red-900 disabled:cursor-not-allowed disabled:opacity-50 dark:text-red-400 dark:hover:text-red-300" @click="logLines = []">
          Clear view
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  ArrowDownTrayIcon,
  ArrowPathIcon,
  EllipsisVerticalIcon,
  TrashIcon,
} from '@heroicons/vue/24/outline';
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, ref } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AppButton from './AppButton.vue';
import SkeletonLoader from './SkeletonLoader.vue';

interface LogSource {
  id: string;
  label: string;
  path: string;
  exists: boolean;
}

const props = defineProps<{
  title: string;
  description: string;
  sourceLoader: (bypassCache?: boolean) => Promise<LogSource[]>;
}>();

const { addToast } = useToast();
const { confirm } = useConfirm();

const logSources = ref<LogSource[]>([]);
const selectedLog = ref('');
const logLines = ref<string[]>([]);
const error = ref('');
const showActions = ref(false);
const loading = ref(true);
const loadingSources = ref(false);
const refreshing = ref(false);
const downloading = ref(false);
const clearing = ref(false);
const lastUpdatedAt = ref<Date | null>(null);
const actionMenu = ref<HTMLElement | null>(null);
const actionButton = ref<HTMLButtonElement | null>(null);

let sourceRequestVersion = 0;
let logRequestVersion = 0;
let initialActivation = true;

const currentSource = computed(() => logSources.value.find(source => source.id === selectedLog.value));
const currentLabel = computed(() => currentSource.value?.label || '');
const currentPath = computed(() => currentSource.value?.path || '');
const refreshStatus = computed(() => {
  if (refreshing.value) return 'Refreshing logs…';
  if (!lastUpdatedAt.value) return '';
  return `Updated ${lastUpdatedAt.value.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })}`;
});

const fetchLogs = async (silent = false, bypassCache = false) => {
  const path = currentPath.value;
  if (!path) return;

  const request = ++logRequestVersion;
  error.value = '';
  refreshing.value = true;
  try {
    const data = await apiClient.getSystemLogs(path, 100, bypassCache);
    if (request !== logRequestVersion || currentPath.value !== path) return;
    logLines.value = Array.isArray(data?.lines) ? data.lines : [];
    lastUpdatedAt.value = new Date();
  } catch (reason: any) {
    if (request !== logRequestVersion || currentPath.value !== path) return;
    const message = reason?.message || 'Failed to load logs';
    error.value = lastUpdatedAt.value
      ? `${message}. Showing the previously loaded entries.`
      : message;
    if (!silent) addToast(message, 'error');
  } finally {
    if (request === logRequestVersion) refreshing.value = false;
  }
};

const fetchLogSources = async () => {
  const request = ++sourceRequestVersion;
  loadingSources.value = true;
  error.value = '';
  try {
    const sources = await props.sourceLoader(true);
    if (request !== sourceRequestVersion) return;
    logSources.value = Array.isArray(sources) ? sources : [];

    const selected = logSources.value.find(source => source.id === selectedLog.value && source.exists)
      || logSources.value.find(source => source.exists);
    if (!selected) {
      selectedLog.value = '';
      logLines.value = [];
      lastUpdatedAt.value = null;
      error.value = 'No readable log files are available.';
      return;
    }

    const sourceChanged = selectedLog.value !== selected.id;
    selectedLog.value = selected.id;
    if (sourceChanged) {
      logLines.value = [];
      lastUpdatedAt.value = null;
    }
    await fetchLogs(true, true);
  } catch (reason: any) {
    if (request !== sourceRequestVersion) return;
    error.value = `Failed to load log sources: ${reason?.message || 'Unknown error'}`;
  } finally {
    if (request === sourceRequestVersion) {
      loadingSources.value = false;
      loading.value = false;
    }
  }
};

const handleLogChange = () => {
  logRequestVersion++;
  logLines.value = [];
  lastUpdatedAt.value = null;
  void fetchLogs(true, true);
};

const downloadLog = async () => {
  const path = currentPath.value;
  if (!path || downloading.value) return;
  showActions.value = false;
  actionButton.value?.focus();
  downloading.value = true;
  try {
    const blob = await apiClient.downloadSystemLog(path);
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = path.split('/').pop() || 'log.txt';
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (reason: any) {
    addToast(reason?.message || 'Failed to download log', 'error');
  } finally {
    downloading.value = false;
  }
};

const clearLog = async () => {
  const path = currentPath.value;
  const label = currentLabel.value;
  if (!path || clearing.value) return;
  showActions.value = false;
  actionButton.value?.focus();
  const approved = await confirm({
    title: 'Clear log file?',
    message: `Clear ${label}? This permanently removes all content from the log file.`,
    confirmText: 'Clear log',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!approved || currentPath.value !== path) return;

  clearing.value = true;
  try {
    await apiClient.clearSystemLog(path);
    logRequestVersion++;
    logLines.value = [];
    lastUpdatedAt.value = new Date();
    addToast(`${label} cleared`, 'success');
  } catch (reason: any) {
    addToast(reason?.message || 'Failed to clear log', 'error');
  } finally {
    clearing.value = false;
  }
};

const handleDocumentClick = (event: MouseEvent) => {
  if (!actionMenu.value?.contains(event.target as Node)) showActions.value = false;
};

const handleDocumentKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && showActions.value) {
    showActions.value = false;
    actionButton.value?.focus();
  }
};

const addDocumentListeners = () => {
  window.addEventListener('click', handleDocumentClick);
  window.addEventListener('keydown', handleDocumentKeydown);
};

const removeDocumentListeners = () => {
  window.removeEventListener('click', handleDocumentClick);
  window.removeEventListener('keydown', handleDocumentKeydown);
};

const cancelPendingRequests = () => {
  sourceRequestVersion++;
  logRequestVersion++;
  refreshing.value = false;
  loadingSources.value = false;
};

onMounted(() => {
  void fetchLogSources();
  addDocumentListeners();
});

onActivated(() => {
  if (initialActivation) {
    initialActivation = false;
    return;
  }
  void fetchLogSources();
  addDocumentListeners();
});

onDeactivated(() => {
  cancelPendingRequests();
  removeDocumentListeners();
});

onUnmounted(() => {
  cancelPendingRequests();
  removeDocumentListeners();
});
</script>
