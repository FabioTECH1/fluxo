<template>
 <div class="space-y-6">
  <Card>
   <h2 class="text-lg font-semibold mb-4">Security Updates</h2>
   <ToggleSwitch :model-value="status?.updates.state === 'enabled'" :disabled="!status?.updates.editable || busy || restartPending" label="Automatic security updates" label-position="left" description="Let Ubuntu install security updates automatically." @update:model-value="setUpdates" />
   <dl class="mt-6 border-t border-gray-100 dark:border-gray-800 divide-y divide-gray-100 dark:divide-gray-800 text-sm">
    <div class="flex flex-wrap items-center justify-between gap-2 py-4"><dt class="text-gray-500 dark:text-gray-400">Status</dt><dd><StatusBadge :label="policyLabel" :variant="status?.updates.state === 'enabled' ? 'green' : 'gray'" /></dd></div>
    <div class="flex flex-wrap items-center justify-between gap-2 py-4"><dt class="text-gray-500 dark:text-gray-400">Last update attempt</dt><dd class="flex flex-wrap items-center gap-2"><span>{{ status?.updates.last_attempt || 'Unavailable' }}</span><StatusBadge :label="resultLabel" :variant="status?.updates.last_result === 'successful' ? 'green' : status?.updates.last_result === 'failed' ? 'red' : 'gray'" /></dd></div>
   </dl>
   <StatusNotice v-if="status && !status.updates.editable" :message="policyLabel" :details="status.updates.detail" :tone="status.updates.automatic_reboot === 'enabled' ? 'warning' : 'info'" class="mb-4" />
   <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">Covers configured security repositories. Some updates may restart services or require a server restart. {{ status?.updates.editable && status.updates.automatic_reboot === 'disabled' ? 'Your server will not restart automatically.' : 'An external automatic-reboot policy may apply; review the policy details.' }}</p>
   <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">Third-party repositories may need separate updates. Nginx package and Ubuntu release upgrades remain managed over SSH.</p>
  </Card>
  <Card>
   <h2 class="text-lg font-semibold mb-4">Power</h2>
   <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
    <div class="min-w-0"><h3 class="text-sm font-medium">Restart server</h3><p class="mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400">All hosted sites and Fluxo will be temporarily unavailable.</p></div>
    <AppButton variant="danger" class="shrink-0" :disabled="!status || restartPending" @click="restart">Restart server</AppButton>
   </div>
  </Card>
  <StatusNotice v-if="pollError" message="Server status is unavailable" :details="pollError" tone="warning"><template #actions><AppButton variant="secondary" size="sm" @click="refresh">Retry check</AppButton></template></StatusNotice>
 </div>
</template>
<script setup lang="ts">
import { computed, onMounted } from 'vue';
import Card from '../components/Card.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';
import StatusBadge from '../components/StatusBadge.vue';
import AppButton from '../components/AppButton.vue';
import StatusNotice from '../components/StatusNotice.vue';
import { useServerSystem } from '../composables/useServerSystem';
const {status,busy,pollError,restartPending,refresh,restart,setUpdates} = useServerSystem();
const policyLabel = computed(() => ({enabled:'Enabled',disabled:'Disabled',not_installed:'Not installed',unsupported:'Unsupported',external:'Externally managed',unknown:'Unknown'}[status.value?.updates.state || 'unknown'] || 'Unknown'));
const resultLabel = computed(() => ({successful:'Successful',failed:'Failed',unknown:'Unknown'}[status.value?.updates.last_result || 'unknown'] || 'Unknown'));
onMounted(refresh);
</script>
