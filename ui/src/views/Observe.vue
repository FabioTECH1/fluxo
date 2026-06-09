<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <h1 class="text-2xl font-bold mb-6">Observe</h1>

    <div class="flex gap-6">
      <nav class="w-56 shrink-0">
        <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
          <router-link to="/observe/monitoring"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="route.path.startsWith('/observe/monitoring') ? 'border-blue-600 bg-blue-50 text-blue-700' : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
              </svg>
              Monitoring
            </span>
          </router-link>
          <router-link to="/observe/logs"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="route.path.startsWith('/observe/logs') ? 'border-blue-600 bg-blue-50 text-blue-700' : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
              </svg>
              Logs
            </span>
          </router-link>
          <router-link to="/observe/activity"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="route.path.startsWith('/observe/activity') ? 'border-blue-600 bg-blue-50 text-blue-700' : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
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
  </div>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router';
const route = useRoute();
</script>