<template>
 <section v-if="restartRequired || visibleResult || error || modalOpen" class="mx-auto w-full max-w-6xl px-6 pt-3 space-y-3" aria-label="Server system status">
  <NoticeBanner v-if="restartRequired && (!visibleResult || canDismissResult)" message="Server restart required" tone="warning">
   <template #actions><button type="button" :class="linkClass" @click="showRestartDetails">Show details</button></template>
  </NoticeBanner>
  <NoticeBanner v-if="visibleResult" :message="stage" :tone="serviceIssues ? 'error' : 'info'">
   <template #actions>
    <button type="button" :class="linkClass" @click="showRestartStatus">Show details</button>
    <button v-if="canDismissResult" type="button" aria-label="Dismiss completed restart status" class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800" @click="dismissResult"><XMarkIcon class="h-4 w-4" aria-hidden="true" /></button>
   </template>
  </NoticeBanner>
  <BaseModal :model-value="modalOpen" @update:model-value="value => { if (!value) closeResult() }" :title="title" max-width="max-w-xl" :dismiss-on-backdrop="restartView === 'details'" :dismiss-on-escape="!busy" :show-close="!busy">
   <div class="space-y-5">
    <template v-if="restartView === 'details'">
     <p class="text-sm leading-6 text-gray-600 dark:text-gray-400">{{ restartRequired ? 'Installed system updates require a restart to take effect.' : 'Restart the server and check its services when it returns.' }}</p>
     <p v-if="restartRequired?.detail" class="whitespace-pre-wrap break-words text-sm text-gray-700 dark:text-gray-300">{{ restartRequired.detail }}</p>
     <div v-if="restartRequired?.packages.length"><h3 class="mb-2 text-sm font-medium">Packages requesting a restart</h3><ul class="max-h-40 overflow-y-auto rounded-lg border border-gray-200 p-3 font-mono text-sm dark:border-gray-700"><li v-for="pkg in restartRequired.packages" :key="pkg">{{ pkg }}</li></ul></div>
    </template>
    <template v-else-if="restartView === 'confirm'"><p class="text-sm leading-6 text-gray-600 dark:text-gray-400">All hosted sites and Fluxo will be temporarily unavailable. Wait for the server to restart before continuing.</p></template>
    <template v-else>
     <div role="status" aria-live="polite" class="flex items-center gap-3"><ArrowPathIcon v-if="!canDismissResult" class="h-5 w-5 shrink-0 animate-spin text-blue-600 motion-reduce:animate-none" aria-hidden="true" /><p class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ stage || 'Checking restart status…' }}</p></div>
     <p v-if="pollError" role="alert" class="text-sm text-red-700 dark:text-red-300">{{ pollError }}</p>
     <DataTable v-if="services.length" :columns="columns" :items="serviceRows" aria-label="Post-restart service status" scroll-class="max-h-64 overflow-y-auto" sticky-header>
      <template #name="{ item }"><span class="font-medium text-gray-900 dark:text-gray-100" :title="item.unit">{{ item.name }}</span></template>
      <template #state="{ item }"><StatusBadge :label="item.label" :variant="item.variant" /><p v-if="item.state !== 'running' && item.detail" class="mt-1 max-w-64 whitespace-normal text-xs leading-5 text-gray-500 dark:text-gray-400">{{ item.detail }}</p></template>
     </DataTable>
     <p v-if="needsConsole" class="text-sm leading-6 text-gray-600 dark:text-gray-400">If the server does not return, check SSH or your provider console. Do not repeatedly request a restart.</p>
     <p v-if="services.length" class="text-xs leading-5 text-gray-500 dark:text-gray-400">Service status only; individual sites are not checked.</p>
    </template>
    <ErrorAlert v-if="error" message="Server action needs attention" :details="error" />
   </div>
   <template #footer>
    <template v-if="restartView === 'details'"><AppButton variant="secondary" @click="modalOpen=false">Close</AppButton><AppButton variant="danger" :disabled="restartPending || !status" @click="reviewRestart">Restart server</AppButton></template>
    <template v-else-if="restartView === 'confirm'"><AppButton variant="secondary" :disabled="busy" @click="restartView='details'">Cancel</AppButton><AppButton variant="danger" :loading="busy" :disabled="restartPending" @click="confirmRestart">Restart server</AppButton></template>
    <template v-else><AppButton variant="secondary" @click="closeResult">{{ canDismissResult ? 'Close' : 'Minimise' }}</AppButton><AppButton :loading="refreshing" @click="refresh">Check status</AppButton></template>
   </template>
  </BaseModal>
 </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import { ArrowPathIcon, XMarkIcon } from '@heroicons/vue/24/outline';
import NoticeBanner from './NoticeBanner.vue';
import ErrorAlert from './ErrorAlert.vue';
import AppButton from './AppButton.vue';
import BaseModal from './BaseModal.vue';
import DataTable from './DataTable.vue';
import StatusBadge from './StatusBadge.vue';
import { useServerSystem } from '../composables/useServerSystem';
const {showRestartDetails,showRestartStatus,modalOpen,restartView,reviewRestart,confirmRestart,status,restartRequired,services,stage,error,pollError,busy,restartPending,refresh,refreshing,start,stop,canDismissResult,resultDismissed,dismissResult} = useServerSystem();
const linkClass = 'rounded-sm text-sm text-gray-600 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-gray-300';
const visibleResult = computed(() => !!stage.value && !resultDismissed.value);
const title = computed(() => restartView.value === 'details' ? restartRequired.value ? 'Server restart required' : 'Restart server' : restartView.value === 'confirm' ? 'Restart server?' : 'Server restart status');
const serviceIssues = computed(() => stage.value.includes('need attention') || stage.value.toLowerCase().includes('unconfirmed') || stage.value === 'Restart not confirmed');
const needsConsole = computed(() => stage.value === 'Still waiting for the server' || stage.value.toLowerCase().includes('unconfirmed'));
const columns = [{ key: 'name', label: 'Service' }, { key: 'state', label: 'Status' }];
function closeResult() { if (restartView.value==='progress' && canDismissResult.value) dismissResult(); modalOpen.value = false; }
const serviceRows = computed(() => services.value.map(service => {
 const known: Record<string,string> = { 'nginx.service':'Nginx', 'mariadb.service':'MariaDB', 'mysql.service':'MySQL', 'redis-server.service':'Redis', 'cronie.service':'Scheduler', 'cron.service':'Scheduler' };
 const php = service.unit.match(/^php([0-9.]+)-fpm\.service$/), daemon = service.unit.match(/^fluxo-daemon-(\d+)\.service$/);
 const name = known[service.unit] || (php ? `PHP ${php[1]} FPM` : daemon ? `Application process ${daemon[1]}` : service.unit);
 const variant = service.state === 'running' ? 'green' : service.state === 'failed' ? 'red' : service.state === 'starting' ? 'yellow' : 'gray';
 const label = ({running:'Running',failed:'Failed',starting:'Starting',unknown:'Unknown'} as Record<string,string>)[service.state] || 'Unknown';
 return {...service,id:service.unit,name,variant,label};
}));
onMounted(start);onUnmounted(stop);
</script>
