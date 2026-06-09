<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-start mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Run new command</h2>
          <p class="text-sm text-gray-600 mt-1">
            Easily execute arbitrary commands on your server. All commands are executed from within the site's root directory. Commands will be executed as the fluxo user and may run for two minutes before timing out.
          </p>
        </div>
      </div>

      <div class="space-y-4">
        <div>
          <input v-model="commandInput" type="text"
            class="w-full font-mono text-sm border border-gray-200 rounded-lg px-4 py-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow"
            placeholder="php artisan about"
            @keyup.enter="runCommand" />
        </div>

        <div class="flex items-center gap-3">
          <button @click="runCommand" :disabled="running"
            class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
            {{ running ? 'Running...' : 'Run' }}
          </button>
          <span v-if="output !== null" class="text-sm text-gray-500">Return to run</span>
        </div>
      </div>

      <div v-if="output !== null" class="mt-4 bg-gray-900 rounded-lg p-4 font-mono text-sm text-green-400 max-h-80 overflow-y-auto whitespace-pre-wrap">{{ output }}</div>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-gray-100">
      <div class="px-6 py-4 border-b border-gray-100 flex justify-between items-center">
        <h2 class="text-lg font-semibold text-gray-900">Recent commands</h2>
        <button @click="() => fetchCommands()" class="p-2 text-gray-600 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </button>
      </div>

      <div v-if="commands.length === 0" class="px-6 py-12 text-center text-gray-400 text-sm">
        No recent commands.
      </div>

      <ul v-else class="divide-y divide-gray-100">
        <li v-for="cmd in commands" :key="cmd.id" class="px-6 py-4 hover:bg-gray-50 transition-colors cursor-pointer" @click="output = cmd.output">
          <div class="flex items-center justify-between">
            <div class="flex-1 min-w-0">
              <p class="text-sm font-mono text-gray-900 truncate">{{ cmd.command }}</p>
              <p class="text-xs text-gray-400 mt-0.5">{{ timeAgo(cmd.created_at) }}</p>
            </div>
            <span :class="cmd.status === 'success' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'"
              class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold ml-3">
              {{ cmd.status }}
            </span>
          </div>
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';

const route = useRoute();
const siteId = route.params.id as string;
const { addToast } = useToast();

const commandInput = ref('');
const running = ref(false);
const output = ref<string | null>(null);
const commands = ref<any[]>([]);

const token = () => localStorage.getItem('fluxo_jwt');

const fetchCommands = async (silent = false) => {
  try {
    const res = await fetch(`/api/v1/sites/${siteId}/commands`, {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    commands.value = await res.json() || [];
    if (!silent) addToast('Commands refreshed', 'success');
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to load commands', 'error');
  }
};

const runCommand = async () => {
  if (!commandInput.value.trim() || running.value) return;
  running.value = true;
  output.value = null;
  try {
    const res = await fetch(`/api/v1/sites/${siteId}/commands`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ command: commandInput.value.trim() })
    });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    output.value = result.output;
    commandInput.value = '';
    fetchCommands(true);
  } catch (e: any) {
    output.value = e.message || 'Command failed';
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

onMounted(() => fetchCommands(true));
</script>
