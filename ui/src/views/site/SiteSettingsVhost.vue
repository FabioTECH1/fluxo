<template>
  <div class="space-y-6">
    <Card :padding="false">
      <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-6 py-4 dark:border-gray-800">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Nginx Vhost</h2>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Edit the complete virtual-host configuration for this site.</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <StatusBadge v-if="loaded" :label="state.customized ? 'Customized' : 'Fluxo managed'" :variant="state.customized ? 'yellow' : 'green'" />
          <span v-if="isDirty" class="inline-flex items-center gap-1.5 rounded border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-semibold text-amber-700 dark:border-amber-900/50 dark:bg-amber-900/30 dark:text-amber-400">
            <span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>
            Unsaved Changes
          </span>
        </div>
      </div>

      <div class="space-y-5 p-6">
        <ErrorAlert :message="errorMessage" />

        <SkeletonLoader v-if="loading" type="block" height="h-[32rem]" />

        <template v-else-if="loaded">
          <div
            class="rounded-lg border p-4"
            :class="state.customized
              ? 'border-amber-200 bg-amber-50 dark:border-amber-900/60 dark:bg-amber-950/30'
              : 'border-blue-200 bg-blue-50 dark:border-blue-900/60 dark:bg-blue-950/30'"
          >
            <p class="text-sm font-semibold" :class="state.customized ? 'text-amber-900 dark:text-amber-200' : 'text-blue-900 dark:text-blue-200'">
              {{ state.customized ? 'Custom configuration is active' : 'Fluxo is managing this configuration' }}
            </p>
            <p class="mt-1 text-xs leading-5" :class="state.customized ? 'text-amber-800 dark:text-amber-300' : 'text-blue-800 dark:text-blue-300'">
              <template v-if="state.customized">
                Fluxo preserves this exact vhost during domain, SSL, PHP, runtime, and upgrade operations. Those settings are not inserted into a customized vhost until you restore the Fluxo default.
              </template>
              <template v-else>
                This is the current generated default. Saving an edit switches this site to a durable custom vhost.
              </template>
            </p>
          </div>

          <div>
            <div class="mb-2 flex flex-wrap items-end justify-between gap-2">
              <div>
                <label for="site-vhost-editor" class="block text-sm font-bold text-gray-700 dark:text-gray-300">Configuration</label>
                <p class="mt-0.5 break-all font-mono text-xs text-gray-500 dark:text-gray-400">{{ state.path }}</p>
              </div>
              <span class="text-xs text-gray-500 dark:text-gray-400">Maximum 256 KiB</span>
            </div>
            <ScriptEditor
              id="site-vhost-editor"
              v-model="config"
              language="plain"
              label="Nginx virtual host configuration editor"
              :visible-lines="25"
              :minimum-lines="25"
              :readonly="busy"
              :busy="busy"
              @keydown="handleKeyDown"
            />
          </div>

          <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
            Saving stages this configuration, runs <code class="font-mono">nginx -t</code>, and reloads Nginx only after validation. A failed validation or activation automatically restores the last working vhost.
          </p>

          <div class="flex flex-col-reverse gap-3 border-t border-gray-100 pt-4 sm:flex-row sm:items-center sm:justify-between dark:border-gray-800">
            <div class="flex flex-wrap justify-end gap-2 sm:justify-start">
              <AppButton v-if="state.customized" variant="secondary" size="sm" :disabled="busy" :loading="restoring" @click="restoreDefault">
                Restore Fluxo Default
              </AppButton>
              <AppButton v-if="isDirty" variant="secondary" size="sm" :disabled="busy" @click="discardChanges">
                Discard Changes
              </AppButton>
            </div>
            <div class="flex justify-end">
              <AppButton variant="primary" :loading="saving" :disabled="busy || !isDirty || !config.trim()" @click="saveVhost">
                Save Vhost
              </AppButton>
            </div>
          </div>
        </template>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onMounted, ref, watch } from 'vue';
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import Card from '../../components/Card.vue';
import ErrorAlert from '../../components/ErrorAlert.vue';
import ScriptEditor from '../../components/ScriptEditor.vue';
import SkeletonLoader from '../../components/SkeletonLoader.vue';
import StatusBadge from '../../components/StatusBadge.vue';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import { useUndoRedo } from '../../composables/useUndoRedo';

interface VhostState {
  config: string;
  customized: boolean;
  revision: string;
  path: string;
  updated_at?: string;
}

const emptyState = (): VhostState => ({ config: '', customized: false, revision: '', path: '' });
const route = useRoute();
let siteId = route.params.id as string;
const state = ref<VhostState>(emptyState());
const config = ref('');
const initialConfig = ref('');
const loading = ref(true);
const loaded = ref(false);
const saving = ref(false);
const restoring = ref(false);
const errorMessage = ref('');
let requestVersion = 0;

const { confirm } = useConfirm();
const { addToast, showToast, updateToast } = useToast();
const { undo, redo, resetHistory } = useUndoRedo(config);
const busy = computed(() => loading.value || saving.value || restoring.value);
const isDirty = computed(() => loaded.value && config.value !== initialConfig.value);

