<template>
  <div class="flex flex-col md:flex-row gap-6">
    <SidebarNav :items="sidebarItems" />

    <div class="flex-1 min-w-0">
      <router-view />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import SidebarNav from '../../components/SidebarNav.vue';

const route = useRoute();
const id = computed(() => route.params.id as string);

const sidebarItems = computed(() => [
  {
    to: `/sites/${id.value}/observe/logs`,
    label: 'Logs',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" /></svg>',
  },
  {
    to: `/sites/${id.value}/observe/activity`,
    label: 'Activity',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>',
  },
]);
</script>
