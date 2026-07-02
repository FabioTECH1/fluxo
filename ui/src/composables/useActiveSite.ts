import { storeToRefs } from 'pinia';
import { useSiteStore } from '../stores/site';

export function useActiveSite() {
  const siteStore = useSiteStore();
  const { activeSite } = storeToRefs(siteStore);

  return {
    activeSite,
    setActiveSite: siteStore.setActiveSite,
    clearActiveSite: siteStore.clearActiveSite,
  };
}
