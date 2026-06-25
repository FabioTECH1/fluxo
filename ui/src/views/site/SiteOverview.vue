<template>
  <div class="flex flex-col lg:flex-row gap-6">
    <!-- Left Column -->
    <div class="flex-1 space-y-6">
      <!-- Deployments -->
      <SkeletonLoader v-if="loading" type="table" :rows="3" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Deployments</h2>
        </div>
        <div v-if="deployments.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No deployments yet.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="dep in deployments.slice(0, 5)" :key="dep.id" class="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
            <div class="flex items-center gap-3 min-w-0 flex-1">
              <span :class="statusBadge(dep.status)">{{ dep.status }}</span>
              <span v-if="dep.trigger_source === 'github_webhook'" class="flex items-center gap-1 text-[10px] uppercase font-bold text-purple-600 bg-purple-100 dark:bg-purple-900/30 dark:text-purple-300 px-1.5 py-0.5 rounded shrink-0" title="Auto-deployed via GitHub Push">
                <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clip-rule="evenodd" /></svg>
                Auto
              </span>
              <div v-if="dep.branch" class="flex items-center gap-1 text-xs text-gray-500 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded shrink-0">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
                <span>{{ dep.branch }}</span>
              </div>
              <span v-if="dep.commit_hash" class="font-mono text-xs font-medium text-blue-600 dark:text-blue-400 shrink-0">{{ dep.commit_hash.slice(0, 7) }}</span>
              <span v-else class="font-mono text-xs text-gray-400 dark:text-gray-500 shrink-0">No commit</span>
            </div>
            <div class="mt-2 flex items-baseline gap-2 min-w-0">
              <span class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ dep.commit_message || 'Manual Deployment' }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400 shrink-0">&middot; {{ timeAgo(dep.created_at) }}</span>
            </div>
          </li>
        </ul>
      </div>
 
      <!-- Background Processes -->
      <SkeletonLoader v-if="loading" type="table" :rows="2" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Background Processes</h2>
          <button @click="showAddDaemon = true" class="bg-blue-600 text-white h-7 w-7 rounded-lg shadow-sm hover:bg-blue-700 flex items-center justify-center font-bold transition-colors"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4" /></svg></button>
        </div>
        <div v-if="daemons.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No background processes.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="d in daemons.slice(0, 5)" :key="d.id" class="px-6 py-4 hover:bg-gray-50/50 dark:hover:bg-gray-800/50 transition-all">
            <div class="flex items-center justify-between">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ d.name || d.command.split(' ').slice(0, 2).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ d.command }} &middot; {{ d.directory || site?.path || '' }}</p>
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
      <SkeletonLoader v-if="loading" type="table" :rows="2" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Scheduled Jobs</h2>
          <button @click="showAddCron = true" class="bg-blue-600 text-white h-7 w-7 rounded-lg shadow-sm hover:bg-blue-700 flex items-center justify-center font-bold transition-colors"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4" /></svg></button>
        </div>
        <div v-if="crons.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No scheduled jobs.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="c in crons.slice(0, 5)" :key="c.id" class="px-6 py-4 hover:bg-gray-50/50 dark:hover:bg-gray-800/50 transition-all">
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
      <SkeletonLoader v-if="loading" type="table" :rows="3" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Activity</h2>
        </div>
        <div v-if="activity.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No recent activity.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="(a, i) in activity.slice(0, 5)" :key="i" class="px-6 py-3">
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
    <div v-if="loading" class="w-full lg:w-72 shrink-0 space-y-4">
      <SkeletonLoader type="card" />
    </div>
    <div v-else-if="site" class="w-full lg:w-72 shrink-0 space-y-4">
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">Details</h3>
        <div class="space-y-2.5">
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Server ID</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ id }}</p>
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
 
      <div v-if="site.app_type === 'laravel'" class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5 space-y-3">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Laravel Features</h3>
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Scheduler</span>
            </div>
            <button @click="toggleScheduler" type="button"
              :class="schedulerEnabled ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
              class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0">
              <span :class="schedulerEnabled ? 'translate-x-6' : 'translate-x-1'"
                class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
            </button>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Nightwatch</span>
            <button @click="toggleNightwatch" type="button" :disabled="nightwatchToggling"
              :class="nightwatchEnabled ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
              class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0 disabled:opacity-50">
              <span :class="nightwatchEnabled ? 'translate-x-6' : 'translate-x-1'"
                class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
            </button>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Maintenance Mode</span>
            <button @click="toggleMaintenance" type="button" :disabled="maintenanceToggling"
              :class="siteUp ? 'bg-gray-200 dark:bg-gray-700' : 'bg-blue-600'"
              class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0 disabled:opacity-50">
              <span :class="siteUp ? 'translate-x-1' : 'translate-x-6'"
                class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
            </button>
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
            <input v-model="newDaemon.command" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm" placeholder="artisan queue:work">
          </div>
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Directory</label>
            <input v-model="newDaemon.directory" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm" :placeholder="site?.path || ''">
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
            <input v-model="newCron.command" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm" placeholder="artisan schedule:run">
          </div>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showAddCron = false" class="px-4 py-2 text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-sm hover:bg-gray-50 dark:hover:bg-gray-800 font-semibold text-sm transition-colors">Cancel</button>
            <button @click="addCron" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors">Add Job</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Nightwatch Modal -->
    <div v-if="showNightwatchModal" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/40" @click="showNightwatchModal = false"></div>
      <div class="relative bg-white dark:bg-gray-900 rounded-xl shadow-2xl w-full max-w-md mx-4 overflow-hidden">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 bg-gray-50 dark:bg-gray-800 flex justify-between items-center">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Enable Nightwatch</h3>
          <button @click="showNightwatchModal = false" class="text-gray-400 dark:text-gray-500 hover:text-gray-600">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Nightwatch Token</label>
            <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">Enter your Laravel Nightwatch ingestion token. The port will be assigned automatically.</p>
            <input v-model="nightwatchToken" type="text" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm font-mono" placeholder="nw_...">
          </div>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showNightwatchModal = false" class="px-4 py-2 text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-sm hover:bg-gray-50 dark:hover:bg-gray-800 font-semibold text-sm transition-colors">Cancel</button>
            <button @click="enableNightwatch" :disabled="!nightwatchToken" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">Enable</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, onActivated, onDeactivated, nextTick, watch, inject } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import SkeletonLoader from '../../components/SkeletonLoader.vue';

