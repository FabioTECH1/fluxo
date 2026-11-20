<template>
  <span class="inline-flex items-baseline justify-end gap-1 whitespace-nowrap text-xs text-gray-600 dark:text-gray-300" :aria-label="duration === '—' ? 'Duration unavailable' : `Duration ${duration}`">
    <span class="text-gray-500 dark:text-gray-400">Duration</span>
    <span class="font-medium tabular-nums">{{ duration }}</span>
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { deploymentDuration, type DeploymentTiming } from '../utils/deploymentPresentation';
const props = defineProps<{ deployment: DeploymentTiming; now: number }>();
const duration = computed(() => deploymentDuration(props.deployment, props.now));
</script>
