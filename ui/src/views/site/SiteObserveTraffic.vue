<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <PageHeader title="Traffic Analytics" subtitle="Hourly origin requests. All dates and hours use UTC." />
      <AppButton variant="secondary" size="sm" :loading="loading" :disabled="saving" @click="manualRefresh">Refresh</AppButton>
    </div>
    <ErrorAlert :message="error" />
    <SkeletonLoader v-if="!report && loading" type="card" />
    <template v-if="report">
      <Card>
        <ToggleSwitch :model-value="report.site.enabled" label="Enable traffic collection for this site" description="Enabling starts new collection. Disabling preserves history until the 30-day retention limit." label-position="left" :disabled="saving || report.site.pending" @update:model-value="setEnabled" />
        <div class="mt-4 flex flex-wrap items-center gap-3" aria-live="polite">
          <StatusBadge v-bind="trafficStatus(report.site)" />
          <p class="text-xs text-gray-500 dark:text-gray-400">Last processed: {{ trafficDate(report.site.last_processed_at) }}</p>
        </div>
        <p v-if="report.site.pending" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Nginx configuration is being applied. Collection changes are not complete yet.</p>
        <p v-if="!report.master_enabled" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Server collection is off. <router-link to="/observe/traffic" class="font-medium text-blue-600 hover:underline dark:text-blue-400">Enable it in Observe → Traffic</router-link> to collect selected sites.</p>
        <ErrorAlert v-if="report.site.error" :message="report.site.error" class="mt-4" />
      </Card>

      <NoticeBanner v-if="delayed" :message="report.site.backlog_bytes > 0 ? `Processing ${trafficBytes(report.site.backlog_bytes)} of queued request data. Recent totals may be delayed.` : 'Collection checks are delayed. Recent totals may be out of date.'" />
      <NoticeBanner v-if="report.site.incomplete || report.site.dropped_bytes > 0 || report.buckets.some(bucket => bucket.incomplete)" :message="`Some data is incomplete or capped${report.site.dropped_bytes > 0 ? `; ${trafficBytes(report.site.dropped_bytes)} of request data could not be retained` : ''}. Counts may be lower than actual traffic.`" />

      <Card>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Traffic overview</h2>
          <div class="flex flex-wrap gap-2" role="group" aria-label="Analytics date range in UTC">
            <AppButton v-for="option in ranges" :key="option.value" :variant="range === option.value ? 'primary' : 'secondary'" size="sm" :aria-pressed="range === option.value" :disabled="saving" @click="range = option.value">{{ option.label }}</AppButton>
          </div>
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ trafficDate(report.from) }} – {{ trafficDate(report.to) }} · Usually updated within 30–60 seconds</p>
        <p v-if="!hasCoverage" class="mt-6 text-sm text-gray-600 dark:text-gray-400">No collected data in this range. Enable collection to begin; requests before collection started are unavailable.</p>
        <div class="mt-6 grid grid-cols-2 gap-x-4 gap-y-6 xl:grid-cols-4">
          <div v-for="metric in metrics" :key="metric.label">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
            <p class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ hasCoverage ? metric.value : '—' }}</p>
          </div>
        </div>
        <div class="mt-6"><TrafficHourlyChart :buckets="report.buckets" :gaps="report.gaps" :from="report.from" :to="report.to" /></div>
        <details class="mt-5 text-sm" @toggle="hourlyOpen = ($event.target as HTMLDetailsElement).open">
          <summary class="w-fit cursor-pointer rounded-sm font-medium text-blue-600 focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-blue-400">Hourly values</summary>
          <div v-if="hourlyOpen" class="mt-3"><DataTable :columns="hourlyColumns" :items="report.buckets" aria-label="Hourly traffic values in UTC" scroll-class="max-h-80" sticky-header empty-text="No hourly data available.">
            <template #hour="{ item }">{{ trafficDate(item.hour) }}</template>
            <template #requests="{ item }">{{ collected(item) ? trafficCount(item.metrics.requests) : '—' }}</template>
            <template #page_views="{ item }">{{ collected(item) ? trafficCount(item.metrics.page_views) : '—' }}</template>
            <template #collection="{ item }">{{ item.incomplete ? 'Incomplete' : item.collection === 'partial' ? 'Partial hour' : item.collection === 'disabled' ? 'Collection disabled' : item.collection === 'unknown' ? 'Unavailable' : 'Collected' }}</template>
          </DataTable></div>
        </details>
        <details v-if="report.gaps.length" class="mt-4 text-sm">
          <summary class="w-fit cursor-pointer rounded-sm font-medium text-gray-600 focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-gray-400">Collection gaps ({{ report.gaps.length }})</summary>
          <ul class="mt-3 space-y-2 text-xs text-gray-500 dark:text-gray-400"><li v-for="(gap, index) in report.gaps" :key="index">{{ trafficDate(gap.from) }} – {{ trafficDate(gap.to) }}: {{ gap.reason }}</li></ul>
        </details>
      </Card>

      <div class="grid min-w-0 gap-6 xl:grid-cols-2">
        <Card class="min-w-0">
          <h2 class="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">Most requested page paths</h2>
          <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Page paths only; query strings are excluded.</p>
          <DataTable :columns="pathColumns" :items="report.top_paths" empty-text="No page paths in this range." aria-label="Most requested page paths"><template #value="{ item }"><span class="block max-w-64 truncate font-mono text-xs" :title="item.value">{{ item.value }}</span></template><template #requests="{ item }">{{ trafficCount(item.requests) }}</template></DataTable>
        </Card>
        <Card class="min-w-0">
          <h2 class="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">Referrer domains</h2>
          <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">A missing referrer can mean direct traffic, an app or a privacy policy.</p>
          <DataTable :columns="referrerColumns" :items="report.referrers" empty-text="No referrers in this range." aria-label="Traffic referrer domains"><template #value="{ item }"><span class="block max-w-64 truncate" :title="item.value">{{ item.value || 'No referrer' }}</span></template><template #requests="{ item }">{{ trafficCount(item.requests) }}</template></DataTable>
        </Card>
      </div>

      <Card>
        <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">About these numbers</h2>
        <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ trafficCoverageNote }}</p>
        <ul class="mt-3 space-y-2 text-xs leading-5 text-gray-500 dark:text-gray-400">
          <li>Estimated page views filter known bots, API requests and non-page assets. They count page requests without identifying people; they are not unique visitors or sessions.</li>
          <li>Nginx response bytes measure data sent by this server. Average Nginx request time measures server request handling, including upstream time; it does not measure browser page speed.</li>
          <li>Collection gaps are unavailable data. A partial hour can contain both collected requests and a gap. No IP, session or location tracking.</li>
          <li v-for="(note, index) in report.notes" :key="index">{{ note }}</li>
        </ul>
      </Card>

      <Card>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div><h2 class="font-semibold text-gray-900 dark:text-gray-100">Analytics history</h2><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">History is retained for 30 days, including while collection is disabled.</p></div>
          <AppButton variant="danger" size="sm" :loading="deleting" :disabled="saving || (report.site.pending && !(report.site.status === 'activated' && !report.site.effective_enabled))" @click="deleteHistory">Delete analytics history</AppButton>
        </div>
      </Card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import Card from '../../components/Card.vue';