const route = useRoute();
let id = route.params.id as string;

const { addToast } = useToast();
const refreshStatuses = inject<() => void>('refreshStatuses', () => {});


const site = ref<any>(null);
const deployments = ref<any[]>([]);
const daemons = ref<any[]>([]);
const crons = ref<any[]>([]);
const activity = ref<any[]>([]);
const logs = ref<string[]>([]);
const terminalBox = ref<HTMLElement | null>(null);

const loading = ref(true);

const showAddDaemon = ref(false);
const newDaemon = ref({ command: '', directory: '' });

const showAddCron = ref(false);
const newCron = ref({ expression: '* * * * *', command: '' });

const showNightwatchModal = ref(false);
const nightwatchToken = ref('');
const nightwatchToggling = ref(false);

const siteUp = ref(true);
const maintenanceToggling = ref(false);

const schedulerEnabled = ref(false);
const nightwatchEnabled = ref(false);

const fetchFeatures = async () => {
  try {
    const data = await apiClient.getSiteFeatures(id);
    schedulerEnabled.value = data.scheduler_enabled;
    nightwatchEnabled.value = data.nightwatch_enabled;
    siteUp.value = !data.in_maintenance;
  } catch (e) {}
};

let ws: WebSocket | null = null;

const fetchSite = async () => {
  try {
    site.value = await apiClient.getSite(id);
  } catch (e) {}
};

const fetchDeployments = async () => {
  try {
    const data = await apiClient.getSiteDeployments(id, 1, true);
    deployments.value = data.data || [];
  } catch (e) {}
};

const fetchDaemons = async () => {
  try {
    daemons.value = await apiClient.getSiteDaemons(id) || [];
  } catch (e) {}
};

