<template>
  <div class="max-w-6xl mx-auto px-6 py-6 space-y-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold">Site Dashboard (ID: {{ id }})</h1>
      <button @click="triggerDeploy" class="px-4 py-2 text-white bg-green-600 rounded-lg hover:bg-green-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="deploying">
        {{ deploying ? 'Deploying...' : 'Deploy Now' }}
      </button>
    </div>

    <div class="flex border-b border-gray-200 mb-6">
      <button @click="activeTab = 'overview'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': activeTab === 'overview', 'text-gray-500 hover:text-gray-700': activeTab !== 'overview'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Overview</button>
      <button @click="activeTab = 'environment'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': activeTab === 'environment', 'text-gray-500 hover:text-gray-700': activeTab !== 'environment'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Environment</button>
      <button @click="activeTab = 'daemons'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': activeTab === 'daemons', 'text-gray-500 hover:text-gray-700': activeTab !== 'daemons'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Daemons</button>
      <button @click="activeTab = 'scheduler'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': activeTab === 'scheduler', 'text-gray-500 hover:text-gray-700': activeTab !== 'scheduler'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Scheduler</button>
      <button @click="activeTab = 'databases'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': activeTab === 'databases', 'text-gray-500 hover:text-gray-700': activeTab !== 'databases'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Databases</button>
      <button @click="activeTab = 'ssl'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': activeTab === 'ssl', 'text-gray-500 hover:text-gray-700': activeTab !== 'ssl'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">SSL</button>
    </div>

    <!-- Overview Tab -->
    <div v-if="activeTab === 'overview'" class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Deployment History</h2>
        <ul class="divide-y divide-gray-200">
          <li v-for="dep in deployments" :key="dep.id" class="py-3">
            <div class="flex justify-between">
              <span class="font-medium text-sm text-gray-900">#{{ dep.id }}</span>
              <span :class="{'text-green-600 font-semibold': dep.status==='success', 'text-red-600 font-semibold': dep.status==='failed', 'text-yellow-600 font-semibold': dep.status==='running'}">{{ dep.status }}</span>
            </div>
            <div class="text-xs text-gray-500 mt-1">{{ new Date(dep.created_at).toLocaleString() }}</div>
          </li>
          <li v-if="deployments.length === 0" class="py-3 text-gray-500 text-sm text-center">No deployments yet.</li>
        </ul>
      </div>

      <div class="bg-gray-900 rounded-lg shadow-sm p-4 text-green-400 font-mono text-sm h-96 overflow-y-auto" ref="terminalBox">
        <div v-for="(line, idx) in logs" :key="idx" class="whitespace-pre-wrap">{{ line }}</div>
        <div v-if="logs.length === 0" class="text-gray-500 italic">Waiting for logs...</div>
      </div>
    </div>

    <!-- Environment Tab -->
    <div v-if="activeTab === 'environment'" class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <h2 class="text-lg font-semibold text-gray-900">Environment Variables (.env)</h2>
        <button @click="saveEnv" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">Save Environment</button>
      </div>
      <textarea v-model="envContent" class="w-full h-96 font-mono text-sm border border-gray-200 rounded-lg p-4 bg-gray-50 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="KEY=VALUE"></textarea>
    </div>

    <!-- Daemons Tab -->
    <div v-if="activeTab === 'daemons'" class="space-y-6">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Add New Daemon</h2>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Command</label>
            <input v-model="newDaemon.command" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="php artisan queue:work">
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Working Directory</label>
            <input v-model="newDaemon.directory" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="/var/www/domain.com/current">
          </div>
        </div>
        <div class="flex justify-end">
          <button @click="addDaemon" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">Add Daemon</button>
        </div>
      </div>

      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Active Daemons</h2>
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Command</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
              <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-200">
            <tr v-for="d in daemons" :key="d.id" class="hover:bg-gray-50 transition-colors">
              <td class="px-6 py-4 whitespace-nowrap font-mono text-sm text-gray-900">{{ d.command }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm">
                <span :class="{'text-green-600 font-semibold': d.status === 'active', 'text-red-600 font-semibold': d.status === 'failed', 'text-gray-500': d.status !== 'active' && d.status !== 'failed'}">{{ d.status }}</span>
              </td>
              <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                <button @click="restartDaemon(d.id)" class="text-blue-600 hover:text-blue-900 mr-4 font-semibold">Restart</button>
                <button @click="deleteDaemon(d.id)" class="text-red-600 hover:text-red-900 font-semibold">Delete</button>
              </td>
            </tr>
            <tr v-if="daemons.length === 0">
              <td colspan="3" class="px-6 py-8 text-center text-gray-500 text-sm">No daemons running.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Scheduler Tab -->
    <div v-if="activeTab === 'scheduler'" class="space-y-6">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Add Cron Job</h2>
        <div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-4">
          <div class="md:col-span-1">
            <label class="block text-gray-700 text-sm font-bold mb-2">Schedule</label>
            <input v-model="newCron.expression" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="* * * * *">
          </div>
          <div class="md:col-span-2">
            <label class="block text-gray-700 text-sm font-bold mb-2">Command</label>
            <input v-model="newCron.command" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="php artisan schedule:run">
          </div>
        </div>
        <div class="flex justify-end">
          <button @click="addCron" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">Add Cron Job</button>
        </div>
      </div>

      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Scheduled Jobs</h2>
        <ul class="divide-y divide-gray-200">
          <li v-for="c in crons" :key="c.id" class="py-3 flex justify-between items-center">
            <div>
              <span class="font-mono bg-gray-100 px-2 py-1 rounded mr-3 text-sm">{{ c.expression }}</span>
              <span class="font-mono text-sm text-gray-700">{{ c.command }}</span>
            </div>
            <button @click="deleteCron(c.id)" class="text-red-600 hover:text-red-900 text-sm font-semibold">Delete</button>
          </li>
          <li v-if="crons.length === 0" class="py-3 text-gray-500 text-sm text-center">No scheduled jobs.</li>
        </ul>
      </div>
    </div>

    <!-- Databases Tab -->
    <div v-if="activeTab === 'databases'" class="space-y-6">
      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Create Database</h2>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Engine</label>
            <select v-model="newDatabase.engine" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
              <option value="mysql" :disabled="!activeEngines.includes('mysql')">MySQL / MariaDB</option>
              <option value="postgres" :disabled="!activeEngines.includes('postgres')">PostgreSQL</option>
            </select>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Database Name</label>
            <input v-model="newDatabase.name" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="forge_db" pattern="^[a-zA-Z0-9_]+$">
          </div>
        </div>
        <div class="flex justify-end">
          <button @click="createDatabase" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">Add Database</button>
        </div>

        <div v-if="!activeEngines.includes('postgres')" class="mt-4 p-4 bg-yellow-50 border border-yellow-200 rounded-lg text-sm text-yellow-800">
          <p>PostgreSQL is not installed on this server.</p>
          <button @click="installPostgres" class="mt-2 underline text-yellow-900 font-medium">Click here to install it in the background.</button>
        </div>
      </div>

      <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
        <h2 class="text-lg font-semibold mb-4 text-gray-900">Attached Databases</h2>
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Engine</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Username</th>
              <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-200">
            <tr v-for="db in databases" :key="db.id" class="hover:bg-gray-50 transition-colors">
              <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ db.engine === 'mysql' ? 'MySQL' : 'PostgreSQL' }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm font-mono text-gray-900">{{ db.name }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm font-mono text-gray-500">{{ db.username }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                <button @click="deleteDatabase(db.id)" class="text-red-600 hover:text-red-900 font-semibold">Delete</button>
              </td>
            </tr>
            <tr v-if="databases.length === 0">
              <td colspan="4" class="px-6 py-8 text-center text-gray-500 text-sm">No databases created yet.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- SSL Tab -->
    <div v-if="activeTab === 'ssl'" class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <h2 class="text-lg font-semibold mb-4 text-gray-900">SSL Management</h2>

      <div class="flex border-b border-gray-200 mb-4">
        <button @click="sslTab = 'letsencrypt'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': sslTab === 'letsencrypt', 'text-gray-500 hover:text-gray-700': sslTab !== 'letsencrypt'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Let's Encrypt</button>
        <button @click="sslTab = 'custom'" :class="{'border-b-2 border-blue-600 text-blue-600 font-semibold': sslTab === 'custom', 'text-gray-500 hover:text-gray-700': sslTab !== 'custom'}" class="px-4 py-2 font-medium text-sm focus:outline-none transition-colors">Custom Certificate</button>
      </div>

      <div v-if="sslTab === 'letsencrypt'">
        <p class="text-sm text-gray-600 mb-4">Automatically issue and renew a free Let's Encrypt certificate. Make sure your domain's DNS is pointing to this server.</p>
        <button @click="issueLetsEncrypt" class="px-4 py-2 text-white bg-green-600 rounded-lg hover:bg-green-700 font-medium shadow-sm transition-colors">Issue Let's Encrypt Certificate</button>
      </div>

      <div v-if="sslTab === 'custom'">
        <div class="mb-4">
          <label class="block text-gray-700 text-sm font-bold mb-2">Certificate / CA Bundle</label>
          <textarea v-model="customSSL.certificate" class="w-full h-32 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
        </div>
        <div class="mb-4">
          <label class="block text-gray-700 text-sm font-bold mb-2">Private Key</label>
          <textarea v-model="customSSL.private_key" class="w-full h-32 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="-----BEGIN PRIVATE KEY-----"></textarea>
        </div>
        <div class="flex justify-end">
          <button @click="installCustomSSL" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">Install Custom Certificate</button>
        </div>
      </div>
    </div>

    <!-- Credential Modal -->
    <div v-if="showCredentialModal" class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden transform transition-all">
        <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
          <h3 class="text-lg font-bold text-green-600">Database Created Successfully</h3>
          <button @click="showCredentialModal = false" class="text-gray-400 hover:text-gray-600 transition-colors">
            <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div class="p-6">
          <p class="text-sm text-gray-700 mb-6">
            Please save this password immediately. For security reasons, it is <strong>not stored</strong> in the database and cannot be recovered if lost.
          </p>

          <div class="mb-4">
            <label class="block text-gray-700 text-sm font-bold mb-2">Database Engine</label>
            <input type="text" readonly :value="generatedCredentials.engine" class="w-full border border-gray-200 bg-gray-100 rounded-lg px-3 py-2 cursor-text text-sm">
          </div>
          <div class="mb-4">
            <label class="block text-gray-700 text-sm font-bold mb-2">Database Name</label>
            <input type="text" readonly :value="generatedCredentials.name" class="w-full border border-gray-200 bg-gray-100 rounded-lg px-3 py-2 cursor-text text-sm font-mono">
          </div>
          <div class="mb-4">
            <label class="block text-gray-700 text-sm font-bold mb-2">Database User</label>
            <input type="text" readonly :value="generatedCredentials.username" class="w-full border border-gray-200 bg-gray-100 rounded-lg px-3 py-2 cursor-text text-sm font-mono">
          </div>
          <div class="mb-6">
            <label class="block text-gray-700 text-sm font-bold mb-2">Database Password</label>
            <input type="text" readonly :value="generatedCredentials.password" class="w-full border border-yellow-300 bg-yellow-50 rounded-lg px-3 py-2 cursor-text text-sm font-mono font-bold text-lg">
          </div>

          <div class="flex justify-end pt-2 border-t border-gray-100 mt-6">
            <button @click="showCredentialModal = false" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">I have saved this password</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue';
import { useRoute } from 'vue-router';
import { router } from '../router';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';

const route = useRoute();
const id = route.params.id as string;

const { confirm } = useConfirm();
const { addToast } = useToast();

const activeTab = ref('overview');

const deployments = ref<any[]>([]);
const deploying = ref(false);
const logs = ref<string[]>([]);
const terminalBox = ref<HTMLElement | null>(null);

const envContent = ref('');

const daemons = ref<any[]>([]);
const newDaemon = ref({ command: '', directory: '' });

const crons = ref<any[]>([]);
const newCron = ref({ expression: '', command: '' });

const databases = ref<any[]>([]);
const newDatabase = ref({ name: '', engine: 'mysql' });
const activeEngines = ref<string[]>(['mysql']);
const showCredentialModal = ref(false);
const generatedCredentials = ref({ engine: '', name: '', username: '', password: '' });

const sslTab = ref('letsencrypt');
const customSSL = ref({ certificate: '', private_key: '' });

let ws: WebSocket | null = null;

const authedFetch = async (url: string, init?: RequestInit) => {
  const token = localStorage.getItem('fluxo_jwt');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (init?.headers) {
    const initHeaders = init.headers as Record<string, string>;
    Object.assign(headers, initHeaders);
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const res = await window.fetch(url, { ...init, headers });
  if (res.status === 401) {
    localStorage.removeItem('fluxo_jwt');
    router.push('/login');
    throw new Error('Unauthorized');
  }
  return res;
};

const fetchDeployments = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/deployments`);
    deployments.value = await res.json();
  } catch (e) {}
};

const triggerDeploy = async () => {
  deploying.value = true;
  logs.value = [];
  try {
    await authedFetch(`/api/v1/sites/${id}/deploy`, { method: 'POST' });
    setTimeout(fetchDeployments, 2000);
  } catch (e) {}
  deploying.value = false;
};

const connectWS = () => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws`);
  ws.onmessage = (event) => {
    logs.value.push(event.data);
    nextTick(() => {
      if (terminalBox.value) {
        terminalBox.value.scrollTop = terminalBox.value.scrollHeight;
      }
    });
  };
};

