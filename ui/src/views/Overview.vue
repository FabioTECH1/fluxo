<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <!-- Server Header -->
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex flex-wrap items-center gap-3 mb-1.5">
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100 break-all">{{ metrics.hostname || 'Fluxo Server' }}</h1>
        <span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-900/50 shrink-0">
          <span class="h-1.5 w-1.5 rounded-full bg-green-500 mr-1.5 animate-pulse"></span>
          Connected
        </span>
      </div>
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-gray-500 dark:text-gray-400 mb-4">
        <span class="font-mono font-medium text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-800/50 px-2 py-0.5 rounded border border-gray-100 dark:border-gray-800/80">{{ metrics.host_address || '127.0.0.1' }}</span>
        <button 
          @click="copyIp" 
          class="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 focus:outline-none transition-all rounded hover:bg-gray-100 dark:hover:bg-gray-800 flex items-center justify-center"
          title="Copy IP Address"
        >
          <svg v-if="!copied" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
          <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
          </svg>
        </button>
        <span v-if="copied" class="text-xs text-green-600 dark:text-green-400 font-semibold transition-all">Copied!</span>
        <span v-if="!copied" class="hidden sm:inline">&middot;</span>
        <span>App server</span>
        <span class="hidden sm:inline">&middot;</span>
        <span>PHP {{ latestPhp }}</span>
        <span class="hidden sm:inline">&middot;</span>
        <span>{{ metrics.os_version || 'Linux' }}</span>
      </div>
      <p class="text-xs text-gray-400 dark:text-gray-500 mb-4">{{ sites.length }} site{{ sites.length !== 1 ? 's' : '' }} &middot; {{ daemons.length }} background {{ daemons.length === 1 ? 'process' : 'processes' }} &middot; {{ crons.length }} scheduled {{ crons.length === 1 ? 'job' : 'jobs' }}</p>
      <div class="flex gap-2 justify-end">
        <AppButton variant="primary" to="/sites">Manage Sites</AppButton>
      </div>
    </div>

    <!-- Main Content Grid -->
    <template v-if="loading">
      <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div class="lg:col-span-2 space-y-6">
          <SkeletonLoader type="card" />
          <SkeletonLoader type="card" />
          <SkeletonLoader type="card" />
        </div>
        <div class="space-y-6">
          <SkeletonLoader type="card" />
          <SkeletonLoader type="card" />
        </div>
      </div>
    </template>
    <div v-else class="grid grid-cols-1 lg:grid-cols-3 gap-6">
      <!-- Left Column: Metrics, Runtimes, Sites, Databases, Processes -->
      <div class="lg:col-span-2 space-y-6">
        <!-- Resource Usage -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">Resource Usage</h2>
          
          <div class="space-y-4">
            <!-- CPU -->
            <div>
              <div class="flex justify-between text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                <span>CPU Load (1m, 5m, 15m)</span>
                <span class="font-mono text-gray-900 dark:text-gray-100">{{ metrics.cpu_load || '0.00 0.00 0.00' }}</span>
              </div>
              <div class="w-full bg-gray-100 dark:bg-gray-800 rounded-full h-2">
                <div class="bg-blue-600 h-2 rounded-full transition-all duration-500" :style="{ width: cpuProgress + '%' }"></div>
              </div>
            </div>

            <!-- Memory -->
            <div>
              <div class="flex justify-between text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                <span>Memory Usage</span>
                <span class="font-mono text-gray-900 dark:text-gray-100">
                  {{ metrics.mem_used }} MB / {{ metrics.mem_total }} MB 
                  ({{ memPercent }}%)
                </span>
              </div>
              <div class="w-full bg-gray-100 dark:bg-gray-800 rounded-full h-2">
                <div class="bg-indigo-600 h-2 rounded-full transition-all duration-500" :style="{ width: memPercent + '%' }"></div>
              </div>
            </div>

            <!-- Disk -->
            <div>
              <div class="flex justify-between text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                <span>Disk Space (/)</span>
                <span class="font-mono text-gray-900 dark:text-gray-100">
                  {{ metrics.disk_used }} / {{ metrics.disk_total }} 
                  ({{ metrics.disk_usage }})
                </span>
              </div>
              <div class="w-full bg-gray-100 dark:bg-gray-800 rounded-full h-2">
                <div class="bg-emerald-600 h-2 rounded-full transition-all duration-500" :style="{ width: diskPercent + '%' }"></div>
              </div>
            </div>
          </div>
        </div>

        <!-- Recent Sites -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <div class="flex justify-between items-center mb-4">
            <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100">Recent Sites</h2>
            <router-link to="/sites" class="text-sm font-semibold text-blue-600 dark:text-blue-400 hover:text-blue-700">View All</router-link>
          </div>
          <div class="divide-y divide-gray-100 dark:divide-gray-800">
            <div v-for="site in sites.slice(0, 5)" :key="site.id" class="py-3 flex justify-between items-center hover:bg-gray-50/50 dark:hover:bg-gray-800/50 rounded-lg px-2 -mx-2 transition-all">
              <div class="flex items-center gap-3">
                <div class="h-8 w-8 rounded bg-gray-100 dark:bg-gray-800 text-gray-700 dark:text-gray-300 flex items-center justify-center font-bold text-sm">
                  {{ site.domain[0].toUpperCase() }}
                </div>
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ site.domain }}</h3>
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ site.app_type === 'node' ? 'Node.js app' : 'PHP ' + site.php_version }} • {{ site.path }}</p>
                </div>
              </div>
              <router-link :to="`/sites/${site.id}`" class="text-xs font-semibold text-gray-600 dark:text-gray-400 hover:text-blue-600 bg-gray-100 dark:bg-gray-800 hover:bg-blue-50 dark:hover:bg-blue-900/30 px-2.5 py-1 rounded transition-colors">
                Manage
              </router-link>
            </div>
            <div v-if="sites.length === 0" class="py-6 text-center text-sm text-gray-400 dark:text-gray-500 italic">
              No sites deployed yet.
            </div>
          </div>
        </div>

        <!-- Databases -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">Databases</h2>
          <div class="divide-y divide-gray-100 dark:divide-gray-800">
            <div v-for="db in databases.slice(0, 5)" :key="db.id" class="py-3 flex justify-between items-center hover:bg-gray-50/50 dark:hover:bg-gray-800/50 rounded-lg px-2 -mx-2 transition-all">
              <div class="flex items-center gap-3">
                <div class="h-8 w-8 rounded bg-purple-50 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 flex items-center justify-center font-bold text-sm border border-purple-100 dark:border-purple-900/50">
                  DB
                </div>
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100 font-mono">{{ db.name }}</h3>
                  <p class="text-xs text-gray-500 dark:text-gray-400 uppercase">{{ db.engine }} • User: {{ db.username }}</p>
                </div>
              </div>
            </div>
            <div v-if="databases.length === 0" class="py-6 text-center text-sm text-gray-400 dark:text-gray-500 italic">
              No databases configured yet.
            </div>
          </div>
        </div>

        <!-- Background Processes -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">Background Processes</h2>
          <div class="divide-y divide-gray-100 dark:divide-gray-800">
            <div v-for="proc in daemons.slice(0, 5)" :key="proc.id" class="py-3 flex justify-between items-center hover:bg-gray-50/50 dark:hover:bg-gray-800/50 rounded-lg px-2 -mx-2 transition-all">
              <div class="min-w-0 flex-1">
                <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ proc.name || proc.command.split(' ').slice(0, 3).join(' ') }}</h3>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ proc.command }} &middot; {{ proc.directory }}</p>
              </div>
              <div class="flex items-center gap-4 shrink-0">
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ proc.instances || 1 }} {{ (proc.instances || 1) > 1 ? 'Processes' : 'Process' }}</span>
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border"
                      :class="proc.status === 'active' || proc.status === 'running'
                        ? 'bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40'
                        : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-700'">
                  <span class="h-1.5 w-1.5 rounded-full" :class="proc.status === 'active' || proc.status === 'running' ? 'bg-green-500' : 'bg-gray-400'"></span>
                  {{ proc.status === 'active' || proc.status === 'running' ? 'Running' : 'Stopped' }}
                </span>
              </div>
            </div>
            <div v-if="daemons.length === 0" class="py-6 text-center text-sm text-gray-400 dark:text-gray-500 italic">
              No active background processes.
            </div>
          </div>
        </div>

        <!-- Scheduled Jobs -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">Scheduled Jobs</h2>
          <ul class="divide-y divide-gray-100 dark:divide-gray-800">
            <li v-for="c in crons.slice(0, 5)" :key="c.id" class="py-3 flex justify-between items-center hover:bg-gray-50/50 dark:hover:bg-gray-800/50 rounded-lg px-2 -mx-2 transition-all">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ c.user || 'fluxo' }} &middot; {{ c.command }}</p>
              </div>
              <div class="flex items-center gap-4 shrink-0">
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ frequencyLabel(c.expression) || c.expression }}</span>
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40">
                  <span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
                  Installed
                </span>
              </div>
            </li>
            <li v-if="crons.length === 0" class="py-6 text-center text-sm text-gray-400 dark:text-gray-500 italic">No scheduled jobs.</li>
          </ul>
        </div>

        <!-- Recent Activity -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">Recent Activity</h2>
          <ul class="divide-y divide-gray-100 dark:divide-gray-800">
            <li v-for="(item, idx) in activities.slice(0, 5)" :key="idx" class="py-3">
              <p class="text-sm text-gray-900 dark:text-gray-100">{{ item.summary }}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{{ formatDate(item.created_at) }}</p>
            </li>
            <li v-if="activities.length === 0" class="py-6 text-center text-sm text-gray-400 dark:text-gray-500 italic">No recent activity.</li>
          </ul>
        </div>
      </div>

      <!-- Right Column: Details & Networking -->
      <div class="space-y-6">
        <!-- Details Card -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">System Details</h2>
          <div class="space-y-3">
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Daemon PID</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.daemon_pid || 'Auto-managed' }}</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Platform</span>
              <span class="text-gray-900 dark:text-gray-100 font-medium capitalize">{{ metrics.platform || 'Linux' }}</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">OS Version</span>
              <span class="text-gray-900 dark:text-gray-100 font-medium">{{ metrics.os_version || 'Loading...' }}</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Created</span>
              <span class="text-gray-900 dark:text-gray-100 font-medium">{{ metrics.os_created || 'Loading...' }}</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Server Type</span>
              <span class="text-gray-900 dark:text-gray-100 font-medium">App Server</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Port</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.port || '9595' }}</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Primary Database</span>
              <span class="text-gray-900 dark:text-gray-100 font-medium">MySQL / MariaDB</span>
            </div>
          </div>
        </div>

        <!-- Networking Card -->
        <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
          <h2 class="text-lg font-bold text-gray-900 dark:text-gray-100 mb-4">Networking</h2>
          <div class="space-y-3">
            <div class="flex justify-between items-center text-sm">
              <span class="text-gray-500 dark:text-gray-400">Host Address</span>
              <div class="flex items-center gap-1.5">
                <span v-if="copied" class="text-xs text-green-600 dark:text-green-400 font-semibold transition-all">Copied!</span>
                <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.host_address || '127.0.0.1' }}</span>
                <button 
                  @click="copyIp" 
                  class="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 focus:outline-none transition-all rounded hover:bg-gray-100 dark:hover:bg-gray-800 flex items-center justify-center"
                  title="Copy IP Address"
                >
                  <svg v-if="!copied" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
                  <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                  </svg>
                </button>
              </div>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Listen Port</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.port || '9595' }}</span>
            </div>
            <div v-if="databaseEngines.includes('mysql') || databaseEngines.includes('mariadb')" class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Database Port (MySQL)</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">3306</span>
            </div>
            <div v-if="databaseEngines.includes('postgres')" class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">PostgreSQL Port</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">5432</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onActivated, onDeactivated, onUnmounted } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';


