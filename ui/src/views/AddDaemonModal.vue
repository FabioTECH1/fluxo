<template>
  <div class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
    <div class="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden transform transition-all">
      <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
        <h3 class="text-lg font-bold text-gray-900">New Background Process</h3>
        <button @click="$emit('close')" class="text-gray-400 hover:text-gray-600 transition-colors">
          <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <form @submit.prevent="submit" class="p-6 space-y-5">
        <div v-if="error" class="text-red-700 bg-red-50 border border-red-200 p-3 rounded-lg text-sm">{{ error }}</div>

        <p class="text-sm text-gray-600">Create a new background process that will be restarted if it crashes or the server restarts.</p>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Name</label>
          <p class="text-xs text-gray-500 mb-1">Add a custom display name for the background process.</p>
          <input v-model="form.name" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. Laravel Horizon">
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Command</label>
          <p class="text-xs text-gray-500 mb-1">The command that should run for this background process.</p>
          <input v-model="form.command" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="php8.4 artisan horizon">
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Working Directory</label>
          <p class="text-xs text-gray-500 mb-1">The directory where the background process should be started.</p>
          <input v-model="form.directory" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="/var/www">
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">User</label>
          <select v-model="form.user" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
            <option value="fluxo">fluxo</option>
          </select>
        </div>

        <!-- Advanced Settings -->
        <div>
          <button type="button" @click="showAdvanced = !showAdvanced" class="flex items-center gap-2 text-sm text-gray-600 hover:text-gray-900 font-medium">
            <svg class="w-4 h-4" :class="showAdvanced ? 'rotate-90' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
            </svg>
            Advanced settings
          </button>
        </div>

        <div v-if="showAdvanced" class="space-y-4 pl-4 border-l-2 border-gray-200">
          <div class="grid grid-cols-3 gap-4">
            <div>
              <label class="block text-gray-700 text-xs font-bold mb-1">Processes</label>
              <input v-model.number="form.instances" type="number" min="1" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm text-center" placeholder="1">
            </div>
            <div>
              <label class="block text-gray-700 text-xs font-bold mb-1">Start (seconds)</label>
              <input v-model.number="form.start_seconds" type="number" min="1" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm text-center" placeholder="1">
            </div>
            <div>
              <label class="block text-gray-700 text-xs font-bold mb-1">Stop (seconds)</label>
              <input v-model.number="form.stop_seconds" type="number" min="1" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm text-center" placeholder="15">
            </div>
          </div>
          <div>
            <label class="block text-gray-700 text-xs font-bold mb-1">Stop signal</label>
            <select v-model="form.stop_signal" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
              <option value="SIGTERM">SIGTERM</option>
              <option value="SIGQUIT">SIGQUIT</option>
              <option value="SIGINT">SIGINT</option>
              <option value="SIGHUP">SIGHUP</option>
              <option value="SIGKILL">SIGKILL</option>
            </select>
          </div>
        </div>

        <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100">
          <button type="button" @click="$emit('close')" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">Cancel</button>
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">
            {{ loading ? 'Creating...' : 'Create background process' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const emit = defineEmits(['close', 'created']);
const props = defineProps<{ siteId?: string }>();

const form = ref({
  name: '',
  command: '',
  directory: '/var/www',
  user: 'fluxo',
  instances: 1,
  start_seconds: 1,
  stop_seconds: 15,
  stop_signal: 'SIGTERM',
});

const showAdvanced = ref(false);
const loading = ref(false);
const error = ref('');

const token = () => localStorage.getItem('fluxo_jwt');

const submit = async () => {
  error.value = '';
  loading.value = true;
  try {
    const endpoint = props.siteId ? `/api/v1/sites/${props.siteId}/daemons` : '/api/v1/daemons';
    const res = await fetch(endpoint, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(form.value)
    });
    if (!res.ok) throw new Error(await res.text());
    emit('created');
  } catch (e: any) {
    error.value = e.message || 'Failed to create daemon';
  } finally {
    loading.value = false;
  }
};
</script>