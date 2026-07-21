<template>
  <div class="space-y-6">
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
      <div class="px-4 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center gap-3 sm:px-6">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Deployments</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Deployment history for this site. Each deployment runs the configured deploy script (git pull, composer install, etc.).
          </p>
        </div>
        <AppButton variant="secondary" size="sm" @click="() => fetchDeployments(true)" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </AppButton>
      </div>

      <div v-if="deployments.length === 0" class="text-center text-gray-500 dark:text-gray-400 text-sm py-12">
        No deployments yet. Trigger a deployment from the Overview tab.
      </div>

      <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <li v-for="dep in deployments" :key="dep.id"
            class="px-4 py-3.5 hover:bg-gray-50 dark:hover:bg-gray-800/60 transition-colors cursor-pointer sm:px-6"
            @click="selectedDeployment = dep; showModal = true">
          <div class="flex items-start gap-3 min-w-0 sm:items-center">
            <div class="min-w-0 flex-1 sm:contents">
              <div class="flex flex-wrap items-center gap-1.5 sm:contents">
                <!-- Status badge -->
                <span :class="statusBadge(dep.status)" class="shrink-0">{{ dep.status }}</span>

                <!-- Rollback badge -->
                <span v-if="dep.trigger_source === 'rollback'"
                  class="shrink-0 inline-flex items-center gap-0.5 text-[9px] uppercase font-bold text-orange-600 bg-orange-100 dark:bg-orange-900/30 dark:text-orange-300 px-1 py-0.5 rounded"
                  title="Rollback deployment">
                  <svg class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a5 5 0 015 5v2M3 10l4-4m-4 4l4 4" /></svg>
                  Rollback
                </span>

                <!-- Auto badge -->
                <span v-if="dep.trigger_source === 'github_webhook'"
                  class="shrink-0 inline-flex items-center gap-0.5 text-[9px] uppercase font-bold text-purple-600 bg-purple-100 dark:bg-purple-900/30 dark:text-purple-300 px-1 py-0.5 rounded"
                  title="Auto-deployed via GitHub Push">
                  <svg class="w-2.5 h-2.5" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clip-rule="evenodd" /></svg>
                  Auto
                </span>

                <!-- Branch -->
                <span v-if="dep.branch" class="shrink-0 text-[10px] text-gray-500 bg-gray-100 dark:bg-gray-700 dark:text-gray-400 px-1.5 py-0.5 rounded font-mono">{{ dep.branch }}</span>

                <!-- Commit hash -->
                <span v-if="dep.commit_hash" class="shrink-0 font-mono text-xs font-semibold text-blue-600 dark:text-blue-400">{{ dep.commit_hash.slice(0, 7) }}</span>
              </div>

              <!-- Commit message — truncated, flexible -->
              <span class="mt-1.5 block min-w-0 truncate text-xs text-gray-700 dark:text-gray-200 sm:mt-0 sm:flex-1 sm:text-sm">
                {{ dep.commit_message || 'Manual Deployment' }}
              </span>
            </div>

            <!-- Deployer and time -->
            <div class="flex w-20 min-w-0 shrink-0 flex-col items-end leading-tight sm:ml-2 sm:w-auto sm:min-w-24">
              <span v-if="dep.commit_author" class="max-w-full truncate text-xs font-medium text-gray-600 dark:text-gray-300 sm:max-w-36" :title="dep.commit_author">
                {{ dep.commit_author }}
              </span>
              <span class="text-[10px] text-gray-400 dark:text-gray-500" :class="dep.commit_author ? 'mt-0.5' : 'text-xs'">
                {{ timeAgo(dep.created_at) }}
              </span>
            </div>
          </div>
        </li>
      </ul>

      <div v-if="totalPages > 1" class="px-4 py-4 border-t border-gray-100 dark:border-gray-800 flex justify-between items-center sm:px-6">
        <AppButton variant="secondary" size="sm" :disabled="currentPage === 1" @click="changePage(currentPage - 1)">Previous</AppButton>
        <span class="text-sm text-gray-600 dark:text-gray-400">Page {{ currentPage }} of {{ totalPages }}</span>
        <AppButton variant="secondary" size="sm" :disabled="currentPage === totalPages" @click="changePage(currentPage + 1)">Next</AppButton>
      </div>
    </div>

    <BaseModal v-if="selectedDeployment" v-model="showModal" 
               title="Deployment Output" 
               maxWidth="max-w-5xl">
      <template #title>
        <div v-if="selectedDeployment.commit_hash" class="flex items-center gap-3 min-w-0 flex-1 pr-4">
          <span class="text-lg font-bold text-gray-900 dark:text-gray-100 truncate flex-1">
            <span v-if="selectedDeployment.trigger_source === 'rollback'" class="inline-flex items-center gap-1 text-[10px] uppercase font-bold text-orange-600 bg-orange-100 dark:bg-orange-900/30 dark:text-orange-300 px-1.5 py-0.5 rounded align-middle mr-2 mt-[-2px]">
              <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a5 5 0 015 5v2M3 10l4-4m-4 4l4 4" /></svg>
              Rollback
            </span>
            <span v-if="selectedDeployment.trigger_source === 'github_webhook'" class="inline-flex items-center gap-1 text-[10px] uppercase font-bold text-purple-600 bg-purple-100 dark:bg-purple-900/30 dark:text-purple-300 px-1.5 py-0.5 rounded align-middle mr-2 mt-[-2px]">
              <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clip-rule="evenodd" /></svg>
              Auto
            </span>
            {{ selectedDeployment.commit_message || 'Manual Deployment' }}
          </span>
          <div class="flex items-center gap-3 shrink-0">
            <div v-if="selectedDeployment.branch" class="flex items-center gap-1.5 text-xs text-gray-500 bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 px-2 py-1 rounded shadow-sm">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
              <span>{{ selectedDeployment.branch }}</span>
            </div>
            <span class="font-mono text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300 px-2 py-1 rounded border border-blue-200 dark:border-blue-800/50 shadow-sm">{{ selectedDeployment.commit_hash.slice(0, 7) }}</span>
          </div>
        </div>
        <h3 v-else class="text-lg font-bold text-gray-900 dark:text-gray-100">Deployment #{{ selectedDeployment.id }} Output</h3>
      </template>
      
      <div class="space-y-4">
        <pre ref="terminalBox" class="bg-gray-900 text-green-400 p-4 rounded-lg text-sm font-mono overflow-auto max-h-[calc(100vh-20rem)] whitespace-pre-wrap">
          <div v-for="(line, idx) in displayLines" :key="idx">{{ line }}</div>
        </pre>
      </div>
      <template #footer>
        <div class="flex justify-between w-full">
          <AppButton v-if="selectedDeployment?.status === 'success' && selectedDeployment?.commit_hash && selectedDeployment.id !== deployments[0]?.id"
                     variant="secondary" size="sm"
                     class="text-red-700 dark:text-red-400 border-red-300 dark:border-red-700 hover:bg-red-50 dark:hover:bg-red-900/20"
                     :disabled="isDeploying || rollingBack"
                     :loading="rollingBack"
                     @click="handleRollback">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a5 5 0 015 5v2M3 10l4-4m-4 4l4 4" /></svg>
            Rollback
          </AppButton>
          <span v-else></span>
          <AppButton variant="secondary" @click="showModal = false">Close</AppButton>
        </div>
      </template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, inject, onMounted, onUnmounted, onActivated, onDeactivated, watch, nextTick, type Ref } from 'vue';