const loading = ref(true);

const sites = ref<any[]>([]);
const databaseEngines = ref<string[]>([]);
const databases = ref<any[]>([]);
const daemons = ref<any[]>([]);
const crons = ref<any[]>([]);
const activities = ref<any[]>([]);
const phpVersions = ref<string[]>([]);

const metrics = ref<any>({
  cpu_load: '',
  mem_total: 0,
  mem_used: 0,
  disk_total: '',
  disk_used: '',
  disk_usage: '0%'
});

const copied = ref(false);

const copyIp = () => {
  const ip = metrics.value.host_address || '127.0.0.1';
  
  const handleSuccess = () => {
    copied.value = true;
    setTimeout(() => {
      copied.value = false;
    }, 2000);
  };

  const handleFailure = (err: any) => {
    console.error('Failed to copy: ', err);
  };

  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(ip)
      .then(handleSuccess)
      .catch(handleFailure);
  } else {
    // Fallback for non-secure HTTP contexts
    try {
      const textArea = document.createElement('textarea');
      textArea.value = ip;
      textArea.style.position = 'fixed';
      textArea.style.top = '0';
      textArea.style.left = '0';
      textArea.style.width = '2em';
      textArea.style.height = '2em';
      textArea.style.padding = '0';
      textArea.style.border = 'none';
      textArea.style.outline = 'none';
      textArea.style.boxShadow = 'none';
      textArea.style.background = 'transparent';
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      const successful = document.execCommand('copy');
      document.body.removeChild(textArea);
      if (successful) {
        handleSuccess();
      } else {
        handleFailure('execCommand returned false');
      }
    } catch (err) {
      handleFailure(err);
    }
  }
};

