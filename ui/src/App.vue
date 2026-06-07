<template>
  <div class="min-h-screen bg-gray-50 flex flex-col text-gray-900 font-sans">
    <ToastContainer />
    <ConfirmModal />
    
    <!-- Top Navigation Header -->
    <header v-if="$route.path !== '/login'" class="bg-white border-b border-gray-200 sticky top-0 z-40">
      <div class="max-w-6xl mx-auto px-6 flex items-center justify-between h-16">
        <div class="flex items-center space-x-8">
          <!-- Logo -->
          <div class="flex items-center space-x-2 mr-4">
            <span class="h-8 w-8 bg-blue-600 rounded-lg flex items-center justify-center text-white font-bold text-lg shadow-sm">
              F
            </span>
            <span class="font-bold tracking-tight text-lg">Fluxo</span>
          </div>

          <!-- Nav Links -->
          <nav class="flex space-x-6 h-16 items-center">
            <router-link to="/overview" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         exact-active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path === '/overview' || $route.path === '/' ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Overview
            </router-link>
            <router-link to="/sites" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path.startsWith('/sites') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Sites
            </router-link>
            <router-link to="/storage" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path.startsWith('/storage') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Storage
            </router-link>
            <router-link to="/processes" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path.startsWith('/processes') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Processes
            </router-link>
            <router-link to="/runtime" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path.startsWith('/runtime') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Runtime
            </router-link>
            <router-link to="/observe" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path.startsWith('/observe') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Observe
            </router-link>
            <router-link to="/settings" 
                         class="text-sm font-medium transition-all border-b-2 py-5 px-1 focus:outline-none"
                         active-class="border-blue-600 text-blue-600 font-semibold"
                         :class="$route.path.startsWith('/settings') ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-950 hover:border-gray-300'">
              Settings
            </router-link>
          </nav>
        </div>

        <div class="flex items-center space-x-4">
          <!-- Server Status Indicator -->
          <span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-blue-50 text-blue-700 border border-blue-100">
            v1.0.0
          </span>
          <button @click="handleLogout" class="text-sm font-semibold text-gray-500 hover:text-red-600 focus:outline-none">
            Logout
          </button>
        </div>
      </div>
    </header>

    <!-- Main Content Panel -->
    <main class="flex-1 bg-gray-50">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import ToastContainer from './components/ToastContainer.vue';
import ConfirmModal from './components/ConfirmModal.vue';
import { apiClient } from './api/client';

const handleLogout = () => {
  apiClient.logout();
};
</script>
