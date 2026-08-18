<template>
  <BaseModal v-model="visible" title="Create New Site" :loading="loading" :confirm-disabled="siteCreationBlocked" confirm-text="Create Site" @submit="formRef?.requestSubmit()">
    <form ref="formRef" @submit.prevent="submit">
      <ErrorAlert :message="error" />

      <div class="mb-5">
        <FormGroup label="Domain Name">
          <input v-model="form.domain" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="example.com">
        </FormGroup>
      </div>

      <div class="mb-5">
        <FormGroup label="Application Type">
          <div class="flex items-start gap-3">
            <AppTypeIcon :app-type="form.app_type" class="mt-0.5" />
            <div class="min-w-0 flex-1">
              <select v-model="form.app_type" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
                <option v-for="type in appTypes" :key="type.value" :value="type.value">{{ type.label }}</option>
              </select>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ selectedAppType.description }}</p>
            </div>
          </div>
        </FormGroup>
      </div>

      <div v-if="form.app_type === 'node' && !nodeRuntimeLoading && !nodeRuntime?.toolchain_ready" class="mb-5 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 dark:border-amber-900 dark:bg-amber-950/30" aria-live="polite">
        <p class="text-sm font-semibold text-amber-900 dark:text-amber-200">Node.js toolchain required</p>
        <p class="mt-1 text-xs text-amber-800 dark:text-amber-300">{{ nodeRuntimeRequirementMessage }}</p>
        <div class="mt-3 flex flex-wrap items-center gap-3">
          <a href="/runtime/node" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 rounded-lg bg-amber-900 px-3 py-2 text-xs font-semibold text-white transition-colors hover:bg-amber-800 focus:outline-none focus:ring-2 focus:ring-amber-600 focus:ring-offset-2 dark:bg-amber-300 dark:text-amber-950 dark:hover:bg-amber-200">
            Open Node.js Runtime
            <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M14 5h5v5M10 14L19 5M19 14v5H5V5h5" /></svg>
          </a>
          <button type="button" :disabled="nodeRuntimeLoading" class="inline-flex items-center gap-1.5 text-xs font-semibold text-amber-900 hover:text-amber-700 disabled:cursor-wait disabled:opacity-60 dark:text-amber-200 dark:hover:text-amber-100" @click="refreshNodeRuntime">
            <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h5M20 20v-5h-5M5.5 15a7 7 0 0011.7 2.6L20 15M4 9l2.8-2.6A7 7 0 0118.5 9" /></svg>
            Check again
          </button>
        </div>
      </div>

      <div v-if="form.app_type === 'node'" class="mb-5 space-y-4">
        <FormGroup label="Preset">
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
        </FormGroup>

        <FormGroup label="Mode">
          <div class="grid grid-cols-1 sm:grid-cols-2 border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
            <label class="flex items-start gap-3 px-4 py-3 cursor-pointer border-b sm:border-b-0 sm:border-r border-gray-200 dark:border-gray-700" :class="form.node_mode === 'server' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
              <input v-model="form.node_mode" type="radio" value="server" class="mt-1 text-blue-600 focus:ring-blue-500">
              <span>
                <span class="block text-sm font-semibold text-gray-800 dark:text-gray-100">Server-rendered app</span>
                <span class="block text-xs text-gray-500 dark:text-gray-400">Run a Node process behind Nginx.</span>
              </span>
            </label>
            <label class="flex items-start gap-3 px-4 py-3 cursor-pointer" :class="form.node_mode === 'static' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
              <input v-model="form.node_mode" type="radio" value="static" class="mt-1 text-blue-600 focus:ring-blue-500">
              <span>
                <span class="block text-sm font-semibold text-gray-800 dark:text-gray-100">Static build</span>
                <span class="block text-xs text-gray-500 dark:text-gray-400">Build files and serve them directly.</span>
              </span>
            </label>
          </div>
        </FormGroup>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <FormGroup label="Package manager">
            <select v-model="form.package_manager" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
              <option value="npm">npm</option>
              <option value="pnpm">pnpm</option>
              <option value="yarn">Yarn</option>
              <option value="bun">Bun</option>
              <option value="none">None</option>
            </select>
          </FormGroup>

          <FormGroup label="Build command">
            <input v-model="form.build_command" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" placeholder="npm run build">
          </FormGroup>
        </div>

        <FormGroup v-if="form.node_mode === 'static'" label="Static output directory">
          <input v-model="form.static_output_dir" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" placeholder="out">
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type === 'node' && form.node_mode === 'server'">
        <FormGroup label="Application Port" hint="The internal port Nginx will proxy traffic to.">
          <input v-model.number="form.app_port" type="number" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="3000">
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type === 'laravel' || form.app_type === 'php' || form.app_type === 'wordpress'">
        <FormGroup label="PHP Version" hint="Need a different PHP version? Install additional runtimes via Server Settings.">
          <select v-model="form.php_version" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option v-for="v in phpVersions" :key="v" :value="v">{{ v }}</option>
          </select>
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type === 'laravel' || form.app_type === 'php'">
        <ToggleSwitch v-model="form.install_composer" label="Install Composer dependencies" />
      </div>

      <div v-if="supportsDatabase && selectableDbEngines.length > 0" class="mb-5">
        <ToggleSwitch v-if="form.app_type !== 'wordpress'" v-model="connectDb" label="Connect database" />
        <div v-else>
          <p class="text-sm font-semibold text-gray-800 dark:text-gray-100">WordPress database</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Select or create a MySQL database for WordPress.</p>
        </div>

        <div v-if="connectDb" class="mt-4 space-y-4">
          <div v-if="dbEngines.length > 0" class="mb-4">
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Database Engine</label>
            <div class="flex gap-4">
              <label v-for="eng in selectableDbEngines" :key="eng" class="flex items-center gap-2 cursor-pointer">
                <input type="radio" v-model="form.db_engine" :value="eng" class="text-blue-600 dark:text-blue-400 focus:ring-blue-500">
                <span class="text-sm text-gray-700 dark:text-gray-300">
                  {{ eng === 'mysql' ? 'MySQL' : (eng === 'postgres' ? 'PostgreSQL' : eng) }}
                </span>
              </label>
            </div>
          </div>

          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Select or create a new database to connect to your site.</label>
            <div class="flex flex-col gap-3 sm:flex-row">
              <select v-model="selectedDb" :disabled="databaseOptionsLoading" class="w-full min-w-0 border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm disabled:cursor-wait disabled:opacity-60 sm:flex-1">
                <option value="">{{ databaseOptionsLoading ? '-- Loading databases --' : filteredDbs.length === 0 ? '-- No available databases --' : '-- Select or create a database --' }}</option>
                <option v-for="db in filteredDbs" :key="db.engine + ':' + db.name" :value="db.engine + ':' + db.name">{{ db.name }}</option>
              </select>
              <button type="button" @click="showAddDbModal = true" class="w-full px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap sm:w-auto">Add Database</button>
            </div>
            <p v-if="databaseOptionsError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ databaseOptionsError }}</p>
            <p v-else-if="!databaseOptionsLoading && filteredDbs.length === 0" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              No unassigned {{ form.db_engine === 'mysql' ? 'MySQL' : 'PostgreSQL' }} databases are available. Create one to continue.
            </p>
          </div>

          <div v-if="selectedDb" class="grid gap-4 rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-gray-700 dark:bg-gray-800/50 sm:grid-cols-2">
            <FormGroup label="Database Username" hint="Required. Control-plane accounts cannot be connected to applications.">
              <input v-model.trim="selectedDbCredentials.user" type="text" required autocomplete="username"
                class="w-full rounded-lg border border-gray-200 px-3 py-2 font-mono text-sm transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                placeholder="app_database_user">
            </FormGroup>
            <FormGroup label="Database Password" hint="Required for the dedicated database user.">
              <div class="relative">
                <input v-model="selectedDbCredentials.password" :type="showSelectedDbPass ? 'text' : 'password'"
                  required autocomplete="current-password"
                  class="w-full rounded-lg border border-gray-200 px-3 py-2 pr-10 font-mono text-sm transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                  placeholder="Enter the database user password">
                <button type="button" @click="showSelectedDbPass = !showSelectedDbPass"
                  class="absolute inset-y-0 right-0 px-3 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
                  :aria-label="showSelectedDbPass ? 'Hide database password' : 'Show database password'">
                  {{ showSelectedDbPass ? '●' : '◉' }}
                </button>
              </div>
            </FormGroup>
          </div>
        </div>
      </div>

      <p v-if="form.app_type === 'wordpress' && !dbEngines.includes('mysql')" class="mb-5 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300">
        Install MySQL or MariaDB from Runtime before creating a WordPress site.
      </p>

      <!-- Source Control Account & Organization Selects in Same Row -->
      <div v-if="form.app_type !== 'wordpress'" :class="selectedAccountId && repos.length > 0 ? 'grid grid-cols-2 gap-4 mb-5' : 'mb-5'">
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

      <div class="mb-5" v-if="form.app_type !== 'wordpress' && selectedAccountId">
        <FormGroup label="GitHub Repository">
          <SearchSelect v-model="form.repository" :options="repoOptions" placeholder="Select a repository" @update:model-value="onRepoSelect" />
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.app_type !== 'wordpress' && selectedAccountId && form.repository">
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
        <div v-if="form.app_type !== 'wordpress'" class="mb-5">
          <ToggleSwitch :model-value="zddEnabled" label="Zero-downtime deployment"
            description="Deploy code without downtime by swapping release symlinks through the /current directory."
            @update:model-value="setZddEnabled" />
          <p v-if="form.app_type === 'laravel' && zddEnabled" class="ml-14 mt-1 text-xs text-amber-600 dark:text-amber-400">Laravel Octane is unavailable while zero-downtime deployment is enabled.</p>
        </div>

        <div class="mb-6" v-if="form.app_type !== 'node'">
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
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Username</label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-1">Create a dedicated user for this application database.</p>
          <input v-model="newDb.user" type="text" required autocomplete="username" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="app_database_user">
        </div>
        <div>
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Password</label>
          <div class="relative">
            <input v-model="newDb.password" :type="showNewDbPass ? 'text' : 'password'" required autocomplete="new-password" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg pl-3 pr-16 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="Click Generate or enter one">
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
import { ref, onMounted, onUnmounted, computed, watch } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import BaseModal from './BaseModal.vue';
import ErrorAlert from './ErrorAlert.vue';
import FormGroup from './FormGroup.vue';
import SearchSelect from './SearchSelect.vue';
import ToggleSwitch from './ToggleSwitch.vue';
import AppTypeIcon from './AppTypeIcon.vue';

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
  deployment_strategy: 'zero-downtime',
  app_type: 'laravel',
  app_port: 3000 as number | null,
  node_preset: 'next',
  node_mode: 'server',
  package_manager: 'npm',
  build_command: 'npm run build',
  static_output_dir: 'out',
  db_engine: '',
  install_composer: true
});

