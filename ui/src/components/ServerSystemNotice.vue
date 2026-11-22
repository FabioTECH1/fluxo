<template>
 <section v-if="restartRequired || stage || error" class="mx-auto w-full max-w-6xl px-6 pt-3 space-y-3" aria-label="Server system status">
  <StatusNotice v-if="restartRequired" message="Server restart required" tone="warning" :details="restartRequired.detail">
   <template #actions><button type="button" :class="actionClass" :disabled="restartPending" @click="restart">Restart server</button></template>
   <p class="mt-1 leading-6 text-gray-600 dark:text-gray-400">Installed system updates require a restart to take effect.</p>
  </StatusNotice>
  <StatusNotice v-if="stage" :message="stage" :tone="serviceIssues ? 'error' : 'info'" :details="flowDetails">
   <template #actions><button type="button" :class="actionClass" @click="refresh">Check status</button></template>
  </StatusNotice>
  <ErrorAlert v-if="error" message="Server action needs attention" :details="error" />
 </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import StatusNotice from './StatusNotice.vue';
import ErrorAlert from './ErrorAlert.vue';
import { useServerSystem } from '../composables/useServerSystem';
const {restartRequired,services,stage,error,pollError,restartPending,restart,refresh,start,stop} = useServerSystem();
const actionClass = 'inline-flex min-h-8 items-center rounded-md text-sm font-medium text-gray-600 transition-colors hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 disabled:opacity-50 dark:text-gray-300 dark:hover:text-white';
const serviceIssues = computed(() => stage.value.includes('need attention') || stage.value === 'Restart not confirmed');
const flowDetails = computed(() => [
 ...services.value.map(s => `${s.unit}: ${s.state}. ${s.detail}`),
 pollError.value,
 stage.value === 'Still waiting for the server' || stage.value.includes('unconfirmed') ? 'Use your provider console or SSH if the server does not return. Check status; do not repeatedly request a restart.' : '',
 'These checks cover expected service status, not the health of individual hosted sites.',
].filter(Boolean).join('\n'));
onMounted(start);onUnmounted(stop);
</script>
