<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <h1 class="text-2xl font-bold mb-6">Server Settings</h1>

    <div class="flex gap-6">
      <!-- Sidebar -->
      <nav class="w-56 shrink-0">
        <div class="bg-white rounded-lg shadow-sm border border-gray-100 overflow-hidden">
          <router-link to="/settings/general"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="$route.path === '/settings/general' || $route.path === '/settings'
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </svg>
              General
            </span>
          </router-link>
          <router-link to="/settings/source-control"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="$route.path.startsWith('/settings/source-control')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
              </svg>
              Source Control
            </span>
          </router-link>
          <router-link to="/settings/ssh-keys"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="$route.path.startsWith('/settings/ssh-keys')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
              </svg>
              SSH Keys
            </span>
          </router-link>
          <router-link to="/settings/network"
            class="block px-4 py-3 text-sm font-medium border-l-4 transition-colors"
            :class="$route.path.startsWith('/settings/network')
              ? 'border-blue-600 bg-blue-50 text-blue-700'
              : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900'">
            <span class="flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
              </svg>
              Network
            </span>
          </router-link>
        </div>
      </nav>

      <!-- Content -->
      <div class="flex-1 min-w-0">
        <router-view />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
</script>