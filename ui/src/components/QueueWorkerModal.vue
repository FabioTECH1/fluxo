<template>
  <BaseModal v-model="isOpen" :title="enabled ? 'Configure Queue Worker' : 'Enable Queue Worker'" max-width="max-w-xl"
    :loading="saving" :confirm-text="enabled ? 'Save and restart worker' : 'Save and start worker'" :confirm-disabled="!formValid"
    @submit="save">
    <div class="space-y-5">
      <p class="text-sm text-gray-600 dark:text-gray-400">
        Fluxo will update <code class="font-mono text-xs">QUEUE_CONNECTION</code>, start the worker with systemd, and reload it gracefully after deployments.
      </p>

      <FormGroup label="Queue connection" for-attr="queue-worker-connection" hint="Choose the connection your application dispatches queued jobs to.">
        <select id="queue-worker-connection" v-model="connectionChoice" class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
          <option value="database">Database</option>
          <option value="redis">Redis</option>
          <option value="sqs">Amazon SQS</option>
          <option value="beanstalkd">Beanstalkd</option>
          <option value="custom">Custom connection</option>
        </select>
      </FormGroup>

      <FormGroup v-if="connectionChoice === 'custom'" label="Custom connection name" for-attr="queue-worker-custom-connection">
        <input id="queue-worker-custom-connection" v-model.trim="customConnection" type="text" maxlength="64" placeholder="e.g. redis-long-running"
          class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 font-mono text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
      </FormGroup>

      <div class="grid gap-4 sm:grid-cols-2">
        <FormGroup label="Queues" for-attr="queue-worker-queues" hint="Comma-separated in priority order.">
          <input id="queue-worker-queues" v-model.trim="form.queues" type="text" placeholder="default"
            class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 font-mono text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
        </FormGroup>
        <FormGroup label="Processes" for-attr="queue-worker-processes" hint="Concurrent worker processes.">
          <input id="queue-worker-processes" v-model.number="form.processes" type="number" min="1" max="16"
            class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
        </FormGroup>
        <FormGroup label="Tries" for-attr="queue-worker-tries" hint="Use 0 to retry indefinitely.">
          <input id="queue-worker-tries" v-model.number="form.tries" type="number" min="0" max="100"
            class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
        </FormGroup>
        <FormGroup label="Timeout" for-attr="queue-worker-timeout" hint="Maximum seconds for one job.">
          <input id="queue-worker-timeout" v-model.number="form.timeout_seconds" type="number" min="1" max="86400"
            class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
        </FormGroup>
      </div>

      <button type="button" class="text-sm font-semibold text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200" @click="showAdvanced = !showAdvanced">
        {{ showAdvanced ? 'Hide advanced settings' : 'Advanced settings' }}
      </button>
      <div v-if="showAdvanced" class="space-y-4 border-l-2 border-gray-200 pl-4 dark:border-gray-700">
        <div class="grid gap-4 sm:grid-cols-2">
          <FormGroup label="Sleep (seconds)" for-attr="queue-worker-sleep">
            <input id="queue-worker-sleep" v-model.number="form.sleep_seconds" type="number" min="0" max="60"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
          <FormGroup label="Backoff (seconds)" for-attr="queue-worker-backoff">
            <input id="queue-worker-backoff" v-model.number="form.backoff_seconds" type="number" min="0" max="86400"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
          <FormGroup label="Memory (MB)" for-attr="queue-worker-memory">
            <input id="queue-worker-memory" v-model.number="form.memory_mb" type="number" min="32" max="4096"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
          <FormGroup label="Max runtime (seconds)" for-attr="queue-worker-max-time" hint="Use 0 to disable lifetime recycling.">
            <input id="queue-worker-max-time" v-model.number="form.max_time_seconds" type="number" min="0" max="86400"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
        </div>
        <ToggleSwitch v-model="form.force" label="Process during maintenance mode"
          description="Continue processing jobs while the application is in maintenance mode." />
      </div>

      <p v-if="customQueueWorkers > 0" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
        {{ customQueueWorkers }} custom queue process{{ customQueueWorkers === 1 ? '' : 'es' }} already exists. Fluxo will not remove or modify custom processes.
      </p>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import BaseModal from './BaseModal.vue';
import FormGroup from './FormGroup.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const props = withDefaults(defineProps<{
  modelValue: boolean;
  siteId: string | number;
  enabled?: boolean;
  config?: Record<string, any>;
  customQueueWorkers?: number;
}>(), {
  enabled: false,
  config: () => ({}),
  customQueueWorkers: 0,
});

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  'saved': [];
}>();

const { addToast } = useToast();
const saving = ref(false);
const showAdvanced = ref(false);
const connectionChoice = ref('database');
const customConnection = ref('');
const form = ref({
  queues: 'default', processes: 1, sleep_seconds: 3, tries: 3,
  timeout_seconds: 60, backoff_seconds: 0, memory_mb: 128,
  max_time_seconds: 3600, force: false,
});

const isOpen = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
});

const resolvedConnection = computed(() => connectionChoice.value === 'custom'
  ? customConnection.value.trim()
  : connectionChoice.value);
const integerInRange = (value: unknown, min: number, max: number) => typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max;
const formValid = computed(() => Boolean(
  resolvedConnection.value
  && form.value.queues.trim()
  && integerInRange(form.value.processes, 1, 16)
  && integerInRange(form.value.sleep_seconds, 0, 60)
  && integerInRange(form.value.tries, 0, 100)
  && integerInRange(form.value.timeout_seconds, 1, 86400)
  && integerInRange(form.value.backoff_seconds, 0, 86400)
  && integerInRange(form.value.memory_mb, 32, 4096)
  && integerInRange(form.value.max_time_seconds, 0, 86400)
));

const loadForm = () => {
  const defaults = {
    connection: 'database', queues: 'default', processes: 1, sleep_seconds: 3,
    tries: 3, timeout_seconds: 60, backoff_seconds: 0, memory_mb: 128,
    max_time_seconds: 3600, force: false,
  };
  const config = { ...defaults, ...(props.config || {}) };
  const commonConnections = ['database', 'redis', 'sqs', 'beanstalkd'];
  if (commonConnections.includes(config.connection)) {
    connectionChoice.value = config.connection;
    customConnection.value = '';
  } else {
    connectionChoice.value = 'custom';
    customConnection.value = config.connection || '';
  }
  form.value = {
    queues: config.queues,
    processes: config.processes,
    sleep_seconds: config.sleep_seconds,
    tries: config.tries,
    timeout_seconds: config.timeout_seconds,
    backoff_seconds: config.backoff_seconds,
    memory_mb: config.memory_mb,
    max_time_seconds: config.max_time_seconds,
    force: Boolean(config.force),
  };
  showAdvanced.value = false;
};

watch(() => props.modelValue, (open) => {
  if (open) loadForm();
});

const save = async () => {
  if (!formValid.value) return;
  saving.value = true;
  try {
    await apiClient.toggleSiteQueueWorker(props.siteId, true, {
      ...form.value,
      connection: resolvedConnection.value,
    });
    addToast(props.enabled ? 'Queue Worker updated' : 'Queue Worker enabled', 'success');
    emit('update:modelValue', false);
    emit('saved');
  } catch (e: any) {
    addToast(e.message || `Failed to ${props.enabled ? 'update' : 'enable'} Queue Worker`, 'error');
  } finally {
    saving.value = false;
  }
};
</script>
