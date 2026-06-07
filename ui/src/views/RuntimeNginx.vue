<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
    <h2 class="text-lg font-semibold text-gray-900 mb-4">Nginx</h2>
    <p class="text-sm text-gray-600 mb-6">View the current Nginx web server configuration on your server.</p>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1">Binary</p>
        <p class="text-sm font-mono text-gray-900">{{ info.binary || 'Not installed' }}</p>
      </div>
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1">Version</p>
        <p class="text-sm font-mono text-gray-900">{{ info.version || 'N/A' }}</p>
      </div>
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1">Config directory</p>
        <p class="text-sm font-mono text-gray-900">{{ info.config_dir || '/etc/nginx' }}</p>
      </div>
      <div class="p-4 bg-gray-50 rounded-lg border border-gray-200">
        <p class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-1">Sites enabled</p>
        <p class="text-sm font-mono text-gray-900">{{ info.sites_enabled || '/etc/nginx/sites-enabled' }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';

const info = ref<any>({});

onMounted(async () => {
  const token = localStorage.getItem('fluxo_jwt');
  try {
    const res = await fetch('/api/v1/server/nginx/info', {
      headers: { 'Authorization': `Bearer ${token}` }
    });
    if (res.ok) info.value = await res.json();
  } catch (e) {
    console.error('Failed to fetch Nginx info:', e);
  }
});
</script>