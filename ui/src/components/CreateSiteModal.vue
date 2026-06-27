<template>
  <BaseModal v-model="visible" title="Create New Site" :loading="loading" confirm-text="Create Site" @submit="formRef?.requestSubmit()">
    <form ref="formRef" @submit.prevent="submit">
      <ErrorAlert :message="error" />

      <div class="mb-5">
        <FormGroup label="Domain Name">
          <input v-model="form.domain" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="example.com">
        </FormGroup>
      </div>

      <div class="mb-5">
        <FormGroup label="Application Type">
          <select v-model="form.app_type" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option value="laravel">Laravel</option>
            <option value="php">PHP</option>
            <option value="html">HTML</option>
          </select>
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type === 'node'">
        <FormGroup label="Application Port" hint="The internal port Nginx will proxy traffic to.">
          <input v-model="form.app_port" type="number" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="3000">
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type === 'laravel' || form.app_type === 'php'">
        <FormGroup label="PHP Version" hint="Need a different PHP version? Install additional runtimes via Server Settings.">
          <select v-model="form.php_version" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option v-for="v in phpVersions" :key="v" :value="v">{{ v }}</option>
          </select>
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type === 'laravel' || form.app_type === 'php'">
        <label class="inline-flex items-center gap-3 cursor-pointer">
          <button type="button" @click="form.install_composer = !form.install_composer"
            class="relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            :class="form.install_composer ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600'">
            <span class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white dark:bg-gray-200 shadow ring-0 transition duration-200 ease-in-out"
              :class="form.install_composer ? 'translate-x-5' : 'translate-x-0'"></span>
          </button>
          <span class="text-sm font-medium text-gray-700 dark:text-gray-300 select-none">Install Composer Dependencies</span>
        </label>
      </div>

      <div v-if="(form.app_type === 'laravel' || form.app_type === 'php') && dbEngines.length > 0" class="mb-5">
        <label class="inline-flex items-center gap-3 cursor-pointer">
          <button type="button" @click="connectDb = !connectDb"
            class="relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            :class="connectDb ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600'">
            <span class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white dark:bg-gray-200 shadow ring-0 transition duration-200 ease-in-out"
              :class="connectDb ? 'translate-x-5' : 'translate-x-0'"></span>
          </button>
          <span class="text-sm font-medium text-gray-700 dark:text-gray-300 select-none">Connect Database</span>
        </label>

        <div v-if="connectDb" class="mt-4 space-y-4">
          <div v-if="dbEngines.length > 1" class="mb-4">
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Database Engine</label>
            <div class="flex gap-4">
              <label class="flex items-center gap-2 cursor-pointer">
                <input type="radio" v-model="form.db_engine" value="mysql" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
                <span class="text-sm text-gray-700 dark:text-gray-300">MySQL</span>
              </label>
              <label class="flex items-center gap-2 cursor-pointer">
                <input type="radio" v-model="form.db_engine" value="postgres" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
                <span class="text-sm text-gray-700 dark:text-gray-300">PostgreSQL</span>
              </label>
            </div>
          </div>

          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Select or create a new database to connect to your site.</label>
            <div class="flex gap-3">
              <select v-model="selectedDb" class="flex-1 border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
                <option value="">-- Select or create a database --</option>
                <option v-for="db in filteredDbs" :key="db.name" :value="db.name">{{ db.name }}</option>
              </select>
              <button type="button" @click="showAddDbModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add Database</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Source Control Account & Organization Selects in Same Row -->
      <div :class="selectedAccountId && repos.length > 0 ? 'grid grid-cols-2 gap-4 mb-5' : 'mb-5'">
        <!-- Source Control Account Select -->
        <div>
          <FormGroup label="Source Control Account">
            <select v-model="selectedAccountId" @change="onAccountChange" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
              <option :value="null">-- Select a GitHub Account (Optional) --</option>
              <option v-for="acc in gitAccounts" :key="acc.id" :value="acc.id">{{ acc.name }}</option>
            </select>
            <p v-if="gitAccounts.length === 0" class="text-xs text-yellow-600 dark:text-yellow-400 mt-1">
              No source control accounts connected. Connect one in 
              <router-link to="/settings/source-control" class="underline font-medium hover:text-yellow-800 dark:hover:text-yellow-300" @click="visible = false">Server Settings</router-link>.
            </p>
          </FormGroup>
        </div>

        <!-- GitHub Organization Select (Optional) -->
        <div v-if="selectedAccountId && repos.length > 0">
          <FormGroup label="GitHub Organization (Optional)">
            <select v-model="selectedOrg" @change="onOrgChange" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
              <option value="">All Organizations / Users</option>
              <option v-for="org in uniqueOrgs" :key="org" :value="org">{{ org }}</option>
            </select>
          </FormGroup>
        </div>
      </div>

      <div class="mb-5" v-if="selectedAccountId">
        <FormGroup label="GitHub Repository">
          <SearchSelect v-model="form.repository" :options="repoOptions" placeholder="Select a repository" @update:model-value="onRepoSelect" />
        </FormGroup>
      </div>

      <div class="mb-5" v-if="selectedAccountId && form.repository">
        <FormGroup label="Branch">
          <SearchSelect v-model="form.branch" :options="branchOptions" :disabled="branchLoading" :placeholder="branchLoading ? 'Loading branches...' : 'Select a branch'" />
        </FormGroup>
      </div>

      <div class="mb-5">
        <button type="button" @click="advancedOpen = !advancedOpen"
          class="flex items-center gap-2 text-sm font-medium text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors">
          <svg class="w-4 h-4 transition-transform" :class="advancedOpen ? 'rotate-90' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
          Advanced
        </button>
      </div>

      <div v-if="advancedOpen">
        <div class="mb-5" v-if="form.repository && form.app_type === 'laravel'">
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Deployment Strategy</label>
          <div class="space-y-2">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.deployment_strategy" value="standard" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
              <span class="text-sm text-gray-700 dark:text-gray-300">Standard (Git Pull + Composer)</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.deployment_strategy" value="zero-downtime" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
              <span class="text-sm text-gray-700 dark:text-gray-300">Zero-Downtime (Symlink Swapping)</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.deployment_strategy" value="octane" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
              <span class="text-sm text-gray-700 dark:text-gray-300">Octane (Laravel Octane Reload)</span>
            </label>
          </div>
        </div>

        <div class="mb-6">
          <FormGroup label="Web Directory">
            <input v-model="form.web_root" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="/public">
          </FormGroup>
        </div>
      </div>
    </form>

    <BaseModal v-model="showAddDbModal" title="New Database" :loading="creatingDb" confirm-text="Create" max-width="max-w-md" @submit="createDatabase">
      <div class="space-y-4">
        <FormGroup label="Database Name">
          <input v-model="newDb.name" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="my_database">
        </FormGroup>
        <div>
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Username <span class="text-gray-400 dark:text-gray-500 font-normal">(optional)</span></label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-1">Leave blank to use the <code class="font-mono">fluxo</code> user.</p>
          <input v-model="newDb.user" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="db_user (or leave empty)">
        </div>
        <div>
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Password <span class="text-gray-400 dark:text-gray-500 font-normal">(optional)</span></label>
          <div class="relative">
            <input v-model="newDb.password" :type="showNewDbPass ? 'text' : 'password'" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg pl-3 pr-16 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="Click Generate or enter one">
            <div class="absolute inset-y-0 right-0 flex items-center gap-1 pr-2">
              <button type="button" @click="newDb.password = generatePassword(); showNewDbPass = true" class="px-1.5 py-0.5 text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 font-semibold">Generate</button>
              <button type="button" @click="showNewDbPass = !showNewDbPass" class="text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400">
                <span v-if="!showNewDbPass" class="text-base leading-none">&#128065;</span>
                <span v-else class="text-base leading-none">&#128064;</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </BaseModal>
  </BaseModal>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import BaseModal from './BaseModal.vue';
