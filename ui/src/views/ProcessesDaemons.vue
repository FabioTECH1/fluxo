<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Background Processes</h2>
          <p class="text-sm text-gray-600 mt-1">
            Background processes are managed using Supervisor, which monitors your processes and automatically restarts them if they crash or stop unexpectedly.
            <a href="https://supervisord.org" class="text-blue-600 hover:underline">Learn more</a>
          </p>
        </div>
        <div class="flex gap-3">
          <button @click="showAddModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add background process</button>
          <button @click="() => fetchDaemons()" class="p-2 text-gray-600 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors" title="Refresh">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          </button>
        </div>
      </div>

      <div v-if="daemons.length === 0" class="text-center text-gray-500 text-sm py-12">
        No background processes configured.
      </div>

      <div class="grid grid-cols-1 gap-4">
        <div v-for="d in daemons" :key="d.id" class="border border-gray-200 rounded-lg p-5 hover:border-gray-300 transition-colors">
          <div class="flex justify-between items-start">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-3 mb-2">
                <h3 class="text-sm font-semibold text-gray-900">{{ d.name || d.command.split(' ').slice(0, 2).join(' ') }}</h3>
                <span class="px-2 py-0.5 text-xs font-semibold rounded-full"
                  :class="d.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'">
                  {{ d.status === 'active' ? 'Running' : d.status }}
                </span>
              </div>
              <p class="text-sm font-mono text-gray-600 truncate">{{ d.command }}</p>
              <p class="text-xs text-gray-400 mt-1">{{ d.directory }}</p>
              <div class="flex items-center gap-3 mt-1">
                <span class="text-xs text-gray-500">{{ d.user || 'www-data' }}</span>
                <span v-if="d.site_domain" class="text-xs text-blue-500">{{ d.site_domain }}</span>
              </div>
            </div>
            <div class="flex items-center gap-2 ml-4 shrink-0">
              <span class="text-xs text-gray-500">{{ d.instances || 1 }} Process{{ d.instances !== 1 ? 'es' : '' }}</span>
              <div class="relative">
                <button @click="toggleMenu(d.id)" class="px-3 py-1.5 text-xs text-gray-600 bg-gray-50 border border-gray-200 rounded-lg hover:bg-gray-100 font-medium transition-colors">
                  Open menu
                </button>
                <div v-if="openMenu === d.id" class="absolute right-0 mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10">
                  <button @click="restartDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 text-left">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
                    Restart
                  </button>
                  <button @click="deleteDaemon(d.id); openMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left">
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

    <AddDaemonModal v-if="showAddModal" @close="showAddModal = false" @created="onCreated" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AddDaemonModal from './AddDaemonModal.vue';

const { confirm } = useConfirm();
const { addToast } = useToast();

const daemons = ref<any[]>([]);
const openMenu = ref<number | null>(null);
const showAddModal = ref(false);

const token = () => localStorage.getItem('fluxo_jwt');

const fetchDaemons = async (silent = false) => {
  try {
    const res = await fetch('/api/v1/daemons', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    daemons.value = await res.json();
    if (!silent) addToast('Daemons refreshed', 'success');
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to refresh daemons', 'error');
  }
};

const restartDaemon = async (id: number) => {
  try {
    const res = await fetch(`/api/v1/daemons/${id}/restart`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Daemon restarted', 'success');
    setTimeout(fetchDaemons, 1000);
  } catch (e: any) {
    addToast(e.message || 'Failed to restart', 'error');
  }
};

const deleteDaemon = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete Daemon',
    message: 'Are you sure you want to delete this daemon? This will stop and remove the systemd service.',
    confirmText: 'Delete',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    const res = await fetch(`/api/v1/daemons/${id}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Daemon deleted', 'success');
    fetchDaemons();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete', 'error');
  }
};

const onCreated = () => {
  showAddModal.value = false;
  fetchDaemons();
};

const toggleMenu = (id: number) => {
  openMenu.value = openMenu.value === id ? null : id;
};

const handleClickOutside = (e: MouseEvent) => {
  const target = e.target as HTMLElement;
  if (!target.closest('.relative')) openMenu.value = null;
};

onMounted(() => {
  fetchDaemons(true);
  window.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  window.removeEventListener('click', handleClickOutside);
});
</script>