<template>
  <section v-if="visible" aria-label="Fluxo update" class="mx-auto w-full max-w-6xl px-6 pt-3">
    <UpdateNotice
      :message="`Fluxo v${latestVersion} is available. Running v${currentVersion}; update through your server terminal.`"
      :href="releaseURL"
      action-label="Release notes"
      :dismiss-label="`Dismiss Fluxo ${latestVersion} update notice`"
      @dismiss="dismiss"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { apiClient } from '../api/client';
import UpdateNotice from './UpdateNotice.vue';

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
