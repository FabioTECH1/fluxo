<template>
  <div class="space-y-6">
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex justify-between items-start mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Run new command</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Easily execute arbitrary commands on your server. All commands are executed from within the site's root directory. Commands will be executed as the fluxo user and may run for two minutes before timing out.
          </p>
        </div>
      </div>

      <div class="space-y-4">
        <div>
          <input v-model="commandInput" type="text"
            class="w-full font-mono text-sm bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-4 py-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow"
            :placeholder="placeholder"
            @keyup.enter="runCommand" />
          <p v-if="dirHint" class="text-xs text-gray-400 dark:text-gray-500 mt-1">Runs from {{ dirHint }} as fluxo</p>
        </div>

        <div class="flex items-center gap-3">
          <AppButton variant="primary" :loading="running" @click="runCommand">
            {{ running ? 'Running...' : 'Run' }}
          </AppButton>
        </div>
      </div>
    </div>

    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Recent commands</h2>
        <button @click="() => fetchCommands()" class="p-2 text-gray-600 dark:text-gray-400 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </button>
      </div>

      <div v-if="commands.length === 0" class="px-6 py-12 text-center text-gray-400 dark:text-gray-500 text-sm">
        No recent commands.
      </div>

      <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <li v-for="cmd in commands" :key="cmd.id" class="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors cursor-pointer" @click="selectedCommand = cmd; showModal = true">
          <div class="flex items-center justify-between">
            <div class="flex-1 min-w-0">
              <p class="text-sm font-mono text-gray-900 dark:text-gray-100 truncate">{{ cmd.command }}</p>
              <p class="text-xs text-gray-400 dark:text-gray-500 mt-0.5">{{ timeAgo(cmd.created_at) }}</p>
            </div>
            <span :class="cmd.status === 'success' ? 'bg-green-100 dark:bg-green-900/40 text-green-800 dark:text-green-300' : 'bg-red-100 dark:bg-red-900/40 text-red-800 dark:text-red-300'"
              class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold ml-3">
              {{ cmd.status }}
            </span>
          </div>
        </li>
      </ul>
    </div>

    <BaseModal v-if="selectedCommand" v-model="showModal" :title="`Command Output`" maxWidth="max-w-4xl">
      <pre class="bg-gray-900 text-green-400 p-4 rounded-lg text-sm font-mono overflow-auto max-h-[calc(100vh-16rem)] whitespace-pre-wrap">{{ selectedCommand.output || 'No output.' }}</pre>
      <template #footer>
        <AppButton variant="secondary" @click="showModal = false">Close</AppButton>
      </template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, inject } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';

const route = useRoute();
const siteId = route.params.id as string;
const { addToast } = useToast();

const commandInput = ref('');
const running = ref(false);
const commands = ref<any[]>([]);
const site = ref<any>(null);
const parentSite = inject<any>('site', null);
const selectedCommand = ref<any>(null);
const showModal = ref(false);

const placeholder = computed(() => {
  if (site.value?.app_type === 'laravel' || site.value?.app_type === 'php') return 'artisan route:list';
  return 'npm run build';
});

const dirHint = computed(() => {
  if (site.value?.domain) return `/home/fluxo/${site.value.domain}`;
  return '';
});

const fetchCommands = async (silent = false) => {
  try {
    commands.value = await apiClient.getSiteCommands(siteId) || [];
    if (!silent) addToast('Commands refreshed', 'success');
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to load commands', 'error');
  }
};

const runCommand = async () => {
  const cmd = commandInput.value.trim();
  if (!cmd || running.value) return;
  running.value = true;
  try {
    const result = await apiClient.runSiteCommand(siteId, { command: cmd });
    selectedCommand.value = result;
    showModal.value = true;
    commandInput.value = '';
    fetchCommands(true);
  } catch (e: any) {
    selectedCommand.value = { command: cmd, output: e.message || 'Command failed', status: 'failed' };
    showModal.value = true;
  } finally {
    running.value = false;
  }
};

const timeAgo = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
};

const fetchSite = async () => {
  if (parentSite?.value?.id) { site.value = parentSite.value; return; }
  try { site.value = await apiClient.getSite(siteId); } catch (e) {}
};

onMounted(() => { fetchSite(); fetchCommands(true); });
</script>
