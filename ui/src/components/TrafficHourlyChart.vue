<template>
  <div>
    <svg viewBox="0 0 720 212" class="w-full text-gray-400 dark:text-gray-500" role="img" :aria-labelledby="`${chartId}-title ${chartId}-description`">
      <title :id="`${chartId}-title`">Hourly requests and estimated page views, UTC</title>
      <desc :id="`${chartId}-description`">Blue shows requests; green shows estimated page views. Blank spans mean collection was disabled or unavailable. Dots with an outline mark partial hours. Hourly values are available below.</desc>
      <line x1="44" y1="20" x2="704" y2="20" stroke="currentColor" stroke-opacity="0.15" />
      <line x1="44" y1="96" x2="704" y2="96" stroke="currentColor" stroke-opacity="0.15" />
      <line x1="44" y1="172" x2="704" y2="172" stroke="currentColor" stroke-opacity="0.3" />
      <text x="36" y="24" text-anchor="end" font-size="10" fill="currentColor">{{ trafficCount(requests.max) }}</text>
      <text x="36" y="176" text-anchor="end" font-size="10" fill="currentColor">0</text>
      <g v-for="(gap, index) in bands" :key="index">
        <rect :x="gap.x" y="20" :width="gap.width" height="152" fill="currentColor" fill-opacity="0.08"><title>{{ trafficDate(gap.from) }} – {{ trafficDate(gap.to) }}: {{ gap.reason }}</title></rect>
      </g>
      <polyline v-for="(run, index) in requests.runs" :key="`r-${index}`" :points="run" fill="none" stroke="var(--color-blue-500)" stroke-width="2" vector-effect="non-scaling-stroke" />
      <polyline v-for="(run, index) in pages.runs" :key="`p-${index}`" :points="run" fill="none" stroke="var(--color-emerald-500)" stroke-width="2" vector-effect="non-scaling-stroke" />
      <circle v-for="point in requests.points" :key="point.hour" :cx="point.x" :cy="point.y" :r="point.partial ? 3.5 : 2" fill="var(--color-blue-500)" :stroke="point.partial ? 'currentColor' : 'none'" stroke-width="1.5"><title>{{ trafficDate(point.hour) }}: {{ trafficCount(point.value) }} requests{{ point.partial ? ' (partial hour)' : '' }}</title></circle>
      <circle v-for="point in pages.points" :key="`p-${point.hour}`" :cx="point.x" :cy="point.y" r="2" fill="var(--color-emerald-500)"><title>{{ trafficDate(point.hour) }}: {{ trafficCount(point.value) }} estimated page views</title></circle>
      <text x="44" y="200" font-size="10" fill="currentColor">{{ trafficDate(from) }}</text>
      <text x="704" y="200" text-anchor="end" font-size="10" fill="currentColor">{{ trafficDate(to) }}</text>
    </svg>
    <div class="flex flex-wrap gap-x-5 gap-y-2 text-xs text-gray-500 dark:text-gray-400">
      <span><span class="mr-1.5 inline-block h-2 w-2 rounded-full bg-blue-500" />Requests</span>
      <span><span class="mr-1.5 inline-block h-2 w-2 rounded-full bg-emerald-500" />Estimated page views</span>
      <span>Shaded spans: collection gap · Outlined dots: partial hour</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue';
import type { TrafficBucket, TrafficGap } from '../types/trafficAnalytics';
import { trafficCount, trafficDate, trafficGapBands, trafficSeries } from '../utils/trafficAnalytics';
const props = defineProps<{ buckets: TrafficBucket[]; gaps: TrafficGap[]; from: string; to: string }>();
const chartId = useId();
const requests = computed(() => trafficSeries(props.buckets, 'requests', props.from, props.to));
// Both lines use the requests scale, so their heights remain directly comparable.
const pages = computed(() => {
  const series = trafficSeries(props.buckets, 'page_views', props.from, props.to);
  const ratio = series.max / requests.value.max;
  return { ...series, points: series.points.map(point => ({ ...point, y: 172 - (172 - point.y) * ratio })), runs: series.runs.map(run => run.split(' ').map(pair => {
    const [x, y] = pair.split(',');
    return `${x},${172 - (172 - Number(y)) * ratio}`;
  }).join(' ')) };
});
const bands = computed(() => trafficGapBands(props.gaps, props.from, props.to));
</script>