const appTypes = [
  {
    value: 'laravel',
    label: 'Laravel',
    description: 'PHP framework app with Laravel defaults.',
  },
  {
    value: 'php',
    label: 'PHP',
    description: 'Custom PHP site served through PHP-FPM.',
  },
  {
    value: 'html',
    label: 'HTML',
    description: 'Static files served directly by Nginx.',
  },
  {
    value: 'node',
    label: 'Node.js',
    description: 'Server-rendered app or static JavaScript build.',
  },
  {
    value: 'wordpress',
    label: 'WordPress',
    description: 'Managed WordPress site with WP-CLI and MySQL.',
  },
];

const selectedAppType = computed(() => appTypes.find(type => type.value === form.value.app_type) || appTypes[0]);

const connectDb = ref(false);
const advancedOpen = ref(false);
const showAddDbModal = ref(false);
const selectedDb = ref('');
const creatingDb = ref(false);
const showNewDbPass = ref(false);
const showSelectedDbPass = ref(false);

const newDb = ref({ name: '', user: '', password: '' });
const selectedDbCredentials = ref({ database: '', user: '', password: '' });

const error = ref('');
const loading = ref(false);
const phpVersions = ref<string[]>(['8.4']);
const repos = ref<any[]>([]);
const branches = ref<any[]>([]);
const branchLoading = ref(false);
const availableDbs = ref<any[]>([]);
const databaseOptionsLoading = ref(false);
const databaseOptionsError = ref('');
const dbEngines = ref<string[]>([]);
const gitAccounts = ref<any[]>([]);
const selectedAccountId = ref<number | null>(null);
const selectedOrg = ref<string>('');
const zddEnabled = ref(true);
const nodeRuntime = ref<{ toolchain_ready: boolean; missing?: string[] } | null>(null);
const nodeRuntimeLoading = ref(false);
const nodeRuntimeError = ref('');
let nodeRuntimeRequest = 0;
let nodeRuntimeRefreshQueued = false;

