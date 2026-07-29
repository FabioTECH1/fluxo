<template>
  <div class="space-y-6">
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex justify-between items-start mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Run new command</h2>
          <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Easily execute arbitrary commands on your server. All commands are executed from the site's active application directory as the fluxo user and may run for two minutes before timing out.
          </p>
          <p v-if="site?.app_type === 'wordpress'" class="mt-3 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-800 dark:border-sky-900 dark:bg-sky-950/30 dark:text-sky-300">
            Fluxo automatically adds the WordPress web directory as <code class="font-mono">--path</code> when a WP-CLI command does not include one.
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
        <AppButton variant="secondary" size="sm" @click="() => fetchCommands(false, true)" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </AppButton>
      </div>

      <div v-if="commands.length === 0" class="px-6 py-12 text-center text-gray-400 dark:text-gray-500 text-sm">
        No recent commands.
      </div>

      <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <li v-for="cmd in commands" :key="cmd.id" class="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors cursor-pointer" @click="openCommand(cmd)">
          <div class="flex items-center justify-between">
            <div class="flex-1 min-w-0">
              <p class="text-sm font-mono text-gray-900 dark:text-gray-100 truncate">{{ cmd.command }}</p>
              <p class="text-xs text-gray-400 dark:text-gray-500 mt-0.5">{{ timeAgo(cmd.created_at) }}</p>
            </div>
            <div class="ml-3 flex items-center gap-2">
              <span :class="commandStatusClass(cmd.status)"
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold">
                {{ cmd.status }}
              </span>
              <TableActionMenu :items="commandMenuItems()" :loading="deletingCommandId === cmd.id || rerunningCommandId === cmd.id" :aria-label="`Actions for ${cmd.command}`" @select="handleCommandAction($event, cmd)" />
            </div>
          </div>
        </li>
      </ul>

      <div class="px-6 pb-4">
        <TablePagination :page="currentPage" :total-items="totalCommands" :page-size="pageSize" @update:page="changeCommandPage" />
      </div>
    </div>

    <BaseModal v-if="selectedCommand" v-model="showModal" :title="`Command Output`" maxWidth="max-w-4xl">
      <pre ref="terminalBox" class="bg-gray-900 text-green-400 p-4 rounded-lg text-sm font-mono overflow-auto max-h-[calc(100vh-16rem)] whitespace-pre-wrap">{{ displayText }}</pre>
      <template #footer>
        <div class="flex w-full justify-between">
          <AppButton variant="secondary" :disabled="running || isSelectedCommandActive || rerunningCommandId !== null" :loading="rerunningCommandId === selectedCommand?.id || rerunningCommandId === 'transient'" @click="rerunCommand(selectedCommand)">Rerun</AppButton>
          <AppButton variant="secondary" @click="showModal = false">Close</AppButton>
        </div>
      </template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated, onDeactivated, onBeforeUnmount, computed, watch, nextTick } from 'vue';
import { useRoute } from 'vue-router';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import { useWebSocket } from '../../composables/useWebSocket';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';
import TableActionMenu from '../../components/TableActionMenu.vue';
import TablePagination from '../../components/TablePagination.vue';

const route = useRoute();
let siteId = route.params.id as string;
const { confirm } = useConfirm();
const { addToast } = useToast();
const { logs: wsLogs, connect: wsConnect, disconnect: wsDisconnect, clear: wsClear } = useWebSocket();

const commandInput = ref('');
const running = ref(false);
const commands = ref<any[]>([]);
const site = ref<any>(null);
const selectedCommand = ref<any>(null);
const showModal = ref(false);
const currentPage = ref(1);
const totalCommands = ref(0);
const pageSize = 10;
const deletingCommandId = ref<number | null>(null);
const rerunningCommandId = ref<number | 'transient' | null>(null);
const terminalBox = ref<HTMLElement | null>(null);
let commandPoll: number | null = null;
let commandFetchToken = 0;
let siteFetchToken = 0;

const placeholder = computed(() => {
  if (site.value?.app_type === 'wordpress') return 'wp core version';
  if (site.value?.app_type === 'laravel') return 'artisan route:list';
  if (site.value?.app_type === 'php') return 'php -v';
  if (site.value?.app_type === 'html') return 'ls -la';
  return 'npm run build';
});

const dirHint = computed(() => {
  if (site.value?.path) {
    return site.value.deployment_strategy === 'zero-downtime'
      ? `${site.value.path}/current`
      : site.value.path;
  }
  return '';
});

