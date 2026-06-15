<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <div class="flex justify-between items-center mb-6">
      <div class="flex items-center gap-3">
        <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">{{ site ? site.domain : `Site #${id}` }}</h1>
        <span v-if="nightwatchEnabled" class="inline-flex items-center gap-1 text-[10px] text-green-600 dark:text-green-400 font-medium">
          <span class="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse"></span>
          Monitored
        </span>
        <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold border"
          :class="siteUp ? 'bg-green-50 text-green-700 border-green-200 dark:bg-green-900/30 dark:text-green-400 dark:border-green-900/50' : 'bg-yellow-50 text-yellow-700 border-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400 dark:border-yellow-900/50'">
          <span class="h-1.5 w-1.5 rounded-full" :class="siteUp ? 'bg-green-500' : 'bg-yellow-500'"></span>
          {{ siteUp ? 'Active' : 'Maintenance' }}
        </span>
      </div>
      <div class="flex gap-2">
        <AppButton variant="primary" :loading="deploying" @click="triggerDeploy">
          {{ deploying ? 'Deploying...' : 'Deploy' }}
        </AppButton>
        <AppButton variant="secondary" @click="openSite">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" /></svg>
          Visit
        </AppButton>
      </div>
    </div>

    <div class="flex border-b border-gray-200 dark:border-gray-700 mb-6 overflow-x-auto">
      <router-link v-for="tab in tabs" :key="tab.key" :to="`/sites/${id}/${tab.key}`"
        class="px-4 py-2.5 font-medium text-sm whitespace-nowrap border-b-2 transition-colors focus:outline-none"
        :class="isTabActive(tab.key) ? 'border-blue-600 text-blue-600 dark:text-blue-400 font-semibold' : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'">
        {{ tab.label }}
      </router-link>
    </div>
    <router-view v-slot="{ Component }">
      <keep-alive>
        <component :is="Component" />
      </keep-alive>
    </router-view>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, provide } from 'vue';
import { useRoute } from 'vue-router';
import AppButton from '../components/AppButton.vue';
import { useToast } from '../composables/useToast';

const route = useRoute();
const id = route.params.id as string;

const site = ref<any>(null);
const deploying = ref(false);
const siteUp = ref(true);
const nightwatchEnabled = ref(false);
const { addToast } = useToast();
let deployInterval: number | null = null;

const fetchStatuses = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/features`);
    if (res.ok) {
      const data = await res.json();
      nightwatchEnabled.value = data.nightwatch_enabled || false;
      siteUp.value = !data.in_maintenance;
    }
  } catch (e) {}
};

provide('refreshStatuses', fetchStatuses);

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
    provide('site', site);
  } catch (e) {}
};

const triggerDeploy = async () => {
  deploying.value = true;
  try {
    await authedFetch(`/api/v1/sites/${id}/deploy`, { method: 'POST' });
    pollDeployStatus();
  } catch (e) {
    deploying.value = false;
  }
};

const pollDeployStatus = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/deployments?page=1`);
    if (res.ok) {
      const data = await res.json();
      const deps = data.data || [];
      if (deps && deps.length > 0) {
        if (deps[0].status === 'running') {
          deploying.value = true;
          if (!deployInterval) {
            deployInterval = window.setInterval(pollDeployStatus, 2000);
          }
          return;
        } else if (deploying.value) {
          // It was running, now it's not
          if (deps[0].status === 'success') {
            addToast('Deployment finished successfully', 'success');
          } else if (deps[0].status === 'failed') {
            addToast('Deployment failed', 'error');
          }
        }
      }
    }
  } catch (e) {}
  
  deploying.value = false;
  if (deployInterval) {
    window.clearInterval(deployInterval);
    deployInterval = null;
  }
};

const openSite = () => {
  window.open(`http://${site.value?.domain}`, '_blank');
};

onMounted(() => {
  fetchSite();
  fetchStatuses();
  pollDeployStatus();
});

onUnmounted(() => {
  if (deployInterval) {
    window.clearInterval(deployInterval);
  }
});
</script>
