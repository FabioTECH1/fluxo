<template>
  <div v-if="site" class="flex gap-6">
    <!-- Left Column -->
    <div class="flex-1 space-y-6">
      <!-- Deployments -->
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Deployments</h2>
        </div>
        <div v-if="deployments.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No deployments yet.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="dep in deployments" :key="dep.id" class="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
            <div class="flex items-start justify-between">
              <div class="flex-1">
                <div class="flex items-center gap-2">
                  <span v-if="dep.commit_hash" class="font-mono text-sm font-medium text-blue-600 dark:text-blue-400">{{ dep.commit_hash.slice(0, 7) }}</span>
                  <span v-else class="font-mono text-sm font-medium text-gray-400 dark:text-gray-500">—</span>
                  <span :class="statusBadge(dep.status)">{{ dep.status }}</span>
                </div>
                <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">Deployed {{ timeAgo(dep.created_at) }}</p>
              </div>
            </div>
          </li>
        </ul>
      </div>

      <!-- Background Processes -->
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Background Processes</h2>
          <button @click="showAddDaemon = true" class="bg-blue-600 text-white h-7 w-7 rounded-lg shadow-sm hover:bg-blue-700 flex items-center justify-center font-bold transition-colors"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4" /></svg></button>
        </div>
        <div v-if="daemons.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No background processes.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="d in daemons" :key="d.id" class="px-6 py-4 hover:bg-gray-50/50 dark:hover:bg-gray-800/50 transition-all">
            <div class="flex items-center justify-between">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ d.name || d.command.split(' ').slice(0, 2).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ d.command }} &middot; {{ d.directory || site.path }}</p>
              </div>
              <div class="flex items-center gap-4 shrink-0">
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ d.instances || 1 }} {{ (d.instances || 1) > 1 ? 'Processes' : 'Process' }}</span>
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border"
                      :class="d.status === 'active' || d.status === 'running' ? 'bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40' : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-700'">
                  <span class="h-1.5 w-1.5 rounded-full" :class="d.status === 'active' || d.status === 'running' ? 'bg-green-500' : 'bg-gray-400'"></span>
                  {{ d.status === 'active' || d.status === 'running' ? 'Running' : 'Stopped' }}
                </span>
              </div>
            </div>
          </li>
        </ul>
      </div>

      <!-- Scheduled Jobs -->
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Scheduled Jobs</h2>
          <button @click="showAddCron = true" class="bg-blue-600 text-white h-7 w-7 rounded-lg shadow-sm hover:bg-blue-700 flex items-center justify-center font-bold transition-colors"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4" /></svg></button>
        </div>
        <div v-if="crons.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No scheduled jobs.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="c in crons" :key="c.id" class="px-6 py-4 hover:bg-gray-50/50 dark:hover:bg-gray-800/50 transition-all">
            <div class="flex items-center justify-between">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ c.user || 'fluxo' }} &middot; {{ c.command }}</p>
              </div>
              <div class="flex items-center gap-4 shrink-0">
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ frequencyLabel(c.expression) || c.expression }}</span>
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40">
                  <span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
                  Installed
                </span>
              </div>
            </div>
          </li>
        </ul>
      </div>

      <!-- Activity -->
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Activity</h2>
        </div>
        <div v-if="activity.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No recent activity.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="(a, i) in activity.slice(0, 10)" :key="i" class="px-6 py-3">
            <div class="flex items-center gap-3">
              <span class="h-7 w-7 rounded-full bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 flex items-center justify-center text-xs font-bold flex-shrink-0">
                {{ a.type === 'deployment' ? 'D' : 'S' }}
              </span>
              <div>
                <p class="text-sm text-gray-700 dark:text-gray-300">{{ a.summary }}</p>
                <p class="text-xs text-gray-400 dark:text-gray-500">{{ timeAgo(a.created_at) }}</p>
              </div>
            </div>
          </li>
        </ul>
      </div>

      <!-- Terminal -->
      <div v-if="logs.length > 0" ref="terminalBox" class="bg-gray-900 rounded-lg shadow-sm p-4 text-green-400 font-mono text-sm h-72 overflow-y-auto">
        <div v-for="(line, idx) in logs" :key="idx" class="whitespace-pre-wrap">{{ line }}</div>
      </div>
    </div>

    <!-- Sidebar -->
    <div class="w-72 flex-shrink-0 space-y-4">
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">Details</h3>
        <div class="space-y-2.5">
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Server ID</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">1000001</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Site ID</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ site.id }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Site User</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">fluxo</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Framework</p>
            <p class="text-sm text-gray-700 dark:text-gray-300 capitalize">{{ site.app_type || 'php' }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">PHP</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ site.php_version || '8.4' }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Public IP</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">—</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Created</p>
            <p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDate(site.created_at) }}</p>
          </div>
        </div>
      </div>

      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5 space-y-3">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Features</h3>
        <div class="space-y-2">
          <div class="flex items-center justify-between text-sm">
            <span class="text-gray-600 dark:text-gray-400">Laravel Scheduler</span>
            <span v-if="hasArtisan" class="h-2 w-2 rounded-full bg-green-500"></span>
            <span v-else class="h-2 w-2 rounded-full bg-gray-300"></span>
          </div>
          <div class="flex items-center justify-between text-sm">
            <span class="text-gray-600 dark:text-gray-400">Horizon</span>
            <span class="h-2 w-2 rounded-full bg-gray-300"></span>
          </div>
          <div class="flex items-center justify-between text-sm">
            <span class="text-gray-600 dark:text-gray-400">Octane</span>
            <span class="h-2 w-2 rounded-full bg-gray-300"></span>
          </div>
          <div class="flex items-center justify-between text-sm">
            <span class="text-gray-600 dark:text-gray-400">Maintenance mode</span>
            <span class="h-2 w-2 rounded-full bg-gray-300"></span>
          </div>
        </div>
      </div>

      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">Environment</h3>
        <div class="space-y-2">
          <button @click="$router.push(`/sites/${site.id}/settings`)" class="w-full text-left text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 font-medium">Edit .env file</button>
        </div>
      </div>
    </div>

    <!-- Add Daemon Modal -->
    <div v-if="showAddDaemon" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/40" @click="showAddDaemon = false"></div>
      <div class="relative bg-white dark:bg-gray-900 rounded-xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 bg-gray-50 dark:bg-gray-800 flex justify-between items-center">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Add Background Process</h3>
          <button @click="showAddDaemon = false" class="text-gray-400 dark:text-gray-500 hover:text-gray-600">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Command</label>
            <input v-model="newDaemon.command" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm" placeholder="php artisan queue:work">
          </div>
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Directory</label>
            <input v-model="newDaemon.directory" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm" :placeholder="site.path">
          </div>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showAddDaemon = false" class="px-4 py-2 text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-sm hover:bg-gray-50 dark:hover:bg-gray-800 font-semibold text-sm transition-colors">Cancel</button>
            <button @click="addDaemon" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors">Add Process</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Add Cron Modal -->
    <div v-if="showAddCron" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/40" @click="showAddCron = false"></div>
      <div class="relative bg-white dark:bg-gray-900 rounded-xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 bg-gray-50 dark:bg-gray-800 flex justify-between items-center">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Add Scheduled Job</h3>
          <button @click="showAddCron = false" class="text-gray-400 dark:text-gray-500 hover:text-gray-600">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Schedule</label>
            <input v-model="newCron.expression" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm font-mono" placeholder="* * * * *">
          </div>
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Command</label>
            <input v-model="newCron.command" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm" placeholder="php artisan schedule:run">
          </div>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showAddCron = false" class="px-4 py-2 text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-sm hover:bg-gray-50 dark:hover:bg-gray-800 font-semibold text-sm transition-colors">Cancel</button>
            <button @click="addCron" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors">Add Job</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue';
