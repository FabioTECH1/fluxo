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
          <div class="flex items-center gap-2">
            <span class="text-sm font-mono text-gray-500 dark:text-gray-400">{{ site.path }}</span>
            <span class="text-sm text-gray-400 dark:text-gray-500">/</span>
          </div>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Web directory</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">The publicly accessible directory that Nginx will serve the site from.</p>
          <div class="flex items-center gap-2">
            <span class="text-sm font-mono text-gray-500 dark:text-gray-400">{{ site.path }}</span>
            <span class="text-sm text-gray-400 dark:text-gray-500">/</span>
            <input v-model="form.web_root" class="w-32 border border-gray-200 rounded-lg px-3 py-1.5 text-sm font-mono focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" />
          </div>
        </div>
      </div>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Git</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Configure your site's Git settings.</p>
      </div>
      <div class="p-6 space-y-5">
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Repository</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">Configure the Git repository that should be deployed.</p>
          <select v-model="form.repository" @change="onRepoChange" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
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
      </div>
    </div>

    <div class="flex justify-between">
      <AppButton variant="primary" :loading="saving" @click="saveSettings">
        {{ saving ? 'Saving...' : 'Save settings' }}
      </AppButton>
    </div>

    <div class="bg-white rounded-lg shadow-sm border border-red-100 dark:bg-gray-900 dark:border-red-900/30">
      <div class="px-6 py-4 border-b border-red-100 dark:border-red-900/30">
        <h2 class="text-lg font-semibold text-red-600 dark:text-red-400">Danger</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Destructive actions that cannot be undone.</p>
      </div>
      <div class="p-6">
        <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Delete site</label>
        <p class="text-xs text-gray-500 mb-3 dark:text-gray-400">Deleting a site will remove all installed application code and untracked files from within the {{ site.path }} directory.</p>
        <AppButton variant="danger" @click="deleteSite">Delete site</AppButton>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import AppButton from '../../components/AppButton.vue';

const route = useRoute();
const router = useRouter();
const siteId = route.params.id as string;
const { confirm } = useConfirm();
const { addToast } = useToast();

const site = ref<any>(null);
const form = ref({ app_type: '', php_version: '', web_root: '', repository: '', branch: '' });
const phpVersions = ref<string[]>([]);
const repos = ref<any[]>([]);
const branches = ref<any[]>([]);
const saving = ref(false);

const token = () => localStorage.getItem('fluxo_jwt');

const fetchPHPVersions = async () => {
  try {
    const res = await fetch('/api/v1/server/php', { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) phpVersions.value = await res.json();
  } catch (e) {}
};

const fetchRepos = async () => {
  try {
    const res = await fetch('/api/v1/github/repos', { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) repos.value = await res.json() || [];
  } catch (e) {}
};

const fetchBranches = async (repo: string) => {
  if (!repo) { branches.value = []; return; }
  try {
    const res = await fetch(`/api/v1/github/branches?repo=${encodeURIComponent(repo)}`, { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) branches.value = await res.json() || [];
  } catch (e) {}
};

const onRepoChange = () => {
  fetchBranches(form.value.repository);
};

const fetchSite = async () => {
  try {
    const res = await fetch(`/api/v1/sites/${siteId}`, { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) {
      site.value = await res.json();
      form.value = {
        app_type: site.value.app_type || 'laravel',
        php_version: site.value.php_version || '8.4',
        web_root: (site.value.web_root || '/public').replace(/^\//, ''),
        repository: site.value.repository || '',
        branch: site.value.branch || 'main',
      };
      if (site.value.repository) {
        fetchBranches(site.value.repository);
      }
    }
  } catch (e) {}
};

const saveSettings = async () => {
  saving.value = true;
  try {
    const res = await fetch(`/api/v1/sites/${siteId}`, {
      method: 'PUT',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(form.value)
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Settings saved', 'success');
    fetchSite();
  } catch (e: any) {
    addToast(e.message || 'Failed to save', 'error');
  } finally {
    saving.value = false;
  }
};

const deleteSite = async () => {
  const confirmed = await confirm({
    title: 'Delete Site',
    message: `Are you sure you want to delete ${site.value?.domain}? This will remove all configurations and files.`,
    confirmText: 'Delete Site',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    const res = await fetch(`/api/v1/sites/${siteId}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Site deleted', 'success');
    router.push('/sites');
  } catch (e: any) {
    addToast(e.message || 'Failed to delete', 'error');
  }
};

onMounted(() => {
  fetchSite();
  fetchPHPVersions();
  fetchRepos();
});
</script>