import ErrorAlert from './ErrorAlert.vue';
import FormGroup from './FormGroup.vue';
import SearchSelect from './SearchSelect.vue';

const { addToast } = useToast();

const visible = defineModel<boolean>({ required: true });
const emit = defineEmits<{
  'submit-create': [payload: any];
}>();

const formRef = ref<HTMLFormElement | null>(null);

const form = ref({
  domain: '',
  php_version: '8.4',
  web_root: '/public',
  repository: '',
  branch: 'main',
  deployment_strategy: 'standard',
  app_type: 'laravel',
  app_port: null,
  db_engine: '',
  install_composer: true
});

const connectDb = ref(false);
const advancedOpen = ref(false);
const showAddDbModal = ref(false);
const selectedDb = ref('');
const creatingDb = ref(false);
const showNewDbPass = ref(false);

const newDb = ref({ name: '', user: '', password: '' });

const error = ref('');
const loading = ref(false);
const phpVersions = ref<string[]>(['8.4']);
const repos = ref<any[]>([]);
const branches = ref<any[]>([]);
const branchLoading = ref(false);
const availableDbs = ref<any[]>([]);
const dbEngines = ref<string[]>([]);
const gitAccounts = ref<any[]>([]);
const selectedAccountId = ref<number | null>(null);
const selectedOrg = ref<string>('');

const onAccountChange = async () => {
  form.value.repository = '';
  form.value.branch = 'main';
  selectedOrg.value = '';
  repos.value = [];
  branches.value = [];
  
  if (selectedAccountId.value) {
    try {
      repos.value = await apiClient.getGithubRepos(selectedAccountId.value) || [];
    } catch (e: any) {
      addToast(e.message || 'Failed to fetch repositories for this account', 'error');
    }
  }
};

const onOrgChange = () => {
  form.value.repository = '';
  form.value.branch = 'main';
  branches.value = [];
};

