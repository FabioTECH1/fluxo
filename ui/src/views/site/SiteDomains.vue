<template>
  <div class="flex gap-6">
    <nav class="w-56 shrink-0">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
        <router-link :to="`/sites/${id}/domains/list`"
          class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900"
          active-class="border-blue-600 bg-blue-50 text-blue-700">
          <span class="flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
            </svg>
            Domains
          </span>
        </router-link>
        <router-link :to="`/sites/${id}/domains/ssl`"
          class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900"
          active-class="border-blue-600 bg-blue-50 text-blue-700">
          <span class="flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
            SSL
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
