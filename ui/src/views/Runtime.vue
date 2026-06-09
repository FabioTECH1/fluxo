<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <h1 class="text-2xl font-bold mb-6">Runtime</h1>

    <div class="flex gap-6">
      <nav class="w-56 shrink-0">
        <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
          <router-link to="/runtime/php"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="route.path.startsWith('/runtime/php')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
              </svg>
              PHP
            </span>
          </router-link>
          <router-link to="/runtime/node"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="route.path.startsWith('/runtime/node')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
              </svg>
              Node.js
            </span>
          </router-link>
          <router-link to="/runtime/nginx"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="route.path.startsWith('/runtime/nginx')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
              </svg>
              Nginx
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