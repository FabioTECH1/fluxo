<template>
  <NoticeBanner :message="message" tone="info">
    <template #actions>
      <RouterLink v-if="to && !isCurrentDestination(to)" :to="to" :class="actionClass">{{ actionLabel }}</RouterLink>
      <a v-else-if="!to && href" :href="href" target="_blank" rel="noopener noreferrer" :class="actionClass">{{ actionLabel }}</a>
      <button v-if="!to && !href" type="button" :class="actionClass" @click="$emit('action')">{{ actionLabel }}</button>
      <button
        type="button"
        class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 disabled:cursor-wait disabled:opacity-60 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200"
        :aria-label="dismissLabel"
        :disabled="dismissing"
        @click="$emit('dismiss')"
      >
        <XMarkIcon class="h-4 w-4" aria-hidden="true" />
      </button>
    </template>
  </NoticeBanner>
</template>

<script setup lang="ts">
import { RouterLink, type RouteLocationRaw } from 'vue-router';
import { XMarkIcon } from '@heroicons/vue/24/outline';
import NoticeBanner from './NoticeBanner.vue';
import { useNotificationNavigation } from '../composables/useNotificationNavigation';
const { isCurrentDestination } = useNotificationNavigation();
withDefaults(defineProps<{
  message: string;
  actionLabel?: string;
  to?: RouteLocationRaw;
  href?: string;
  dismissLabel: string;
  dismissing?: boolean;
}>(), { actionLabel: 'Review updates', dismissing: false });
defineEmits<{ dismiss: []; action: [] }>();
const actionClass = 'inline-flex min-h-8 items-center rounded-md text-sm font-medium text-gray-600 transition-colors hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-300 dark:hover:text-white';
</script>
