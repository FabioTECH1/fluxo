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

      <div v-if="form.app_type === 'laravel' || form.app_type === 'php'" class="mb-5">
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
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Select or create a new database to connect to your site.</label>
            <div class="flex gap-3">
              <select v-model="selectedDb" class="flex-1 border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
                <option value="">-- Select or create a database --</option>
                <option v-for="db in availableDbs" :key="db.name" :value="db.name">{{ db.name }}</option>
              </select>
              <button type="button" @click="showAddDbModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add Database</button>
            </div>
          </div>
        </div>
      </div>

      <div class="mb-5">
        <FormGroup label="GitHub Repository">
          <select v-model="form.repository" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option value="">None (Static Directory)</option>
            <option v-for="repo in repos" :key="repo.full_name" :value="repo.full_name">{{ repo.full_name }}</option>
          </select>
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.repository">
        <FormGroup label="Branch">
          <input v-model="form.branch" type="text" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="main">
        </FormGroup>
      </div>

      <div class="mb-5" v-if="form.repository">
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
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import BaseModal from './BaseModal.vue';
import ErrorAlert from './ErrorAlert.vue';
import FormGroup from './FormGroup.vue';

const { addToast } = useToast();

const visible = defineModel<boolean>({ required: true });
const emit = defineEmits(['created']);

const formRef = ref<HTMLFormElement | null>(null);

const form = ref({
  domain: '',
  php_version: '8.4',
  web_root: '/public',
  repository: '',
  branch: 'main',
  deployment_strategy: 'standard',
  app_type: 'laravel',
  app_port: null
});

const connectDb = ref(false);
const showAddDbModal = ref(false);
const selectedDb = ref('');
const creatingDb = ref(false);
const showNewDbPass = ref(false);

const newDb = ref({ name: '', user: '', password: '' });

const error = ref('');
const loading = ref(false);
const phpVersions = ref<string[]>(['8.4']);
const repos = ref<any[]>([]);
const availableDbs = ref<any[]>([]);

const token = () => localStorage.getItem('fluxo_jwt');

const fetchVersionsAndRepos = async () => {
  try {
    const versions = await apiClient.getPhpVersions();
    if (versions && versions.length > 0) {
      phpVersions.value = versions;
      // Use site default from settings if available
      try {
        const settingsRes = await fetch('/api/v1/settings', { headers: { 'Authorization': `Bearer ${token()}` } });
        if (settingsRes.ok) {
          const settings = await settingsRes.json();
          if (settings.default_php && versions.includes(settings.default_php)) {
            form.value.php_version = settings.default_php;
          } else {
            const sorted = [...versions].sort();
            form.value.php_version = sorted[sorted.length - 1];
          }
        }
      } catch {
        const sorted = [...versions].sort();
        form.value.php_version = sorted[sorted.length - 1];
      }
    }
  } catch (e) { console.error(e); }

  try {
    repos.value = await apiClient.getGithubRepos();
  } catch(e) { console.error(e); }

  try {
    const res = await fetch('/api/v1/databases', { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) availableDbs.value = await res.json();
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
    const dbRes = await fetch('/api/v1/databases', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newDb.value.name })
    });
    if (!dbRes.ok) throw new Error(await dbRes.text());

    if (newDb.value.user) {
      const pass = newDb.value.password || generatePassword();
      const userRes = await fetch('/api/v1/databases/users', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ user: newDb.value.user, password: pass, databases: [newDb.value.name] })
      });
      if (!userRes.ok) throw new Error(await userRes.text());
    } else {
      // Grant the global fluxo user access
      const grantRes = await fetch('/api/v1/databases/users/grants', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ user: 'fluxo', databases: [newDb.value.name] })
      });
      if (!grantRes.ok) throw new Error(await grantRes.text());
    }

    addToast('Database created successfully', 'success');
    showAddDbModal.value = false;
    selectedDb.value = newDb.value.name;

    const res = await fetch('/api/v1/databases', { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) availableDbs.value = await res.json();

    newDb.value = { name: '', user: '', password: '' };
  } catch (e: any) {
    addToast(e.message || 'Failed to create database', 'error');
  } finally {
    creatingDb.value = false;
  }
};

const submit = async () => {
  error.value = '';
  loading.value = true;
  try {
    const payload: any = { ...form.value };
    if (connectDb.value && selectedDb.value) {
      payload.database_name = selectedDb.value;
      payload.database_user = newDb.value.user || '';
      payload.database_password = newDb.value.password || '';
    }
    await apiClient.createSite(payload);
    if (connectDb.value && selectedDb.value) {
      addToast('Site created with database: ' + selectedDb.value, 'success');
    }
    emit('created');
  } catch (e: any) {
    error.value = e.message;
  } finally {
    loading.value = false;
  }
};

onMounted(fetchVersionsAndRepos);
</script>