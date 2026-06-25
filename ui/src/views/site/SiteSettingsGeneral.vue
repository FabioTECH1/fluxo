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
          </select>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">PHP version</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">You may need to update your deployment script, schedulers, and background processes when changing the site's PHP version.</p>
          <select v-model="form.php_version" class="w-64 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
            <option v-for="v in phpVersions" :key="v" :value="v">PHP {{ v }}</option>
          </select>
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

        <div>
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
          <select v-model="form.repository" @change="onRepoChange" class="w-64 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
            <option value="">Select a repository</option>
            <option v-for="r in repos" :key="r.full_name" :value="r.full_name">{{ r.full_name }}</option>
          </select>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Branch</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Configure the Git branch that should be deployed.</p>
          <select v-model="form.branch" :disabled="!form.repository" class="w-64 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono disabled:bg-gray-100 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 dark:disabled:bg-gray-800">
            <option value="">Select a branch</option>
            <option v-for="b in branches" :key="b.name" :value="b.name">{{ b.name }}</option>
          </select>
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
import { ref, onMounted, onActivated, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { useConfirm } from '../../composables/useConfirm';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';
import FormGroup from '../../components/FormGroup.vue';

const route = useRoute();
const router = useRouter();
let siteId = route.params.id as string;
const { addToast } = useToast();
const { confirm } = useConfirm();

const site = ref<any>(null);
const form = ref({ app_type: '', php_version: '', web_root: '', repository: '', branch: '' });
const phpVersions = ref<string[]>([]);
const repos = ref<any[]>([]);
const branches = ref<any[]>([]);
const saving = ref(false);

const showDeleteModal = ref(false);
const typedDomain = ref('');
const deleting = ref(false);

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

const onRepoChange = () => {
  fetchBranches(form.value.repository);
};

const fetchSite = async () => {
  try {
    site.value = await apiClient.getSite(siteId);
    if (site.value) {
      form.value = {
        app_type: site.value.app_type || 'laravel',
        php_version: site.value.php_version || '8.4',
        web_root: site.value.web_root || '/public',
        repository: site.value.repository || '',
        branch: site.value.branch || 'main',
      };
      if (site.value.repository) {
        fetchBranches(site.value.repository);
      }
    }
  } catch (e) {}
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
    await apiClient.updateSite(siteId, form.value);
    addToast('Settings saved', 'success');
    fetchSite();
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
</script>
