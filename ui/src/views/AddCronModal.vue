<template>
  <BaseModal v-model="visible" title="Add Scheduled Job" :loading="loading" confirm-text="Add scheduled job" @submit="formRef?.requestSubmit()">
    <form ref="formRef" @submit.prevent="submit" class="space-y-5">
      <ErrorAlert :message="error" />

      <p class="text-sm text-gray-600 dark:text-gray-400">Schedule any recurring tasks that need to run on your server.</p>

      <FormGroup label="Name">
        <input v-model="form.name" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. Laravel Scheduler">
      </FormGroup>

      <FormGroup label="Command">
        <input v-model="form.command" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="/usr/bin/php /home/fluxo/example.com/current/artisan schedule:run">
      </FormGroup>

      <div class="grid grid-cols-2 gap-4">
        <FormGroup label="User">
          <select v-model="form.user" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
            <option value="fluxo">fluxo</option>
          </select>
        </FormGroup>
        <div>
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Frequency</label>
          <select v-model="frequency" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
            <option value="every-minute">Every minute (* * * * *)</option>
            <option value="every-5-minutes">Every 5 minutes (*/5 * * * *)</option>
            <option value="hourly">Hourly (0 * * * *)</option>
            <option value="daily">Daily (0 0 * * *)</option>
            <option value="weekly">Weekly (0 0 * * 0)</option>
            <option value="monthly">Monthly (0 0 1 * *)</option>
            <option value="custom">Custom</option>
          </select>
          <div v-if="frequency === 'custom'" class="mt-2">
            <input v-model="customExpression" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" placeholder="*/15 * * * *">
          </div>
        </div>
      </div>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import BaseModal from '../components/BaseModal.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';

const visible = defineModel<boolean>({ required: true });
const props = defineProps<{ siteId?: string }>();
const emit = defineEmits(['created']);

const formRef = ref<HTMLFormElement | null>(null);

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
    const endpoint = props.siteId ? `/api/v1/sites/${props.siteId}/crons` : '/api/v1/crons';
    const res = await fetch(endpoint, {
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