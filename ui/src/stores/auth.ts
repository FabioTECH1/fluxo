import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

const TOKEN_KEY = 'fluxo_jwt';

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(TOKEN_KEY) || '');

  const isAuthenticated = computed(() => !!token.value);

  const setToken = (value: string) => {
    token.value = value;
    localStorage.setItem(TOKEN_KEY, value);
  };

  const clearToken = () => {
    token.value = '';
    localStorage.removeItem(TOKEN_KEY);
  };

  const syncFromStorage = () => {
    token.value = localStorage.getItem(TOKEN_KEY) || '';
  };

  return {
    token,
    isAuthenticated,
    setToken,
    clearToken,
    syncFromStorage,
  };
});