const isCommandActive = (cmd: any) => cmd?.status === 'running' || cmd?.status === 'pending';

const isSelectedCommandActive = computed(() => isCommandActive(selectedCommand.value));

const selectedCommandId = computed(() => {
  const commandId = selectedCommand.value?.id;
  return /^[1-9]\d*$/.test(String(commandId)) ? commandId : null;
});

const displayText = computed(() => {
  if (wsLogs.value.length > 0) return wsLogs.value.join('');
  const output = selectedCommand.value?.output;
  if (output) return output;
  if (isSelectedCommandActive.value) return 'Waiting for command output...';
  if (selectedCommand.value?.status === 'success') return 'Command completed successfully with no output.';
  return 'No output.';
});

const commandStatusClass = (status: string) => {
  if (status === 'success') return 'bg-green-100 dark:bg-green-900/40 text-green-800 dark:text-green-300';
  if (status === 'running' || status === 'pending') return 'bg-blue-100 dark:bg-blue-900/40 text-blue-800 dark:text-blue-300';
  return 'bg-red-100 dark:bg-red-900/40 text-red-800 dark:text-red-300';
};

const connectSelectedCommandLog = () => {
  if (!selectedCommandId.value) return;
  wsConnect(siteId, { commandId: selectedCommandId.value, replay: true });
};

const startCommandPoll = () => {
  if (commandPoll) return;
  commandPoll = window.setInterval(() => fetchCommands(true, true), 1500);
};

const stopCommandPoll = () => {
  if (!commandPoll) return;
  window.clearInterval(commandPoll);
  commandPoll = null;
};

const refreshCommandPoll = () => {
  const hasActiveListCommand = commands.value.some(isCommandActive);
  if (isSelectedCommandActive.value || hasActiveListCommand) {
    startCommandPoll();
  } else {
    stopCommandPoll();
  }
};

watch(displayText, () => {
  nextTick(() => {
    terminalBox.value?.scrollTo({ top: terminalBox.value.scrollHeight });
  });
});

watch(showModal, (open) => {
  if (open && isSelectedCommandActive.value) {
    wsClear();
    connectSelectedCommandLog();
    startCommandPoll();
  } else if (!open) {
    wsDisconnect();
    wsClear();
    refreshCommandPoll();
  }
});

watch(selectedCommand, (next, previous) => {
  if (!showModal.value) return;

  const changedCommand = next?.id !== previous?.id;
  const changedActiveState = isCommandActive(next) !== isCommandActive(previous);
  if (!changedCommand && !changedActiveState) return;

  wsDisconnect();
  if (changedCommand) wsClear();
  if (isCommandActive(next)) {
    connectSelectedCommandLog();
    startCommandPoll();
  } else {
    refreshCommandPoll();
  }
});

const fetchCommands = async (silent = false, bypassCache = false) => {
  const requestToken = ++commandFetchToken;
  const requestSiteId = siteId;
  const requestPage = currentPage.value;
  try {
    const result = await apiClient.getSiteCommands(requestSiteId, requestPage, bypassCache);
    if (requestToken !== commandFetchToken || requestSiteId !== siteId || requestPage !== currentPage.value) return;
    commands.value = result?.data || [];
    totalCommands.value = result?.total || 0;
    if (selectedCommandId.value) {
      const updatedSelection = commands.value.find(cmd => cmd.id === selectedCommandId.value);
      if (updatedSelection) {
        selectedCommand.value = updatedSelection;
      } else if (isSelectedCommandActive.value) {
        let updatedCommand = null;
        try {
          updatedCommand = await apiClient.getSiteCommand(requestSiteId, selectedCommandId.value, true);
        } catch {
          if (requestToken !== commandFetchToken || requestSiteId !== siteId || requestPage !== currentPage.value) return;
          selectedCommand.value = { ...selectedCommand.value, status: 'failed', output: 'Command no longer exists.' };
          refreshCommandPoll();
          return;
        }
        if (requestToken !== commandFetchToken || requestSiteId !== siteId || requestPage !== currentPage.value) return;
        selectedCommand.value = updatedCommand;
      }
    }
    refreshCommandPoll();
    if (totalCommands.value === 0 && currentPage.value !== 1) {
      currentPage.value = 1;
      return;
    }
    if (commands.value.length === 0 && currentPage.value > 1 && totalCommands.value > 0) {
      currentPage.value = Math.ceil(totalCommands.value / pageSize);
      await fetchCommands(true, true);
      return;
    }
    if (!silent) addToast('Commands refreshed', 'success');
  } catch (e: any) {
    if (requestToken !== commandFetchToken || requestSiteId !== siteId || requestPage !== currentPage.value) return;
    if (!silent) addToast(e.message || 'Failed to load commands', 'error');
  }
};

