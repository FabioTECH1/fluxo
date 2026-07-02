<template>
  <div class="min-h-screen bg-gray-50 dark:bg-gray-950 flex flex-col text-gray-900 dark:text-gray-100 font-sans">
    <ToastContainer />
    <ConfirmModal />

    <!-- One-time Setup Credentials Modal -->
    <BaseModal
      v-model="showCredentialsModal"
      title="Administrative Credentials"
      :prevent-dismiss="true"
      :show-close="false"
      maxWidth="max-w-lg"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          Please copy and securely store the administrative credentials for your server below.
          <strong class="text-red-600 dark:text-red-400 block mt-1">Once you dismiss this modal, these credentials can never be shown or queried again.</strong>
        </p>

        <div class="space-y-3">
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-xs font-bold mb-1">System User 'fluxo' Sudo Password</label>
            <div class="relative">
              <input type="text" readonly :value="credentials.sudoPassword || ''" class="w-full border border-gray-200 dark:border-gray-600 bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2 pr-10 text-sm font-mono text-gray-900 dark:text-gray-100 cursor-text">
              <button type="button" @click="copyText(credentials.sudoPassword || '')" class="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 dark:text-gray-500 hover:text-blue-600 dark:hover:text-blue-400" title="Copy">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
              </button>
            </div>
          </div>

          <div v-if="credentials.mysqlPassword">
            <label class="block text-gray-700 dark:text-gray-300 text-xs font-bold mb-1">MySQL Superuser 'fluxo' Password</label>
            <div class="relative">
              <input type="text" readonly :value="credentials.mysqlPassword" class="w-full border border-gray-200 dark:border-gray-600 bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2 pr-10 text-sm font-mono text-gray-900 dark:text-gray-100 cursor-text">
              <button type="button" @click="copyText(credentials.mysqlPassword || '')" class="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 dark:text-gray-500 hover:text-blue-600 dark:hover:text-blue-400" title="Copy">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
              </button>
            </div>
          </div>

          <div v-if="credentials.postgresPassword">
            <label class="block text-gray-700 dark:text-gray-300 text-xs font-bold mb-1">PostgreSQL Superuser 'fluxo' Password</label>
            <div class="relative">
              <input type="text" readonly :value="credentials.postgresPassword" class="w-full border border-gray-200 dark:border-gray-600 bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2 pr-10 text-sm font-mono text-gray-900 dark:text-gray-100 cursor-text">
              <button type="button" @click="copyText(credentials.postgresPassword || '')" class="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 dark:text-gray-500 hover:text-blue-600 dark:hover:text-blue-400" title="Copy">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
              </button>
            </div>
          </div>
        </div>

        <div class="mt-4 pt-4 border-t border-gray-100 dark:border-gray-800">
          <label class="flex items-start gap-3 cursor-pointer">
            <input type="checkbox" v-model="credentialsCopiedCheckbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500">
            <span class="text-xs text-gray-600 dark:text-gray-400">
              I have copied and securely stored these credentials
            </span>
          </label>
        </div>
      </div>

      <template #footer>
        <AppButton
          variant="primary"
          :disabled="!credentialsCopiedCheckbox"
          :loading="submittingCredentialsMark"
          @click="dismissCredentialsModal"
        >
          Acknowledge & Close
        </AppButton>
      </template>
    </BaseModal>

    <!-- Top Navigation Header -->
    <header v-if="$route.path !== '/login'" class="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 sticky top-0 z-40">
      <div class="max-w-6xl mx-auto px-6 flex items-center justify-between h-16">
        <div class="flex items-center space-x-8">
          <!-- Logo -->
          <router-link to="/overview" class="flex items-center space-x-2 mr-12">
            <img src="/logo.png" alt="fluxo" class="h-8 w-8 object-cover" />
            <span class="font-bold tracking-tight text-lg">fluxo</span>
          </router-link>

          <!-- Nav Links (Desktop) -->
          <nav class="hidden md:flex space-x-6 h-16 items-center">
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

        <!-- Desktop Action Controls -->
        <div class="hidden md:flex items-center space-x-4">
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

        <!-- Mobile Controls -->
        <div class="flex md:hidden items-center space-x-2">
          <!-- Quick Theme Toggle for Mobile (cycles through) -->
          <button @click="cycleTheme" class="text-base leading-none p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors focus:outline-none" aria-label="Toggle theme">
            <span v-if="theme === 'light'">&#9728;</span>
            <span v-else-if="theme === 'dark'">&#9789;</span>
            <span v-else>&#9783;</span>
          </button>
          
          <!-- Hamburger Button -->
          <button @click="mobileMenuOpen = !mobileMenuOpen" class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200 focus:outline-none" aria-label="Open menu">
            <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path v-if="!mobileMenuOpen" stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h16" />
              <path v-else stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      </div>

      <!-- Mobile Navigation Drawer -->
      <div v-show="mobileMenuOpen" class="md:hidden border-t border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 px-6 py-4 space-y-4 shadow-inner">
        <nav class="flex flex-col space-y-2">
          <router-link to="/overview" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="(route.path === '/overview' || route.path === '/') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Overview
          </router-link>
          <router-link to="/sites" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="route.path.startsWith('/sites') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Sites
          </router-link>
          <router-link to="/storage" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="route.path.startsWith('/storage') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Storage
          </router-link>
          <router-link to="/processes" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="route.path.startsWith('/processes') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Processes
          </router-link>
          <router-link to="/runtime" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="route.path.startsWith('/runtime') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Runtime
          </router-link>
          <router-link to="/observe" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="route.path.startsWith('/observe') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Observe
          </router-link>
          <router-link to="/settings" @click="mobileMenuOpen = false"
            class="block px-3 py-2 rounded-md text-base font-medium transition-colors"
            :class="route.path.startsWith('/settings') ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-semibold' : 'text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
            Settings
          </router-link>
        </nav>

        <div class="pt-4 border-t border-gray-100 dark:border-gray-800 flex items-center justify-between">
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300">
            v{{ fluxoVersion }}
          </span>
          <button @click="handleLogout(); mobileMenuOpen = false"
            class="text-sm font-semibold text-red-600 dark:text-red-400 hover:text-red-700 focus:outline-none">
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
import { ref, onMounted, onUnmounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import ToastContainer from './components/ToastContainer.vue';
import ConfirmModal from './components/ConfirmModal.vue';
import BaseModal from './components/BaseModal.vue';
import AppButton from './components/AppButton.vue';
import { apiClient } from './api/client';
import { useTheme } from './composables/useTheme';
import { useToast } from './composables/useToast';
import { useDeploymentsStore } from './stores/deployments';

const route = useRoute();
const { theme } = useTheme();
const { addToast } = useToast();
const deploymentsStore = useDeploymentsStore();

const themeOpen = ref(false);
const mobileMenuOpen = ref(false);
const fluxoVersion = ref('...');

const showCredentialsModal = ref(false);
const credentials = ref<{ sudoPassword?: string; mysqlPassword?: string; postgresPassword?: string }>({});
const credentialsCopiedCheckbox = ref(false);
const submittingCredentialsMark = ref(false);
const credentialsChecked = ref(false);

const cycleTheme = () => {
  if (theme.value === 'light') {
    theme.value = 'dark';
  } else if (theme.value === 'dark') {
    theme.value = 'system';
  } else {
    theme.value = 'light';
  }
};

const copyText = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
    addToast('Copied to clipboard', 'success');
  } catch {
    addToast('Failed to copy', 'error');
  }
};

