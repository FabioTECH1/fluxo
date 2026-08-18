<template>
  <section
    v-if="visible"
    class="border-b border-blue-200 bg-blue-50 dark:border-blue-900/70 dark:bg-blue-950/35"
    role="status"
    aria-live="polite"
  >
    <div class="mx-auto flex max-w-6xl items-start gap-3 px-6 py-3 sm:items-center">
      <div class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-blue-100 text-blue-700 dark:bg-blue-900/60 dark:text-blue-300 sm:mt-0">
        <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 16V4m0 0L7.5 8.5M12 4l4.5 4.5M5 20h14" />
        </svg>
      </div>

      <div class="min-w-0 flex-1">
        <p class="text-sm font-semibold text-blue-950 dark:text-blue-100">
          Fluxo v{{ latestVersion }} is available
        </p>
        <p class="mt-0.5 text-xs leading-5 text-blue-800 dark:text-blue-300">
          You are running v{{ currentVersion }}. Update through your server terminal when convenient.
        </p>
      </div>

      <div class="flex shrink-0 items-center gap-2">
        <a
          :href="releaseURL"
          target="_blank"
          rel="noopener noreferrer"
          class="rounded-lg px-3 py-1.5 text-xs font-semibold text-blue-700 transition-colors hover:bg-blue-100 hover:text-blue-900 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:text-blue-300 dark:hover:bg-blue-900/60 dark:hover:text-blue-100"
        >
          Release notes
        </a>
        <button
          type="button"
          class="rounded-lg p-1.5 text-blue-500 transition-colors hover:bg-blue-100 hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:text-blue-400 dark:hover:bg-blue-900/60 dark:hover:text-blue-200"
          :aria-label="`Dismiss Fluxo ${latestVersion} update notice`"
          @click="dismiss"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
          </svg>
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { apiClient } from '../api/client';

const dismissedVersionKey = 'fluxo_dismissed_update_version';

const visible = ref(false);
const currentVersion = ref('');
const latestVersion = ref('');
const releaseURL = ref('');

const dismissedVersion = () => {
  try {
    return localStorage.getItem(dismissedVersionKey) || '';
  } catch {
    return '';
  }
};

const dismiss = () => {
  visible.value = false;
  try {
    localStorage.setItem(dismissedVersionKey, latestVersion.value);
  } catch {
    // Dismiss for this page view when browser storage is unavailable.
  }
};

onMounted(async () => {
  try {
    const status = await apiClient.getUpdateStatus();
    if (
      status?.check_available !== true
      || status?.update_available !== true
      || typeof status.current_version !== 'string'
      || typeof status.latest_version !== 'string'
      || typeof status.release_url !== 'string'
      || !status.current_version
      || !status.latest_version
      || !status.release_url
      || dismissedVersion() === status.latest_version
    ) {
      return;
    }

    currentVersion.value = status.current_version;
    latestVersion.value = status.latest_version;
    releaseURL.value = status.release_url;
    visible.value = true;
  } catch {
    // Update awareness is optional and must never interfere with the dashboard.
  }
});
</script>