const nodeCreationBlocked = computed(() => {
  return form.value.app_type === 'node' && (nodeRuntimeLoading.value || !nodeRuntime.value?.toolchain_ready);
});

const databaseSelectionIncomplete = computed(() => {
  if (!supportsDatabase.value || !connectDb.value) return false;
  return !selectedDb.value
    || !selectedDbCredentials.value.user.trim()
    || !selectedDbCredentials.value.password;
});

const siteCreationBlocked = computed(() => nodeCreationBlocked.value || databaseSelectionIncomplete.value);

const nodeRuntimeRequirementMessage = computed(() => {
  if (nodeRuntimeError.value) return nodeRuntimeError.value;
  const missing = nodeRuntime.value?.missing?.length ? ` (${nodeRuntime.value.missing.join(', ')})` : '';
  return `Install or repair Node.js, npm, pnpm, Yarn, Corepack, and Bun before creating this site${missing}.`;
});

const refreshNodeRuntime = async () => {
  if (!visible.value || form.value.app_type !== 'node') return;
  if (nodeRuntimeLoading.value) {
    nodeRuntimeRefreshQueued = true;
    return;
  }
  const requestID = ++nodeRuntimeRequest;
  nodeRuntimeLoading.value = true;
  nodeRuntimeError.value = '';
  apiClient.invalidate('/api/v1/server/node/info');
  try {
    const runtime = await apiClient.get('/api/v1/server/node/info', { bypassCache: true, useCache: false });
    if (requestID === nodeRuntimeRequest) nodeRuntime.value = runtime;
  } catch (e: any) {
    if (requestID === nodeRuntimeRequest) {
      nodeRuntime.value = null;
      nodeRuntimeError.value = e.message || 'Unable to check the Node.js runtime. Check the runtime page and try again.';
    }
  } finally {
    if (requestID === nodeRuntimeRequest) {
      nodeRuntimeLoading.value = false;
      if (nodeRuntimeRefreshQueued) {
        nodeRuntimeRefreshQueued = false;
        void refreshNodeRuntime();
      }
    }
  }
};

