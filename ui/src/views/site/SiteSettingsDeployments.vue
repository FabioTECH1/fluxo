<template>
  <div v-if="site" class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <div class="flex justify-between items-center">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Deployments</h2>
            <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage build and deployment settings.</p>
          </div>
          <span v-if="isDirty" class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[10px] font-semibold bg-amber-50 text-amber-700 border border-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:border-amber-900/50 animate-pulse">
            <span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>
            Unsaved Changes
          </span>
        </div>
      </div>
      <div class="p-6 space-y-5">
        <div class="flex items-start justify-between">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Push to deploy</label>
            <p class="text-xs text-gray-500 dark:text-gray-400">Automatically trigger a new deployment when changes are pushed to the environment's Git branch.</p>
          </div>
          <button @click="togglePushToDeploy" type="button"
            :class="form.push_to_deploy ? 'bg-blue-600' : 'bg-gray-200'"
            class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0">
            <span :class="form.push_to_deploy ? 'translate-x-6' : 'translate-x-1'"
              class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
          </button>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Deploy script</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">The commands that will be run to deploy your application. Deployments are limited to 10 minutes. If a deployment takes longer, it will fail automatically.</p>
          <div class="relative w-full h-128 border border-gray-200 rounded-lg overflow-hidden focus-within:ring-2 focus-within:ring-blue-500 focus-within:border-blue-500 transition-shadow dark:bg-gray-800 dark:border-gray-600">
            <div ref="highlightRef"
              class="absolute inset-0 pointer-events-none p-3 font-mono text-xs leading-5 overflow-hidden whitespace-pre-wrap break-all dark:text-gray-100"
              v-html="highlightedContent"></div>
            <textarea v-model="form.deploy_script" @scroll="syncScroll" ref="textareaRef" @keydown="handleKeyDown"
              class="block w-full h-full font-mono text-xs p-3 bg-transparent resize-none outline-none leading-5 text-transparent caret-gray-900 dark:caret-gray-100 whitespace-pre-wrap break-all"
              placeholder="git pull origin main&#10;composer install --no-dev --optimize-autoloader&#10;php artisan migrate --force"></textarea>
          </div>
        </div>

        <div class="flex items-start justify-between">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Make .env variables available to deployment script</label>
          </div>
          <button @click="toggleExposeEnv" type="button"
            :class="form.expose_env ? 'bg-blue-600' : 'bg-gray-200'"
            class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0">
            <span :class="form.expose_env ? 'translate-x-6' : 'translate-x-1'"
              class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
          </button>
        </div>

        <div class="flex justify-end pt-2 border-t border-gray-100 dark:border-gray-800">
          <button @click="saveSettings" :disabled="saving"
            class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-all disabled:opacity-50"
            :class="isDirty ? 'ring-2 ring-blue-500/50 ring-offset-2 dark:ring-offset-gray-900 shadow-lg' : ''">
            {{ saving ? 'Saving...' : 'Save settings' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onActivated, inject } from 'vue';
import { useRoute, onBeforeRouteLeave } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';

const route = useRoute();
const siteId = route.params.id as string;
const { addToast } = useToast();

const site = ref<any>(null);
const parentSite = inject<any>('site', null);
const form = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const initialForm = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const saving = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const highlightRef = ref<HTMLDivElement | null>(null);

const highlightedContent = computed(() => {
  const text = form.value.deploy_script || '';
  const escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
  
  return escaped.split('\n').map(line => {
    const trimmed = line.trim();
    if (trimmed.startsWith('#')) {
      return `<span class="text-gray-400 dark:text-gray-500 font-normal italic">${line}</span>`;
    }
    return line.replace(/\b(git|composer|npm|php|artisan|sudo|systemctl|mkdir|chown|chmod|cd|cp|mv|rm|echo|export|set|if|then|fi|else|elif)\b/g, '<span class="text-blue-600 dark:text-blue-400 font-semibold">$1</span>');
  }).join('\n');
});

const syncScroll = () => {
  if (highlightRef.value && textareaRef.value) {
    highlightRef.value.scrollTop = textareaRef.value.scrollTop;
    highlightRef.value.scrollLeft = textareaRef.value.scrollLeft;
  }
};

const isDirty = computed(() => {
  return form.value.push_to_deploy !== initialForm.value.push_to_deploy ||
         form.value.deploy_script !== initialForm.value.deploy_script ||
         form.value.expose_env !== initialForm.value.expose_env;
});

const handleKeyDown = (e: KeyboardEvent) => {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault();
    if (!saving.value) {
      saveSettings();
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
    form.value.deploy_script = newText;
    
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
      form.value.deploy_script = newText;
      
      setTimeout(() => {
        textarea.focus();
        textarea.setSelectionRange(startLine, startLine);
      }, 0);
    }
  }
};

const fetchSite = async () => {
  if (parentSite?.value?.id) {
    site.value = parentSite.value;
    form.value = { push_to_deploy: !!site.value.push_to_deploy, deploy_script: site.value.deploy_script || '', expose_env: !!site.value.expose_env };
    initialForm.value = { ...form.value };
    return;
  }
  try {
    site.value = await apiClient.getSite(siteId);
    if (site.value) {
      form.value = {
        push_to_deploy: !!site.value.push_to_deploy,
        deploy_script: site.value.deploy_script || '',
        expose_env: !!site.value.expose_env,
      };
      initialForm.value = { ...form.value };
    }
  } catch (e) {}
};

const togglePushToDeploy = () => {
  form.value.push_to_deploy = !form.value.push_to_deploy;
};

const toggleExposeEnv = () => {
  form.value.expose_env = !form.value.expose_env;
};

const saveSettings = async () => {
  saving.value = true;
  try {
    await apiClient.updateSite(siteId, {
      push_to_deploy: form.value.push_to_deploy,
      deploy_script: form.value.deploy_script,
      expose_env: form.value.expose_env,
    });
    initialForm.value = { ...form.value };
    addToast('Settings saved', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save', 'error');
  } finally {
    saving.value = false;
  }
};

onBeforeRouteLeave((_to, _from, next) => {
  if (isDirty.value) {
    const answer = window.confirm('You have unsaved changes in your deployment settings. Do you really want to leave?');
    if (!answer) {
      next(false);
      return;
    }
  }
  next();
});

onMounted(fetchSite);
onActivated(fetchSite);
</script>
