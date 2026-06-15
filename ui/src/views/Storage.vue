<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <h1 class="text-2xl font-bold mb-6">Storage</h1>

    <div class="flex gap-6">
      <SidebarNav :items="sidebarItems" />

      <div class="flex-1 min-w-0">
        <keep-alive>
          <router-view />
        </keep-alive>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import SidebarNav from '../components/SidebarNav.vue';

const sidebarItems = [
  {
    to: '/storage/databases',
    label: 'Databases',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4" /></svg>',
  },
  {
    to: '/storage/backups',
    label: 'Backups',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>',
  },
];
</script>