import { useRoute } from 'vue-router';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';

import { apiClient } from '../../api/client';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import { useWebSocket } from '../../composables/useWebSocket';

const route = useRoute();
let id = route.params.id as string;

const deployments = ref<any[]>([]);
const selectedDeployment = ref<any>(null);
const showModal = ref(false);
const currentPage = ref(1);
const totalPages = ref(1);
const rollingBack = ref(false);
const { confirm } = useConfirm();
const { addToast } = useToast();
const { logs: wsLogs, connect: wsConnect, disconnect: wsDisconnect, clear: wsClear } = useWebSocket();
const terminalBox = ref<HTMLElement | null>(null);

const isDeployActive = computed(() =>
  selectedDeployment.value?.status === 'running' || selectedDeployment.value?.status === 'pending'
);

const displayLines = computed(() => {
  if (wsLogs.value.length > 0) return wsLogs.value;
  const out = selectedDeployment.value?.output;
  return out ? [out] : ['No output captured.'];
});

watch(wsLogs, () => {
  nextTick(() => {
    terminalBox.value?.scrollTo({ top: terminalBox.value.scrollHeight });
  });
});

watch(showModal, (open) => {
  if (open && isDeployActive.value) {
    wsClear();
    wsConnect(id);
  } else if (!open) {
    wsDisconnect();
    wsClear();
  }
});

