<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Environment</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Below you may edit the .env file for your application, which is a standard default environment file that typically loaded by applications. If the application is uninstalled, the environment file will also be removed.</p>
        </div>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Environment variables</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Your application's environment variables.</p>
          <p class="text-xs text-red-600 mb-2 dark:text-red-400">Environment variables should not be shared publicly.</p>
          <div class="relative w-full">
            <div class="flex justify-between items-center mb-2">
              <div class="flex items-center gap-2">
                <span class="text-xs text-gray-400 dark:text-gray-500">{{ lineCount }} lines</span>
                <span v-if="isDirty" class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[10px] font-semibold bg-amber-50 text-amber-700 border border-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:border-amber-900/50 animate-pulse">
                  <span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>
                  Unsaved Changes
                </span>
              </div>
              <button @click="revealed = !revealed" class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-600 bg-white border border-gray-200 rounded-lg hover:bg-gray-50 font-semibold transition-colors dark:text-gray-400 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-800">
                <span v-if="!revealed">&#128065;</span>
                <span v-else>&#128064;</span>
                {{ revealed ? 'Hide' : 'Reveal' }}
              </button>
            </div>
            <div class="relative flex w-full h-[calc(100vh-24rem)] bg-gray-50 border border-gray-200 rounded-lg overflow-hidden focus-within:ring-2 focus-within:ring-blue-500 focus-within:border-blue-500 transition-shadow dark:bg-gray-800 dark:border-gray-600">
              <div id="env-line-numbers" class="w-10 shrink-0 bg-gray-100 border-r border-gray-200 py-2 select-none overflow-hidden dark:bg-gray-800 dark:border-gray-600">
                <div v-for="n in lineCount" :key="n" class="text-right px-2 text-xs font-mono text-gray-400 leading-5 dark:text-gray-500">{{ n }}</div>
              </div>
              <div class="relative flex-1 min-w-0 h-full overflow-hidden">
                <div v-if="revealed" ref="highlightRef"
                  class="absolute inset-0 pointer-events-none p-2 font-mono text-sm leading-5 overflow-hidden whitespace-pre-wrap break-all dark:text-gray-100"
                  v-html="highlightedContent"></div>
                <textarea v-model="envContent" @scroll="syncScroll" ref="textareaRef" @keydown="handleKeyDown" data-gramm="false"
                  class="block w-full h-full font-mono text-sm p-2 bg-transparent resize-none outline-none leading-5 whitespace-pre-wrap break-all"
                  :class="revealed ? 'text-transparent caret-gray-900 dark:caret-gray-100' : 'text-gray-900 dark:text-gray-100'"
                  :readonly="!revealed"
                  placeholder="APP_NAME=Laravel&#10;APP_ENV=production&#10;APP_KEY=&#10;APP_DEBUG=false"></textarea>
                <!-- Blur overlay when not revealed -->
                <div v-if="!revealed"
                  class="absolute inset-0 flex flex-col items-center justify-center gap-3 cursor-pointer backdrop-blur-sm bg-gray-50/60 dark:bg-gray-800/60"
                  @click="revealed = true">
                  <svg class="w-8 h-8 text-gray-400 dark:text-gray-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
                  </svg>
                  <span class="text-xs font-semibold text-gray-500 dark:text-gray-400">Click to reveal environment variables</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <ToggleSwitch v-model="cacheConfig" label="Cache configuration" label-position="left"
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
import { ref, computed, onMounted, onActivated, watch } from 'vue';
import { useRoute, onBeforeRouteLeave } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import { useUndoRedo } from '../../composables/useUndoRedo';
import ToggleSwitch from '../../components/ToggleSwitch.vue';

const route = useRoute();
let siteId = route.params.id as string;
const { addToast } = useToast();

const envContent = ref('');
const { undo: undoEnv, redo: redoEnv } = useUndoRedo(envContent);
const initialEnvContent = ref('');
const revealed = ref(false);
const cacheConfig = ref(false);
const saving = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const highlightRef = ref<HTMLDivElement | null>(null);

