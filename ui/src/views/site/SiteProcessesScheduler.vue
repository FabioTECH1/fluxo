<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Scheduled Jobs</h2>
          <p class="text-sm text-gray-600 mt-1">
            Manage recurring tasks that need to run on your server.
          </p>
        </div>
        <div class="flex gap-3">
          <button @click="showAddModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add scheduled job</button>
          <button @click="() => fetchCrons()" class="p-2 text-gray-600 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors" title="Refresh">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          </button>
        </div>
      </div>

      <div v-if="crons.length === 0" class="text-center text-gray-500 text-sm py-12">
        No scheduled jobs configured.
      </div>

      <div class="grid grid-cols-1 gap-4">
        <div v-for="c in crons" :key="c.id" class="border border-gray-200 rounded-lg p-5 hover:border-gray-300 transition-colors">
          <div class="flex justify-between items-start">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-3 mb-2">
                <h3 class="text-sm font-semibold text-gray-900">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</h3>
                <span class="px-2 py-0.5 text-xs font-semibold rounded-full bg-green-100 text-green-800">Installed</span>
              </div>
              <p class="text-xs text-gray-500 mb-1">{{ c.user || 'fluxo' }} &middot; {{ c.command }}</p>
              <div class="flex items-center gap-3">
                <span class="text-xs font-mono text-blue-600 bg-blue-50 px-2 py-0.5 rounded">{{ c.expression }}</span>
                <span v-if="frequencyLabel(c.expression)" class="text-xs text-gray-500">{{ frequencyLabel(c.expression) }}</span>
              </div>
            </div>
            <div class="flex items-center gap-2 ml-4 shrink-0">
              <div class="relative">
                <button @click="toggleMenu(c.id)" class="px-3 py-1.5 text-xs text-gray-600 bg-gray-50 border border-gray-200 rounded-lg hover:bg-gray-100 font-medium transition-colors">
                  Open menu
                </button>
                <div v-if="openMenu === c.id" class="absolute right-0 mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10">
                  <button @click="deleteCron(c.id); openMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                    Delete
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <AddCronModal v-if="showAddModal" :site-id="siteId" @close="showAddModal = false" @created="onCreated" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { useRoute } from 'vue-router';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import AddCronModal from '../AddCronModal.vue';

const route = useRoute();
const siteId = route.params.id as string;

const { confirm } = useConfirm();
const { addToast } = useToast();

const crons = ref<any[]>([]);
const openMenu = ref<number | null>(null);
const showAddModal = ref(false);

const token = () => localStorage.getItem('fluxo_jwt');

const fetchCrons = async (silent = false) => {
  try {
    const res = await fetch(`/api/v1/sites/${siteId}/crons`, {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    crons.value = await res.json() || [];
    if (!silent) addToast('Crons refreshed', 'success');
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to refresh crons', 'error');
  }
};

const deleteCron = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete Scheduled Job',
    message: 'Are you sure you want to delete this scheduled job?',
    confirmText: 'Delete',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await fetch(`/api/v1/sites/${siteId}/crons/${id}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    addToast('Scheduled job deleted', 'success');
    fetchCrons(true);
  } catch (e: any) {
    addToast(e.message || 'Failed to delete', 'error');
  }
};

const onCreated = () => {
  showAddModal.value = false;
  fetchCrons(true);
};

const frequencyLabel = (expr: string) => {
  const map: Record<string, string> = {
    '* * * * *': 'Every minute',
    '*/5 * * * *': 'Every 5 minutes',
    '0 * * * *': 'Hourly',
    '0 0 * * *': 'Daily',
    '0 0 * * 0': 'Weekly',
    '0 0 1 * *': 'Monthly',
  };
  return map[expr] || '';
};

const toggleMenu = (id: number) => {
  openMenu.value = openMenu.value === id ? null : id;
};

const handleClickOutside = (e: MouseEvent) => {
  const target = e.target as HTMLElement;
  if (!target.closest('.relative')) openMenu.value = null;
};

onMounted(() => {
  fetchCrons(true);
  window.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  window.removeEventListener('click', handleClickOutside);
});
</script>
