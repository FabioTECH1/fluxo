<template>
  <NoticeBanner v-if="message" :message="message" :tone="tone">
    <template v-if="details || $slots.actions" #actions>
      <button v-if="details" type="button" class="inline-flex min-h-8 items-center rounded-md text-sm font-medium text-gray-600 transition-colors hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-300 dark:hover:text-white" :aria-expanded="expanded" :aria-controls="detailsId" @click="expanded = !expanded">
        {{ expanded ? 'Hide details' : 'Show details' }}
      </button>
      <slot name="actions" />
    </template>
    <slot />
    <p v-if="details && expanded" :id="detailsId" class="mt-3 max-h-64 overflow-y-auto overscroll-contain whitespace-pre-wrap leading-6 text-gray-600 [overflow-wrap:anywhere] dark:text-gray-400">{{ details }}</p>
  </NoticeBanner>
</template>

<script setup lang="ts">
import { ref, useId } from 'vue';
import NoticeBanner from './NoticeBanner.vue';
withDefaults(defineProps<{ message: string; details?: string; tone?: 'error'|'info'|'warning' }>(), {tone:'info'});
const expanded = ref(false);
const detailsId = useId();
</script>
