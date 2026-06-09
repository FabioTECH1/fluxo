<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-gray-900">{{ site ? site.domain : `Site #${id}` }}</h1>
      <div class="flex gap-2">
        <button @click="triggerDeploy" :disabled="deploying" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
          {{ deploying ? 'Deploying...' : 'Deploy' }}
        </button>
        <button @click="openSite" class="bg-white border border-gray-200 text-gray-700 px-4 py-2 rounded-lg shadow-sm hover:bg-gray-50 font-semibold text-sm transition-colors inline-flex items-center gap-1.5">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" /></svg>
          Visit
        </button>
      </div>
    </div>

    <div class="flex border-b border-gray-200 mb-6 overflow-x-auto">
      <router-link v-for="tab in tabs" :key="tab.key" :to="`/sites/${id}/${tab.key}`"
        class="px-4 py-2.5 font-medium text-sm whitespace-nowrap border-b-2 transition-colors focus:outline-none"
        :class="isTabActive(tab.key) ? 'border-blue-600 text-blue-600 font-semibold' : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'">
        {{ tab.label }}
      </router-link>
    </div>
    <router-view />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';

const route = useRoute();
const id = route.params.id as string;

const site = ref<any>(null);
const deploying = ref(false);

const tabs = [
  { key: 'overview', label: 'Overview' },
  { key: 'deployments', label: 'Deployments' },
  { key: 'processes', label: 'Processes' },
  { key: 'commands', label: 'Commands' },
  { key: 'observe', label: 'Observe' },
  { key: 'domains', label: 'Domains' },
  { key: 'settings', label: 'Settings' },
];

const isTabActive = (key: string) => {
  const prefix = `/sites/${id}/${key}`;
  return route.path === prefix || route.path.startsWith(prefix + '/');
};

const authedFetch = async (url: string, init?: RequestInit) => {
  const token = localStorage.getItem('fluxo_jwt');
  const headers: Record<string, string> = {};
  if (init?.headers) {
    Object.assign(headers, init.headers as Record<string, string>);
  }
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (!headers['Content-Type']) headers['Content-Type'] = 'application/json';
  const res = await window.fetch(url, { ...init, headers });
  if (res.status === 401) {
    localStorage.removeItem('fluxo_jwt');
    window.location.href = '/login';
    throw new Error('Unauthorized');
  }
  return res;
};

const fetchSite = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}`);
    site.value = await res.json();
  } catch (e) {}
};

const triggerDeploy = async () => {
  deploying.value = true;
  try {
    await authedFetch(`/api/v1/sites/${id}/deploy`, { method: 'POST' });
  } catch (e) {}
  deploying.value = false;
};

const openSite = () => {
  window.open(`http://${site.value?.domain}`, '_blank');
};

onMounted(fetchSite);
</script>
