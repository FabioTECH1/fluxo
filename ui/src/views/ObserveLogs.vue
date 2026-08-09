<template>
  <LogViewer
    cache-key="server"
    title="Server Logs"
    description="View recent log entries from the server."
    :source-loader="loadSources"
  />
</template>

<script setup lang="ts">
import { apiClient } from '../api/client';
import LogViewer from '../components/LogViewer.vue';

const loadSources = (bypassCache = false) => (
  apiClient.get('/api/v1/system/logs/list', { bypassCache })
);
</script>
