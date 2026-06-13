<template>
  <div class="min-h-screen bg-gray-50 dark:bg-gray-950 flex flex-col justify-center py-12 sm:px-6 lg:px-8 font-sans">
    <div class="sm:mx-auto sm:w-full sm:max-w-md">
      <!-- Logo block -->
      <div class="flex justify-center items-center space-x-2">
        <span class="h-10 w-10 bg-blue-600 rounded-lg flex items-center justify-center text-white font-bold text-xl shadow-md">
          F
        </span>
        <span class="font-bold tracking-tight text-2xl text-gray-900 dark:text-gray-100">Fluxo</span>
      </div>
      <h2 class="mt-6 text-center text-3xl font-extrabold text-gray-900 dark:text-gray-100">
        Sign in to your server
      </h2>
      <p class="mt-2 text-center text-sm text-gray-600 dark:text-gray-400">
        <span v-if="isFirstTime">Enter the Day Zero token generated on server startup.</span>
        <span v-else>Enter your credentials to access the panel.</span>
      </p>
    </div>

    <div class="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
      <div class="bg-white dark:bg-gray-900 py-8 px-4 shadow sm:rounded-lg sm:px-10 border border-gray-100 dark:border-gray-800">
        <form class="space-y-6" @submit.prevent="handleLogin">


          <div>
            <label for="username" class="block text-sm font-semibold text-gray-700 dark:text-gray-300">
              Username
            </label>
            <div class="mt-1">
              <input id="username" v-model="username" type="text" required
                     class="appearance-none block w-full px-3 py-2 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 rounded-lg shadow-sm placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                     placeholder="e.g. admin" />
            </div>
            <p v-if="isFirstTime" class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              First-time login? Any username you type here will become your administrator account name.
            </p>
          </div>

          <div>
            <label for="token" class="block text-sm font-semibold text-gray-700 dark:text-gray-300">
              {{ isFirstTime ? 'Day Zero Token' : 'Password' }}
            </label>
            <div class="mt-1 relative">
              <input id="token" v-model="token" :type="showToken ? 'text' : 'password'" required
                     class="appearance-none block w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 rounded-lg shadow-sm placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                     :placeholder="isFirstTime ? 'Your secret token' : '••••••••'" />
              <button type="button" @click="showToken = !showToken" class="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400 dark:text-gray-400">
                <span v-if="!showToken" class="text-lg leading-none">&#128065;</span>
                <span v-else class="text-lg leading-none">&#128064;</span>
              </button>
            </div>
          </div>

          <div>
            <button type="submit" :disabled="loading"
                    class="w-full flex justify-center py-2 px-4 border border-transparent rounded-lg shadow-sm text-sm font-bold text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 transition-colors disabled:opacity-50">
              {{ loading ? 'Authenticating...' : 'Sign In' }}
            </button>
          </div>

          <div v-if="isFirstTime" class="text-center text-xs text-gray-500 dark:text-gray-400 border-t border-gray-100 dark:border-gray-800 pt-4 mt-4">
            Note: Once signed in, you can change your password to a custom one anytime via the <strong>Settings</strong> tab.
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';

const router = useRouter();
const { addToast } = useToast();
const username = ref('');
const token = ref('');
const showToken = ref(false);
const loading = ref(false);
const isFirstTime = ref(false);

const checkBootstrapStatus = async () => {
  try {
    const res = await fetch('/api/v1/auth/bootstrap');
    if (res.ok) {
      const data = await res.json();
      isFirstTime.value = data.bootstrap;
    }
  } catch (e) {
    console.error('Failed to check bootstrap status:', e);
  }
};

onMounted(() => {
  checkBootstrapStatus();
});

const handleLogin = async () => {
  loading.value = true;
  try {
    await apiClient.login(username.value, token.value);
    router.push('/overview');
  } catch (e: any) {
    const errMsg = e.message || 'Invalid credentials';
    addToast(errMsg, 'error');
  } finally {
    loading.value = false;
  }
};
</script>
