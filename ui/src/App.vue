<template>
  <div class="min-h-screen bg-gray-50 dark:bg-gray-950 flex flex-col text-gray-900 dark:text-gray-100 font-sans">
    <ToastContainer />
    <ConfirmModal />

    <!-- Top Navigation Header -->
    <header v-if="$route.path !== '/login'" class="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 sticky top-0 z-40">
      <div class="max-w-6xl mx-auto px-6 flex items-center justify-between h-16">
        <div class="flex items-center space-x-8">
          <!-- Logo -->
          <router-link to="/overview" class="flex items-center space-x-2 mr-12">
            <img src="/logo.png" alt="fluxo" class="h-8 w-8 object-cover" />
            <span class="font-bold tracking-tight text-lg">fluxo</span>
          </router-link>

          <!-- Nav Links -->
          <nav class="flex space-x-6 h-16 items-center">
            <router-link to="/overview"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="(route.path === '/overview' || route.path === '/') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Overview
            </router-link>
            <router-link to="/sites"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="route.path.startsWith('/sites') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Sites
            </router-link>
            <router-link to="/storage"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="route.path.startsWith('/storage') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Storage
            </router-link>
            <router-link to="/processes"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="route.path.startsWith('/processes') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Processes
            </router-link>
            <router-link to="/runtime"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="route.path.startsWith('/runtime') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Runtime
            </router-link>
            <router-link to="/observe"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="route.path.startsWith('/observe') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Observe
            </router-link>
            <router-link to="/settings"
              class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
              :class="route.path.startsWith('/settings') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-950 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'">
              Settings
            </router-link>
          </nav>
        </div>

        <div class="flex items-center space-x-4">
          <!-- Server Status Indicator -->
          <span
            class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-100 dark:border-blue-900/50">
            v{{ fluxoVersion }}
          </span>

          <!-- Theme Toggle Dropdown -->
          <div class="relative" ref="themeDropdownRef">
            <button @click="themeOpen = !themeOpen" class="text-base leading-none p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors focus:outline-none" aria-label="Toggle theme" title="Theme settings">
              <span v-if="theme === 'light'">&#9728;</span>
              <span v-else-if="theme === 'dark'">&#9789;</span>
              <span v-else>&#9783;</span>
            </button>
            <div v-if="themeOpen" @click="themeOpen = false" class="fixed inset-0 z-10" @mousedown.stop></div>
            <div v-if="themeOpen" class="absolute right-0 mt-2 w-36 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-20 py-1">
              <button @click="setTheme('light')" class="flex items-center gap-2 w-full px-3 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors" :class="theme === 'light' ? 'font-semibold' : ''">
                <span>&#9728;</span> Light
              </button>
              <button @click="setTheme('dark')" class="flex items-center gap-2 w-full px-3 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors" :class="theme === 'dark' ? 'font-semibold' : ''">
                <span>&#9789;</span> Dark
              </button>
              <button @click="setTheme('system')" class="flex items-center gap-2 w-full px-3 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors" :class="theme === 'system' ? 'font-semibold' : ''">
                <span>&#9783;</span> System
              </button>
            </div>
          </div>

          <button @click="handleLogout"
            class="text-sm font-semibold text-gray-500 dark:text-gray-400 hover:text-red-600 dark:hover:text-red-400 focus:outline-none">
            Logout
          </button>
        </div>
      </div>
    </header>

    <!-- Main Content Panel -->
    <main class="flex-1 bg-gray-50 dark:bg-gray-950">
      <router-view v-slot="{ Component }">
        <keep-alive :max="10">
          <component :is="Component" />
        </keep-alive>
      </router-view>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import ToastContainer from './components/ToastContainer.vue';
import ConfirmModal from './components/ConfirmModal.vue';
import { apiClient } from './api/client';
import { useTheme } from './composables/useTheme';

const route = useRoute();
const { theme } = useTheme();

const themeOpen = ref(false);
const fluxoVersion = ref('...');

onMounted(async () => {
  try {
    const res = await fetch('/api/v1/version');
    const data = await res.json();
    fluxoVersion.value = data.version || 'dev';
  } catch {
    fluxoVersion.value = '0.0.0';
  }
});

const setTheme = (t: 'light' | 'dark' | 'system') => {
  theme.value = t;
  themeOpen.value = false;
};

const handleLogout = () => {
  apiClient.logout();
};
</script>
