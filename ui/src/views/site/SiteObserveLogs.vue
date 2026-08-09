<template>
  <LogViewer
    :key="siteId"
    title="Site Logs"
    description="View recent log entries for this site."
    :source-loader="loadSources"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import LogViewer from '../../components/LogViewer.vue';

const route = useRoute();
const siteId = computed(() => String(route.params.id || ''));
const loadSources = (bypassCache = false) => (
  apiClient.getSiteLogsList(siteId.value, bypassCache)
);
</script>
