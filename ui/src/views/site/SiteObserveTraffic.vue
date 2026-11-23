<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <PageHeader title="Traffic Analytics" subtitle="Origin requests · All times UTC." />
      <AppButton variant="secondary" class="h-10" :loading="loading" :disabled="saving" @click="manualRefresh">Refresh</AppButton>
    </div>
    <ErrorAlert :message="error" />
    <SkeletonLoader v-if="!report && loading" type="card" />
    <template v-if="report">
      <Card>
        <ToggleSwitch :model-value="report.site.enabled" label="Enable traffic collection for this site" description="Collect from now on. Turning this off keeps history under your retention policy." label-position="left" :disabled="saving || report.site.pending" @update:model-value="setEnabled" />
        <div class="mt-4 flex flex-wrap items-center gap-3" aria-live="polite">
          <StatusBadge v-bind="trafficStatus(report.site)" />
          <p class="text-sm text-gray-500 dark:text-gray-400">Last processed: {{ trafficDate(report.site.last_processed_at) }}</p>
        </div>
        <p v-if="report.site.pending" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Applying collection settings…</p>
        <p v-if="!report.master_enabled" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Server collection is off. <router-link to="/observe/traffic" class="font-medium text-blue-600 hover:underline dark:text-blue-400">Enable it in Observe → Traffic Analytics</router-link> to collect selected sites.</p>
        <ErrorAlert v-if="report.site.error" :message="report.site.error" class="mt-4" />
      </Card>

      <NoticeBanner v-if="delayed" :message="report.site.backlog_bytes > 0 ? `Processing ${trafficBytes(report.site.backlog_bytes)} of queued request data. Recent totals may be delayed.` : 'Collection checks are delayed. Recent totals may be out of date.'" />
      <NoticeBanner v-if="report.site.incomplete || report.site.dropped_bytes > 0 || report.buckets.some(bucket => bucket.incomplete)" :message="`Some data is incomplete or capped${report.site.dropped_bytes > 0 ? `; ${trafficBytes(report.site.dropped_bytes)} of request data could not be retained` : ''}. Counts may be lower than actual traffic.`" />

      <Card>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Traffic overview</h2>
          <div class="flex w-full items-center gap-3 sm:w-auto">
            <label :for="`${tabsId}-range`" class="shrink-0 text-sm text-gray-500 dark:text-gray-400">Range</label>
            <select :id="`${tabsId}-range`" v-model="range" :disabled="saving" aria-label="Traffic history range" class="h-10 min-w-0 flex-1 rounded-lg border border-gray-300 bg-white px-3 pr-8 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 sm:w-48 sm:flex-none">
              <option v-for="option in ranges" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </div>
        </div>
        <div class="mt-5 flex gap-5 border-b border-gray-200 dark:border-gray-700" role="tablist" aria-label="Traffic views" @keydown="onTabKeydown">
          <button v-for="tab in tabs" :id="`${tabsId}-${tab.value}-tab`" :key="tab.value" type="button" role="tab" :aria-selected="activeTab === tab.value" :aria-controls="`${tabsId}-${tab.value}-panel`" :tabindex="activeTab === tab.value ? 0 : -1" class="-mb-px border-b-2 px-1 pb-3 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500" :class="activeTab === tab.value ? 'border-blue-600 text-blue-600 dark:border-blue-400 dark:text-blue-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'" @click="activeTab = tab.value">{{ tab.label }}</button>
        </div>
        <p class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ trafficDate(report.from, true) }} – {{ trafficDate(report.to, true) }} · Updated in 30–60 seconds</p>
        <p v-if="historyStartsLater" class="mt-2 text-sm leading-6 text-amber-700 dark:text-amber-400">History starts {{ trafficDate(report.available_from, true) }}. Earlier dates are unavailable.</p>
        <section v-for="panel in panels" v-show="activeTab === panel.value" :id="`${tabsId}-${panel.value}-panel`" :key="panel.value" role="tabpanel" :aria-labelledby="`${tabsId}-${panel.value}-tab`" tabindex="0" class="mt-6 rounded-sm focus-visible:outline-2 focus-visible:outline-blue-500">
          <div v-if="panel.value === 'api'" class="mb-6 space-y-4">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0 text-sm leading-6 text-gray-500 dark:text-gray-400">

                <p class="break-words">Prefixes: <span class="font-mono text-gray-700 dark:text-gray-300">{{ report.api.settings.prefixes.join(', ') || 'None' }}</span> · {{ report.api.settings.routes.length }} normalized {{ report.api.settings.routes.length === 1 ? 'route' : 'routes' }}</p>
                <p v-if="report.api.effective_from">Current settings apply from {{ trafficDate(report.api.effective_from, true) }}.</p>
              </div>
              <AppButton variant="secondary" class="h-10" :disabled="saving || report.site.pending" @click="openAPISettings">Configure API paths</AppButton>
            </div>
            <NoticeBanner v-if="report.api.incomplete || report.api.buckets.some(bucket => bucket.incomplete)" message="Some API traffic data is incomplete or capped. Counts may be lower than actual traffic; unavailable hours are not zero requests." />
            <p class="text-sm leading-6 text-gray-500 dark:text-gray-400">{{ report.api.available_from ? `API history starts ${trafficDate(report.api.available_from, true)}; earlier requests are unavailable.` : 'No API history yet. Earlier overview data cannot be reclassified.' }}</p>
          </div>
          <p v-if="panel.value === 'pages'" class="text-sm leading-6 text-gray-500 dark:text-gray-400">Estimated page requests, excluding known bots, API paths and assets; not unique visitors.</p>
          <p v-if="panel.value === 'overview' && (!apiHasCoverage || report.api.gaps.length || report.api.incomplete)" class="text-sm leading-6 text-gray-500 dark:text-gray-400">API totals cover collected periods only. See API for gaps.</p>
          <p v-if="!panel.hasCoverage" class="text-sm text-gray-600 dark:text-gray-400">{{ panel.value === 'api' ? 'No collected API data in this range; missing periods are unavailable.' : 'No collected data in this range. Turn on collection to start; earlier traffic is unavailable.' }}</p>
          <div class="mt-6 grid grid-cols-2 gap-x-4 gap-y-6 xl:grid-cols-4">
            <div v-for="metric in panel.metrics" :key="metric.label">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ panel.hasCoverage ? metric.value : '—' }}</p>
            </div>
          </div>
          <div class="mt-6">
            <p class="mb-3 text-sm font-medium text-gray-700 dark:text-gray-300">{{ periodLabel }} totals</p>
            <TrafficHourlyChart :bucket-seconds="bucketSeconds" :buckets="panel.buckets" :gaps="panel.gaps" :from="report.from" :to="report.to" :mode="panel.value" />
            <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ trafficCoverageNote }}</p>
          </div>
          <p v-if="panel.value === 'api'" class="mt-3 text-sm leading-6 text-gray-500 dark:text-gray-400">HTTP status is not application success: even a 200 may contain an error.</p>
          <details class="mt-5 text-sm" @toggle="hourlyOpen[panel.value] = ($event.target as HTMLDetailsElement).open">
            <summary class="w-fit cursor-pointer rounded-sm font-medium text-blue-600 focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-blue-400">{{ periodLabel }} values</summary>
            <div v-if="hourlyOpen[panel.value]" class="mt-3"><DataTable :columns="panel.value === 'api' ? apiHourlyColumns : panel.value === 'pages' ? pageHourlyColumns : hourlyColumns" :items="panel.buckets" :aria-label="`${periodLabel} ${panel.value === 'api' ? 'API traffic' : panel.value === 'pages' ? 'estimated page views' : 'traffic'} values in UTC`" scroll-class="max-h-80" sticky-header empty-text="No data available in this range.">
              <template #hour="{ item }">{{ trafficDate(item.hour, true) }}</template>
              <template #requests="{ item }">{{ trafficCollected(item) ? trafficCount(item.metrics.requests) : '—' }}</template>
              <template #page_views="{ item }">{{ trafficCollected(item) ? trafficCount(item.metrics.page_views) : '—' }}</template>
              <template #http_errors="{ item }">{{ trafficCollected(item) ? trafficCount(item.metrics.status_4xx + item.metrics.status_5xx) : '—' }}</template>
              <template #collection="{ item }">{{ item.incomplete ? 'Incomplete' : item.collection === 'partial' ? 'Partial period' : item.collection === 'disabled' ? 'Collection disabled' : item.collection === 'unknown' ? 'Unavailable' : 'Collected' }}</template>
            </DataTable></div>
          </details>
          <details v-if="panel.gaps.length" class="mt-4 text-sm">
            <summary class="w-fit cursor-pointer rounded-sm font-medium text-gray-600 focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-gray-400">Collection gaps ({{ panel.gaps.length }})</summary>
            <ul class="mt-3 space-y-2 text-sm text-gray-500 dark:text-gray-400"><li v-for="(gap, index) in panel.gaps" :key="index">{{ trafficDate(gap.from, true) }} – {{ trafficDate(gap.to, true) }}: {{ gap.reason }}</li></ul>
          </details>
        </section>
      </Card>

      <div v-if="activeTab === 'pages'" class="grid min-w-0 gap-6 xl:grid-cols-2">
        <Card class="min-w-0">
          <h2 class="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">Most requested page paths</h2>
          <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">Page paths only; query strings are excluded.</p>
          <DataTable :columns="pathColumns" :items="report.top_paths" empty-text="No page paths in this range." aria-label="Most requested page paths"><template #value="{ item }"><span class="block max-w-64 truncate font-mono text-sm" :title="item.value">{{ item.value }}</span></template><template #requests="{ item }">{{ trafficCount(item.requests) }}</template></DataTable>
        </Card>
        <Card class="min-w-0">
          <h2 class="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">Referrer domains</h2>
          <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">Missing referrers may be direct traffic, apps or privacy settings.</p>
          <DataTable :columns="referrerColumns" :items="report.referrers" empty-text="No referrers in this range." aria-label="Traffic referrer domains"><template #value="{ item }"><span class="block max-w-64 truncate" :title="item.value">{{ item.value || 'No referrer' }}</span></template><template #requests="{ item }">{{ trafficCount(item.requests) }}</template></DataTable>
        </Card>
      </div>

      <Card v-if="activeTab === 'api'">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Top API endpoints</h2>
        <p class="mb-4 mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400">Configured routes group matching paths; query strings are excluded. Totals include every matching request.</p>
        <DataTable :columns="endpointColumns" :items="report.api.endpoints" :empty-text="apiHasCoverage ? 'No API requests in this range.' : 'API endpoint history is unavailable in this range.'" aria-label="API endpoints and HTTP response status groups">
          <template #method="{ item }"><span class="font-mono text-sm font-semibold">{{ item.method }}</span></template>
          <template #path="{ item }"><span class="block max-w-72 truncate font-mono text-sm" :title="item.path">{{ item.path }}</span></template>
          <template v-for="status in apiCountKeys" :key="status" #[status]="{ item }">{{ trafficCount(item.metrics[status]) }}</template>
          <template #avg_request_time_ms="{ item }">{{ item.metrics.requests ? trafficMilliseconds(item.metrics.avg_request_time_ms) : '—' }}</template>
        </DataTable>
        <details class="mt-4 text-sm leading-6 text-gray-500 dark:text-gray-400"><summary class="w-fit cursor-pointer font-medium focus-visible:outline-2 focus-visible:outline-blue-500">Endpoint details</summary><p class="mt-2">Up to 100 groups are shown; (other) includes endpoints beyond the limit. Unconfigured matching paths keep their sanitized path. Request time is weighted by request count.</p><ul class="mt-2 space-y-2"><li v-for="(note, index) in report.api.notes" :key="index">{{ note }}</li></ul></details>
      </Card>

      <Card>
        <details><summary class="w-fit cursor-pointer text-sm font-medium focus-visible:outline-2 focus-visible:outline-blue-500">About these numbers</summary>
        <ul class="mt-3 space-y-2 text-sm leading-6 text-gray-500 dark:text-gray-400">
          <li>Estimated page views filter known bots, configured API requests, common API paths and non-page assets. They count page requests without identifying people; they are not unique visitors or sessions.</li>
          <li>Nginx response bytes measure data sent by this server. Average Nginx request time measures server request handling, including upstream time; it does not measure browser page speed.</li>
          <li>Collection gaps are unavailable data. A partial hour can contain both collected requests and a gap. No IP, session or location tracking.</li>
          <li v-for="(note, index) in report.notes" :key="index">{{ note }}</li>
        </ul></details>
      </Card>

      <Card>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div><h2 class="font-semibold text-gray-900 dark:text-gray-100">Analytics history</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">History follows the <router-link to="/observe/traffic" class="text-blue-600 hover:underline dark:text-blue-400">server retention policy</router-link>, including while collection is disabled.</p></div>
          <AppButton variant="danger" class="h-10" :loading="deleting" :disabled="saving || (report.site.pending && !(report.site.status === 'activated' && !report.site.effective_enabled))" @click="deleteHistory">Delete analytics history</AppButton>
        </div>
      </Card>
    </template>
    <BaseModal v-model="apiSettingsOpen" title="Configure API paths" max-width="max-w-2xl" :loading="saving" :confirm-disabled="!!settingsValidation.errors.prefixes || !!settingsValidation.errors.routes" confirm-text="Save API paths" @submit="saveAPISettings">
      <form :id="`${tabsId}-settings`" class="space-y-5" @submit.prevent="saveAPISettings">
        <p class="text-sm leading-6 text-gray-600 dark:text-gray-400">Only new requests use these settings. History keeps its original classification.</p>
        <ErrorAlert :message="settingsError" />
        <FormGroup label="Literal API prefixes" :for-attr="`${tabsId}-prefixes`" hint="One per line, up to 16. /api matches itself and descendants, not /apiculture. Use / for all paths." :error="settingsValidation.errors.prefixes">
          <textarea :id="`${tabsId}-prefixes`" v-model="prefixesText" rows="4" :disabled="saving" :aria-invalid="!!settingsValidation.errors.prefixes" :aria-describedby="`${tabsId}-prefixes-hint${settingsValidation.errors.prefixes ? ` ${tabsId}-prefixes-error` : ''}`" spellcheck="false" autocapitalize="off" class="w-full rounded-lg border border-gray-300 bg-white p-3 font-mono text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" placeholder="/api" />
        </FormGroup>
        <FormGroup label="Explicit normalized routes" :for-attr="`${tabsId}-routes`" hint="One per line, up to 32. Example: /api/users/:id. Each parameter matches one segment, including outside your prefixes. No wildcards or guessed routes." :error="settingsValidation.errors.routes">
          <textarea :id="`${tabsId}-routes`" v-model="routesText" rows="5" :disabled="saving" :aria-invalid="!!settingsValidation.errors.routes" :aria-describedby="`${tabsId}-routes-hint${settingsValidation.errors.routes ? ` ${tabsId}-routes-error` : ''}`" spellcheck="false" autocapitalize="off" class="w-full rounded-lg border border-gray-300 bg-white p-3 font-mono text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" placeholder="/api/users/:id" />
        </FormGroup>
        <p class="text-sm leading-6 text-gray-500 dark:text-gray-400">No query strings, fragments or trailing slash (except /); maximum 512 bytes per path. Clearing both lists stops future API classification.</p>
      </form>
      <template #footer><AppButton variant="secondary" class="h-10" :disabled="saving" @click="apiSettingsOpen = false">Cancel</AppButton><AppButton type="submit" class="h-10" :form="`${tabsId}-settings`" :loading="saving" :disabled="!!settingsValidation.errors.prefixes || !!settingsValidation.errors.routes">Save API paths</AppButton></template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';
import { useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';
import FormGroup from '../../components/FormGroup.vue';
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
import type { TrafficRange, TrafficReport } from '../../types/trafficAnalytics';
import { trafficAPISettings, trafficBytes, trafficCollected, trafficCount, trafficCoverageNote, trafficDate, trafficError, trafficIsDelayed, trafficMilliseconds, trafficPollDelay, trafficPeriodLabel, trafficRangeOptions, trafficStatus } from '../../utils/trafficAnalytics';

const route = useRoute();
const { confirm } = useConfirm();
const id = computed(() => Number(route.params.id));
const report = ref<TrafficReport | null>(null);
const range = ref<TrafficRange>('today');
const loading = ref(false);
const saving = ref(false);
const deleting = ref(false);
type TrafficTab = 'overview' | 'pages' | 'api';
const tabsId = `traffic-${useId()}`;
const tabs: { value: TrafficTab; label: string }[] = [{ value: 'overview', label: 'Overview' }, { value: 'pages', label: 'Pages' }, { value: 'api', label: 'API traffic' }];
const activeTab = ref<TrafficTab>('overview');
const hourlyOpen = ref({ overview: false, pages: false, api: false });
const apiSettingsOpen = ref(false);
const prefixesText = ref('/api');
const routesText = ref('');
const settingsError = ref('');
const settingsValidation = computed(() => trafficAPISettings(prefixesText.value, routesText.value));
const error = ref('');
let request = 0;
const ranges = computed(() => trafficRangeOptions(report.value?.retention_days ?? 30));
const bucketSeconds = computed(() => report.value?.bucket_seconds ?? 3600);
const periodLabel = computed(() => trafficPeriodLabel(bucketSeconds.value));
const historyStartsLater = computed(() => !!report.value?.available_from && Date.parse(report.value.available_from) > Date.parse(report.value.from));
const periodColumn = computed(() => periodLabel.value === 'Hourly' ? 'Hour (UTC)' : periodLabel.value === 'Daily' ? 'Date (UTC)' : 'Period from (UTC)');
const pathColumns = [{ key: 'value', label: 'Path' }, { key: 'requests', label: 'Requests' }];
const referrerColumns = [{ key: 'value', label: 'Domain' }, { key: 'requests', label: 'Requests' }];
const hourlyColumns = computed(() => [{ key: 'hour', label: periodColumn.value }, { key: 'requests', label: 'Requests' }, { key: 'page_views', label: 'Est. page views' }, { key: 'collection', label: 'Coverage' }]);
const pageHourlyColumns = computed(() => hourlyColumns.value.filter(column => column.key !== 'requests'));
const apiHourlyColumns = computed(() => [{ key: 'hour', label: periodColumn.value }, { key: 'requests', label: 'API requests' }, { key: 'http_errors', label: 'HTTP 4xx + 5xx' }, { key: 'collection', label: 'Coverage' }]);
const apiCountKeys = ['requests', 'status_1xx', 'status_2xx', 'status_3xx', 'status_4xx', 'status_5xx'] as const;
const endpointColumns = [{ key: 'method', label: 'Method' }, { key: 'path', label: 'Path group' }, { key: 'requests', label: 'Requests' }, { key: 'status_1xx', label: '1xx' }, { key: 'status_2xx', label: '2xx' }, { key: 'status_3xx', label: '3xx' }, { key: 'status_4xx', label: '4xx' }, { key: 'status_5xx', label: '5xx' }, { key: 'avg_request_time_ms', label: 'Avg request time' }];
const apiHasCoverage = computed(() => !!report.value?.api.buckets.some(trafficCollected));
const delayed = computed(() => !!report.value && trafficIsDelayed(report.value.site));
const metrics = computed(() => {
  const totals = report.value?.totals;
  if (!totals) return [];
  return [
    { label: 'Total origin requests', value: trafficCount(totals.requests) },
    { label: 'Estimated page views', value: trafficCount(totals.page_views) },
    { label: 'API requests', value: apiHasCoverage.value && report.value ? trafficCount(report.value.api.totals.requests) : '—' },
    { label: 'HTTP 4xx + 5xx responses', value: trafficCount(totals.not_found + totals.other_4xx + totals.server_errors) },
    { label: 'Nginx response bytes', value: trafficBytes(totals.response_bytes) },
    { label: 'Avg Nginx request time', value: trafficMilliseconds(totals.avg_request_time_ms) },
  ];
});
const apiMetrics = computed(() => {
  const totals = report.value?.api.totals;
  if (!totals) return [];
  return [
    { label: 'API requests', value: trafficCount(totals.requests) },
    { label: '2xx successful responses', value: trafficCount(totals.status_2xx) },
    { label: '4xx client error responses', value: trafficCount(totals.status_4xx) },
    { label: '5xx server error responses', value: trafficCount(totals.status_5xx) },
    { label: '1xx informational responses', value: trafficCount(totals.status_1xx) },
    { label: '3xx redirection responses', value: trafficCount(totals.status_3xx) },
    { label: 'Nginx response bytes', value: trafficBytes(totals.response_bytes) },
    { label: 'Avg Nginx request time', value: totals.requests ? trafficMilliseconds(totals.avg_request_time_ms) : '—' },
  ];
});
const panels = computed(() => [
  { value: 'overview' as const, buckets: report.value?.buckets || [], gaps: report.value?.gaps || [], metrics: metrics.value, hasCoverage: !!report.value?.buckets.some(trafficCollected) },
  { value: 'pages' as const, buckets: report.value?.buckets || [], gaps: report.value?.gaps || [], metrics: metrics.value.filter(metric => metric.label === 'Estimated page views'), hasCoverage: !!report.value?.buckets.some(trafficCollected) },
  { value: 'api' as const, buckets: report.value?.api.buckets || [], gaps: report.value?.api.gaps || [], metrics: apiMetrics.value, hasCoverage: apiHasCoverage.value },
]);
const onTabKeydown = (event: KeyboardEvent) => {
  const index = tabs.findIndex(tab => tab.value === activeTab.value);
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : event.key === 'ArrowRight' ? (index + 1) % tabs.length : event.key === 'ArrowLeft' ? (index + tabs.length - 1) % tabs.length : -1;
  if (next < 0) return;
  event.preventDefault();
  activeTab.value = tabs[next]!.value;
  (event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
};
const openAPISettings = () => {
  if (!report.value) return;
  prefixesText.value = report.value.api.settings.prefixes.join('\n');
  routesText.value = report.value.api.settings.routes.join('\n');
  settingsError.value = '';
  apiSettingsOpen.value = true;
};
const saveAPISettings = async () => {
  if (saving.value || settingsValidation.value.errors.prefixes || settingsValidation.value.errors.routes) return;
  const siteId = id.value;
  saving.value = true; request++; settingsError.value = '';
  try {
    const result = await apiClient.setSiteTrafficAPISettings(siteId, settingsValidation.value.settings);
    if (report.value && id.value === siteId) {
      report.value.api.settings = result.settings;
      report.value.api.effective_from = result.effective_from;
      apiSettingsOpen.value = false;
    }
  } catch (err) { if (id.value === siteId) settingsError.value = trafficError(err); }
  finally { saving.value = false; await refresh(); }
};
const load = async () => {
  if (saving.value) return;
  const current = ++request;
  loading.value = true;
  try {
    const result = await apiClient.getSiteTrafficAnalytics(id.value, range.value);
    if (request === current) {
      const allowed = trafficRangeOptions(result.retention_days);
      if (!allowed.some(option => option.value === range.value)) {
        // Do not leave an out-of-policy report under a newly selected label.
        report.value = null;
        range.value = allowed.at(-1)?.value ?? 'today';
        return;
      }
      report.value = result;
    }
  } catch (err) { if (request === current) error.value = trafficError(err); }
  finally { if (request === current) loading.value = false; }
};
const { refresh } = useTrafficPolling(load, () => trafficPollDelay(!!report.value?.site.pending, !!report.value?.site.effective_enabled));
const manualRefresh = () => { error.value = ''; void refresh(); };
watch([id, range], ([siteId], [previousId]) => {
  request++; error.value = '';
  if (siteId !== previousId) { report.value = null; apiSettingsOpen.value = false; activeTab.value = 'overview'; }
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