const refreshNodeRuntimeOnReturn = () => {
  if (document.visibilityState === 'visible') void refreshNodeRuntime();
};

const onZddToggle = () => {
  form.value.deployment_strategy = zddEnabled.value ? 'zero-downtime' : 'standard';
};

const setZddEnabled = (enabled: boolean) => {
  zddEnabled.value = enabled;
  onZddToggle();
};

const defaultBuildCommand = (pm: string) => {
  if (pm === 'pnpm') return 'pnpm build';
  if (pm === 'yarn') return 'yarn build';
  if (pm === 'bun') return 'bun run build';
  if (pm === 'none') return '';
  return 'npm run build';
};

const defaultStaticOutputDir = (preset: string) => {
  if (preset === 'nuxt') return '.output/public';
  if (preset === 'generic') return 'dist';
  return 'out';
};

const applyNodeDefaults = () => {
  form.value.build_command = defaultBuildCommand(form.value.package_manager);
  form.value.static_output_dir = defaultStaticOutputDir(form.value.node_preset);
};

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
  return availableDbs.value.filter((db: any) => {
    const engine = db.engine || 'mysql';
    const isAvailable = !Number(db.site_id || 0);
    const matchesEngine = !form.value.db_engine || engine === form.value.db_engine;
    return matchesEngine && isAvailable;
  });
});

const selectableDbEngines = computed(() => {
  if (form.value.app_type === 'wordpress') return dbEngines.value.filter(engine => engine === 'mysql');
  return dbEngines.value;
});

