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
          <div class="flex items-start gap-3">
            <span class="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-lg" :class="selectedAppType.iconClass">
              <svg v-if="form.app_type === 'laravel'" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3l7 4v10l-7 4-7-4V7l7-4z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9 8v8h6" /></svg>
              <svg v-else-if="form.app_type === 'php'" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 9l-3 3 3 3" /><path stroke-linecap="round" stroke-linejoin="round" d="M16 9l3 3-3 3" /><path stroke-linecap="round" stroke-linejoin="round" d="M13 7l-2 10" /></svg>
              <svg v-else-if="form.app_type === 'html'" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M7 4h10l-1 16-4 1-4-1L7 4z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9 8h6M10 12h4" /></svg>
              <span v-else-if="form.app_type === 'wordpress'" class="flex h-6 w-6 items-center justify-center rounded-full border-2 border-current font-serif text-sm font-bold">W</span>
              <svg v-else class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 8h12v8H6z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9 8V5m6 3V5M9 19v-3m6 3v-3M6 11H3m3 4H3m18-4h-3m3 4h-3" /></svg>
            </span>
            <div class="min-w-0 flex-1">
              <select v-model="form.app_type" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
                <option v-for="type in appTypes" :key="type.value" :value="type.value">{{ type.label }}</option>
              </select>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ selectedAppType.description }}</p>
            </div>
          </div>
        </FormGroup>
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

      <div v-if="(form.app_type === 'laravel' || form.app_type === 'php' || form.app_type === 'wordpress') && selectableDbEngines.length > 0" class="mb-5">
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
            <div class="flex gap-3">
              <select v-model="selectedDb" class="flex-1 border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
                <option value="">-- Select or create a database --</option>
                <option v-for="db in filteredDbs" :key="db.engine + ':' + db.name" :value="db.engine + ':' + db.name">{{ db.name }}</option>
              </select>
              <button type="button" @click="showAddDbModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add Database</button>
            </div>
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
import ToggleSwitch from './ToggleSwitch.vue';

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
    iconClass: 'bg-red-50 text-red-600 dark:bg-red-950/30 dark:text-red-300',
  },
  {
    value: 'php',
    label: 'PHP',
    description: 'Custom PHP site served through PHP-FPM.',
    iconClass: 'bg-indigo-50 text-indigo-600 dark:bg-indigo-950/30 dark:text-indigo-300',
  },
  {
    value: 'html',
    label: 'HTML',
    description: 'Static files served directly by Nginx.',
    iconClass: 'bg-orange-50 text-orange-600 dark:bg-orange-950/30 dark:text-orange-300',
  },
  {
    value: 'node',
    label: 'Node.js',
    description: 'Server-rendered app or static JavaScript build.',
    iconClass: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-950/30 dark:text-emerald-300',
  },
  {
    value: 'wordpress',
    label: 'WordPress',
    description: 'Managed WordPress site with WP-CLI and MySQL.',
    iconClass: 'bg-sky-50 text-sky-700 dark:bg-sky-950/30 dark:text-sky-300',
  },
];

const selectedAppType = computed(() => appTypes.find(type => type.value === form.value.app_type) || appTypes[0]);

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
const zddEnabled = ref(true);

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
  if (!form.value.db_engine) return availableDbs.value;
  return availableDbs.value.filter((db: any) => {
    const engine = db.engine || 'mysql';
    const availableForWordPress = form.value.app_type !== 'wordpress' || !Number(db.site_id || 0);
    return engine === form.value.db_engine && availableForWordPress;
  });
});

const selectableDbEngines = computed(() => {
  if (form.value.app_type === 'wordpress') return dbEngines.value.filter(engine => engine === 'mysql');
  return dbEngines.value;
});

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
  } else {
    form.value.web_root = '/';
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
      form.value.db_engine = form.value.app_type === 'wordpress' && dbEngines.value.includes('mysql') ? 'mysql' : dbEngines.value[0];
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
      await apiClient.createDatabaseUser({ user: newDb.value.user, password: pass, databases: [newDb.value.name], engine: form.value.db_engine });
    } else {
      await apiClient.createDatabaseUserGrant({ user: 'fluxo', databases: [newDb.value.name], engine: form.value.db_engine });
    }

    addToast('Database created successfully', 'success');
    showAddDbModal.value = false;
    selectedDb.value = form.value.db_engine + ':' + newDb.value.name;

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
  if (connectDb.value && selectedDb.value) {
    const parts = selectedDb.value.split(':');
    payload.db_engine = parts[0];
    payload.database_name = parts[1];
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
