<template>
  <div class="flex gap-6">
    <nav class="w-56 shrink-0">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
        <router-link :to="`/sites/${id}/processes/daemons`"
          class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900"
          active-class="border-blue-600 bg-blue-50 text-blue-700">
          <span class="flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
            </svg>
            Background Processes
          </span>
        </router-link>
        <router-link :to="`/sites/${id}/processes/scheduler`"
          class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900"
          active-class="border-blue-600 bg-blue-50 text-blue-700">
          <span class="flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
            Scheduler
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