const highlightedContent = computed(() => {
  const text = envContent.value || '';
  const escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
  
  return escaped.split('\n').map(line => {
    const trimmed = line.trim();
    if (trimmed.startsWith('#')) {
      return `<span class="text-gray-400 dark:text-gray-500 font-normal italic">${line}</span>`;
    }
    const eqIdx = line.indexOf('=');
    if (eqIdx !== -1) {
      const key = line.substring(0, eqIdx);
      const val = line.substring(eqIdx);
      return `<span class="text-blue-600 dark:text-blue-400 font-semibold">${key}</span><span class="text-emerald-600 dark:text-emerald-400">${val}</span>`;
    }
    return line;
  }).join('\n');
});

const lineCount = computed(() => {
  const lines = envContent.value.split('\n').length;
  return Math.max(lines, 15);
});

const isDirty = computed(() => {
  return envContent.value !== initialEnvContent.value;
});

const syncScroll = () => {
  const lineEl = document.getElementById('env-line-numbers');
  if (lineEl && textareaRef.value) {
    lineEl.scrollTop = textareaRef.value.scrollTop;
  }
  if (highlightRef.value && textareaRef.value) {
    highlightRef.value.scrollTop = textareaRef.value.scrollTop;
    highlightRef.value.scrollLeft = textareaRef.value.scrollLeft;
  }
};

const handleKeyDown = (e: KeyboardEvent) => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'z') {
    e.preventDefault();
    undoEnv();
  } else if ((e.ctrlKey || e.metaKey) && e.key === 'y') {
    e.preventDefault();
    redoEnv();
  } else if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault();
    if (!saving.value && revealed.value) {
      saveEnv();
    }
  } else if ((e.ctrlKey || e.metaKey) && e.key === '/') {
    e.preventDefault();
    const textarea = textareaRef.value;
    if (!textarea) return;
    
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
  } else if ((e.ctrlKey || e.metaKey) && e.key === 'c') {
    const textarea = textareaRef.value;
    if (textarea && textarea.selectionStart === textarea.selectionEnd) {
      e.preventDefault();
      const pos = textarea.selectionStart;
      const text = textarea.value;
      const startLine = text.lastIndexOf('\n', pos - 1) + 1;
      let endLine = text.indexOf('\n', pos);
      if (endLine === -1) endLine = text.length;
      
      const lineText = text.substring(startLine, endLine) + (endLine < text.length ? '\n' : '');
      navigator.clipboard.writeText(lineText).catch(() => {});
      addToast('Line copied to clipboard', 'success');
    }
  } else if ((e.ctrlKey || e.metaKey) && e.key === 'x') {
    const textarea = textareaRef.value;
    if (textarea && textarea.selectionStart === textarea.selectionEnd) {
      e.preventDefault();
      const pos = textarea.selectionStart;
      const text = textarea.value;
      const startLine = text.lastIndexOf('\n', pos - 1) + 1;
      let endLine = text.indexOf('\n', pos);
      if (endLine === -1) {
        endLine = text.length;
      } else {
        endLine += 1;
      }
      
      const lineText = text.substring(startLine, endLine);
      navigator.clipboard.writeText(lineText).catch(() => {});
      
      const newText = text.substring(0, startLine) + text.substring(endLine);
      envContent.value = newText;
      
      setTimeout(() => {
        textarea.focus();
        textarea.setSelectionRange(startLine, startLine);
      }, 0);
    }
  }
};

const fetchEnv = async () => {
  try {
    const data = await apiClient.getSiteEnv(siteId);
    envContent.value = data.content || '';
    initialEnvContent.value = data.content || '';
  } catch (e) {}
};

const saveEnv = async () => {
  saving.value = true;
  try {
    await apiClient.saveSiteEnv(siteId, envContent.value);
    initialEnvContent.value = envContent.value;
    addToast('Environment saved', 'success');
    
    if (cacheConfig.value) {
      addToast('Caching configuration...', 'info');
      try {
        await apiClient.runSiteCommand(siteId, { command: 'artisan config:cache' });
        addToast('Configuration cached successfully', 'success');
      } catch (err: any) {
        addToast(err.message || 'Failed to cache configuration', 'error');
      }
    }
    
    revealed.value = false;
  } catch (e: any) {
    addToast(e.message || 'Failed to save', 'error');
  } finally {
    saving.value = false;
  }
};

onBeforeRouteLeave((_to, _from, next) => {
  if (isDirty.value) {
    const answer = window.confirm('You have unsaved changes in your .env file. Do you really want to leave?');
    if (!answer) {
      next(false);
      return;
    }
  }
  next();
});

onMounted(fetchEnv);
onActivated(fetchEnv);

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  fetchEnv();
});
</script>
