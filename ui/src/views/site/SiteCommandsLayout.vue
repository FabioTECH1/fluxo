<template>
  <div class="flex flex-col md:flex-row gap-6">
    <SidebarNav :items="items" />
    <div class="flex-1 min-w-0"><router-view :key="String(route.params.id)" /></div>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import SidebarNav from '../../components/SidebarNav.vue';
import { apiClient } from '../../api/client';
const route = useRoute();
const laravel = ref(false);
watch(() => route.params.id, async (id, _, onCleanup) => {
  let current = true;
  onCleanup(() => { current = false; });
  laravel.value = false;
  try { const site = await apiClient.getSite(String(id)); if (current) laravel.value = site.app_type === 'laravel'; } catch { /* Site page owns load errors. */ }
}, { immediate: true });
const items = computed(() => {
  const base = `/sites/${route.params.id}/commands`;
  const items = [{ to: base, label: 'Commands', match: `${base}/run`, also: [base] }];
  if (laravel.value) items.push({ to: `${base}/tinker`, label: 'Tinker', match: `${base}/tinker`, also: [] });
  return items;
});
</script>
