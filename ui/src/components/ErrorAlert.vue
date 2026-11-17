<template>
  <div v-if="message" role="alert" class="mb-4 rounded-lg border border-gray-200 bg-white p-4 text-sm text-gray-950 dark:border-gray-700/80 dark:bg-gray-900 dark:text-gray-50">
    <div class="flex items-start gap-3">
      <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-400" aria-hidden="true">
        <XCircleIcon class="h-5 w-5" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="flex flex-col items-start gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p class="min-w-0 flex-1 font-medium leading-6 [overflow-wrap:anywhere]">{{ message }}</p>
          <div v-if="details || $slots.actions" class="flex flex-wrap items-center gap-3 sm:shrink-0">
            <button v-if="details" type="button" class="inline-flex min-h-8 items-center rounded-md text-sm font-medium text-gray-600 transition-colors hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-300 dark:hover:text-white" :aria-expanded="expanded" :aria-controls="detailsId" @click="expanded = !expanded">
              {{ expanded ? 'Hide details' : 'Show details' }}
            </button>
            <slot name="actions" />
          </div>
        </div>
        <p v-if="details && expanded" :id="detailsId" class="mt-3 max-h-64 overflow-y-auto overscroll-contain whitespace-pre-wrap leading-6 text-gray-600 [overflow-wrap:anywhere] dark:text-gray-400">{{ details }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, useId } from 'vue';
import { XCircleIcon } from '@heroicons/vue/24/outline';
defineProps<{ message: string; details?: string }>();
const expanded = ref(false);
const detailsId = useId();
</script>
