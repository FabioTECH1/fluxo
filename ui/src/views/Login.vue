<template>
  <div class="min-h-screen bg-gray-50 flex flex-col justify-center py-12 sm:px-6 lg:px-8 font-sans">
    <div class="sm:mx-auto sm:w-full sm:max-w-md">
      <!-- Logo block -->
      <div class="flex justify-center items-center space-x-2">
        <span class="h-10 w-10 bg-blue-600 rounded-lg flex items-center justify-center text-white font-bold text-xl shadow-md">
          F
        </span>
        <span class="font-bold tracking-tight text-2xl text-gray-900">Fluxo</span>
      </div>
      <h2 class="mt-6 text-center text-3xl font-extrabold text-gray-900">
        Sign in to your server
      </h2>
      <p class="mt-2 text-center text-sm text-gray-600">
        Enter the Day Zero token generated on server startup.
      </p>
    </div>

    <div class="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
      <div class="bg-white py-8 px-4 shadow sm:rounded-lg sm:px-10 border border-gray-100">
        <form class="space-y-6" @submit.prevent="handleLogin">
          <div v-if="error" class="bg-red-50 border border-red-200 text-red-700 text-sm px-4 py-3 rounded-lg">
            {{ error }}
          </div>

          <div>
            <label for="username" class="block text-sm font-semibold text-gray-700">
              Username
            </label>
            <div class="mt-1">
              <input id="username" v-model="username" type="text" required
                     class="appearance-none block w-full px-3 py-2 border border-gray-300 rounded-lg shadow-sm placeholder-gray-400 focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                     placeholder="admin" />
            </div>
          </div>

          <div>
            <label for="token" class="block text-sm font-semibold text-gray-700">
              Day Zero Token / Password
            </label>
            <div class="mt-1">
              <input id="token" v-model="token" type="password" required
                     class="appearance-none block w-full px-3 py-2 border border-gray-300 rounded-lg shadow-sm placeholder-gray-400 focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                     placeholder="Your secret token" />
            </div>
          </div>

          <div>
            <button type="submit" :disabled="loading"
                    class="w-full flex justify-center py-2 px-4 border border-transparent rounded-lg shadow-sm text-sm font-bold text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 transition-colors disabled:opacity-50">
              {{ loading ? 'Authenticating...' : 'Sign In' }}
            </button>
          </div>

          <div class="text-center text-xs text-gray-500 border-t border-gray-100 pt-4 mt-4">
            Note: Once signed in, you can change your password to a custom one anytime via the <strong>Settings</strong> tab.
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { apiClient } from '../api/client';

const router = useRouter();
const username = ref('admin');
const token = ref('');
const error = ref('');
const loading = ref(false);

const handleLogin = async () => {
  error.value = '';
  loading.value = true;
  try {
    await apiClient.login(username.value, token.value);
    router.push('/overview');
  } catch (e: any) {
    error.value = e.message || 'Invalid credentials';
  } finally {
    loading.value = false;
  }
};
</script>