const checkBootstrapCredentials = async () => {
  if (credentialsChecked.value) {
    return;
  }
  if (!apiClient.isAuthenticated() || route.path === '/login') {
    return;
  }
  try {
    const data = await apiClient.getBootstrapCredentials();
    if (data) {
      const creds: { sudoPassword?: string; mysqlPassword?: string; postgresPassword?: string } = {};
      if (data.fluxo_sudo_password) creds.sudoPassword = data.fluxo_sudo_password;
      if (data.fluxo_mysql_password) creds.mysqlPassword = data.fluxo_mysql_password;
      if (data.fluxo_postgres_password) creds.postgresPassword = data.fluxo_postgres_password;
      if (Object.keys(creds).length > 0) {
        credentials.value = creds;
        showCredentialsModal.value = true;
      } else {
        credentialsChecked.value = true;
      }
    } else {
      credentialsChecked.value = true;
    }
  } catch (err) {
    // 403 or other errors mean credentials have already been copied, so we avoid checking again.
    credentialsChecked.value = true;
  }
};

const dismissCredentialsModal = async () => {
  if (!credentialsCopiedCheckbox.value) return;
  submittingCredentialsMark.value = true;
  try {
    await apiClient.markCredentialsCopied();
    showCredentialsModal.value = false;
    credentialsChecked.value = true;
    addToast('Credentials acknowledged and cleared successfully', 'success');
  } catch (err: any) {
    addToast(err.message || 'Failed to acknowledge credentials', 'error');
  } finally {
    submittingCredentialsMark.value = false;
  }
};

onMounted(async () => {
  try {
    const data = await apiClient.getVersion();
    fluxoVersion.value = data.version || 'dev';
  } catch {
    fluxoVersion.value = '0.0.0';
  }
  checkBootstrapCredentials();
});

watch(() => route.path, (newPath) => {
  if (newPath !== '/login' && apiClient.isAuthenticated()) {
    checkBootstrapCredentials();
  }
});

watch(() => route.path, (path) => {
  const match = path.match(/^\/sites\/(\d+)/)
  if (!match) {
    deploymentsStore.setSite(null)
    return
  }
  deploymentsStore.startBackgroundPolling(match[1], true)
}, { immediate: true })

onUnmounted(() => {
  deploymentsStore.stopPolling()
})

const setTheme = (t: 'light' | 'dark' | 'system') => {
  theme.value = t;
  themeOpen.value = false;
};

const handleLogout = () => {
  credentialsChecked.value = false;
  deploymentsStore.setSite(null);
  apiClient.logout();
};
</script>