const fetchCrons = async () => {
  try {
    crons.value = await apiClient.getSiteCrons(id) || [];
  } catch (e) {}
};

const fetchActivity = async () => {
  try {
    const data = await apiClient.getSiteActivity(id, 5);
    activity.value = data.items || [];
  } catch (e) {}
};

const addDaemon = async () => {
  if (!newDaemon.value.command) return;
  try {
    await apiClient.createSiteDaemon(id, newDaemon.value);
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
    await apiClient.createSiteCron(id, newCron.value);
    addToast('Scheduled job added', 'success');
    newCron.value = { expression: '* * * * *', command: '' };
    showAddCron.value = false;
    fetchCrons();
  } catch (e: any) {
    addToast(e.message || 'Failed to add job', 'error');
  }
};

const toggleScheduler = async () => {
  try {
    await apiClient.toggleSiteScheduler(id, !schedulerEnabled.value);
    fetchFeatures();
    fetchCrons();
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const toggleNightwatch = async () => {
  if (nightwatchEnabled.value) {
    try {
      await apiClient.toggleSiteNightwatch(id, false);
      fetchFeatures();
      fetchDaemons();
    } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
  } else {
    showNightwatchModal.value = true;
  }
};

const enableNightwatch = async () => {
  const token = nightwatchToken.value.trim();
  if (!token) return;
  nightwatchToggling.value = true;
  try {
    await apiClient.toggleSiteNightwatch(id, true, token);
    addToast('Nightwatch enabled', 'success');
    showNightwatchModal.value = false;
    nightwatchToken.value = '';
    fetchFeatures();
    fetchDaemons();
  } catch (e: any) {
    addToast(e.message || 'Failed to enable Nightwatch', 'error');
  } finally {
    nightwatchToggling.value = false;
  }
};

const toggleMaintenance = async () => {
  maintenanceToggling.value = true;
  try {
    await apiClient.toggleSiteMaintenance(id, siteUp.value);
    siteUp.value = !siteUp.value;
    refreshStatuses();
    addToast(`Site ${!siteUp.value ? 'put into maintenance mode' : 'brought back online'}`, 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed', 'error');
  } finally {
    maintenanceToggling.value = false;
  }
};

const connectWS = () => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws?site_id=${id}&token=${localStorage.getItem('fluxo_jwt') || ''}`);
  let buf: string[] = [];
  let flushTimer: number | null = null;
  ws.onmessage = (event) => {
    buf.push(event.data);
    if (!flushTimer) {
      flushTimer = window.setTimeout(() => {
        logs.value.push(...buf);
        buf = [];
        flushTimer = null;
        nextTick(() => {
          terminalBox.value?.scrollTo({ top: terminalBox.value.scrollHeight });
        });
      }, 100);
    }
  };
};

const statusBadge = (status: string) => {
  const base = 'inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold';
  if (status === 'success') return `${base} bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-900/50`;
  if (status === 'failed') return `${base} bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-900/50`;
  if (status === 'running') return `${base} bg-yellow-50 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 border border-yellow-200 dark:border-yellow-900/50`;
  if (status === 'pending') return `${base} bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 border border-indigo-200 dark:border-indigo-900/50`;
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

const fetchAllData = async () => {
  loading.value = true;
  try {
    await Promise.allSettled([
      fetchSite(),
      fetchFeatures(),
      fetchDeployments(),
      fetchDaemons(),
      fetchCrons(),
      fetchActivity()
    ]);
  } finally {
    loading.value = false;
  }
};

const silentRefresh = async () => {
  await Promise.allSettled([
    fetchSite(),
    fetchFeatures(),
    fetchDeployments(),
    fetchDaemons(),
    fetchCrons(),
    fetchActivity()
  ]);
};

onMounted(() => {
  fetchAllData();
  connectWS();
});

onActivated(() => {
  silentRefresh();
  connectWS();
});

onDeactivated(() => {
  ws?.close();
  ws = null;
});

onUnmounted(() => {
  ws?.close();
});

watch(() => route.params.id, (newId) => {
  id = newId as string;
  ws?.close();
  ws = null;
  fetchAllData();
  connectWS();
});
</script>
