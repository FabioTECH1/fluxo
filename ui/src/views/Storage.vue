<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <h1 class="text-2xl font-bold mb-6">Storage</h1>

    <div class="flex gap-6">
      <nav class="w-56 shrink-0">
        <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
          <router-link to="/storage/databases"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="$route.path.startsWith('/storage/databases')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4" /></svg>
              Databases
            </span>
          </router-link>
          <router-link to="/storage/backups"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="$route.path.startsWith('/storage/backups')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>
              Backups
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
</script>