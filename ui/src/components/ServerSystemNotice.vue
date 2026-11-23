<template>
 <section v-if="restartRequired || visibleResult || error" class="mx-auto w-full max-w-6xl px-6 pt-3 space-y-3" aria-label="Server system status">
  <StatusNotice v-if="restartRequired" message="Server restart required" tone="warning" :details="restartRequired.detail">
   <template #actions><AppButton variant="secondary" size="sm" :disabled="restartPending" @click="restart">Restart server</AppButton></template>
   <p class="mt-1 leading-6 text-gray-600 dark:text-gray-400">Installed system updates require a restart to take effect.</p>
  </StatusNotice>
  <NoticeBanner v-if="visibleResult" :message="stage" :tone="serviceIssues ? 'error' : 'info'">
   <template #actions>
    <AppButton variant="secondary" size="sm" @click="detailsOpen = true">View details</AppButton>
    <button v-if="canDismissResult" type="button" aria-label="Dismiss completed restart status" class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200" @click="dismissResult">
     <XMarkIcon class="h-4 w-4" aria-hidden="true" />
    </button>
   </template>
  </NoticeBanner>
  <ErrorAlert v-if="error" message="Server action needs attention" :details="error" />
  <BaseModal v-model="detailsOpen" title="Server restart status" max-width="max-w-xl">
   <div class="space-y-5">
    <div><p class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ stage }}</p><p v-if="pollError" role="alert" class="mt-2 text-sm text-red-700 dark:text-red-300">{{ pollError }}</p></div>
    <DataTable v-if="services.length" :columns="columns" :items="serviceRows" aria-label="Post-restart service status">
     <template #name="{ item }"><span class="font-medium text-gray-900 dark:text-gray-100" :title="item.unit">{{ item.name }}</span></template>
     <template #state="{ item }"><StatusBadge :label="item.label" :variant="item.variant" /><p v-if="item.state !== 'running' && item.detail" class="mt-1 max-w-64 whitespace-normal text-xs leading-5 text-gray-500 dark:text-gray-400">{{ item.detail }}</p></template>
    </DataTable>
    <p v-if="needsConsole" class="text-sm leading-6 text-gray-600 dark:text-gray-400">If the server does not return, check SSH or your provider console. Do not repeatedly request a restart.</p>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">Service status only; individual sites are not checked.</p>
   </div>
   <template #footer>
    <AppButton variant="secondary" @click="detailsOpen = false">Close</AppButton>
    <AppButton :loading="refreshing" @click="refresh">{{ refreshing ? 'Checking…' : 'Check status' }}</AppButton>
   </template>
  </BaseModal>
 </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { XMarkIcon } from '@heroicons/vue/24/outline';
import StatusNotice from './StatusNotice.vue';
import NoticeBanner from './NoticeBanner.vue';
import ErrorAlert from './ErrorAlert.vue';
import AppButton from './AppButton.vue';
import BaseModal from './BaseModal.vue';
import DataTable from './DataTable.vue';
import StatusBadge from './StatusBadge.vue';
import { useServerSystem } from '../composables/useServerSystem';
const {restartRequired,services,stage,error,pollError,restartPending,restart,refresh,refreshing,start,stop,canDismissResult,resultDismissed,dismissResult} = useServerSystem();
const detailsOpen = ref(false);
const visibleResult = computed(() => !!stage.value && !resultDismissed.value);
const serviceIssues = computed(() => stage.value.includes('need attention') || stage.value === 'Restart not confirmed');
const needsConsole = computed(() => stage.value === 'Still waiting for the server' || stage.value.toLowerCase().includes('unconfirmed'));
const columns = [{ key: 'name', label: 'Service' }, { key: 'state', label: 'Status' }];
const serviceRows = computed(() => services.value.map(service => {
 const known: Record<string,string> = { 'nginx.service':'Nginx', 'mariadb.service':'MariaDB', 'mysql.service':'MySQL', 'redis-server.service':'Redis', 'cronie.service':'Scheduler', 'cron.service':'Scheduler' };
 const php = service.unit.match(/^php([0-9.]+)-fpm\.service$/);
 const daemon = service.unit.match(/^fluxo-daemon-(\d+)\.service$/);
 const name = known[service.unit] || (php ? `PHP ${php[1]} FPM` : daemon ? `Application process ${daemon[1]}` : service.unit);
 const variant = service.state === 'running' ? 'green' : service.state === 'failed' ? 'red' : service.state === 'starting' ? 'yellow' : 'gray';
 const label = ({running:'Running',failed:'Failed',starting:'Starting',unknown:'Unknown'} as Record<string,string>)[service.state] || 'Unknown';
 return {...service,id:service.unit,name,variant,label};
}));
onMounted(start);onUnmounted(stop);
</script>
