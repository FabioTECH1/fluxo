<template>
  <div class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
    <div class="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden transform transition-all">
      <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
        <h3 class="text-lg font-bold text-gray-900">Add Scheduled Job</h3>
        <button @click="$emit('close')" class="text-gray-400 hover:text-gray-600 transition-colors">
          <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <form @submit.prevent="submit" class="p-6 space-y-5">
        <div v-if="error" class="text-red-700 bg-red-50 border border-red-200 p-3 rounded-lg text-sm">{{ error }}</div>

        <p class="text-sm text-gray-600">Schedule any recurring tasks that need to run on your server.</p>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Name</label>
          <input v-model="form.name" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. Laravel Scheduler">
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Command</label>
          <input v-model="form.command" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="/usr/bin/php /var/www/artisan schedule:run">
        </div>

        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">User</label>
            <select v-model="form.user" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
              <option value="fluxo">fluxo</option>
            </select>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Frequency</label>
            <select v-model="frequency" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
              <option value="every-minute">Every minute (* * * * *)</option>
              <option value="every-5-minutes">Every 5 minutes (*/5 * * * *)</option>
              <option value="hourly">Hourly (0 * * * *)</option>
              <option value="daily">Daily (0 0 * * *)</option>
              <option value="weekly">Weekly (0 0 * * 0)</option>
              <option value="monthly">Monthly (0 0 1 * *)</option>
              <option value="custom">Custom</option>
            </select>
            <div v-if="frequency === 'custom'" class="mt-2">
              <input v-model="customExpression" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" placeholder="*/15 * * * *">
            </div>
          </div>
        </div>

        <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100">
          <button type="button" @click="$emit('close')" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">Cancel</button>
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">
            {{ loading ? 'Adding...' : 'Add scheduled job' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const emit = defineEmits(['close', 'created']);

const frequency = ref('every-minute');
const customExpression = ref('');

const expressionMap: Record<string, string> = {
  'every-minute': '* * * * *',
  'every-5-minutes': '*/5 * * * *',
  'hourly': '0 * * * *',
  'daily': '0 0 * * *',
  'weekly': '0 0 * * 0',
  'monthly': '0 0 1 * *',
};

const form = ref({
  name: '',
  command: '',
  user: 'fluxo',
});

const loading = ref(false);
const error = ref('');

const token = () => localStorage.getItem('fluxo_jwt');

const submit = async () => {
  error.value = '';
  loading.value = true;
  try {
    const expression = frequency.value === 'custom' ? customExpression.value : expressionMap[frequency.value];
    if (!expression) {
      error.value = 'Please select a frequency or enter a custom expression.';
      return;
    }
    const res = await fetch('/api/v1/crons', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: form.value.name, command: form.value.command, user: form.value.user, expression })
    });
    if (!res.ok) throw new Error(await res.text());
    emit('created');
  } catch (e: any) {
    error.value = e.message || 'Failed to add cron job';
  } finally {
    loading.value = false;
  }
};
</script>