const applyState = (next: VhostState) => {
  state.value = { ...emptyState(), ...next };
  config.value = state.value.config;
  initialConfig.value = state.value.config;
  errorMessage.value = '';
  loaded.value = true;
  resetHistory();
};

const fetchVhost = async (silent = false) => {
  const request = ++requestVersion;
  const requestedSiteId = siteId;
  if (!silent) loading.value = true;
  try {
    const next = await apiClient.getSiteVhost(requestedSiteId);
    if (request === requestVersion && requestedSiteId === siteId) applyState(next);
  } catch (error: any) {
    if (request === requestVersion && requestedSiteId === siteId) {
      errorMessage.value = error.message || 'Failed to load the site vhost.';
      if (!silent) loaded.value = false;
    }
  } finally {
    if (request === requestVersion && requestedSiteId === siteId) loading.value = false;
  }
};

const discardChanges = () => {
  config.value = initialConfig.value;
  resetHistory();
};

const saveVhost = async () => {
  if (busy.value || !isDirty.value || !config.value.trim()) return;
  const approved = await confirm({
    title: 'Save custom Nginx vhost?',
    message: 'Fluxo will validate this complete configuration and reload Nginx. A valid but incorrect vhost can make this site unavailable or bypass future managed domain and SSL changes.',
    confirmText: 'Validate and Save',
    cancelText: 'Keep Editing',
    variant: 'info',
  });
  if (!approved) return;

  saving.value = true;
  errorMessage.value = '';
  const toastId = showToast({ title: 'Validating Nginx vhost', description: 'The current working configuration remains recoverable.', type: 'loading' });
  try {
    const next = await apiClient.updateSiteVhost(siteId, config.value, state.value.revision);
    applyState(next);
    updateToast(toastId, { title: 'Custom vhost saved', description: null, type: 'success' });
  } catch (error: any) {
    errorMessage.value = error.message || 'The vhost could not be saved.';
    updateToast(toastId, { title: 'Vhost was not changed', description: errorMessage.value, type: 'error' });
  } finally {
    saving.value = false;
  }
};

const restoreDefault = async () => {
  if (busy.value || !state.value.customized) return;
  const approved = await confirm({
    title: 'Restore Fluxo default vhost?',
    message: 'This removes the custom vhost and generates a fresh configuration from the site’s current domain, SSL, application runtime, and directory settings. Copy any custom directives you want to keep before continuing.',
    confirmText: 'Restore Default',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!approved) return;

  restoring.value = true;
  errorMessage.value = '';
  const toastId = showToast({ title: 'Restoring Fluxo vhost', description: 'Generating and validating the current managed default.', type: 'loading' });
  try {
    const next = await apiClient.restoreSiteVhost(siteId, state.value.revision);
    applyState(next);
    updateToast(toastId, { title: 'Fluxo default vhost restored', description: null, type: 'success' });
  } catch (error: any) {
    errorMessage.value = error.message || 'The default vhost could not be restored.';
    updateToast(toastId, { title: 'Custom vhost remains active', description: errorMessage.value, type: 'error' });
  } finally {
    restoring.value = false;
  }
};

const handleKeyDown = (event: KeyboardEvent) => {
  const key = event.key.toLowerCase();
  if ((event.ctrlKey || event.metaKey) && key === 's') {
    event.preventDefault();
    saveVhost();
  } else if ((event.ctrlKey || event.metaKey) && key === 'z' && !event.shiftKey) {
    event.preventDefault();
    undo();
  } else if ((event.ctrlKey || event.metaKey) && (key === 'y' || (key === 'z' && event.shiftKey))) {
    event.preventDefault();
    redo();
  }
};

const confirmDiscard = async (to?: { path?: string }) => {
  if (to?.path === '/login') {
    discardChanges();
    return true;
  }
  if (saving.value || restoring.value) {
    addToast('Please wait for the vhost operation to finish.', 'info');
    return false;
  }
  if (!isDirty.value) return true;
  const approved = await confirm({
    title: 'Discard vhost changes?',
    message: 'Your unsaved Nginx configuration changes will be lost if you leave this page.',
    confirmText: 'Discard Changes',
    cancelText: 'Keep Editing',
    variant: 'danger',
  });
  if (approved) discardChanges();
  return approved;
};

onBeforeRouteLeave(confirmDiscard);
onBeforeRouteUpdate((to) => (to.params.id !== siteId ? confirmDiscard(to) : true));

onMounted(() => fetchVhost());
onActivated(() => {
  if (!isDirty.value && loaded.value) fetchVhost(true);
});
watch(() => route.params.id, (nextId) => {
  if (typeof nextId !== 'string' || !/^[1-9]\d*$/.test(nextId) || nextId === siteId) return;
  requestVersion++;
  siteId = nextId;
  state.value = emptyState();
  config.value = '';
  initialConfig.value = '';
  loaded.value = false;
  fetchVhost();
});
</script>
