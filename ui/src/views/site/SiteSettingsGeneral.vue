<template>
  <div v-if="site" class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Environment Settings</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Configure your site's basic settings.</p>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Framework</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">The framework used by the installed application. Changing the framework does not modify the Nginx configuration.</p>
          <select v-model="form.app_type" class="w-64 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
            <option value="laravel">Laravel</option>
            <option value="php">PHP</option>
            <option value="html">HTML</option>
            <option value="node">Node.js</option>
          </select>
        </div>

        <div v-if="form.app_type === 'laravel' || form.app_type === 'php'">
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">PHP version</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">You may need to update your deployment script, schedulers, and background processes when changing the site's PHP version.</p>
          <select v-model="form.php_version" class="w-64 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
            <option v-for="v in phpVersions" :key="v" :value="v">PHP {{ v }}</option>
          </select>
        </div>

        <div v-if="form.app_type === 'node'" class="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <div class="lg:col-span-2">
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Preset</label>
            <div class="grid grid-cols-1 sm:grid-cols-3 border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
              <label class="flex items-center gap-2 px-4 py-3 cursor-pointer border-b sm:border-b-0 sm:border-r border-gray-200 dark:border-gray-700" :class="form.node_preset === 'next' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
                <input v-model="form.node_preset" type="radio" value="next" class="text-blue-600 focus:ring-blue-500">
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">Next.js</span>
              </label>
              <label class="flex items-center gap-2 px-4 py-3 cursor-pointer border-b sm:border-b-0 sm:border-r border-gray-200 dark:border-gray-700" :class="form.node_preset === 'nuxt' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
                <input v-model="form.node_preset" type="radio" value="nuxt" class="text-blue-600 focus:ring-blue-500">
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">Nuxt</span>
              </label>
              <label class="flex items-center gap-2 px-4 py-3 cursor-pointer" :class="form.node_preset === 'generic' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
                <input v-model="form.node_preset" type="radio" value="generic" class="text-blue-600 focus:ring-blue-500">
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">Generic</span>
              </label>
            </div>
          </div>

          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Mode</label>
            <select v-model="form.node_mode" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
              <option value="server">Server-rendered app</option>
              <option value="static">Static build</option>
            </select>
          </div>

          <div v-if="form.node_mode === 'server'">
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Server port</label>
            <input v-model.number="form.app_port" type="number" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" />
          </div>

          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Package manager</label>
            <select v-model="form.package_manager" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
              <option value="npm">npm</option>
              <option value="pnpm">pnpm</option>
              <option value="yarn">Yarn</option>
              <option value="none">None</option>
            </select>
          </div>

          <div class="lg:col-span-2">
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Build command</label>
            <input v-model="form.build_command" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" />
          </div>

          <div v-if="form.node_mode === 'server'" class="lg:col-span-2">
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Start command</label>
            <input v-model="form.start_command" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" />
          </div>

          <div v-if="form.node_mode === 'static'" class="lg:col-span-2">
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Static output directory</label>
            <input v-model="form.static_output_dir" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm font-mono focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" />
          </div>
        </div>
      </div>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Directories</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Configure your site's directory settings. If you have queue workers, background processes or scheduled jobs configured for this site, you will need to re-create them after updating the directories.</p>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Root directory</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">The root directory for your site. This is where your application code lives.</p>
          <div class="flex items-center gap-2 min-w-0">
            <span class="text-sm font-mono text-gray-500 dark:text-gray-400 break-all">{{ site.path }}</span>
            <span class="text-sm text-gray-400 dark:text-gray-500">/</span>
          </div>
        </div>

        <div v-if="form.app_type !== 'node'">
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Web directory</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">The publicly accessible directory that Nginx will serve the site from.</p>
          <div class="flex flex-col sm:flex-row sm:items-center gap-2 min-w-0">
            <div class="flex items-center gap-2 min-w-0">
              <span class="text-sm font-mono text-gray-500 dark:text-gray-400 break-all">{{ site.path }}</span>
              <span class="text-sm text-gray-400 dark:text-gray-500">/</span>
            </div>
            <input v-model="form.web_root" class="w-full sm:w-32 border border-gray-200 rounded-lg px-3 py-1.5 text-sm font-mono focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" />
          </div>
        </div>
      </div>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Git</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Configure your site's Git settings.</p>
        </div>
        <AppButton variant="secondary" size="sm" @click="refreshGit" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </AppButton>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Repository</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Configure the Git repository that should be deployed.</p>
          <div class="w-80">
            <SearchSelect v-model="form.repository" :options="repoOptions" placeholder="Select a repository" @update:model-value="onRepoChange" />
          </div>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Branch</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Configure the Git branch that should be deployed.</p>
          <div class="w-80">
            <SearchSelect v-model="form.branch" :options="branchOptions" :disabled="!form.repository" placeholder="Select a branch" />
          </div>
        </div>

        <div class="flex justify-end pt-2 border-t border-gray-100 dark:border-gray-800">
          <AppButton variant="primary" :loading="saving" @click="saveSettings">
            {{ saving ? 'Saving...' : 'Save settings' }}
          </AppButton>
        </div>
      </div>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-red-100 dark:bg-gray-900 dark:border-red-900/30">
      <div class="px-6 py-4 border-b border-red-100 dark:border-red-900/30">
        <h2 class="text-lg font-semibold text-red-600 dark:text-red-400">Danger</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Destructive actions that cannot be undone.</p>
      </div>
      <div class="p-6">
        <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Delete site</label>
        <p class="text-xs text-gray-500 mb-3 dark:text-gray-400">Deleting a site will remove all installed application code and untracked files from within the {{ site.path }} directory.</p>
        <AppButton variant="danger" @click="openDeleteModal">Delete site</AppButton>
      </div>
    </div>

    <BaseModal v-model="showDeleteModal" title="Delete Site" max-width="max-w-md">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          This action <strong>cannot</strong> be undone. This will permanently delete the site <strong>{{ site.domain }}</strong>, its configurations, databases mappings, and all associated files.
        </p>
        <FormGroup :label="'Please type ' + site.domain + ' to confirm:'">
          <input v-model="typedDomain" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" :placeholder="site.domain" autocomplete="off" />
        </FormGroup>
      </div>
      <template #footer>
        <AppButton variant="secondary" :disabled="deleting" @click="showDeleteModal = false">Cancel</AppButton>
        <AppButton variant="danger" :loading="deleting" :disabled="typedDomain !== site.domain" @click="performDelete">Delete site</AppButton>
      </template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated, watch, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { useConfirm } from '../../composables/useConfirm';
