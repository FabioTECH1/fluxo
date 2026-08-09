<template>
  <LogViewer
    :key="siteId"
    :cache-key="`site:${siteId}`"
    title="Site Logs"
    description="View recent log entries for this site."
    :source-loader="loadSources"
  />
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import LogViewer from '../../components/LogViewer.vue';

const route = useRoute();
const normalizeSiteId = (value: unknown) => (
  typeof value === 'string' && /^[1-9]\d*$/.test(value) ? value : null
);
const siteId = ref(normalizeSiteId(route.params.id) || '');
const loadSources = (bypassCache = false) => (
  apiClient.getSiteLogsList(siteId.value, bypassCache)
);

watch(() => route.params.id, (newId) => {
  const nextId = normalizeSiteId(newId);
  if (nextId && nextId !== siteId.value) siteId.value = nextId;
});
</script>