import { useRoute } from 'vue-router';
import { router } from '../../router';
import { useToast } from '../../composables/useToast';

const route = useRoute();
const id = route.params.id as string;

const { addToast } = useToast();

const site = ref<any>(null);
const deployments = ref<any[]>([]);
const daemons = ref<any[]>([]);
const crons = ref<any[]>([]);
const activity = ref<any[]>([]);
const logs = ref<string[]>([]);
const terminalBox = ref<HTMLElement | null>(null);

const showAddDaemon = ref(false);
const newDaemon = ref({ command: '', directory: '' });

const showAddCron = ref(false);
const newCron = ref({ expression: '* * * * *', command: '' });

let ws: WebSocket | null = null;

const hasArtisan = computed(() => {
  return site.value?.app_type === 'laravel';
});

const authedFetch = async (url: string, init?: RequestInit) => {
  const token = localStorage.getItem('fluxo_jwt');
  const headers: Record<string, string> = {};
  if (init?.headers) {
    const h = init.headers as Record<string, string>;
    Object.assign(headers, h);
  }
  if (!headers['Content-Type'] && !(init?.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
  }
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await window.fetch(url, { ...init, headers });
  if (res.status === 401) {
    localStorage.removeItem('fluxo_jwt');
    router.push('/login');
    throw new Error('Unauthorized');
  }
  return res;
};

const fetchSite = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}`);
    site.value = await res.json();
  } catch (e) {}
};

const fetchDeployments = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/deployments`);
    deployments.value = await res.json();
  } catch (e) {}
};