watch(selectedDeployment, () => {
  if (showModal.value) {
    wsDisconnect();
    wsClear();
    if (isDeployActive.value) {
      wsConnect(id);
    }
  }
});

const deploySignal = inject<Ref<number>>('deploySignal', ref(0));

watch(deploySignal, async () => {
  currentPage.value = 1;
  await fetchDeployments(true);
  const latest = deployments.value[0];
  if (latest && (latest.status === 'running' || latest.status === 'pending')) {
    selectedDeployment.value = latest;
    showModal.value = true;
  }
});

let fastPoll: number | null = null;
let slowPoll: number | null = null;

const fetchDeployments = async (bypassCache = false) => {
  try {
    const data = await apiClient.getSiteDeployments(id, currentPage.value, bypassCache);
    deployments.value = data.data || [];
    totalPages.value = Math.ceil(data.total / data.per_page) || 1;
    
    if (deployments.value && deployments.value.length > 0 && (deployments.value[0].status === 'running' || deployments.value[0].status === 'pending')) {
      if (!fastPoll) fastPoll = window.setInterval(() => fetchDeployments(true), 2000);
    } else {
      if (fastPoll) {
        window.clearInterval(fastPoll);
        fastPoll = null;
      }
    }
  } catch (e) {}
};

const startPolls = () => {
  if (slowPoll) return;
  slowPoll = window.setInterval(() => fetchDeployments(true), 10000);
};

const stopAllPolls = () => {
  if (fastPoll) { window.clearInterval(fastPoll); fastPoll = null; }
  if (slowPoll) { window.clearInterval(slowPoll); slowPoll = null; }
};

const changePage = (page: number) => {
  if (page < 1 || page > totalPages.value) return;
  currentPage.value = page;
  fetchDeployments();
};

const isDeploying = computed(() =>
  deployments.value.some(d => d.status === 'running' || d.status === 'pending')
);

const handleRollback = async () => {
  if (!selectedDeployment.value || rollingBack.value) return;
  const confirmed = await confirm({
    title: 'Rollback Deployment',
    message: `Roll back to commit ${selectedDeployment.value.commit_hash.slice(0, 7)}?`,
    confirmText: 'Rollback',
    variant: 'danger'
  });
  if (!confirmed) return;
  rollingBack.value = true;
  try {
    await apiClient.rollbackDeployment(id, selectedDeployment.value.id);
    addToast('Rollback deployment queued', 'success');
    showModal.value = false;
    fetchDeployments(true);
    selectedDeployment.value = null;
  } catch (e: any) {
    addToast(e.message || 'Failed to queue rollback', 'error');
  } finally {
    rollingBack.value = false;
  }
};

  const statusBadge = (status: string) => {
    const base = 'inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold';
    if (status === 'success') return `${base} bg-green-100 dark:bg-green-900/40 text-green-800 dark:text-green-300`;
    if (status === 'failed') return `${base} bg-red-100 dark:bg-red-900/40 text-red-800 dark:text-red-300`;
    if (status === 'running') return `${base} bg-yellow-100 dark:bg-yellow-900/40 text-yellow-800 dark:text-yellow-300`;
    if (status === 'pending') return `${base} bg-indigo-100 dark:bg-indigo-900/40 text-indigo-800 dark:text-indigo-300`;
    return `${base} bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400`;
  };

const timeAgo = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)} minute${Math.floor(diff / 60) > 1 ? 's' : ''} ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} hour${Math.floor(diff / 3600) > 1 ? 's' : ''} ago`;
  if (diff < 604800) return `${Math.floor(diff / 86400)} day${Math.floor(diff / 86400) > 1 ? 's' : ''} ago`;
  return `${Math.floor(diff / 604800)} week${Math.floor(diff / 604800) > 1 ? 's' : ''} ago`;
};

onMounted(() => {
  fetchDeployments();
  startPolls();
});

onActivated(() => {
  fetchDeployments();
  startPolls();
});

onDeactivated(stopAllPolls);

onUnmounted(stopAllPolls);

watch(() => route.params.id, (newId) => {
  wsDisconnect();
  wsClear();
  stopAllPolls();
  if (typeof newId !== 'string' || !/^[1-9]\d*$/.test(newId)) return;
  id = newId;
  fetchDeployments();
  startPolls();
});
</script>
