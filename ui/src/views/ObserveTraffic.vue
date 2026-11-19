<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <PageHeader title="Traffic" subtitle="Origin request analytics for your sites. Collection is off by default." />
      <AppButton variant="secondary" size="sm" :loading="loading" :disabled="saving !== null" @click="manualRefresh">Refresh</AppButton>
    </div>
    <ErrorAlert :message="error" />
    <SkeletonLoader v-if="!state && loading" type="card" />
    <template v-if="state">
      <Card>
        <ToggleSwitch :model-value="state.master_enabled" label="Enable traffic collection on this server" description="Selected sites collect only while this is enabled. Disabling keeps their history for 30 days." label-position="left" :disabled="saving !== null || state.pending" @update:model-value="setMaster" />
        <div class="mt-4 flex flex-wrap items-center gap-3" aria-live="polite">
          <StatusBadge :label="masterLabel" :variant="state.pending ? 'yellow' : state.error ? 'red' : state.master_enabled ? 'green' : 'gray'" />
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ state.pending ? 'Nginx configuration is being applied. Collection changes are not complete yet.' : 'Hourly aggregates · Usually updated within 30–60 seconds · 30-day retention' }}</p>
        </div>
        <ErrorAlert v-if="state.error" :message="state.error" class="mt-4" />
      </Card>

      <Card>
        <h2 class="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">Sites</h2>
        <p class="mb-5 text-sm text-gray-500 dark:text-gray-400">Choose the sites to collect. Enabling starts new collection; previous requests are not imported. Site selections are preserved when server collection is off.</p>
        <DataTable :columns="columns" :items="state.sites" empty-text="Create a site to collect traffic analytics." aria-label="Traffic collection by site">
          <template #domain="{ item }"><router-link :to="`/sites/${item.site_id}/observe/traffic`" class="font-medium text-blue-600 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-blue-400">{{ item.domain }}</router-link></template>
          <template #enabled="{ item }"><ToggleSwitch :model-value="item.enabled" :label="`Collect for ${item.domain}`" :disabled="saving !== null || state.pending || item.pending" @update:model-value="value => setSite(item.site_id, value)" /></template>
          <template #status="{ item }">
            <StatusBadge v-bind="trafficStatus(item)" />
            <p v-if="item.error" class="mt-1 max-w-64 whitespace-normal text-xs text-red-600 dark:text-red-400">{{ item.error }}</p>
            <p v-else-if="item.backlog_bytes" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ trafficBytes(item.backlog_bytes) }} awaiting processing</p>
          </template>
          <template #last_processed_at="{ item }"><span class="text-xs text-gray-500 dark:text-gray-400">{{ trafficDate(item.last_processed_at) }}</span></template>
          <template #actions="{ item }"><AppButton :to="`/sites/${item.site_id}/observe/traffic`" variant="secondary" size="sm">View analytics</AppButton></template>
        </DataTable>
      </Card>
    </template>
    <Card>
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ trafficCoverageNote }}</p>
      <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">Request counts do not identify people. No IP, session or location tracking. Disabling collection preserves history; each site's analytics page has a separate action to delete it.</p>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import Card from '../components/Card.vue';
import DataTable from '../components/DataTable.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import PageHeader from '../components/PageHeader.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';
import { useTrafficPolling } from '../composables/useTrafficPolling';
import type { TrafficState } from '../types/trafficAnalytics';
import { trafficBytes, trafficCoverageNote, trafficDate, trafficError, trafficPollDelay, trafficStatus } from '../utils/trafficAnalytics';

const state = ref<TrafficState | null>(null);
const loading = ref(false);
const error = ref('');
const saving = ref<number | 'master' | null>(null);
let request = 0;
const columns = [{ key: 'domain', label: 'Site' }, { key: 'enabled', label: 'Collection selection' }, { key: 'status', label: 'Status' }, { key: 'last_processed_at', label: 'Last processed' }];
const masterLabel = computed(() => state.value?.pending ? state.value.status === 'draining' ? 'Draining' : 'Applying change' : state.value?.error ? 'Needs attention' : state.value?.master_enabled ? 'Enabled' : 'Disabled');
const load = async () => {
  if (saving.value !== null) return;
  const current = ++request;
  loading.value = true;
  try {
    const result = await apiClient.getTrafficAnalytics();
    if (request === current) state.value = result;
  } catch (err) { if (request === current) error.value = trafficError(err); }
  finally { if (request === current) loading.value = false; }
};
const { refresh } = useTrafficPolling(load, () => trafficPollDelay(!!state.value?.pending || !!state.value?.sites.some(site => site.pending), !!state.value?.master_enabled));
const manualRefresh = () => { error.value = ''; void refresh(); };
const setMaster = async (enabled: boolean) => {
  saving.value = 'master'; request++; error.value = '';
  try { state.value = await apiClient.setTrafficAnalytics(enabled); }
  catch (err) { error.value = trafficError(err); }
  finally { saving.value = null; await refresh(); }
};
const setSite = async (id: number, enabled: boolean) => {
  saving.value = id; request++; error.value = '';
  try {
    const result = await apiClient.setSiteTrafficAnalytics(id, enabled);
    if (state.value) state.value.sites = state.value.sites.map(site => site.site_id === id ? result : site);
  } catch (err) { error.value = trafficError(err); }
  finally { saving.value = null; await refresh(); }
};
</script>