const fetchDaemons = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/daemons`);
    daemons.value = await res.json() || [];
  } catch (e) {}
};

const fetchCrons = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/crons`);
    crons.value = await res.json() || [];
  } catch (e) {}
};

const fetchActivity = async () => {
  try {
    const res = await authedFetch('/api/v1/system/activity');
    activity.value = await res.json() || [];
  } catch (e) {}
};

const addDaemon = async () => {
  if (!newDaemon.value.command) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/daemons`, {
      method: 'POST',
      body: JSON.stringify(newDaemon.value)
    });
    addToast('Process added', 'success');
    newDaemon.value = { command: '', directory: '' };
    showAddDaemon.value = false;
    fetchDaemons();
  } catch (e: any) {
    addToast(e.message || 'Failed to add process', 'error');
  }
};

const addCron = async () => {
  if (!newCron.value.expression || !newCron.value.command) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/crons`, {
      method: 'POST',
      body: JSON.stringify(newCron.value)
    });
    addToast('Scheduled job added', 'success');
    newCron.value = { expression: '* * * * *', command: '' };
    showAddCron.value = false;
    fetchCrons();
  } catch (e: any) {
    addToast(e.message || 'Failed to add job', 'error');
  }
};

const connectWS = () => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws`);
  ws.onmessage = (event) => {
    logs.value.push(event.data);
    nextTick(() => {
      terminalBox.value?.scrollTo({ top: terminalBox.value.scrollHeight });
    });
  };
};

  const statusBadge = (status: string) => {
    const base = 'inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium';
    if (status === 'success') return `${base} bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-900/50`;
    if (status === 'failed') return `${base} bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-900/50`;
    if (status === 'running') return `${base} bg-yellow-50 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 border border-yellow-200 dark:border-yellow-900/50`;
    return `${base} bg-gray-50 dark:bg-gray-800 text-gray-500 dark:text-gray-400 border border-gray-200 dark:border-gray-700`;
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

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  return new Date(dateStr).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
};

const frequencyLabel = (expr: string) => {
  const map: Record<string, string> = {
    '* * * * *': 'Every minute', '*/5 * * * *': 'Every 5 min',
    '0 * * * *': 'Hourly', '0 0 * * *': 'Daily',
    '0 0 * * 0': 'Weekly', '0 0 1 * *': 'Monthly',
  };
  return map[expr] || '';
};

onMounted(() => {
  fetchSite();
  fetchDeployments();
  fetchDaemons();
  fetchCrons();
  fetchActivity();
  connectWS();
});

onUnmounted(() => {
  ws?.close();
});
</script>
