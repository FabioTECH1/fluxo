<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <h1 class="text-2xl font-bold mb-6">Runtime</h1>

    <div class="flex flex-col md:flex-row gap-6">
      <SidebarNav :items="sidebarItems" />

      <div class="flex-1 min-w-0">
        <router-view v-slot="{ Component }">
          <keep-alive>
            <component :is="Component" />
          </keep-alive>
        </router-view>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import SidebarNav from '../components/SidebarNav.vue';

const sidebarItems = [
  {
    to: '/runtime/php',
    label: 'PHP',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" /></svg>',
  },
  {
    to: '/runtime/node',
    label: 'Node.js',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>',
  },
  {
    to: '/runtime/python',
    label: 'Python',
    icon: '<svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><path d="M12 3c-4 0-4 1.7-4 3v2h4v1H6c-2 0-3 1.5-3 4s1 4 3 4h2v-3c0-2 1.5-3 3-3h4c1.7 0 3-1.3 3-3V6c0-2-1.8-3-6-3Zm-2 2.2a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6ZM16 7v3c0 2-1.5 3-3 3H9c-1.7 0-3 1.3-3 3v2c0 2 1.8 3 6 3s4-1.7 4-3v-2h-4v-1h6c2 0 3-1.5 3-4s-1-4-3-4h-2Zm-2 10.2a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Z"/></svg>',
  },
  {
    to: '/runtime/databases',
    label: 'Databases',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 7v10c0 2 1 3 3 3h10c2 0 3-1 3-3V7M4 7c0 2 1 3 3 3h10c2 0 3-1 3-3M4 7c0-2 1-3 3-3h10c2 0 3 1 3 3m0 5c0 2-1 3-3 3H7c-2 0-3-1-3-3" /></svg>',
  },
  {
    to: '/runtime/nginx',
    label: 'Nginx',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>',
  },
];
</script>
