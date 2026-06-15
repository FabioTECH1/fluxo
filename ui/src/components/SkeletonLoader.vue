<template>
  <div class="animate-pulse bg-gray-200 dark:bg-gray-800 rounded-lg" :class="[customClass, dimensions]">
    <template v-if="type === 'card'">
      <div class="h-6 bg-gray-300 dark:bg-gray-700 rounded w-1/3 mb-4"></div>
      <div class="space-y-3">
        <div class="h-4 bg-gray-300 dark:bg-gray-700 rounded w-full"></div>
        <div class="h-4 bg-gray-300 dark:bg-gray-700 rounded w-5/6"></div>
        <div class="h-4 bg-gray-300 dark:bg-gray-700 rounded w-2/3"></div>
      </div>
    </template>
    <template v-else-if="type === 'table'">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex items-center justify-between">
        <div class="h-5 bg-gray-300 dark:bg-gray-700 rounded w-1/4"></div>
        <div class="h-5 bg-gray-300 dark:bg-gray-700 rounded w-8"></div>
      </div>
      <div class="divide-y divide-gray-100 dark:divide-gray-800">
        <div v-for="i in rows" :key="i" class="px-6 py-4 flex items-center justify-between">
          <div class="space-y-2 flex-1">
            <div class="h-4 bg-gray-300 dark:bg-gray-700 rounded w-1/3"></div>
            <div class="h-3 bg-gray-300 dark:bg-gray-700 rounded w-1/2"></div>
          </div>
          <div class="h-4 bg-gray-300 dark:bg-gray-700 rounded w-16"></div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{
  type?: 'line' | 'block' | 'card' | 'table';
  rows?: number;
  height?: string;
  width?: string;
  customClass?: string;
}>(), {
  type: 'block',
  rows: 3,
  height: '',
  width: '',
  customClass: '',
});

const dimensions = computed(() => {
  if (props.type === 'card') return 'p-6 h-auto w-full border border-gray-100 dark:border-gray-800 bg-white dark:bg-gray-900';
  if (props.type === 'table') return 'h-auto w-full border border-gray-100 dark:border-gray-800 bg-white dark:bg-gray-900';
  
  const h = props.height || (props.type === 'line' ? 'h-4' : 'h-12');
  const w = props.width || 'w-full';
  return `${h} ${w}`;
});
</script>