const supportsDatabase = computed(() => ['laravel', 'php', 'wordpress'].includes(form.value.app_type));

watch(() => form.value.app_type, (newType, oldType) => {
  if (newType === 'wordpress') {
    setZddEnabled(false);
    connectDb.value = true;
    form.value.web_root = '/public';
    form.value.repository = '';
    form.value.branch = '';
    if (dbEngines.value.includes('mysql')) form.value.db_engine = 'mysql';
  } else if (oldType === 'wordpress') {
    setZddEnabled(true);
    form.value.branch = 'main';
  } else {
    onZddToggle();
  }
  if (newType === 'laravel' || newType === 'wordpress') {
    form.value.web_root = '/public';
  } else if (newType === 'node') {
    form.value.web_root = '/';
    form.value.app_port = form.value.app_port || 3000;
    applyNodeDefaults();
    void refreshNodeRuntime();
  } else {
    form.value.web_root = '/';
  }

  if (!['laravel', 'php', 'wordpress'].includes(newType)) {
    connectDb.value = false;
    selectedDb.value = '';
    selectedDbCredentials.value = { database: '', user: '', password: '' };
    showSelectedDbPass.value = false;
  }
});

watch(() => [form.value.node_preset, form.value.package_manager], () => {
  if (form.value.app_type === 'node') {
    applyNodeDefaults();
  }
});

watch(() => form.value.node_mode, (mode) => {
  if (mode === 'server') {
    form.value.app_port = form.value.app_port || 3000;
  }
});

watch(() => form.value.db_engine, () => {
  selectedDb.value = '';
  selectedDbCredentials.value = { database: '', user: '', password: '' };
  showSelectedDbPass.value = false;
});

watch(selectedDb, (databaseKey) => {
  showSelectedDbPass.value = false;
  if (!databaseKey) {
    selectedDbCredentials.value = { database: '', user: '', password: '' };
    return;
  }
  if (selectedDbCredentials.value.database === databaseKey) return;

  const database = availableDbs.value.find((item: any) => `${item.engine || 'mysql'}:${item.name}` === databaseKey);
  const recordedUser = String(database?.username || '').trim();
  selectedDbCredentials.value = {
    database: databaseKey,
    user: recordedUser && recordedUser !== 'fluxo' ? recordedUser : '',
    password: '',
  };
});

watch(() => selectedDbCredentials.value.user, (user) => {
  if (!user) selectedDbCredentials.value.password = '';
});

const clearDatabaseSecrets = () => {
  selectedDbCredentials.value.password = '';
  newDb.value.password = '';
  showSelectedDbPass.value = false;
  showNewDbPass.value = false;
};

watch(connectDb, (enabled) => {
  if (!enabled) clearDatabaseSecrets();
});

watch(showAddDbModal, (open) => {
  if (!open) {
    newDb.value.password = '';
    showNewDbPass.value = false;
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

const refreshAvailableDatabases = async () => {
  databaseOptionsLoading.value = true;
  databaseOptionsError.value = '';
  try {
    availableDbs.value = await apiClient.getDatabases(true) || [];
    if (selectedDb.value && !filteredDbs.value.some((db: any) => `${db.engine || 'mysql'}:${db.name}` === selectedDb.value)) {
      selectedDb.value = '';
    }
  } catch (e: any) {
    availableDbs.value = [];
    selectedDb.value = '';
    databaseOptionsError.value = e.message || 'Failed to load available databases.';
  } finally {
    databaseOptionsLoading.value = false;
  }
};

watch(visible, (isOpen) => {
  if (isOpen) {
    void refreshAvailableDatabases();
    if (form.value.app_type === 'node') void refreshNodeRuntime();
  } else {
    showAddDbModal.value = false;
    clearDatabaseSecrets();
  }
}, { immediate: true });

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
      form.value.db_engine = form.value.app_type === 'wordpress' && dbEngines.value.includes('mysql') ? 'mysql' : dbEngines.value[0];
    }
  } catch(e) { console.error(e); }

};