const uniqueOrgs = computed(() => {
  const orgs = new Set<string>();
  for (const r of repos.value) {
    if (r.full_name && r.full_name.includes('/')) {
      const org = r.full_name.split('/')[0];
      orgs.add(org);
    }
  }
  return Array.from(orgs).sort();
});

const repoOptions = computed(() => {
  const opts: { label: string; value: string }[] = [
    { label: '-- Select a repository --', value: '' }
  ];
  const filteredRepos = selectedOrg.value
    ? repos.value.filter(r => r.full_name && r.full_name.startsWith(selectedOrg.value + '/'))
    : repos.value;
  for (const r of filteredRepos) {
    opts.push({ label: r.full_name, value: r.full_name });
  }
  return opts;
});

const branchOptions = computed(() => {
  return branches.value.map((b: any) => ({ label: b.name, value: b.name }));
});

const filteredDbs = computed(() => {
  if (!form.value.db_engine) return availableDbs.value;
  return availableDbs.value.filter((db: any) => {
    const engine = db.engine || 'mysql';
    return engine === form.value.db_engine;
  });
});

watch(() => form.value.app_type, (newType) => {
  if (newType === 'laravel') {
    form.value.web_root = '/public';
  } else {
    form.value.web_root = '/';
    form.value.deployment_strategy = 'standard';
  }
});

const fetchBranches = async (repo: string) => {
  if (!repo) {
    branches.value = [];
    return;
  }
  branchLoading.value = true;
  try {
    branches.value = await apiClient.getGithubBranches(repo, selectedAccountId.value || undefined) || [];
    if (branches.value.length > 0) {
      const hasMain = branches.value.some((b: any) => b.name === 'main');
      const hasMaster = branches.value.some((b: any) => b.name === 'master');
      if (hasMain) form.value.branch = 'main';
      else if (hasMaster) form.value.branch = 'master';
      else form.value.branch = branches.value[0].name;
    }
  } catch {
    branches.value = [];
  } finally {
    branchLoading.value = false;
  }
};

const onRepoSelect = () => {
  fetchBranches(form.value.repository);
};

const fetchVersionsAndRepos = async () => {
  try {
    const versions = await apiClient.getPhpVersions();
    if (versions && versions.length > 0) {
      phpVersions.value = versions;
      try {
        const settings = await apiClient.getSettings();
        if (settings.default_php && versions.includes(settings.default_php)) {
          form.value.php_version = settings.default_php;
        } else {
          const sorted = [...versions].sort();
          form.value.php_version = sorted[sorted.length - 1];
        }
      } catch {
        const sorted = [...versions].sort();
        form.value.php_version = sorted[sorted.length - 1];
      }
    }
  } catch (e) { console.error(e); }

  try {
    gitAccounts.value = await apiClient.getGithubAccounts() || [];
    if (gitAccounts.value.length > 0) {
      selectedAccountId.value = gitAccounts.value[0].id;
      await onAccountChange();
    }
  } catch(e) { console.error(e); }

  try {
    const engines = await apiClient.getDatabaseEngines();
    dbEngines.value = (engines || []).filter((e: string) => e === 'mysql' || e === 'postgres');
    if (dbEngines.value.length > 0) {
      form.value.db_engine = dbEngines.value[0];
    }
  } catch(e) { console.error(e); }

  try {
    availableDbs.value = await apiClient.getDatabases() || [];
  } catch(e) { console.error(e); }
};

const generatePassword = () => {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
  let pwd = '';
  for (let i = 0; i < 20; i++) pwd += chars.charAt(Math.floor(Math.random() * chars.length));
  return pwd;
};

const createDatabase = async () => {
  if (!newDb.value.name) return;
  creatingDb.value = true;
  try {
    await apiClient.createDatabase({ name: newDb.value.name, engine: form.value.db_engine });

    if (newDb.value.user) {
      const pass = newDb.value.password || generatePassword();
      await apiClient.createDatabaseUser({ user: newDb.value.user, password: pass, databases: [newDb.value.name] });
    } else {
      await apiClient.createDatabaseUserGrant({ user: 'fluxo', databases: [newDb.value.name] });
    }

    addToast('Database created successfully', 'success');
    showAddDbModal.value = false;
    selectedDb.value = newDb.value.name;

    availableDbs.value = await apiClient.getDatabases() || [];

    newDb.value = { name: '', user: '', password: '' };
  } catch (e: any) {
    addToast(e.message || 'Failed to create database', 'error');
  } finally {
    creatingDb.value = false;
  }
};
const submit = () => {
  error.value = '';

  const payload: any = { ...form.value };
  if (selectedAccountId.value) {
    payload.github_account_id = selectedAccountId.value;
  }
  if (connectDb.value && selectedDb.value) {
    payload.database_name = selectedDb.value;
    payload.database_user = newDb.value.user || '';
    payload.database_password = newDb.value.password || '';
  } else {
    delete payload.db_engine;
  }

  emit('submit-create', payload);
  visible.value = false;
};

onMounted(fetchVersionsAndRepos);
</script>
