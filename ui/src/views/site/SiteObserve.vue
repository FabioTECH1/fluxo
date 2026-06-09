<template>
  <div class="flex gap-6">
    <nav class="w-56 shrink-0">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
        <router-link :to="`/sites/${id}/observe/logs`"
          class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900"
          active-class="border-blue-600 bg-blue-50 text-blue-700">
          <span class="flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
            Logs
          </span>
        </router-link>
        <router-link :to="`/sites/${id}/observe/activity`"
          class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900"
          active-class="border-blue-600 bg-blue-50 text-blue-700">
          <span class="flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            Activity
          </span>
        </router-link>
      </div>
    </nav>

    <div class="flex-1 min-w-0">
      <router-view />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router';
const route = useRoute();
const id = route.params.id as string;
</script>
