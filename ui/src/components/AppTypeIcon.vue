<template>
  <span
    class="flex shrink-0 items-center justify-center rounded-lg"
    :class="[containerSizeClass, colorClass]"
    aria-hidden="true"
  >
    <svg v-if="normalizedType === 'laravel'" :class="iconSizeClass" fill="currentColor" viewBox="0 0 24 24">
      <path d="M23.642 5.43a.364.364 0 0 1 .014.1v5.149c0 .135-.073.26-.189.326l-4.323 2.49v4.934a.378.378 0 0 1-.188.326L9.93 23.949a.316.316 0 0 1-.066.027c-.008.002-.016.008-.024.01a.348.348 0 0 1-.192 0c-.011-.002-.02-.008-.03-.012-.02-.008-.042-.014-.062-.025L.533 18.755a.376.376 0 0 1-.189-.326V2.974c0-.033.005-.066.014-.098.003-.012.01-.02.014-.032a.369.369 0 0 1 .023-.058c.004-.013.015-.022.023-.033l.033-.045c.012-.01.025-.018.037-.027.014-.012.027-.024.041-.034H.53L5.043.05a.375.375 0 0 1 .375 0L9.93 2.647h.002c.015.01.027.021.04.033l.038.027c.013.014.02.03.033.045.008.011.02.021.025.033.01.02.017.038.024.058.003.011.01.021.013.032.01.031.014.064.014.098v9.652l3.76-2.164V5.527c0-.033.004-.066.013-.098.003-.01.01-.02.013-.032a.487.487 0 0 1 .024-.059c.007-.012.018-.02.025-.033.012-.015.021-.03.033-.043.012-.012.025-.02.037-.028.014-.01.026-.023.041-.032h.001l4.513-2.598a.375.375 0 0 1 .375 0l4.513 2.598c.016.01.027.021.042.031.012.01.025.018.036.028.013.014.022.03.034.044.008.012.019.021.024.033.011.02.018.04.024.06.006.01.012.021.015.032Zm-.74 5.032V6.179l-1.578.908-2.182 1.256v4.283Zm-4.51 7.75v-4.287l-2.147 1.225-6.126 3.498v4.325ZM1.093 3.624v14.588l8.273 4.761v-4.325l-4.322-2.445-.002-.003H5.04c-.014-.01-.025-.021-.04-.031-.011-.01-.024-.018-.035-.027l-.001-.002c-.013-.012-.021-.025-.031-.04-.01-.011-.021-.022-.028-.036h-.002c-.008-.014-.013-.031-.02-.047-.006-.016-.014-.027-.018-.043a.49.49 0 0 1-.008-.057c-.002-.014-.006-.027-.006-.041V5.789l-2.18-1.257ZM5.23.81 1.47 2.974l3.76 2.164 3.758-2.164Zm1.956 13.505 2.182-1.256V3.624l-1.58.91-2.182 1.255v9.435Zm11.581-10.95-3.76 2.163 3.76 2.163 3.759-2.164Zm-.376 4.978L16.21 7.087 14.63 6.18v4.283l2.182 1.256 1.58.908Zm-8.65 9.654 5.514-3.148 2.756-1.572-3.757-2.163-4.323 2.489-3.941 2.27Z" />
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