const fetchEnv = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/env`);
    const data = await res.json();
    envContent.value = data.content;
  } catch (e) {}
};

const saveEnv = async () => {
  try {
    await authedFetch(`/api/v1/sites/${id}/env`, {
      method: 'POST',
      body: JSON.stringify({ content: envContent.value })
    });
    addToast('Environment saved successfully!', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save environment', 'error');
  }
};

const fetchDaemons = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/daemons`);
    daemons.value = await res.json();
  } catch (e) {}
};

const addDaemon = async () => {
  if (!newDaemon.value.command || !newDaemon.value.directory) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/daemons`, {
      method: 'POST',
      body: JSON.stringify(newDaemon.value)
    });
    newDaemon.value.command = '';
    newDaemon.value.directory = '';
    fetchDaemons();
  } catch (e) {}
};

const restartDaemon = async (daemonId: number) => {
  try {
    await authedFetch(`/api/v1/sites/${id}/daemons/${daemonId}/restart`, { method: 'POST' });
    setTimeout(fetchDaemons, 1000);
  } catch (e) {}
};

const deleteDaemon = async (daemonId: number) => {
  const confirmed = await confirm({
    title: 'Delete Daemon',
    message: 'Are you sure you want to delete this daemon? This will stop and remove the background systemd service.',
    confirmText: 'Delete Daemon',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/daemons/${daemonId}`, { method: 'DELETE' });
    addToast('Daemon deleted successfully', 'success');
    fetchDaemons();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete daemon', 'error');
  }
};

