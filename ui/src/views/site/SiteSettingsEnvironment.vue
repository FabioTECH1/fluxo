<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Environment</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Below you may edit the .env file for your application, which is a standard default environment file that typically loaded by applications. If the application is uninstalled, the environment file will also be removed.</p>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Environment variables</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Your application's environment variables.</p>
          <p class="text-xs text-red-600 mb-2 dark:text-red-400">Environment variables should not be shared publicly.</p>
          <div class="relative">
            <div class="flex justify-between items-center mb-2">
              <span class="text-xs text-gray-400 dark:text-gray-500">{{ lineCount }} lines</span>
              <button @click="revealed = !revealed" class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-600 bg-white border border-gray-200 rounded-lg hover:bg-gray-50 font-semibold transition-colors dark:text-gray-400 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-800">
                <span v-if="!revealed">&#128065;</span>
                <span v-else>&#128064;</span>
                {{ revealed ? 'Hide' : 'Reveal' }}
              </button>
            </div>
            <div class="relative flex bg-gray-50 border border-gray-200 rounded-lg overflow-hidden focus-within:ring-2 focus-within:ring-blue-500 focus-within:border-blue-500 transition-shadow dark:bg-gray-800 dark:border-gray-600">
              <div id="env-line-numbers" class="w-10 shrink-0 bg-gray-100 border-r border-gray-200 py-2 select-none overflow-hidden dark:bg-gray-800 dark:border-gray-600">
                <div v-for="n in lineCount" :key="n" class="text-right px-2 text-xs font-mono text-gray-400 leading-5 dark:text-gray-500">{{ n }}</div>
              </div>
              <textarea v-model="envContent" @scroll="syncScroll" ref="textareaRef"
                class="w-full h-72 font-mono text-sm p-2 bg-transparent resize-none outline-none leading-5 dark:text-gray-100"
                :class="!revealed ? 'select-none text-transparent caret-transparent' : ''"
                :readonly="!revealed"
                placeholder="APP_NAME=Laravel&#10;APP_ENV=production&#10;APP_KEY=&#10;APP_DEBUG=false"></textarea>
            </div>
          </div>
        </div>

        <div class="flex items-start justify-between">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Cache</label>
            <p class="text-xs text-gray-500 dark:text-gray-400">Run php artisan config:cache after updating environment variables.</p>
          </div>
          <button @click="cacheConfig = !cacheConfig" type="button"
            :class="cacheConfig ? 'bg-blue-600' : 'bg-gray-200'"
            class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0">
            <span :class="cacheConfig ? 'translate-x-6' : 'translate-x-1'"
              class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
          </button>
        </div>
      </div>
    </div>

    <div class="flex justify-between">
      <button @click="saveEnv" :disabled="saving" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
        {{ saving ? 'Saving...' : 'Save environment' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';

const route = useRoute();
const siteId = route.params.id as string;
const { addToast } = useToast();

const envContent = ref('');
const revealed = ref(false);
const cacheConfig = ref(false);
const saving = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);

const lineCount = computed(() => {
  const lines = envContent.value.split('\n').length;
  return Math.max(lines, 15);
});

const syncScroll = () => {
  const lineEl = document.getElementById('env-line-numbers');
  if (lineEl && textareaRef.value) {
    lineEl.scrollTop = textareaRef.value.scrollTop;
  }
};

const token = () => localStorage.getItem('fluxo_jwt');

const fetchEnv = async () => {
  try {
    const res = await fetch(`/api/v1/sites/${siteId}/env`, { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) {
      const data = await res.json();
      envContent.value = data.content || '';
    }
  } catch (e) {}
};

const saveEnv = async () => {
  saving.value = true;
  try {
    const res = await fetch(`/api/v1/sites/${siteId}/env`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: envContent.value })
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Environment saved', 'success');
    revealed.value = false;
  } catch (e: any) {
    addToast(e.message || 'Failed to save', 'error');
  } finally {
    saving.value = false;
  }
};

onMounted(fetchEnv);
</script>
