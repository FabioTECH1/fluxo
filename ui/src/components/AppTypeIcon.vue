<template>
  <span
    class="flex shrink-0 items-center justify-center rounded-lg"
    :class="[containerSizeClass, colorClass]"
    aria-hidden="true"
  >
    <svg v-if="normalizedType === 'laravel'" :class="iconSizeClass" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M12 3l7 4v10l-7 4-7-4V7l7-4z" />
      <path stroke-linecap="round" stroke-linejoin="round" d="M9 8v8h6" />
    </svg>
    <svg v-else-if="normalizedType === 'php'" :class="iconSizeClass" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M8 9l-3 3 3 3" />
      <path stroke-linecap="round" stroke-linejoin="round" d="M16 9l3 3-3 3" />
      <path stroke-linecap="round" stroke-linejoin="round" d="M13 7l-2 10" />
    </svg>
    <svg v-else-if="normalizedType === 'html'" :class="iconSizeClass" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M7 4h10l-1 16-4 1-4-1L7 4z" />
      <path stroke-linecap="round" stroke-linejoin="round" d="M9 8h6M10 12h4" />
    </svg>
    <span
      v-else-if="normalizedType === 'wordpress'"
      class="flex items-center justify-center rounded-full border-2 border-current font-serif font-bold"
      :class="wordpressSizeClass"
    >W</span>
    <svg v-else-if="normalizedType === 'node'" :class="iconSizeClass" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M6 8h12v8H6z" />
      <path stroke-linecap="round" stroke-linejoin="round" d="M9 8V5m6 3V5M9 19v-3m6 3v-3M6 11H3m3 4H3m18-4h-3m3 4h-3" />
    </svg>
    <svg v-else :class="iconSizeClass" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <rect x="5" y="5" width="14" height="14" rx="2" />
      <path stroke-linecap="round" d="M8 9h8M8 13h5" />
    </svg>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{
  appType: string;
  size?: 'md' | 'lg';
}>(), {
  size: 'md',
});

const normalizedType = computed(() => props.appType.toLowerCase().trim());

const colorClass = computed(() => ({
  laravel: 'bg-red-50 text-red-600 dark:bg-red-950/30 dark:text-red-300',
  php: 'bg-indigo-50 text-indigo-600 dark:bg-indigo-950/30 dark:text-indigo-300',
  html: 'bg-orange-50 text-orange-600 dark:bg-orange-950/30 dark:text-orange-300',
  node: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-950/30 dark:text-emerald-300',
  wordpress: 'bg-sky-50 text-sky-700 dark:bg-sky-950/30 dark:text-sky-300',
}[normalizedType.value] || 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'));

const containerSizeClass = computed(() => props.size === 'lg' ? 'h-12 w-12' : 'h-10 w-10');
const iconSizeClass = computed(() => props.size === 'lg' ? 'h-6 w-6' : 'h-5 w-5');
const wordpressSizeClass = computed(() => props.size === 'lg' ? 'h-7 w-7 text-base' : 'h-6 w-6 text-sm');
</script>