const generatePassword = () => {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
  let pwd = '';
  const limit = 256 - (256 % chars.length);
  while (pwd.length < 20) {
    const bytes = new Uint8Array(32);
    crypto.getRandomValues(bytes);
    for (const byte of bytes) {
      if (byte < limit) pwd += chars[byte % chars.length];
      if (pwd.length === 20) break;
    }
  }
  return pwd;
};

const createDatabase = async () => {
  const user = newDb.value.user.trim();
  if (!newDb.value.name || !user || !newDb.value.password) {
    addToast('Database name, username, and password are required', 'error');
    return;
  }
  if (['fluxo', 'root', 'postgres'].includes(user.toLowerCase())) {
    addToast('Choose a dedicated database username', 'error');
    return;
  }
  creatingDb.value = true;
  try {
    const pass = newDb.value.password;
    const created = await apiClient.createDatabase({
      name: newDb.value.name,
      engine: form.value.db_engine,
      username: user,
      password: pass,
    });

    addToast('Database created successfully', 'success');
    availableDbs.value = await apiClient.getDatabases(true) || [];
    const databaseKey = form.value.db_engine + ':' + newDb.value.name;
    selectedDbCredentials.value = {
      database: databaseKey,
      user,
      password: created?.password || pass,
    };
    selectedDb.value = databaseKey;
    showAddDbModal.value = false;
    newDb.value = { name: '', user: '', password: '' };
  } catch (e: any) {
    addToast(e.message || 'Failed to create database', 'error');
  } finally {
    creatingDb.value = false;
  }
};
const submit = () => {
  error.value = '';

  if (nodeCreationBlocked.value) {
    error.value = 'Install or repair the Node.js toolchain before creating this site';
    return;
  }
  if (zddEnabled.value && !form.value.repository) {
    error.value = 'Zero-downtime deployment requires a repository';
    return;
  }
  if (form.value.app_type === 'node' && form.value.node_mode === 'server' && !form.value.app_port) {
    error.value = 'Node.js server sites require an application port';
    return;
  }
  if (form.value.app_type === 'wordpress' && !selectedDb.value) {
    error.value = 'WordPress requires a MySQL database';
    return;
  }
  if (supportsDatabase.value && connectDb.value && !selectedDb.value) {
    error.value = 'Select or create a database to connect';
    return;
  }
  if (supportsDatabase.value && connectDb.value && (!selectedDbCredentials.value.user.trim() || !selectedDbCredentials.value.password)) {
    error.value = 'A dedicated database username and password are required';
    return;
  }
  if (supportsDatabase.value && connectDb.value && ['fluxo', 'root', 'postgres'].includes(selectedDbCredentials.value.user.trim().toLowerCase())) {
    error.value = 'Choose a dedicated database user instead of a control-plane account';
    return;
  }

  const payload: any = { ...form.value };
  if (payload.app_type === 'node') {
    if (payload.node_mode === 'static') {
      payload.app_port = 0;
    }
  } else {
    delete payload.app_port;
    delete payload.node_preset;
    delete payload.node_mode;
    delete payload.package_manager;
    delete payload.build_command;
    delete payload.static_output_dir;
  }
  if (selectedAccountId.value && payload.app_type !== 'wordpress') {
    payload.github_account_id = selectedAccountId.value;
  }
  if (supportsDatabase.value && connectDb.value && selectedDb.value) {
    const parts = selectedDb.value.split(':');
    payload.db_engine = parts[0];
    payload.database_name = parts[1];
    const credentials = selectedDbCredentials.value.database === selectedDb.value
      ? selectedDbCredentials.value
      : { user: '', password: '' };
    payload.database_user = credentials.user;
    payload.database_password = credentials.password;
  } else {
    delete payload.db_engine;
    delete payload.database_name;
    delete payload.database_user;
    delete payload.database_password;
  }

  emit('submit-create', payload);
  visible.value = false;
};

onMounted(() => {
  void fetchVersionsAndRepos();
  window.addEventListener('focus', refreshNodeRuntimeOnReturn);
  document.addEventListener('visibilitychange', refreshNodeRuntimeOnReturn);
});

onUnmounted(() => {
  window.removeEventListener('focus', refreshNodeRuntimeOnReturn);
  document.removeEventListener('visibilitychange', refreshNodeRuntimeOnReturn);
});
</script>
