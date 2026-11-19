<template>
  <div>
    <svg viewBox="0 0 720 212" class="w-full text-gray-400 dark:text-gray-500" role="img" :aria-labelledby="`${chartId}-title ${chartId}-description`">
      <title :id="`${chartId}-title`">{{ api ? 'Hourly API requests and HTTP 4xx + 5xx responses, UTC' : pagesOnly ? 'Hourly estimated page views, UTC' : 'Hourly requests and estimated page views, UTC' }}</title>
      <desc :id="`${chartId}-description`">{{ api ? 'Blue shows API requests; red shows HTTP 4xx + 5xx responses. Both lines use the same scale.' : pagesOnly ? 'Green shows estimated page views.' : 'Blue shows requests; green shows estimated page views. Both lines use the same scale.' }} Blank spans mean collection was disabled or unavailable. Dots with an outline mark partial hours. Hourly values are available below.</desc>
      <line x1="44" y1="20" x2="704" y2="20" stroke="currentColor" stroke-opacity="0.15" />
      <line x1="44" y1="96" x2="704" y2="96" stroke="currentColor" stroke-opacity="0.15" />
      <line x1="44" y1="172" x2="704" y2="172" stroke="currentColor" stroke-opacity="0.3" />
      <text x="36" y="24" text-anchor="end" font-size="10" fill="currentColor">{{ trafficCount(requests.max) }}</text>
      <text x="36" y="176" text-anchor="end" font-size="10" fill="currentColor">0</text>
      <g v-for="(gap, index) in bands" :key="index">
        <rect :x="gap.x" y="20" :width="gap.width" height="152" fill="currentColor" fill-opacity="0.08"><title>{{ trafficDate(gap.from) }} – {{ trafficDate(gap.to) }}: {{ gap.reason }}</title></rect>
      </g>
      <polyline v-for="(run, index) in requests.runs" :key="`r-${index}`" :points="run" fill="none" :stroke="primaryColor" stroke-width="2" vector-effect="non-scaling-stroke" />
      <polyline v-for="(run, index) in visibleSecondary.runs" :key="`s-${index}`" :points="run" fill="none" :stroke="secondaryColor" stroke-width="2" vector-effect="non-scaling-stroke" />
      <circle v-for="point in requests.points" :key="point.hour" :cx="point.x" :cy="point.y" :r="point.partial ? 3.5 : 2" :fill="primaryColor" :stroke="point.partial ? 'currentColor' : 'none'" stroke-width="1.5"><title>{{ trafficDate(point.hour) }}: {{ trafficCount(point.value) }} {{ primaryLabel }}{{ point.partial ? ' (partial hour)' : '' }}</title></circle>
      <circle v-for="point in visibleSecondary.points" :key="`s-${point.hour}`" :cx="point.x" :cy="point.y" :r="point.partial ? 3.5 : 2" :fill="secondaryColor" :stroke="point.partial ? 'currentColor' : 'none'" stroke-width="1.5"><title>{{ trafficDate(point.hour) }}: {{ trafficCount(point.value) }} {{ secondaryLabel }}{{ point.partial ? ' (partial hour)' : '' }}</title></circle>
      <text x="44" y="200" font-size="10" fill="currentColor">{{ trafficDate(from) }}</text>
      <text x="704" y="200" text-anchor="end" font-size="10" fill="currentColor">{{ trafficDate(to) }}</text>
    </svg>
    <div class="flex flex-wrap gap-x-5 gap-y-2 text-xs text-gray-500 dark:text-gray-400">
      <span><span class="mr-1.5 inline-block h-2 w-2 rounded-full" :class="pagesOnly ? 'bg-emerald-500' : 'bg-blue-500'" />{{ primaryLabel }}</span>
      <span v-if="!pagesOnly"><span class="mr-1.5 inline-block h-2 w-2 rounded-full" :class="api ? 'bg-red-500' : 'bg-emerald-500'" />{{ secondaryLabel }}</span>
      <span>Shaded spans: collection gap · Outlined dots: partial hour</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue';
import type { TrafficAPIBucket, TrafficBucket, TrafficGap } from '../types/trafficAnalytics';
import { trafficCount, trafficDate, trafficGapBands, trafficSeries } from '../utils/trafficAnalytics';
const props = defineProps<{ buckets: (TrafficBucket | TrafficAPIBucket)[]; gaps: TrafficGap[]; from: string; to: string; mode?: 'overview' | 'pages' | 'api' }>();
const chartId = useId();
const api = computed(() => props.mode === 'api');
const pagesOnly = computed(() => props.mode === 'pages');
const primaryColor = computed(() => pagesOnly.value ? 'var(--color-emerald-500)' : 'var(--color-blue-500)');
const primaryLabel = computed(() => api.value ? 'API requests' : pagesOnly.value ? 'Estimated page views' : 'Requests');
const requests = computed(() => trafficSeries(props.buckets, pagesOnly.value ? 'page_views' : 'requests', props.from, props.to));
const secondary = computed(() => trafficSeries(props.buckets, api.value ? 'http_errors' : 'page_views', props.from, props.to, requests.value.max));
const visibleSecondary = computed(() => pagesOnly.value ? { runs: [], points: [] } : secondary.value);
const secondaryColor = computed(() => api.value ? 'var(--color-red-500)' : 'var(--color-emerald-500)');
const secondaryLabel = computed(() => api.value ? 'HTTP 4xx + 5xx responses' : 'Estimated page views');
const bands = computed(() => trafficGapBands(props.gaps, props.from, props.to));
</script>
