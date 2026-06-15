<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6 dark:bg-gray-900 dark:border-gray-800">
    <div class="flex justify-between items-center mb-4">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Site Activity</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Recent actions and events for this site.</p>
      </div>
      <AppButton variant="secondary" size="sm" @click="() => fetchActivity()" title="Refresh">
        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
      </AppButton>
    </div>

    <div v-if="activities.length === 0" class="text-center text-gray-500 text-sm py-12 dark:text-gray-400">
      No activity recorded yet.
    </div>

    <ul v-else class="divide-y divide-gray-200 dark:divide-gray-700">
      <li v-for="(item, idx) in activities" :key="idx" class="py-4 flex items-start gap-3">
        <div class="w-8 h-8 rounded-full flex items-center justify-center shrink-0 mt-0.5"
             :class="item.type === 'deployment' ? 'bg-green-50 text-green-600 dark:bg-green-900/30 dark:text-green-400' : 'bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400'">
          <svg v-if="item.type === 'deployment' || item.type === 'site_created' || item.type === 'site_deleted'" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
          </svg>
          <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div class="flex-1 min-w-0">
          <p class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ item.summary }}</p>
          <p class="text-xs text-gray-500 mt-0.5 dark:text-gray-400">{{ formatDate(item.created_at) }}</p>
        </div>
      </li>
    </ul>

    <div v-if="totalPages > 1" class="flex justify-between items-center mt-4 pt-3 border-t border-gray-100 dark:border-gray-800">
      <AppButton variant="secondary" size="sm" :disabled="page === 1" @click="prevPage">Previous</AppButton>
      <span class="text-sm text-gray-600 dark:text-gray-400">Page {{ page }} of {{ totalPages }}</span>
      <AppButton variant="secondary" size="sm" :disabled="page === totalPages" @click="nextPage">Next</AppButton>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';

const route = useRoute();
const siteId = route.params.id as string;

const { addToast } = useToast();
const activities = ref<any[]>([]);
const page = ref(1);
const total = ref(0);
const pageSize = 20;

const totalPages = computed(() => Math.ceil(total.value / pageSize) || 1);

const fetchActivity = async (silent = false) => {
  try {
    const offset = (page.value - 1) * pageSize;
    const data = await apiClient.getSiteActivityPaginated(siteId, pageSize, offset);
    activities.value = data.items || [];
    total.value = data.total || 0;
    if (!silent) addToast('Activity refreshed', 'success');
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to refresh activity', 'error');
  }
};

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  return new Date(dateStr).toLocaleString();
};

const prevPage = () => { if (page.value > 1) { page.value--; fetchActivity(true); } };
const nextPage = () => { if (page.value < totalPages.value) { page.value++; fetchActivity(true); } };

onMounted(() => fetchActivity(true));
</script>