const fetchCrons = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/crons`);
    crons.value = await res.json() || [];
  } catch (e) {}
};

const addCron = async () => {
  if (!newCron.value.expression || !newCron.value.command) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/crons`, {
      method: 'POST',
      body: JSON.stringify(newCron.value)
    });
    newCron.value.expression = '';
    newCron.value.command = '';
    fetchCrons();
  } catch (e) {}
};

const deleteCron = async (cronId: number) => {
  const confirmed = await confirm({
    title: 'Delete Scheduled Job',
    message: 'Are you sure you want to delete this scheduled job? This will remove it from the server cron configurations.',
    confirmText: 'Delete Job',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/crons/${cronId}`, { method: 'DELETE' });
    addToast('Scheduled job deleted successfully', 'success');
    fetchCrons();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete scheduled job', 'error');
  }
};

const issueLetsEncrypt = async () => {
  try {
    addToast('Requesting Let\'s Encrypt certificate... This may take a minute.', 'info');
    const res = await authedFetch(`/api/v1/sites/${id}/ssl/letsencrypt`, { method: 'POST' });
    if (!res.ok) throw new Error(await res.text());
    addToast('Let\'s Encrypt certificate installed successfully!', 'success');
  } catch (e: any) {
    addToast('Failed: ' + e.message, 'error');
  }
};

const installCustomSSL = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/ssl/custom`, {
      method: 'POST',
      body: JSON.stringify(customSSL.value)
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Custom certificate installed successfully!', 'success');
    customSSL.value.certificate = '';
    customSSL.value.private_key = '';
  } catch (e: any) {
    addToast('Installation failed: ' + e.message, 'error');
  }
};

const fetchEngines = async () => {
  try {
    const res = await authedFetch('/api/v1/server/engines');
    activeEngines.value = await res.json() || ['mysql'];
  } catch (e) {}
};

const installPostgres = async () => {
  try {
    addToast('Installing PostgreSQL in the background. This may take a minute or two.', 'info');
    const res = await authedFetch('/api/v1/server/engines/postgres/install', { method: 'POST' });
    const result = await res.json();
    addToast(result.message || 'PostgreSQL installation completed.', 'success');
    fetchEngines();
  } catch (e: any) {
    addToast('Failed to install PostgreSQL: ' + e.message, 'error');
  }
};

const fetchDatabases = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/databases`);
    databases.value = await res.json();
  } catch (e) {}
};

const createDatabase = async () => {
  try {
    const res = await authedFetch(`/api/v1/sites/${id}/databases`, {
      method: 'POST',
      body: JSON.stringify({ name: newDatabase.value.name, engine: newDatabase.value.engine })
    });
    const db = await res.json();
    newDatabase.value.name = '';
    generatedCredentials.value = db;
    showCredentialModal.value = true;
    fetchDatabases();
  } catch (e: any) {
    addToast(e.message || 'Failed to create database', 'error');
  }
};

const deleteDatabase = async (dbId: number) => {
  const confirmed = await confirm({
    title: 'Delete Database',
    message: 'Are you sure you want to delete this database and its user? All data will be lost permanently.',
    confirmText: 'Delete Database',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await authedFetch(`/api/v1/sites/${id}/databases/${dbId}`, { method: 'DELETE' });
    addToast('Database deleted successfully', 'success');
    fetchDatabases();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete database', 'error');
  }
};

onMounted(() => {
  fetchDeployments();
  fetchEnv();
  fetchDaemons();
  fetchCrons();
  fetchDatabases();
  fetchEngines();
  connectWS();
});

onUnmounted(() => {
  if (ws) ws.close();
});
</script>