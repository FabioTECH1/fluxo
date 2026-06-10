<template>
  <div class="space-y-6">
    <!-- CPU -->
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">CPU Load</h2>
      <div class="flex justify-between text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        <span>Load Averages (1m, 5m, 15m)</span>
        <span class="font-mono text-gray-900 dark:text-gray-100">{{ metrics.cpu_load || '0.00 0.00 0.00' }}</span>
      </div>
      <div class="w-full bg-gray-100 dark:bg-gray-700 rounded-full h-3">
        <div class="bg-blue-600 h-3 rounded-full transition-all duration-500" :style="{ width: cpuPercent + '%' }"></div>
      </div>
    </div>

    <!-- Memory -->
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">Memory</h2>
      <div class="flex justify-between text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        <span>Usage</span>
        <span class="font-mono text-gray-900 dark:text-gray-100">{{ metrics.mem_used }} MB / {{ metrics.mem_total }} MB ({{ memPercent }}%)</span>
      </div>
      <div class="w-full bg-gray-100 dark:bg-gray-700 rounded-full h-3">
        <div class="bg-indigo-600 h-3 rounded-full transition-all duration-500" :style="{ width: memPercent + '%' }"></div>
      </div>
    </div>

    <!-- Disk -->
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">Disk</h2>
      <div class="flex justify-between text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        <span>Usage (/)</span>
        <span class="font-mono text-gray-900 dark:text-gray-100">{{ metrics.disk_used }} / {{ metrics.disk_total }} ({{ metrics.disk_usage }})</span>
      </div>
      <div class="w-full bg-gray-100 dark:bg-gray-700 rounded-full h-3">
        <div class="bg-emerald-600 h-3 rounded-full transition-all duration-500" :style="{ width: diskPercent + '%' }"></div>
      </div>
    </div>

    <!-- System Info -->
    <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">System Info</h2>
      <div class="grid grid-cols-2 gap-4 text-sm">
        <div><span class="text-gray-500 dark:text-gray-400">Hostname</span><br><span class="font-medium text-gray-900 dark:text-gray-100">{{ metrics.hostname || 'Fluxo Server' }}</span></div>
        <div><span class="text-gray-500 dark:text-gray-400">Platform</span><br><span class="font-medium text-gray-900 dark:text-gray-100 capitalize">{{ metrics.platform || 'Linux' }}</span></div>
        <div><span class="text-gray-500 dark:text-gray-400">OS Version</span><br><span class="font-medium text-gray-900 dark:text-gray-100">{{ metrics.os_version || 'Loading...' }}</span></div>
        <div><span class="text-gray-500 dark:text-gray-400">Created</span><br><span class="font-medium text-gray-900 dark:text-gray-100">{{ metrics.os_created || 'Loading...' }}</span></div>
        <div><span class="text-gray-500 dark:text-gray-400">Daemon PID</span><br><span class="font-medium text-gray-900 dark:text-gray-100 font-mono">{{ metrics.daemon_pid || 'N/A' }}</span></div>
        <div><span class="text-gray-500 dark:text-gray-400">Daemon Port</span><br><span class="font-medium text-gray-900 dark:text-gray-100 font-mono">{{ metrics.port || '9595' }}</span></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { apiClient } from '../api/client';

const metrics = ref<any>({});

const cpuPercent = computed(() => {
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

const fetchMetrics = async () => {
  try {
    metrics.value = await apiClient.getMetrics();
  } catch (e) {
    console.error('Failed to fetch metrics:', e);
  }
};

let interval: any;

onMounted(() => {
  fetchMetrics();
  interval = setInterval(fetchMetrics, 5000);
});

onUnmounted(() => {
  if (interval) clearInterval(interval);
});
</script>