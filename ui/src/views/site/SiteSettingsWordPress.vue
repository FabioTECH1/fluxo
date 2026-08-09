<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="flex flex-col gap-3 border-b border-gray-100 px-6 py-4 dark:border-gray-800 sm:flex-row sm:items-center sm:justify-between">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">WordPress configuration</h2>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Manage the site's wp-config.php file.</p>
        </div>
        <a v-if="site?.domain" :href="`${site.ssl_active ? 'https' : 'http'}://${site.domain}/wp-admin/`" target="_blank" rel="noopener noreferrer"
          class="inline-flex shrink-0 items-center justify-center rounded-lg bg-gray-900 px-3 py-2 text-sm font-semibold text-white hover:bg-gray-800 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-white">
          Open WordPress admin
        </a>
      </div>

      <div class="space-y-5 p-6">
        <div class="flex items-center justify-between">
          <p class="text-xs text-red-600 dark:text-red-400">This file contains database credentials and security keys.</p>
          <button type="button" :aria-label="revealed ? 'Hide WordPress configuration' : 'Reveal WordPress configuration'" @click="revealed = !revealed"
            class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs font-semibold text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700">
            <EyeSlashIcon v-if="revealed" class="h-4 w-4" aria-hidden="true" />
            <EyeIcon v-else class="h-4 w-4" aria-hidden="true" />
            {{ revealed ? 'Hide' : 'Reveal' }}
          </button>
        </div>

        <ScriptEditor
          v-model="content"
          language="plain"
          label="WordPress configuration editor"
          :readonly="saving"
          :busy="saving"
          :masked="!revealed"
          masked-message="Click to reveal WordPress configuration"
          @keydown="handleKeyDown"
          @reveal="revealed = true"
        />

        <div class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-gray-800">
          <span v-if="isDirty" class="text-xs font-semibold text-amber-600 dark:text-amber-400">Unsaved changes</span>
          <span v-else></span>
          <button type="button" :disabled="saving || !revealed || !isDirty" @click="save"
            class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50">
            {{ saving ? 'Saving...' : 'Save configuration' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { EyeIcon, EyeSlashIcon } from '@heroicons/vue/24/outline';
import { computed, onDeactivated, onMounted, ref, watch } from 'vue';
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import ScriptEditor from '../../components/ScriptEditor.vue';

const route = useRoute();
const { addToast } = useToast();
const { confirm } = useConfirm();
let siteId = route.params.id as string;
const site = ref<any>(null);
const content = ref('');
const initialContent = ref('');
const revealed = ref(false);
const saving = ref(false);
let dataRequestVersion = 0;

const isDirty = computed(() => content.value !== initialContent.value);

const handleKeyDown = (event: KeyboardEvent) => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault();
    save();
  }
};

const fetchData = async () => {
  const request = ++dataRequestVersion;
  const requestedSiteId = siteId;
  try {
    const [siteData, configData] = await Promise.all([
      apiClient.getSite(requestedSiteId, true),
      apiClient.getWordPressConfig(requestedSiteId, true),
    ]);
    if (request !== dataRequestVersion || requestedSiteId !== siteId) return;
    site.value = siteData;
    content.value = configData?.content || '';
    initialContent.value = content.value;
    revealed.value = false;
  } catch (e: any) {
    addToast(e.message || 'Failed to load WordPress configuration', 'error');
  }
};

const save = async () => {
  if (!revealed.value || !isDirty.value || saving.value) return;
  saving.value = true;
  const requestedSiteId = siteId;
  const contentToSave = content.value;
  try {
    await apiClient.saveWordPressConfig(requestedSiteId, contentToSave);
    if (siteId === requestedSiteId) {
      initialContent.value = contentToSave;
      revealed.value = false;
    }
    addToast('WordPress configuration saved', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save WordPress configuration', 'error');
  } finally {
    saving.value = false;
  }
};

const confirmDiscardChanges = async (to?: { path?: string }) => {
  if (to?.path === '/login') {
    content.value = initialContent.value;
    revealed.value = false;
    return true;
  }
  if (saving.value) {
    addToast('Please wait for the WordPress configuration save to finish.', 'info');
    return false;
  }
  if (!isDirty.value) return true;
  const approved = await confirm({
    title: 'Discard WordPress changes?',
    message: 'Your unsaved wp-config.php changes will be lost if you leave this page.',
    confirmText: 'Discard changes',
    cancelText: 'Keep editing',
    variant: 'danger',
  });
  if (approved) {
    content.value = initialContent.value;
    revealed.value = false;
  }
  return approved;
};

onBeforeRouteLeave(confirmDiscardChanges);
onBeforeRouteUpdate((to) => (
  to.params.id !== siteId ? confirmDiscardChanges(to) : true
));

onMounted(fetchData);
onDeactivated(() => { revealed.value = false; });
watch(() => route.params.id, newId => {
  if (typeof newId !== 'string' || !/^[1-9]\d*$/.test(newId) || newId === siteId) return;
  dataRequestVersion++;
  siteId = newId;
  site.value = null;
  content.value = '';
  initialContent.value = '';
  revealed.value = false;
  fetchData();
});
</script>
