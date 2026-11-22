<template>
  <div
    :role="tone === 'error' ? 'alert' : 'status'"
    class="rounded-lg border border-gray-200 bg-white p-4 text-sm text-gray-950 dark:border-gray-700/80 dark:bg-gray-900 dark:text-gray-50"
  >
    <div class="flex items-start gap-3">
      <span
        class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
        :class="tone === 'error' ? 'bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-400' : tone === 'warning' ? 'bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-400' : 'bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-400'"
        aria-hidden="true"
      >
        <XCircleIcon v-if="tone === 'error'" class="h-5 w-5" />
        <ExclamationTriangleIcon v-else-if="tone === 'warning'" class="h-5 w-5" />
        <ArrowUpTrayIcon v-else class="h-5 w-5" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="flex flex-col items-start gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p class="min-w-0 flex-1 font-medium leading-6 [overflow-wrap:anywhere]">{{ message }}</p>
          <div v-if="$slots.actions" class="flex flex-wrap items-center gap-3 sm:shrink-0">
            <slot name="actions" />
          </div>
        </div>
        <slot />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ArrowUpTrayIcon, XCircleIcon, ExclamationTriangleIcon } from '@heroicons/vue/24/outline';
withDefaults(defineProps<{
  message: string;
  tone?: 'error' | 'info' | 'warning';
}>(), { tone: 'info' });
</script>
