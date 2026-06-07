<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
    <div class="flex justify-between items-center mb-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900">Recent Activity</h2>
        <p class="text-sm text-gray-600 mt-1">Latest actions and events on this server.</p>
      </div>
      <button @click="() => fetchActivity()" class="p-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors" title="Refresh">
        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
      </button>
    </div>

    <div v-if="activities.length === 0" class="text-center text-gray-500 text-sm py-12">
      No activity recorded yet.
    </div>

    <ul v-else class="divide-y divide-gray-200">
      <li v-for="(item, idx) in activities" :key="idx" class="py-4 flex items-start gap-3">
        <div class="w-8 h-8 rounded-full flex items-center justify-center shrink-0 mt-0.5"
             :class="item.type === 'deployment' ? 'bg-green-50 text-green-600' : 'bg-blue-50 text-blue-600'">
          <svg v-if="item.type === 'deployment'" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
          </svg>
          <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
          </svg>
        </div>
        <div class="flex-1 min-w-0">
          <p class="text-sm font-medium text-gray-900">{{ item.summary }}</p>
          <p class="text-xs text-gray-500 mt-0.5">{{ formatDate(item.created_at) }}</p>
        </div>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';

const activities = ref<any[]>([]);

import { useToast } from '../composables/useToast';

const { addToast } = useToast();

const fetchActivity = async (silent = false) => {
  const token = localStorage.getItem('fluxo_jwt');
  try {
    const res = await fetch('/api/v1/system/activity', {
      headers: { 'Authorization': `Bearer ${token}` }
    });
    if (!res.ok) throw new Error(await res.text());
    activities.value = await res.json();
    if (!silent) addToast('Activity refreshed', 'success');
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to refresh activity', 'error');
  }
};

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  return d.toLocaleString();
};

onMounted(() => fetchActivity(true));
</script>