const openCommand = (cmd: any) => {
  selectedCommand.value = cmd;
  showModal.value = true;
};

const runCommand = async () => {
  const cmd = commandInput.value.trim();
  if (!cmd || running.value) return;
  running.value = true;
  selectedCommand.value = { command: cmd, output: '', status: 'running' };
  showModal.value = true;
  wsClear();
  try {
    const result = await apiClient.runSiteCommand(siteId, { command: cmd, stream: true });
    selectedCommand.value = result;
    commandInput.value = '';
    currentPage.value = 1;
    startCommandPoll();
    await fetchCommands(true, true);
  } catch (e: any) {
    wsDisconnect();
    selectedCommand.value = { command: cmd, output: e.message || 'Command failed', status: 'failed' };
    showModal.value = true;
  } finally {
    running.value = false;
  }
};

const commandMenuItems = () => [
  { id: 'rerun', label: 'Rerun', variant: 'primary' as const, disabled: running.value || rerunningCommandId.value !== null },
  { id: 'delete', label: 'Delete', variant: 'danger' as const, disabled: deletingCommandId.value !== null },
];

const changeCommandPage = (page: number) => {
  if (page === currentPage.value) return;
  currentPage.value = page;
  fetchCommands(true, true);
};

const handleCommandAction = (action: string, cmd: any) => {
  if (action === 'rerun') {
    rerunCommand(cmd);
  } else if (action === 'delete') {
    deleteCommand(cmd);
  }
};

const rerunCommand = async (cmd: any) => {
  if (!cmd?.command || running.value || rerunningCommandId.value !== null) return;
  rerunningCommandId.value = typeof cmd.id === 'number' ? cmd.id : 'transient';
  const command = cmd.command;
  selectedCommand.value = { command, output: '', status: 'running' };
  showModal.value = true;
  wsClear();
  try {
    const result = await apiClient.runSiteCommand(siteId, { command, stream: true });
    selectedCommand.value = result;
    currentPage.value = 1;
    startCommandPoll();
    await fetchCommands(true, true);
  } catch (e: any) {
    wsDisconnect();
    selectedCommand.value = { command, output: e.message || 'Command failed', status: 'failed' };
    addToast(e.message || 'Failed to rerun command', 'error');
  } finally {
    rerunningCommandId.value = null;
  }
};

const deleteCommand = async (cmd: any) => {
  if (!cmd?.id || deletingCommandId.value !== null) return;
  const confirmed = await confirm({
    title: 'Delete Command',
    message: `Delete "${cmd.command}" from command history?`,
    confirmText: 'Delete',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!confirmed) return;
  deletingCommandId.value = cmd.id;
  try {
    await apiClient.deleteSiteCommand(siteId, cmd.id);
    if (selectedCommand.value?.id === cmd.id) {
      showModal.value = false;
      selectedCommand.value = null;
      stopCommandPoll();
      wsDisconnect();
      wsClear();
    }
    await fetchCommands(true, true);
    addToast('Command deleted', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to delete command', 'error');
  } finally {
    deletingCommandId.value = null;
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
  const requestToken = ++siteFetchToken;
  const requestSiteId = siteId;
  try {
    const result = await apiClient.getSite(requestSiteId);
    if (requestToken !== siteFetchToken || requestSiteId !== siteId) return;
    site.value = result;
  } catch (e) {}
};

onMounted(() => { fetchSite(); fetchCommands(true); });

onActivated(() => {
  fetchSite();
  fetchCommands(true);
  if (showModal.value && isSelectedCommandActive.value) {
    connectSelectedCommandLog();
    startCommandPoll();
  }
});

onDeactivated(() => {
  stopCommandPoll();
  wsDisconnect();
  wsClear();
});

onBeforeUnmount(() => {
  stopCommandPoll();
  wsDisconnect();
  wsClear();
});

watch(() => route.params.id, (newId) => {
  commandFetchToken++;
  siteFetchToken++;
  stopCommandPoll();
  wsDisconnect();
  wsClear();
  siteId = newId as string;
  currentPage.value = 1;
  showModal.value = false;
  selectedCommand.value = null;
  commands.value = [];
  totalCommands.value = 0;
  site.value = null;
  fetchSite();
  fetchCommands(true);
});
</script>
