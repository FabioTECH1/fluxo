<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Environment</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Edit this application's .env file. Fluxo removes the file if the application is uninstalled.</p>
        </div>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <p class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Environment variables</p>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Your application's environment variables.</p>
          <p class="text-xs text-red-600 mb-2 dark:text-red-400">Environment variables should not be shared publicly.</p>
          <div class="relative w-full">
            <div class="flex justify-between items-center mb-2">
              <div class="flex items-center gap-2">
                <span class="text-xs text-gray-400 dark:text-gray-500">{{ lineCount }} {{ lineCount === 1 ? 'line' : 'lines' }}</span>
                <span v-if="isDirty" class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[10px] font-semibold bg-amber-50 text-amber-700 border border-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:border-amber-900/50 animate-pulse">
                  <span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>
                  Unsaved Changes
                </span>
              </div>
              <button type="button" :aria-label="revealed ? 'Hide environment variables' : 'Reveal environment variables'" @click="revealed = !revealed" class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-600 bg-white border border-gray-200 rounded-lg hover:bg-gray-50 font-semibold transition-colors dark:text-gray-400 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-700">
                <EyeSlashIcon v-if="revealed" class="h-4 w-4" aria-hidden="true" />
                <EyeIcon v-else class="h-4 w-4" aria-hidden="true" />
                {{ revealed ? 'Hide' : 'Reveal' }}
              </button>
            </div>
            <ScriptEditor
              v-model="envContent"
              language="env"
              label="Environment variables editor"
              placeholder="APP_NAME=Laravel&#10;APP_ENV=production&#10;APP_KEY=&#10;APP_DEBUG=false"
              :readonly="saving"
              :busy="saving"
              :masked="!revealed"
              masked-message="Click to reveal environment variables"
              @keydown="handleKeyDown"
              @reveal="revealed = true"
            />
          </div>
        </div>

        <ToggleSwitch v-if="site?.app_type === 'laravel'" v-model="cacheConfig" label="Cache configuration" label-position="left" :disabled="saving"
          description="Run php artisan config:cache after updating environment variables." />

        <div class="flex justify-end pt-2 border-t border-gray-100 dark:border-gray-800">
          <button @click="saveEnv" :disabled="saving" 
            class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-all disabled:opacity-50"
            :class="isDirty ? 'ring-2 ring-blue-500/50 ring-offset-2 dark:ring-offset-gray-900 shadow-lg' : ''">
            {{ saving ? 'Saving...' : 'Save environment' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { EyeIcon, EyeSlashIcon } from '@heroicons/vue/24/outline';
import { ref, computed, onDeactivated, onMounted, watch } from 'vue';
import { useRoute, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import { useConfirm } from '../../composables/useConfirm';
import { useUndoRedo } from '../../composables/useUndoRedo';
import ScriptEditor from '../../components/ScriptEditor.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';

const route = useRoute();
let siteId = route.params.id as string;
const { addToast, showToast, updateToast } = useToast();
const { confirm } = useConfirm();

const envContent = ref('');
const site = ref<any>(null);
const { undo: undoEnv, redo: redoEnv, resetHistory } = useUndoRedo(envContent);
const initialEnvContent = ref('');
const revealed = ref(false);
const cacheConfig = ref(false);
const saving = ref(false);
let envRequestVersion = 0;
let siteRequestVersion = 0;

const lineCount = computed(() => {
  return envContent.value.split('\n').length;
});

const isDirty = computed(() => {
  return envContent.value !== initialEnvContent.value;
});

const handleKeyDown = (e: KeyboardEvent, textarea: HTMLTextAreaElement) => {
  const key = e.key.toLowerCase();
  if ((e.ctrlKey || e.metaKey) && key === 'z' && !e.shiftKey) {
    e.preventDefault();
    undoEnv();
  } else if ((e.ctrlKey || e.metaKey) && (key === 'y' || (key === 'z' && e.shiftKey))) {
    e.preventDefault();
    redoEnv();
  } else if ((e.ctrlKey || e.metaKey) && key === 's') {
    e.preventDefault();
    if (!saving.value && revealed.value) {
      saveEnv();
    }
  } else if ((e.ctrlKey || e.metaKey) && key === '/') {
    e.preventDefault();

    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    const text = textarea.value;
    
    const startLineIndex = text.lastIndexOf('\n', start - 1) + 1;
    let endLineIndex = text.indexOf('\n', end);
    if (endLineIndex === -1) endLineIndex = text.length;
    
    const selectedText = text.substring(startLineIndex, endLineIndex);
    const lines = selectedText.split('\n');
    
    const allCommented = lines.every(line => line.trim().startsWith('#') || line.trim() === '');
    
    const newLines = lines.map(line => {
      if (allCommented) {
        if (line.trim().startsWith('#')) {
          return line.replace(/^\s*#\s?/, '');
        }
        return line;
      } else {
        return `# ${line}`;
      }
    });
    
    const newText = text.substring(0, startLineIndex) + newLines.join('\n') + text.substring(endLineIndex);
    envContent.value = newText;
    
    setTimeout(() => {
      textarea.focus();
      textarea.setSelectionRange(startLineIndex, startLineIndex + newLines.join('\n').length);
    }, 0);
  } else if ((e.ctrlKey || e.metaKey) && key === 'c') {
    if (textarea.selectionStart === textarea.selectionEnd) {
      const pos = textarea.selectionStart;
      const text = textarea.value;
      const startLine = text.lastIndexOf('\n', pos - 1) + 1;
      let endLine = text.indexOf('\n', pos);
      if (endLine === -1) endLine = text.length;
      textarea.setSelectionRange(startLine, endLine < text.length ? endLine + 1 : endLine);
      window.setTimeout(() => {
        if (document.activeElement === textarea) textarea.setSelectionRange(pos, pos);
      }, 0);
    }
  } else if ((e.ctrlKey || e.metaKey) && key === 'x') {
    if (textarea.selectionStart === textarea.selectionEnd) {
      const pos = textarea.selectionStart;
      const text = textarea.value;
      let startLine = text.lastIndexOf('\n', pos - 1) + 1;
      let endLine = text.indexOf('\n', pos);
      if (endLine === -1) {
        endLine = text.length;
        if (startLine > 0) startLine -= 1;
      } else {
        endLine += 1;
      }
      textarea.setSelectionRange(startLine, endLine);
    }
  }
};

const fetchEnv = async () => {
  const request = ++envRequestVersion;
  const requestedSiteId = siteId;
  try {
    const data = await apiClient.getSiteEnv(requestedSiteId, true);
    if (request !== envRequestVersion || requestedSiteId !== siteId) return;
    envContent.value = data.content || '';
    initialEnvContent.value = data.content || '';
    resetHistory();
  } catch (e) {}
};

const fetchSite = async () => {
  const request = ++siteRequestVersion;
  const requestedSiteId = siteId;
  try {
    const nextSite = await apiClient.getSite(requestedSiteId);
    if (request === siteRequestVersion && requestedSiteId === siteId) site.value = nextSite;
  } catch (e) {}
};

const saveEnv = async () => {
  if (saving.value) return;
  saving.value = true;
  const requestedSiteId = siteId;
  const contentToSave = envContent.value;
  const toastId = showToast({
    title: 'Saving environment',
    description: 'This may take a moment.',
    type: 'loading',
  });
  try {
    await apiClient.saveSiteEnv(requestedSiteId, contentToSave);
    if (siteId === requestedSiteId) initialEnvContent.value = contentToSave;

    if (cacheConfig.value) {
      updateToast(toastId, {
        title: 'Caching configuration',
        description: 'The environment was saved. Finishing up now.',
        type: 'loading',
      });
      try {
        await apiClient.runSiteCommand(requestedSiteId, { command: 'artisan config:cache' });
        updateToast(toastId, {
          title: 'Environment saved',
          description: 'The configuration cache was rebuilt successfully.',
          type: 'success',
        });
      } catch (err: any) {
        updateToast(toastId, {
          title: 'Environment saved with a warning',
          description: err.message || 'The configuration cache could not be rebuilt.',
          type: 'warning',
        });
      }
    } else {
      updateToast(toastId, {
        title: 'Environment saved',
        description: null,
        type: 'success',
      });
    }

    if (siteId === requestedSiteId) revealed.value = false;
  } catch (e: any) {
    updateToast(toastId, {
      title: 'Environment could not be saved',
      description: e.message || 'Please try again.',
      type: 'error',
    });
  } finally {
    saving.value = false;
  }
};

const confirmDiscardChanges = async (to?: { path?: string }) => {
  if (to?.path === '/login') {
    envContent.value = initialEnvContent.value;
    resetHistory();
    revealed.value = false;
    return true;
  }
  if (saving.value) {
    addToast('Please wait for the environment save to finish.', 'info');
    return false;
  }
  if (!isDirty.value) return true;
  const approved = await confirm({
    title: 'Discard environment changes?',
    message: 'Your unsaved .env changes will be lost if you leave this page.',
    confirmText: 'Discard changes',
    cancelText: 'Keep editing',
    variant: 'danger',
  });
  if (approved) {
    envContent.value = initialEnvContent.value;
    resetHistory();
    revealed.value = false;
  }
  return approved;
};

onBeforeRouteLeave(confirmDiscardChanges);
onBeforeRouteUpdate((to) => (
  to.params.id !== siteId ? confirmDiscardChanges(to) : true
));

onMounted(() => { fetchSite(); fetchEnv(); });
onDeactivated(() => { revealed.value = false; });

watch(() => route.params.id, (newId) => {
  if (typeof newId !== 'string' || !/^[1-9]\d*$/.test(newId) || newId === siteId) return;
  envRequestVersion++;
  siteRequestVersion++;
  siteId = newId;
  site.value = null;
  envContent.value = '';
  initialEnvContent.value = '';
  resetHistory();
  revealed.value = false;
  cacheConfig.value = false;
  fetchSite();
  fetchEnv();
});
</script>
