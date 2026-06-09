<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <!-- Server Header -->
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <div class="flex items-center gap-2 mb-1">
        <PageHeader :title="metrics.hostname || 'Fluxo Server'" :subtitle="headerSubtitle" />
        <span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-900/50 shrink-0 -mt-4">
          <span class="h-1.5 w-1.5 rounded-full bg-green-500 mr-1.5 animate-pulse"></span>
          Connected
        </span>
      </div>
      <p class="text-xs text-gray-400 dark:text-gray-500 -mt-3 mb-4">{{ sites.length }} site{{ sites.length !== 1 ? 's' : '' }} &middot; {{ daemons.length }} background {{ daemons.length === 1 ? 'process' : 'processes' }} &middot; {{ crons.length }} scheduled {{ crons.length === 1 ? 'job' : 'jobs' }}</p>
      <div class="flex gap-2 justify-end">
        <AppButton variant="primary" to="/sites">Manage Sites</AppButton>
      </div>
    </div>

    <!-- Main Content Grid -->
    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
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
            <div v-for="db in databases" :key="db.id" class="py-3 flex justify-between items-center hover:bg-gray-50/50 dark:hover:bg-gray-800/50 rounded-lg px-2 -mx-2 transition-all">
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
            <div v-for="proc in daemons" :key="proc.id" class="py-3 flex justify-between items-center hover:bg-gray-50/50 dark:hover:bg-gray-800/50 rounded-lg px-2 -mx-2 transition-all">
              <div class="flex items-center gap-3">
                <div class="h-8 w-8 rounded bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 flex items-center justify-center font-bold text-sm border border-green-100 dark:border-green-900/50">
                  ⚙️
                </div>
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100 font-mono">{{ proc.command }}</h3>
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ proc.directory }}</p>
                </div>
              </div>
              <span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold"
                    :class="proc.status === 'active' ? 'bg-green-100 dark:bg-green-900/40 text-green-800 dark:text-green-300' : 'bg-gray-100 dark:bg-gray-800 text-gray-800 dark:text-gray-200'">
                {{ proc.status }}
              </span>
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
            <li v-for="c in crons.slice(0, 5)" :key="c.id" class="py-3 flex justify-between items-center">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 truncate">{{ c.expression }} &middot; {{ c.user || 'fluxo' }}</p>
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
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.port || '8080' }}</span>
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
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Host Address</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.host_address || '127.0.0.1' }}</span>
            </div>
            <div class="flex justify-between text-sm">
              <span class="text-gray-500 dark:text-gray-400">Listen Port</span>
              <span class="font-mono text-gray-900 dark:text-gray-100 font-medium">{{ metrics.port || '8080' }}</span>
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
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { apiClient } from '../api/client';
import PageHeader from '../components/PageHeader.vue';
import AppButton from '../components/AppButton.vue';

const token = () => localStorage.getItem('fluxo_jwt');

const authFetch = async (url: string) => {
  const res = await fetch(url, { headers: { 'Authorization': `Bearer ${token()}` } });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
};

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

const headerSubtitle = computed(() => {
  return `${metrics.value.host_address || '127.0.0.1'} · App server · PHP ${latestPhp.value} · ${metrics.value.os_version || 'Linux'}`;
});

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

const loadData = async () => {
  try { sites.value = await apiClient.getSites(); } catch (e) { console.error(e); }
  try { databaseEngines.value = await apiClient.getDatabaseEngines(); } catch (e) { console.error(e); }
  try { phpVersions.value = await apiClient.getPhpVersions(); } catch (e) { console.error(e); }
  try { databases.value = await apiClient.getDatabases(); } catch (e) { console.error(e); }
  try { daemons.value = await apiClient.getDaemons(); } catch (e) { console.error(e); }
  try { crons.value = await authFetch('/api/v1/crons'); } catch (e) { console.error(e); }
  try { activities.value = await authFetch('/api/v1/system/activity'); } catch (e) { console.error(e); }
  try { metrics.value = await apiClient.getMetrics(); } catch (e) { console.error(e); }
};

let intervalId: any = null;

onMounted(() => {
  loadData();
  intervalId = setInterval(() => {
    apiClient.getMetrics().then(d => metrics.value = d).catch(() => {});
  }, 5000);
});

onUnmounted(() => {
  if (intervalId) clearInterval(intervalId);
});
</script>
