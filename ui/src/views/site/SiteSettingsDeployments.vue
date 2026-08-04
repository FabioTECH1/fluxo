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
        <div class="flex flex-col gap-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 sm:flex-row sm:items-center sm:justify-between dark:border-gray-700 dark:bg-gray-800/60">
          <div>
            <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">Deployment strategy</p>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ deploymentStrategyDescription }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <StatusBadge
              :label="isZeroDowntime ? 'Zero-downtime' : 'Standard'"
              :variant="isZeroDowntime ? 'blue' : 'gray'" />
            <StatusBadge
              :label="isManaged ? 'Managed lifecycle' : 'Legacy script'"
              :variant="isManaged ? 'green' : 'yellow'" />
          </div>
        </div>

        <div v-if="isManaged" class="rounded-lg border border-blue-200 bg-blue-50/70 p-4 dark:border-blue-900/70 dark:bg-blue-950/30">
          <p class="text-sm font-semibold text-blue-900 dark:text-blue-200">Fluxo-managed deployment</p>
          <p class="mt-1 text-xs text-blue-800/80 dark:text-blue-300/80">Repository operations, release activation, runtime hooks, and cleanup are protected. The editor below contains only your application commands.</p>
          <ol class="mt-3 grid gap-1 text-xs text-blue-900 dark:text-blue-200 sm:grid-cols-2">
            <li v-for="(step, index) in managedSteps" :key="step" class="flex gap-2">
              <span class="font-mono text-blue-500">{{ index + 1 }}.</span><span>{{ step }}</span>
            </li>
          </ol>
          <div v-if="managedHooks.length" class="mt-3 border-t border-blue-200 pt-3 dark:border-blue-900/70">
            <p class="text-xs font-semibold uppercase tracking-wide text-blue-700 dark:text-blue-300">Enabled runtime hooks</p>
            <code v-for="hook in managedHooks" :key="hook" class="mt-1 block break-all text-xs text-blue-900 dark:text-blue-100">{{ hook }}</code>
          </div>
        </div>

        <div v-else class="rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-900/70 dark:bg-amber-950/30">
          <p class="text-sm font-semibold text-amber-900 dark:text-amber-200">Legacy full deployment script</p>
          <p class="mt-1 text-xs text-amber-800 dark:text-amber-300">This existing site keeps its complete script unchanged. Resetting replaces it with app-specific defaults and lets Fluxo manage Git, zero-downtime activation, hooks, and cleanup.</p>
          <button type="button" class="mt-3 rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-700 disabled:opacity-50" :disabled="converting" @click="resetToManaged">
            {{ converting ? 'Resetting…' : 'Reset to managed defaults' }}
          </button>
        </div>

        <ToggleSwitch v-if="site.app_type !== 'wordpress'" :model-value="form.push_to_deploy" label="Push to deploy" label-position="left"
          description="Automatically deploy when changes are pushed to the environment's Git branch."
          @update:model-value="togglePushToDeploy" />

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">{{ isManaged ? 'Application commands' : 'Deploy script' }}</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">{{ scriptDescription }}</p>
          <div class="relative w-full h-128 border border-gray-200 rounded-lg overflow-hidden focus-within:ring-2 focus-within:ring-blue-500 focus-within:border-blue-500 transition-shadow dark:bg-gray-800 dark:border-gray-600">
            <div ref="highlightRef"
              class="absolute inset-0 pointer-events-none p-3 font-mono text-xs leading-5 overflow-hidden whitespace-pre-wrap break-all dark:text-gray-100"
              v-html="highlightedContent"></div>
            <textarea v-model="deployScript" @scroll="syncScroll" ref="textareaRef" @keydown="handleKeyDown" data-gramm="false"
              class="block w-full h-full font-mono text-xs p-3 bg-transparent resize-none outline-none leading-5 text-transparent caret-gray-900 dark:caret-gray-100 whitespace-pre-wrap break-all"
              :placeholder="deployPlaceholder"></textarea>
          </div>
        </div>

        <ToggleSwitch :model-value="form.expose_env" label="Expose .env to deployment script" label-position="left"
          description="Make the site's environment variables available while the deployment script runs."
          @update:model-value="toggleExposeEnv" />

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
import { ref, computed, onMounted, onActivated, watch } from 'vue';
import { useRoute, onBeforeRouteLeave } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import { useUndoRedo } from '../../composables/useUndoRedo';
import StatusBadge from '../../components/StatusBadge.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';
import { useSiteStore } from '../../stores/site';

