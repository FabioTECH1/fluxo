<template>
  <div class="max-w-6xl mx-auto px-6 py-6">
    <div class="flex flex-col md:flex-row gap-6">
      <SidebarNav :items="sidebarItems" />

      <div class="flex-1 min-w-0">
        <router-view v-slot="{ Component, route: childRoute }">
          <keep-alive :max="3">
            <component
              :is="Component"
              v-if="childRoute.meta.cacheSiteEditor"
              :key="childRoute.name"
            />
          </keep-alive>
          <component
            :is="Component"
            v-if="!childRoute.meta.cacheSiteEditor"
            :key="childRoute.name"
          />
        </router-view>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import SidebarNav from '../../components/SidebarNav.vue';
import { apiClient } from '../../api/client';

const route = useRoute();
const normalizeSiteId = (value: unknown) => (
  typeof value === 'string' && /^[1-9]\d*$/.test(value) ? value : null
);
const id = ref(normalizeSiteId(route.params.id) || '');
const site = ref<any>(null);
let siteRequestVersion = 0;

const sidebarItems = computed(() => {
  const items = [
  {
    to: `/sites/${id.value}/settings/general`,
    label: 'General',
    match: `/sites/${id.value}/settings/general`,
    also: [`/sites/${id.value}/settings`],
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>',
  },
  {
    to: `/sites/${id.value}/settings/deployments`,
    label: 'Deployments',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>',
  },
  {
    to: `/sites/${id.value}/settings/environment`,
    label: 'Environment',
    icon: '<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" /></svg>',
  },
  ];
  if (site.value?.app_type === 'wordpress') {
    items.push({
      to: `/sites/${id.value}/settings/wordpress`,
      label: 'WordPress',
      icon: '<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M7.5 8.5l3 8 1.5-4 1.5 4 3-8"/></svg>',
    });
  }
  return items;
});

const fetchSite = async () => {
  const request = ++siteRequestVersion;
  const requestedSiteId = id.value;
  try {
    const nextSite = await apiClient.getSite(requestedSiteId);
    if (request === siteRequestVersion && requestedSiteId === id.value) site.value = nextSite;
  } catch (e) {}
};

onMounted(fetchSite);
watch(() => route.params.id, (newId) => {
  const nextId = normalizeSiteId(newId);
  if (!nextId || nextId === id.value) return;
  siteRequestVersion++;
  id.value = nextId;
  site.value = null;
  fetchSite();
});
</script>
