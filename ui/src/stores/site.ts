import { ref } from 'vue';
import { defineStore } from 'pinia';
import { apiClient } from '../api/client';

export const useSiteStore = defineStore('site', () => {
  const activeSite = ref<any>(null);
  const activeSiteId = ref<string | number | null>(null);
  const loading = ref(false);
  let fetchSeq = 0;

  const setActiveSite = (site: any) => {
    activeSite.value = site;
    activeSiteId.value = site?.id ?? null;
  };

  const clearActiveSite = () => {
    activeSite.value = null;
    activeSiteId.value = null;
  };

  const fetchSite = async (id: string | number, bypassCache = false) => {
    const seq = ++fetchSeq;
    const requestedId = String(id);
    if (activeSiteId.value !== null && String(activeSiteId.value) !== requestedId) {
      activeSite.value = null;
    }
    activeSiteId.value = id;
    loading.value = true;
    try {
      const site = await apiClient.getSite(id, bypassCache);
      if (seq === fetchSeq && String(activeSiteId.value) === requestedId) {
        setActiveSite(site);
      }
      return site;
    } finally {
      if (seq === fetchSeq) {
        loading.value = false;
      }
    }
  };

  return {
    activeSite,
    activeSiteId,
    loading,
    setActiveSite,
    clearActiveSite,
    fetchSite,
  };
});
