import { computed, ref } from 'vue';
import { apiClient } from '../api/client';
import { apiErrorMessage } from '../utils/apiError';
import { useConfirm } from './useConfirm';
import type { WordPressCronState } from '../types/wordpressCron';

export function useWordPressCron(siteId: () => string) {
  const state = ref<WordPressCronState | null>(null);
  const loading = ref(false);
  const saving = ref(false);
  const error = ref('');
  const busy = computed(() => loading.value || saving.value);
  const { confirm } = useConfirm();
  let request = 0;

  async function refresh(preserveError = false) {
    const seq = ++request, id = siteId();
    loading.value = true;
    try {
      const result = await apiClient.getWordPressCron(id);
      if (seq === request && id === siteId()) {
        state.value = result;
        if (!preserveError) error.value = '';
      }
    } catch (failure) {
      if (seq === request && id === siteId()) {
        state.value = null;
        if (!preserveError) error.value = apiErrorMessage(failure, 'Could not load WordPress Cron.');
      }
    } finally {
      if (seq === request && id === siteId()) loading.value = false;
    }
  }

  async function setEnabled(enabled: boolean) {
    if (busy.value || !state.value) return false;
    const id = siteId();
    saving.value = true;
    try {
      const accepted = await confirm({
        title: enabled ? 'Enable WordPress Cron?' : 'Disable WordPress Cron?',
        message: enabled
          ? 'Run due WordPress tasks every minute, even without visitors. Fluxo will replace visitor-triggered scheduling for this site.'
          : 'Remove the managed schedule and restore visitor-triggered WordPress tasks.',
        confirmText: enabled ? 'Enable' : 'Disable', variant: enabled ? 'info' : 'danger',
      });
      if (!accepted || id !== siteId()) return false;
      error.value = '';
      const result = await apiClient.setWordPressCron(id, enabled);
      if (id === siteId()) state.value = result;
      return id === siteId();
    } catch (failure) {
      if (id === siteId()) {
        error.value = apiErrorMessage(failure, 'Could not change WordPress Cron.');
        // A failed publication may leave durable setup evidence. Show its
        // actual state and allow explicit retry/off instead of hiding it.
        await refresh(true);
      }
      return false;
    } finally {
      saving.value = false;
    }
  }
  return { state, busy, error, refresh, setEnabled };
}