import { apiClient } from '../../api/client';
import { useSiteStore } from '../../stores/site';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';
import FormGroup from '../../components/FormGroup.vue';
import SearchSelect from '../../components/SearchSelect.vue';

const route = useRoute();
const router = useRouter();
let siteId = route.params.id as string;
const { addToast } = useToast();
const { confirm } = useConfirm();
const siteStore = useSiteStore();

const site = ref<any>(null);
const form = ref({
  app_type: '',
  php_version: '',
  web_root: '',
  repository: '',
  branch: '',
  app_port: 3000 as number | null,
  node_preset: 'generic',
  node_mode: 'server',
  package_manager: 'npm',
  build_command: '',
  start_command: '',
  static_output_dir: '',
});
const phpVersions = ref<string[]>([]);
const repos = ref<any[]>([]);
const branches = ref<any[]>([]);
const saving = ref(false);
const loadingSite = ref(false);

const showDeleteModal = ref(false);
const typedDomain = ref('');
const deleting = ref(false);

const repoOptions = computed(() => {
  const opts: { label: string; value: string }[] = [
    { label: 'Select a repository', value: '' },
  ];
  for (const r of repos.value) {
    opts.push({ label: r.full_name, value: r.full_name });
  }
  return opts;
});

const branchOptions = computed(() => {
  return branches.value.map((b: any) => ({ label: b.name, value: b.name }));
});

const defaultBuildCommand = (pm: string) => {
  if (pm === 'pnpm') return 'pnpm build';
  if (pm === 'yarn') return 'yarn build';
  if (pm === 'none') return '';
  return 'npm run build';
};

const defaultStartCommand = (preset: string, pm: string) => {
  if (preset === 'nuxt') return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 node .output/server/index.mjs';
  if (preset === 'next') {
    if (pm === 'pnpm') return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 pnpm start -- -p $FLUXO_APP_PORT -H 127.0.0.1';
    if (pm === 'yarn') return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 yarn start -p $FLUXO_APP_PORT -H 127.0.0.1';
    return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 npm run start -- -p $FLUXO_APP_PORT -H 127.0.0.1';
  }
  if (pm === 'pnpm') return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 pnpm start';
  if (pm === 'yarn') return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 yarn start';
  return '/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 npm run start';
};

const defaultStaticOutputDir = (preset: string) => {
  if (preset === 'nuxt') return '.output/public';
  if (preset === 'generic') return 'dist';
  return 'out';
};

const applyNodeDefaults = () => {
  form.value.build_command = defaultBuildCommand(form.value.package_manager);
  form.value.start_command = defaultStartCommand(form.value.node_preset, form.value.package_manager);
  form.value.static_output_dir = defaultStaticOutputDir(form.value.node_preset);
};

const fetchPHPVersions = async () => {
  try {
    phpVersions.value = await apiClient.getPhpVersions() || [];
  } catch (e) {}
};

const fetchRepos = async () => {
  try {
    repos.value = await apiClient.getGithubRepos() || [];
  } catch (e) {}
};

const fetchBranches = async (repo: string) => {
  if (!repo) { branches.value = []; return; }
  try {
    branches.value = await apiClient.getGithubBranches(repo) || [];
  } catch (e) {}
};