import DataTable from '../../components/DataTable.vue';
import ErrorAlert from '../../components/ErrorAlert.vue';
import NoticeBanner from '../../components/NoticeBanner.vue';
import PageHeader from '../../components/PageHeader.vue';
import SkeletonLoader from '../../components/SkeletonLoader.vue';
import StatusBadge from '../../components/StatusBadge.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';
import TrafficHourlyChart from '../../components/TrafficHourlyChart.vue';
import { useConfirm } from '../../composables/useConfirm';
import { useTrafficPolling } from '../../composables/useTrafficPolling';
import type { TrafficBucket, TrafficRange, TrafficReport } from '../../types/trafficAnalytics';
import { trafficBytes, trafficCount, trafficCoverageNote, trafficDate, trafficError, trafficIsDelayed, trafficPollDelay, trafficStatus } from '../../utils/trafficAnalytics';

const route = useRoute();
const { confirm } = useConfirm();
const id = computed(() => Number(route.params.id));
const report = ref<TrafficReport | null>(null);
const range = ref<TrafficRange>('today');
const loading = ref(false);
const saving = ref(false);
const deleting = ref(false);
const hourlyOpen = ref(false);
const error = ref('');
let request = 0;
const ranges: { value: TrafficRange; label: string }[] = [{ value: 'today', label: 'Today' }, { value: '7d', label: '7 days' }, { value: '30d', label: '30 days' }];
const pathColumns = [{ key: 'value', label: 'Path' }, { key: 'requests', label: 'Requests' }];
const referrerColumns = [{ key: 'value', label: 'Domain' }, { key: 'requests', label: 'Requests' }];
const hourlyColumns = [{ key: 'hour', label: 'Hour (UTC)' }, { key: 'requests', label: 'Requests' }, { key: 'page_views', label: 'Est. page views' }, { key: 'collection', label: 'Coverage' }];
const collected = (bucket: TrafficBucket) => !!bucket.metrics && bucket.collection !== 'disabled' && bucket.collection !== 'unknown';
const hasCoverage = computed(() => report.value?.buckets.some(collected));
const delayed = computed(() => !!report.value && trafficIsDelayed(report.value.site));
const metrics = computed(() => {
  const totals = report.value?.totals;
  if (!totals) return [];
  return [
    { label: 'Requests', value: trafficCount(totals.requests) },
    { label: 'Estimated page views', value: trafficCount(totals.page_views) },
    { label: '404 responses', value: trafficCount(totals.not_found) },
    { label: 'Other 4xx responses', value: trafficCount(totals.other_4xx) },
    { label: '5xx responses', value: trafficCount(totals.server_errors) },
    { label: 'Nginx response bytes', value: trafficBytes(totals.response_bytes) },
    { label: 'Avg Nginx request time', value: `${new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format(totals.avg_request_time_ms)} ms` },
  ];
});
const load = async () => {
  if (saving.value) return;
  const current = ++request;
  loading.value = true;
  try {
    const result = await apiClient.getSiteTrafficAnalytics(id.value, range.value);
    if (request === current) report.value = result;
  } catch (err) { if (request === current) error.value = trafficError(err); }
  finally { if (request === current) loading.value = false; }
};
const { refresh } = useTrafficPolling(load, () => trafficPollDelay(!!report.value?.site.pending, !!report.value?.site.effective_enabled));
const manualRefresh = () => { error.value = ''; void refresh(); };
watch([id, range], ([siteId], [previousId]) => {
  request++; error.value = '';
  if (siteId !== previousId) report.value = null;
  void refresh();
});
const setEnabled = async (enabled: boolean) => {
  const siteId = id.value;
  saving.value = true; request++; error.value = '';
  try {
    const result = await apiClient.setSiteTrafficAnalytics(siteId, enabled);
    if (report.value && id.value === siteId) report.value.site = result;
  } catch (err) { error.value = trafficError(err); }
  finally { saving.value = false; await refresh(); }
};
const deleteHistory = async () => {
  const siteId = id.value;
  if (!await confirm({ title: 'Delete analytics history?', message: `Permanently delete all retained traffic analytics for ${report.value?.site.domain}? This cannot be undone. The collection setting is unchanged; if enabled, new requests will continue to be collected.`, confirmText: 'Delete history', variant: 'danger' })) return;
  if (id.value !== siteId) return;
  saving.value = true; deleting.value = true; request++; error.value = '';
  try { await apiClient.deleteSiteTrafficAnalytics(siteId); }
  catch (err) { error.value = trafficError(err); }
  finally { saving.value = false; deleting.value = false; await refresh(); }
};
</script>
