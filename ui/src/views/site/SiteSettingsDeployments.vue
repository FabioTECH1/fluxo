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
          :disabled="saving || converting"
          @update:model-value="togglePushToDeploy" />

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">{{ isManaged ? 'Application commands' : 'Deploy script' }}</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">{{ scriptDescription }}</p>
          <ScriptEditor
            v-model="deployScript"
            language="shell"
            :label="isManaged ? 'Application commands editor' : 'Deploy script editor'"
            :placeholder="deployPlaceholder"
            :readonly="saving || converting"
            :busy="saving || converting"
            @keydown="handleKeyDown"
          />
        </div>

        <ToggleSwitch :model-value="form.expose_env" label="Expose .env to deployment script" label-position="left"
          description="Make the site's environment variables available while the deployment script runs."
          :disabled="saving || converting"
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
import { ref, computed, onMounted, watch } from 'vue';
import { useRoute, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import { useConfirm } from '../../composables/useConfirm';
import { useUndoRedo } from '../../composables/useUndoRedo';
import ScriptEditor from '../../components/ScriptEditor.vue';
import StatusBadge from '../../components/StatusBadge.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';
import { useSiteStore } from '../../stores/site';

const route = useRoute();
let siteId = route.params.id as string;
const { addToast, showToast, updateToast } = useToast();
const { confirm } = useConfirm();
const siteStore = useSiteStore();

const site = ref<any>(null);
const form = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const deployScript = computed({
  get: () => form.value.deploy_script,
  set: (val) => { form.value.deploy_script = val; }
});
const { undo: undoScript, redo: redoScript, resetHistory } = useUndoRedo(deployScript);
const initialForm = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const saving = ref(false);
const converting = ref(false);
const features = ref<any>({});
let siteRequestVersion = 0;
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
  if (features.value?.queue_worker_enabled) hooks.push('cd "$FLUXO_ACTIVE_SITE_PATH" && $FLUXO_PHP artisan queue:restart');
  if (features.value?.octane_enabled && !isZeroDowntime.value) hooks.push('$FLUXO_PHP artisan octane:reload');
  if (site.value?.app_type === 'node' && site.value?.node_mode === 'server') hooks.push('Restart managed Node.js service');
  return hooks;
});

const deployPlaceholder = computed(() => {
  if (site.value?.app_type === 'wordpress') return 'wp core update --path="$FLUXO_WEB_ROOT"\nwp plugin update --all --path="$FLUXO_WEB_ROOT"';
  if (site.value?.app_type === 'node') return 'if [ -f package.json ]; then\n  if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then\n    bash -lc "$FLUXO_NODE_INSTALL_COMMAND"\n  fi\n\n  if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then\n    bash -lc "$FLUXO_NODE_BUILD_COMMAND"\n  fi\nfi';
  if (site.value?.app_type === 'html') return 'if [ -f package.json ]; then\n  npm ci || npm install\n  npm run --if-present build\nfi';
  if (site.value?.app_type === 'php') return '$FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader';
  const composer = '$FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader';
  return site.value?.db_engine ? `${composer}\n$FLUXO_PHP artisan migrate --force` : composer;
});

const isDirty = computed(() => {
	return form.value.push_to_deploy !== initialForm.value.push_to_deploy ||
	       form.value.deploy_script !== initialForm.value.deploy_script ||
	       form.value.expose_env !== initialForm.value.expose_env;
});

const handleKeyDown = (e: KeyboardEvent, textarea: HTMLTextAreaElement) => {
  const key = e.key.toLowerCase();
  if ((e.ctrlKey || e.metaKey) && key === 'z' && !e.shiftKey) {
    e.preventDefault();
    undoScript();
  } else if ((e.ctrlKey || e.metaKey) && (key === 'y' || (key === 'z' && e.shiftKey))) {
    e.preventDefault();
    redoScript();
  } else if ((e.ctrlKey || e.metaKey) && key === 's') {
    e.preventDefault();
    if (!saving.value) {
      saveSettings();
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
    form.value.deploy_script = newText;
    
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

const fetchSite = async () => {
  const request = ++siteRequestVersion;
  const requestedSiteId = siteId;
  try {
    const nextSite = await apiClient.getSite(requestedSiteId, true);
    let nextFeatures: any = {};
    try { nextFeatures = await apiClient.getSiteFeatures(requestedSiteId, true); } catch {}
    if (request !== siteRequestVersion || requestedSiteId !== siteId) return;
    site.value = nextSite;
    features.value = nextFeatures;
    if (nextSite) {
      form.value = {
        push_to_deploy: !!nextSite.push_to_deploy,
        deploy_script: nextSite.deploy_script || '',
        expose_env: !!nextSite.expose_env,
      };
      initialForm.value = { ...form.value };
      resetHistory();
    }
  } catch (e) {}
};

const resetToManaged = async () => {
  const confirmed = await confirm({
    title: 'Reset deployment script?',
    message: 'Replace this complete legacy script with managed app-specific defaults? Copy any custom commands you need before continuing.',
    confirmText: 'Reset to defaults',
    cancelText: 'Cancel',
    variant: 'danger',
  });
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
  if (saving.value || converting.value) return;
  saving.value = true;
  const requestedSiteId = siteId;
  const payload = {
    push_to_deploy: form.value.push_to_deploy,
    deploy_script: form.value.deploy_script,
    expose_env: form.value.expose_env,
  };
  const toastId = showToast({
    title: 'Saving deployment settings',
    description: 'This may take a moment.',
    type: 'loading',
  });
  try {
    await apiClient.updateSite(requestedSiteId, payload);
    if (siteId === requestedSiteId) initialForm.value = { ...payload };
    try { await siteStore.fetchSite(requestedSiteId, true); } catch (e) {}
    updateToast(toastId, {
      title: 'Deployment settings saved',
      description: null,
      type: 'success',
    });
  } catch (e: any) {
    updateToast(toastId, {
      title: 'Deployment settings could not be saved',
      description: e.message || 'Please try again.',
      type: 'error',
    });
  } finally {
    saving.value = false;
  }
};

const confirmDiscardChanges = async (to?: { path?: string }) => {
  if (to?.path === '/login') {
    form.value = { ...initialForm.value };
    resetHistory();
    return true;
  }
  if (saving.value || converting.value) {
    addToast('Please wait for the deployment settings operation to finish.', 'info');
    return false;
  }
  if (!isDirty.value) return true;
  const approved = await confirm({
    title: 'Discard deployment changes?',
    message: 'Your unsaved application commands and deployment settings will be lost if you leave this page.',
    confirmText: 'Discard changes',
    cancelText: 'Keep editing',
    variant: 'danger',
  });
  if (approved) {
    form.value = { ...initialForm.value };
    resetHistory();
  }
  return approved;
};

onBeforeRouteLeave(confirmDiscardChanges);
onBeforeRouteUpdate((to) => (
  to.params.id !== siteId ? confirmDiscardChanges(to) : true
));

onMounted(fetchSite);

watch(() => route.params.id, (newId) => {
  if (typeof newId !== 'string' || !/^[1-9]\d*$/.test(newId) || newId === siteId) return;
  siteRequestVersion++;
  siteId = newId;
  site.value = null;
  features.value = {};
  form.value = { push_to_deploy: false, deploy_script: '', expose_env: false };
  initialForm.value = { ...form.value };
  resetHistory();
  fetchSite();
});
</script>