const onRepoChange = (value: string) => {
  if (value) fetchBranches(value);
  else branches.value = [];
};

const fetchSite = async () => {
  try {
    loadingSite.value = true;
    site.value = await apiClient.getSite(siteId);
    if (site.value) {
      siteStore.setActiveSite(site.value);
      form.value = {
        app_type: site.value.app_type || 'laravel',
        php_version: site.value.php_version || '8.4',
        web_root: site.value.web_root || '/public',
        repository: site.value.repository || '',
        branch: site.value.branch || 'main',
        app_port: site.value.app_port || 3000,
        node_preset: site.value.node_preset || 'generic',
        node_mode: site.value.node_mode || 'server',
        package_manager: site.value.package_manager || 'npm',
        build_command: site.value.build_command || defaultBuildCommand(site.value.package_manager || 'npm'),
        start_command: site.value.start_command || defaultStartCommand(site.value.node_preset || 'generic', site.value.package_manager || 'npm'),
        static_output_dir: site.value.static_output_dir || defaultStaticOutputDir(site.value.node_preset || 'generic'),
      };
      if (site.value.repository) {
        fetchBranches(site.value.repository);
      }
    }
  } catch (e) {
  } finally {
    loadingSite.value = false;
  }
};

const refreshGit = async () => {
  try {
    repos.value = await apiClient.get('/api/v1/github/repos?refresh=1') || [];
    apiClient.invalidate('/api/v1/github/repos');
  } catch (e) {}
  await fetchSite();
  if (site.value?.repository) {
    try {
      branches.value = await apiClient.get(`/api/v1/github/branches?repo=${encodeURIComponent(site.value.repository)}&refresh=1`) || [];
      apiClient.invalidate('/api/v1/github/branches');
    } catch (e) {}
  }
  addToast('GitHub data refreshed', 'success');
};

const saveSettings = async () => {
  const repoChanged = form.value.repository !== (site.value?.repository || '');
  const branchChanged = form.value.branch !== (site.value?.branch || 'main');

  if (repoChanged || branchChanged) {
    const confirmed = await confirm({
      title: 'Update Repository',
      message: `Changing the ${repoChanged && branchChanged ? 'repository and branch' : repoChanged ? 'repository' : 'branch'} will trigger a git sync on the server. Continue?`,
      confirmText: 'Update',
      cancelText: 'Cancel',
      variant: 'danger',
    });
    if (!confirmed) return;
  }

  saving.value = true;
  try {
    const payload: any = { ...form.value };
    if (payload.app_type === 'node') {
      if (payload.node_mode === 'static') {
        payload.app_port = 0;
      }
    } else {
      const octanePort = payload.app_type === 'laravel' ? Number(site.value?.app_port || 0) : 0;
      if (octanePort > 0) {
        payload.app_port = octanePort;
      } else {
        delete payload.app_port;
      }
      delete payload.node_preset;
      delete payload.node_mode;
      delete payload.package_manager;
      delete payload.build_command;
      delete payload.start_command;
      delete payload.static_output_dir;
    }
    await apiClient.updateSite(siteId, payload);
    addToast('Settings saved', 'success');
    await fetchSite();
  } catch (e: any) {
    addToast(e.message || 'Failed to save', 'error');
  } finally {
    saving.value = false;
  }
};

const openDeleteModal = () => {
  typedDomain.value = '';
  showDeleteModal.value = true;
};

const performDelete = async () => {
  if (typedDomain.value !== site.value.domain) return;
  deleting.value = true;
  try {
    await apiClient.deleteSite(Number(siteId));
    siteStore.clearActiveSite();
    addToast('Site deleted', 'success');
    router.push('/sites');
  } catch (e: any) {
    addToast(e.message || 'Failed to delete', 'error');
  } finally {
    deleting.value = false;
  }
};

onMounted(() => {
  fetchSite();
  fetchPHPVersions();
  fetchRepos();
});

onActivated(() => {
  fetchSite();
  fetchPHPVersions();
  fetchRepos();
});

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  fetchSite();
  fetchPHPVersions();
  fetchRepos();
});

watch(() => form.value.app_type, (type) => {
  if (loadingSite.value) return;
  if (type === 'laravel') {
    form.value.web_root = '/public';
  } else if (type === 'node') {
    form.value.web_root = '/';
    form.value.app_port = form.value.app_port || 3000;
    applyNodeDefaults();
  } else {
    form.value.web_root = '/';
  }
});

watch(() => [form.value.node_preset, form.value.package_manager], () => {
  if (loadingSite.value || form.value.app_type !== 'node') return;
  applyNodeDefaults();
});

watch(() => form.value.node_mode, (mode) => {
  if (loadingSite.value) return;
  if (mode === 'server') {
    form.value.app_port = form.value.app_port || 3000;
  }
});
</script>
