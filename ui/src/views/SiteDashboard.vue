<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 mb-6">
      <div class="flex flex-wrap items-center gap-3">
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
        <AppButton variant="primary" :loading="deploying" :disabled="!canDeploy" :title="canDeploy ? 'Deploy site' : 'Configure a deployment script first'" @click="triggerDeploy">
          {{ deploying ? (latestStatus === 'pending' ? 'Queued...' : 'Deploying...') : 'Deploy' }}
        </AppButton>
        <AppButton variant="secondary" :disabled="!site?.domain" @click="openSite">
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
import { computed, ref, onMounted, onActivated, watch, provide } from 'vue';
import { storeToRefs } from 'pinia';
import { useRoute } from 'vue-router';
import AppButton from '../components/AppButton.vue';
import { apiClient } from '../api/client';
import { useSiteStore } from '../stores/site';
import { useDeploymentsStore } from '../stores/deployments';

const route = useRoute();
const id = ref(route.params.id as string);

const siteStore = useSiteStore();
const deploymentsStore = useDeploymentsStore();
const { activeSite: site } = storeToRefs(siteStore);
const { deploying, latestStatus, deploySignal } = storeToRefs(deploymentsStore);
const siteUp = ref(true);
const nightwatchEnabled = ref(false);
const canDeploy = computed(() => site.value?.app_type !== 'wordpress' || !!site.value?.deploy_script?.trim());

const fetchStatuses = async () => {
  try {
    const data = await apiClient.getSiteFeatures(id.value);
    nightwatchEnabled.value = data.nightwatch_enabled || false;
    siteUp.value = !data.in_maintenance;
  } catch (e) {}
};

provide('refreshStatuses', fetchStatuses);
provide('deploySignal', deploySignal);

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
  const prefix = `/sites/${id.value}/${key}`;
  return route.path === prefix || route.path.startsWith(prefix + '/');
};

const fetchSite = async () => {
  try {
    await siteStore.fetchSite(id.value);
  } catch (e) {}
};

const triggerDeploy = async () => {
  deploymentsStore.triggerDeploy();
};

const openSite = () => {
  if (!site.value?.domain) return;
  window.open(`http://${site.value.domain}`, '_blank');
};

onMounted(() => {
  fetchSite();
  fetchStatuses();
});

onActivated(() => {
  fetchSite();
  fetchStatuses();
});

watch(() => route.params.id, (newId) => {
  id.value = newId as string;
  fetchSite();
  fetchStatuses();
});
</script>
