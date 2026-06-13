<template>
  <BaseModal v-model="visible" title="New Background Process" :loading="loading" confirm-text="Create background process" @submit="formRef?.requestSubmit()">
    <form ref="formRef" @submit.prevent="submit" class="space-y-5">
      <ErrorAlert :message="error" />

      <p class="text-sm text-gray-600 dark:text-gray-400">Create a new background process that will be restarted if it crashes or the server restarts.</p>

      <FormGroup label="Name" hint="Add a custom display name for the background process.">
        <input v-model="form.name" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="e.g. Laravel Horizon">
      </FormGroup>

      <FormGroup label="Command" hint="The command that should run for this background process.">
        <input v-model="form.command" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="artisan horizon">
      </FormGroup>

      <FormGroup label="Working Directory" hint="The directory where the background process should be started.">
        <input v-model="form.directory" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" readonly :placeholder="dirPlaceholder">
      </FormGroup>

      <FormGroup label="User">
        <select v-model="form.user" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
          <option value="fluxo">fluxo</option>
        </select>
      </FormGroup>

      <div>
        <button type="button" @click="showAdvanced = !showAdvanced" class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 font-medium">
          <svg class="w-4 h-4" :class="showAdvanced ? 'rotate-90' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
          </svg>
          Advanced settings
        </button>
      </div>

      <div v-if="showAdvanced" class="space-y-4 pl-4 border-l-2 border-gray-200 dark:border-gray-700">
        <div class="grid grid-cols-3 gap-4">
          <FormGroup label="Processes">
            <input v-model.number="form.instances" type="number" min="1" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm text-center" placeholder="1">
          </FormGroup>
          <FormGroup label="Start (seconds)">
            <input v-model.number="form.start_seconds" type="number" min="1" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm text-center" placeholder="1">
          </FormGroup>
          <FormGroup label="Stop (seconds)">
            <input v-model.number="form.stop_seconds" type="number" min="1" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm text-center" placeholder="15">
          </FormGroup>
        </div>
        <FormGroup label="Stop signal">
          <select v-model="form.stop_signal" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
            <option value="SIGTERM">SIGTERM</option>
            <option value="SIGQUIT">SIGQUIT</option>
            <option value="SIGINT">SIGINT</option>
            <option value="SIGHUP">SIGHUP</option>
            <option value="SIGKILL">SIGKILL</option>
          </select>
        </FormGroup>
      </div>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import BaseModal from '../components/BaseModal.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';

const visible = defineModel<boolean>({ required: true });
const props = defineProps<{ siteId?: string }>();
const emit = defineEmits(['created']);

const formRef = ref<HTMLFormElement | null>(null);
const site = ref<any>(null);

const dirPlaceholder = computed(() => {
  if (site.value?.domain) return `/home/fluxo/${site.value.domain}`;
  return '/home/fluxo/example.com';
});

const form = ref({
  name: '',
  command: '',
  directory: '/home/fluxo',
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

watch(visible, async (v) => {
  if (v && props.siteId) {
    try {
      const res = await fetch(`/api/v1/sites/${props.siteId}`, { headers: { 'Authorization': `Bearer ${token()}` } });
      if (res.ok) {
        site.value = await res.json();
        form.value.directory = `/home/fluxo/${site.value.domain}`;
      }
    } catch (e) {}
  }
});

const submit = async () => {
  error.value = '';
  loading.value = true;
  try {
    let cmd = form.value.command;
    const isPhpApp = site.value?.app_type === 'laravel' || site.value?.app_type === 'php';
    if (props.siteId && isPhpApp && site.value?.php_version && cmd.startsWith('artisan')) {
      cmd = `php${site.value.php_version} ${cmd}`;
    }

    const endpoint = props.siteId ? `/api/v1/sites/${props.siteId}/daemons` : '/api/v1/daemons';
    const res = await fetch(endpoint, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...form.value, command: cmd })
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