const route = useRoute();
let siteId = route.params.id as string;
const { addToast } = useToast();
const siteStore = useSiteStore();

const site = ref<any>(null);
const form = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const deployScript = computed({
  get: () => form.value.deploy_script,
  set: (val) => { form.value.deploy_script = val; }
});
const { undo: undoScript, redo: redoScript } = useUndoRedo(deployScript);
const initialForm = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const saving = ref(false);
const converting = ref(false);
const features = ref<any>({});
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const highlightRef = ref<HTMLDivElement | null>(null);
const isZeroDowntime = computed(() => site.value?.deployment_strategy === 'zero-downtime');
const isManaged = computed(() => site.value?.deploy_script_mode === 'managed');
const deploymentStrategyDescription = computed(() => isZeroDowntime.value
  ? 'Each deployment creates a new release and atomically updates the current symlink.'
  : 'Deployments update the application directly in its site directory.');
const scriptDescription = computed(() => isManaged.value
  ? 'Commands run inside $FLUXO_DEPLOY_PATH after Fluxo prepares the repository or release. Deployments are limited to 10 minutes.'
  : 'The complete legacy Bash lifecycle. Deployments are limited to 10 minutes.');
const managedSteps = computed(() => isZeroDowntime.value
  ? ['Create and clone release', 'Link shared persistence', 'Run application commands', 'Activate current atomically', 'Run managed runtime hooks', 'Clean old releases']
  : ['Update repository in place', 'Run application commands', 'Run managed runtime hooks']);
const managedHooks = computed(() => {
  const hooks: string[] = [];
  if (features.value?.horizon_enabled) hooks.push('cd "$FLUXO_ACTIVE_SITE_PATH" && $FLUXO_PHP artisan horizon:terminate');
  if (features.value?.octane_enabled && !isZeroDowntime.value) hooks.push('$FLUXO_PHP artisan octane:reload');
  if (site.value?.app_type === 'node' && site.value?.node_mode === 'server') hooks.push('Restart managed Node.js service');
  return hooks;
});

const deployPlaceholder = computed(() => {
  if (site.value?.app_type === 'wordpress') return 'wp core update --path="$FLUXO_WEB_ROOT"\nwp plugin update --all --path="$FLUXO_WEB_ROOT"';
  if (site.value?.app_type === 'node') return 'if [ -f package.json ]; then\n  if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then\n    bash -lc "$FLUXO_NODE_INSTALL_COMMAND"\n  fi\n\n  if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then\n    bash -lc "$FLUXO_NODE_BUILD_COMMAND"\n  fi\nfi';
  if (site.value?.app_type === 'html') return 'if [ -f package.json ]; then\n  npm ci || npm install\n  npm run --if-present build\nfi';
  if (site.value?.app_type === 'php') return '$FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader';
  return '$FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader\n$FLUXO_PHP artisan migrate --force';
});

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
  if ((e.ctrlKey || e.metaKey) && e.key === 'z') {
    e.preventDefault();
    undoScript();
  } else if ((e.ctrlKey || e.metaKey) && e.key === 'y') {
    e.preventDefault();
    redoScript();
  } else if ((e.ctrlKey || e.metaKey) && e.key === 's') {
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
  try {
    site.value = await apiClient.getSite(siteId);
    try { features.value = await apiClient.getSiteFeatures(siteId, true); } catch { features.value = {}; }
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

const resetToManaged = async () => {
  const confirmed = window.confirm('Replace this complete legacy script with managed app-specific defaults? Copy any custom commands you need before continuing.');
  if (!confirmed) return;
  converting.value = true;
  try {
    await apiClient.updateSite(siteId, { deploy_script_mode: 'managed' });
    await fetchSite();
    try { await siteStore.fetchSite(siteId, true); } catch (e) {}
    addToast('Managed deployment lifecycle enabled', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to enable managed deployments', 'error');
  } finally {
    converting.value = false;
  }
};

const togglePushToDeploy = (enabled: boolean) => {
	form.value.push_to_deploy = enabled;
};

const toggleExposeEnv = (enabled: boolean) => {
	form.value.expose_env = enabled;
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
    try { await siteStore.fetchSite(siteId, true); } catch (e) {}
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

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  fetchSite();
});
</script>
