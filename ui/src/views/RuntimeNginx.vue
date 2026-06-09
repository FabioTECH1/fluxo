<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6 dark:bg-gray-900 dark:border-gray-800">
    <div class="flex justify-between items-start mb-6">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Nginx</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">View the current Nginx web server configuration on your server.</p>
      </div>
      <button @click="restartNginx" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap" :disabled="restarting">
        {{ restarting ? 'Restarting...' : 'Restart Nginx' }}
      </button>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200 dark:bg-gray-800 dark:border-gray-700">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1 dark:text-gray-400">Binary</p>
        <p class="text-sm font-mono text-gray-900 dark:text-gray-100">{{ info.binary || 'Not installed' }}</p>
      </div>
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200 dark:bg-gray-800 dark:border-gray-700">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1 dark:text-gray-400">Version</p>
        <p class="text-sm font-mono text-gray-900 dark:text-gray-100">{{ info.version || 'N/A' }}</p>
      </div>
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200 dark:bg-gray-800 dark:border-gray-700">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1 dark:text-gray-400">Config directory</p>
        <p class="text-sm font-mono text-gray-900 dark:text-gray-100">{{ info.config_dir || '/etc/nginx' }}</p>
      </div>
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200 dark:bg-gray-800 dark:border-gray-700">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1 dark:text-gray-400">Sites enabled</p>
        <p class="text-sm font-mono text-gray-900 dark:text-gray-100">{{ info.sites_enabled || '/etc/nginx/sites-enabled' }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';

const { addToast } = useToast();
const { confirm } = useConfirm();

const info = ref<any>({});
const restarting = ref(false);

const token = () => localStorage.getItem('fluxo_jwt');

const restartNginx = async () => {
  const ok = await confirm({ title: 'Restart Nginx', message: 'Restart Nginx? This will briefly reload the web server. Active connections may be interrupted.', confirmText: 'Restart', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  restarting.value = true;
  try {
    const res = await fetch('/api/v1/server/nginx/restart', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Nginx restarted successfully', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to restart Nginx', 'error');
  } finally {
    restarting.value = false;
  }
};

onMounted(async () => {
  try {
    const res = await fetch('/api/v1/server/nginx/info', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (res.ok) info.value = await res.json();
  } catch (e) {
    console.error('Failed to fetch Nginx info:', e);
  }
});
</script>