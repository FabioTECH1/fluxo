<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="flex flex-col gap-3 border-b border-gray-100 px-6 py-4 dark:border-gray-800 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">WordPress configuration</h2>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Manage the site's wp-config.php file.</p>
        </div>
        <a v-if="site?.domain" :href="`${site.ssl_active ? 'https' : 'http'}://${site.domain}/wp-admin/`" target="_blank" rel="noopener noreferrer"
          class="inline-flex items-center justify-center rounded-lg bg-gray-900 px-3 py-2 text-sm font-semibold text-white hover:bg-gray-800 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-white">
          Open WordPress admin
        </a>
      </div>

      <div class="space-y-5 p-6">
        <div class="flex items-center justify-between">
          <p class="text-xs text-red-600 dark:text-red-400">This file contains database credentials and security keys.</p>
          <button type="button" @click="revealed = !revealed"
            class="rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs font-semibold text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300">
            {{ revealed ? 'Hide' : 'Reveal' }}
          </button>
        </div>

        <div class="relative flex h-[calc(100vh-22rem)] min-h-96 w-full overflow-hidden rounded-lg border border-gray-200 bg-gray-50 focus-within:border-blue-500 focus-within:ring-2 focus-within:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
          <div ref="lineNumbersRef" class="w-12 shrink-0 select-none overflow-hidden border-r border-gray-200 bg-gray-100 py-3 dark:border-gray-700 dark:bg-gray-900">
            <div v-for="line in lineCount" :key="line" class="px-2 text-right font-mono text-xs leading-5 text-gray-400">{{ line }}</div>
          </div>
          <textarea ref="textareaRef" v-model="content" :readonly="!revealed" data-gramm="false"
            class="h-full min-w-0 flex-1 resize-none bg-transparent p-3 font-mono text-sm leading-5 text-gray-900 outline-none dark:text-gray-100"
            :class="revealed ? '' : 'blur-sm select-none'" @scroll="syncScroll" @keydown.ctrl.s.prevent="save" @keydown.meta.s.prevent="save"></textarea>
          <button v-if="!revealed" type="button" class="absolute inset-0 bg-gray-50/60 text-sm font-semibold text-gray-500 backdrop-blur-sm dark:bg-gray-800/60 dark:text-gray-400" @click="revealed = true">
            Reveal WordPress configuration
          </button>
        </div>

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
import { computed, onActivated, onMounted, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRoute } from 'vue-router';
import { apiClient } from '../../api/client';
import { useToast } from '../../composables/useToast';

const route = useRoute();
const { addToast } = useToast();
let siteId = route.params.id as string;
const site = ref<any>(null);
const content = ref('');
const initialContent = ref('');
const revealed = ref(false);
const saving = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const lineNumbersRef = ref<HTMLElement | null>(null);

const lineCount = computed(() => Math.max(content.value.split('\n').length, 20));
const isDirty = computed(() => content.value !== initialContent.value);

const syncScroll = () => {
  if (textareaRef.value && lineNumbersRef.value) {
    lineNumbersRef.value.scrollTop = textareaRef.value.scrollTop;
  }
};

const fetchData = async () => {
  try {
    const [siteData, configData] = await Promise.all([
      apiClient.getSite(siteId),
      apiClient.getWordPressConfig(siteId, true),
    ]);
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
  try {
    await apiClient.saveWordPressConfig(siteId, content.value);
    initialContent.value = content.value;
    revealed.value = false;
    addToast('WordPress configuration saved', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save WordPress configuration', 'error');
  } finally {
    saving.value = false;
  }
};

onBeforeRouteLeave((_to, _from, next) => {
  if (isDirty.value && !window.confirm('You have unsaved WordPress configuration changes. Leave this page?')) {
    next(false);
    return;
  }
  next();
});

onMounted(fetchData);
onActivated(fetchData);
watch(() => route.params.id, newId => {
  siteId = newId as string;
  fetchData();
});
</script>