const latestPhp = computed(() => {
  if (phpVersions.value.length === 0) return 'N/A';
  const sorted = [...phpVersions.value].sort();
  return sorted[sorted.length - 1];
});

const cpuProgress = computed(() => {
  if (!metrics.value.cpu_load) return 0;
  const parts = metrics.value.cpu_load.split(' ');
  const first = parseFloat(parts[0]);
  if (isNaN(first)) return 0;
  return Math.min(Math.round((first / 4) * 100), 100);
});

const memPercent = computed(() => {
  if (!metrics.value.mem_total) return 0;
  return Math.round((metrics.value.mem_used / metrics.value.mem_total) * 100);
});

const diskPercent = computed(() => {
  if (!metrics.value.disk_usage) return 0;
  const val = parseFloat(metrics.value.disk_usage.replace('%', ''));
  return isNaN(val) ? 0 : val;
});

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  return new Date(dateStr).toLocaleString();
};

const frequencyLabel = (expr: string) => {
  const map: Record<string, string> = {
    '* * * * *': 'Every minute', '*/5 * * * *': 'Every 5 min',
    '0 * * * *': 'Hourly', '0 0 * * *': 'Daily',
    '0 0 * * 0': 'Weekly', '0 0 1 * *': 'Monthly',
  };
  return map[expr] || '';
};

