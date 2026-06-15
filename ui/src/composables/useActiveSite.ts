import { ref } from 'vue';

const activeSite = ref<any>(null);

export function useActiveSite() {
  const setActiveSite = (site: any) => {
    activeSite.value = site;
  };

  const clearActiveSite = () => {
    activeSite.value = null;
  };

  return {
    activeSite,
    setActiveSite,
    clearActiveSite,
  };
}
