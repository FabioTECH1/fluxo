<template>
  <div v-if="message" role="alert" class="text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/30 border border-red-200 p-3 rounded-lg text-sm mb-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p class="min-w-0 break-words">{{ message }}</p>
      <button v-if="details" type="button" class="shrink-0 font-semibold underline underline-offset-2" :aria-expanded="expanded" :aria-controls="detailsId" @click="expanded = !expanded">
        {{ expanded ? 'Hide details' : 'Review details' }}
      </button>
    </div>
    <p v-if="details && expanded" :id="detailsId" class="mt-3 whitespace-pre-wrap break-words">{{ details }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, useId } from 'vue';
defineProps<{ message: string; details?: string }>();
const expanded = ref(false);
const detailsId = useId();
</script>