const loadData = async () => {
  const [
    sitesResult, enginesResult, phpResult, dbsResult, daemonsResult,
    cronsResult, activityResult, metricsResult
  ] = await Promise.allSettled([
    apiClient.getSites(),
    apiClient.getDatabaseEngines(),
    apiClient.getPhpVersions(),
    apiClient.getDatabases(),
    apiClient.getDaemons(),
    apiClient.getCrons(),
    apiClient.getSystemActivity(5),
    apiClient.getMetrics(),
  ]);

  if (sitesResult.status === 'fulfilled') sites.value = sitesResult.value;
  if (enginesResult.status === 'fulfilled') databaseEngines.value = enginesResult.value;
  if (phpResult.status === 'fulfilled') phpVersions.value = phpResult.value;
  if (dbsResult.status === 'fulfilled') databases.value = dbsResult.value;
  if (daemonsResult.status === 'fulfilled') daemons.value = daemonsResult.value;
  if (cronsResult.status === 'fulfilled') crons.value = cronsResult.value;
  if (activityResult.status === 'fulfilled') activities.value = activityResult.value.items || activityResult.value || [];
  if (metricsResult.status === 'fulfilled') metrics.value = metricsResult.value;
  loading.value = false;
};

let intervalId: any = null;

const startMetricsPolling = () => {
  if (intervalId) return;
  intervalId = setInterval(() => {
    apiClient.get('/api/v1/system/metrics', { bypassCache: true }).then(d => metrics.value = d).catch(() => {});
  }, 5000);
};

const stopMetricsPolling = () => {
  if (!intervalId) return;
  clearInterval(intervalId);
  intervalId = null;
};

onMounted(() => {
  loadData();
  startMetricsPolling();
});

onActivated(() => {
  loadData();
  startMetricsPolling();
});

onDeactivated(stopMetricsPolling);
onUnmounted(stopMetricsPolling);
</script>
