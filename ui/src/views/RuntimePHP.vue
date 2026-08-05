<template>
  <div class="space-y-6">
    <!-- PHP Settings -->
    <SkeletonLoader v-if="loading" type="card" class="mb-6" />
    <Card v-else>
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">PHP Settings</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">General settings related to PHP. <a href="https://php.net" target="_blank" rel="noopener noreferrer" class="text-blue-600 hover:underline dark:text-blue-400">Learn more</a></p>
        </div>
        <select v-model="selectedVersion" @change="fetchSettings" class="border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
          <option v-for="v in installedVersions" :key="v" :value="v">PHP {{ v }}</option>
        </select>
      </div>

      <form @submit.prevent="saveSettings" class="space-y-5">
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2 dark:text-gray-300">Max file upload size</label>
            <p class="text-xs text-gray-500 mb-2 dark:text-gray-400">Specify the maximum file size allowed for uploads in PHP. Files larger than this limit will be rejected.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.upload_max_filesize" type="number" min="1" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="50">
              <span class="text-sm text-gray-500 font-medium dark:text-gray-400">MB</span>
            </div>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2 dark:text-gray-300">Max execution time</label>
            <p class="text-xs text-gray-500 mb-2 dark:text-gray-400">Specify the maximum time a PHP script can run for before it's terminated.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.max_execution_time" type="number" min="1" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="30">
              <span class="text-sm text-gray-500 font-medium dark:text-gray-400">seconds</span>
            </div>
          </div>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2 dark:text-gray-300">Memory Limit</label>
            <p class="text-xs text-gray-500 mb-2 dark:text-gray-400">The maximum amount of memory a script may consume.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.memory_limit" type="number" min="8" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="128">
              <span class="text-sm text-gray-500 font-medium dark:text-gray-400">MB</span>
            </div>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2 dark:text-gray-300">Max input time</label>
            <p class="text-xs text-gray-500 mb-2 dark:text-gray-400">Maximum time a script can spend parsing request data.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.max_input_time" type="number" min="1" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="60">
              <span class="text-sm text-gray-500 font-medium dark:text-gray-400">seconds</span>
            </div>
          </div>
        </div>

        <div class="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
          <ToggleSwitch v-model="opcacheEnabled" label="Enable OPcache" label-position="left"
            description="Optimize PHP OPcache for production by disabling file change detection." />
        </div>

        <div class="flex justify-end pt-2 border-t border-gray-100 dark:border-gray-800">
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="saving">
            {{ saving ? 'Saving...' : 'Save Settings' }}
          </button>
        </div>
      </form>
    </Card>

    <!-- PHP Versions -->
    <SkeletonLoader v-if="loading" type="card" />
    <Card v-else>
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Versions</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage the available PHP versions on your server. <a href="https://php.net" target="_blank" rel="noopener noreferrer" class="text-blue-600 hover:underline dark:text-blue-400">Learn more</a></p>
        </div>
        <div class="flex gap-3">
          <select v-model="installVersion" class="border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
            <option v-for="v in availableVersions" :key="v.version" :value="v.version" :disabled="v.installed">
              PHP {{ v.version }}{{ v.installed ? ' (installed)' : '' }}
            </option>
          </select>
          <button @click="installVersionAction" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm disabled:opacity-50" :disabled="installing">
            {{ installing ? 'Installing...' : 'Install version' }}
          </button>
        </div>
      </div>

      <div class="max-h-[300px] overflow-y-auto">
        <DataTable :columns="versionColumns" :items="installedVersionsList" empty-text="No PHP versions installed.">
          <template #version="{ item }">
            <span class="font-medium text-gray-900 dark:text-gray-100">PHP {{ item.version }}</span>
          </template>
          <template #status="{ item }">
            <StatusBadge v-if="item.status === 'running'" label="Running" variant="green" />
            <StatusBadge v-else label="Stopped" variant="red" />
          </template>
          <template #site_default="{ item }">
            <StatusBadge v-if="item.version === siteDefault" label="Default" variant="blue" />
            <button v-else @click="setSiteDefault(item.version)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs dark:text-blue-400 dark:hover:text-blue-300">Set as default</button>
          </template>
          <template #cli_default="{ item }">
            <StatusBadge v-if="item.version === cliDefault" label="Default" variant="blue" />
            <button v-else @click="setDefaultCLI(item.version)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs dark:text-blue-400 dark:hover:text-blue-300">Set as default</button>
          </template>
          <template #actions="{ item }">
            <div class="space-x-3">
              <button v-if="item.status === 'running'" @click="stopFPM(item.version)" class="text-yellow-600 hover:text-yellow-900 font-semibold text-xs dark:text-yellow-400 dark:hover:text-yellow-300">Stop</button>
              <button v-else @click="startFPM(item.version)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs dark:text-blue-400 dark:hover:text-blue-300">Start</button>
              <button v-if="item.status === 'running'" @click="restartFPM(item.version)" class="text-green-600 hover:text-green-900 font-semibold text-xs dark:text-green-400 dark:hover:text-green-300">Restart FPM</button>
              <button @click="removeVersion(item.version)" class="text-red-600 hover:text-red-900 font-semibold text-xs dark:text-red-400 dark:hover:text-red-300">Remove</button>
            </div>
          </template>
        </DataTable>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onActivated } from 'vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import { apiClient } from '../api/client';
import Card from '../components/Card.vue';
import DataTable from '../components/DataTable.vue';
import StatusBadge from '../components/StatusBadge.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';

const versionColumns = [
  { key: 'version', label: 'Version' },
  { key: 'status', label: 'Status' },
  { key: 'site_default', label: 'Site default' },
  { key: 'cli_default', label: 'CLI default' },
];

const { addToast } = useToast();
const { confirm } = useConfirm();

const installedVersions = ref<string[]>([]);
const selectedVersion = ref('8.4');
const siteDefault = ref('8.4');
const cliDefault = ref('8.4');
const saving = ref(false);
const loading = ref(true);
const installing = ref(false);
const installVersion = ref('8.3');

const displayValues = reactive({
  upload_max_filesize: 50,
  max_execution_time: 30,
  memory_limit: 128,
  post_max_size: 50,
  max_input_time: 60,
});

const opcacheEnabled = ref(true);

function stripSuffix(val: string): number {
  const num = parseInt(val, 10);
  return isNaN(num) ? 0 : num;
}

interface PhpVersion {
  version: string;
  installed: boolean;
}

const availableVersions = ref<PhpVersion[]>([]);

const installedVersionsList = computed(() => availableVersions.value.filter(v => v.installed));

const fetchInstalledVersions = async () => {
  try {
    installedVersions.value = await apiClient.get('/api/v1/server/php');
    if (installedVersions.value.length > 0) {
      if (installedVersions.value.includes(siteDefault.value)) {
        selectedVersion.value = siteDefault.value;
      } else {
        selectedVersion.value = installedVersions.value[installedVersions.value.length - 1];
      }
    }
  } catch (e) {
    console.error('Failed to fetch PHP versions:', e);
  }
};

const fetchSiteDefault = async () => {
  try {
    const data = await apiClient.get('/api/v1/settings');
    siteDefault.value = data.default_php || '8.4';
  } catch (e) {
    console.error('Failed to fetch site default:', e);
  }
};

const fetchCLIDefault = async () => {
  try {
    const data = await apiClient.get('/api/v1/server/php/cli-default');
    cliDefault.value = data.version || '8.4';
  } catch (e) {
    console.error('Failed to fetch CLI default:', e);
  }
};

const setSiteDefault = async (version: string) => {
  try {
    const current = await apiClient.get('/api/v1/settings');
    await apiClient.post('/api/v1/settings', { ...current, default_php: version });
    apiClient.invalidate('/api/v1/settings');
    siteDefault.value = version;
    addToast(`PHP ${version} set as site default`, 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to set site default', 'error');
  }
};

const fetchAvailableVersions = async () => {
  try {
    availableVersions.value = await apiClient.get('/api/v1/server/php/versions/available');
    const firstNotInstalled = availableVersions.value.find(v => !v.installed);
    if (firstNotInstalled) installVersion.value = firstNotInstalled.version;
  } catch (e) {
    console.error('Failed to fetch available versions:', e);
  }
};

const fetchSettings = async () => {
  try {
    loading.value = true;
    const data = await apiClient.get(`/api/v1/server/php/settings?version=${selectedVersion.value}`);
    displayValues.upload_max_filesize = stripSuffix(data.upload_max_filesize || '50');
    displayValues.max_execution_time = stripSuffix(data.max_execution_time || '30');
    displayValues.memory_limit = stripSuffix(data.memory_limit || '128');
    displayValues.post_max_size = stripSuffix(data.post_max_size || '50');
    displayValues.max_input_time = stripSuffix(data.max_input_time || '60');
    opcacheEnabled.value = data.opcache_enable === '1';
  } catch (e) {
    console.error('Failed to fetch PHP settings:', e);
  } finally {
    loading.value = false;
  }
};

const saveSettings = async () => {
  saving.value = true;
  try {
    const body = {
      version: selectedVersion.value,
      upload_max_filesize: displayValues.upload_max_filesize + 'M',
      max_execution_time: String(displayValues.max_execution_time),
      memory_limit: displayValues.memory_limit + 'M',
      post_max_size: displayValues.post_max_size + 'M',
      max_input_time: String(displayValues.max_input_time),
      opcache_enable: opcacheEnabled.value ? '1' : '0',
    };
    await apiClient.post('/api/v1/server/php/settings', body);
    apiClient.invalidate('/api/v1/server/php');
    addToast('PHP settings saved successfully', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save PHP settings', 'error');
  } finally {
    saving.value = false;
  }
};

const installVersionAction = async () => {
  installing.value = true;
  try {
    await apiClient.post('/api/v1/server/php/versions/install', { version: installVersion.value });
    apiClient.invalidate('/api/v1/server/php');
    addToast(`PHP ${installVersion.value} installed successfully`, 'success');
    fetchInstalledVersions();
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to install PHP version', 'error');
  } finally {
    installing.value = false;
  }
};

const startFPM = async (version: string) => {
  try {
    await apiClient.post(`/api/v1/server/php/start/${version}`);
    apiClient.invalidate('/api/v1/server/php');
    addToast(`PHP ${version} FPM started`, 'success');
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to start PHP-FPM', 'error');
  }
};

const stopFPM = async (version: string) => {
  const ok = await confirm({ title: 'Stop PHP-FPM', message: `Stop PHP ${version} FPM? Sites using this version will be offline.`, confirmText: 'Stop', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    await apiClient.post(`/api/v1/server/php/stop/${version}`);
    apiClient.invalidate('/api/v1/server/php');
    addToast(`PHP ${version} FPM stopped`, 'success');
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to stop PHP-FPM', 'error');
  }
};

const restartFPM = async (version: string) => {
  const ok = await confirm({ title: 'Restart PHP-FPM', message: `Restart PHP ${version} FPM? This will briefly reload the PHP service.`, confirmText: 'Restart', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  try {
    await apiClient.post(`/api/v1/server/php/restart/${version}`);
    apiClient.invalidate('/api/v1/server/php');
    addToast(`PHP ${version} FPM restarted`, 'success');
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to restart PHP-FPM', 'error');
  }
};

const removeVersion = async (version: string) => {
  const confirmed = await confirm({
    title: 'Remove PHP Version',
    message: `Remove PHP ${version}? This will uninstall the PHP-FPM and CLI packages.`,
    confirmText: 'Remove',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  try {
    await apiClient.post('/api/v1/server/php/versions/remove', { version });
    apiClient.invalidate('/api/v1/server/php');
    addToast(`PHP ${version} removed`, 'success');
    fetchInstalledVersions();
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to remove PHP version', 'error');
  }
};

const setDefaultCLI = async (version: string) => {
  try {
    await apiClient.post('/api/v1/server/php/versions/default', { version });
    apiClient.invalidate('/api/v1/server/php');
    cliDefault.value = version;
    addToast(`PHP ${version} set as CLI default`, 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to set default', 'error');
  }
};

onMounted(async () => {
  loading.value = true;
  await Promise.allSettled([
    fetchSiteDefault(),
    fetchCLIDefault(),
    fetchInstalledVersions(),
    fetchAvailableVersions(),
  ]);
  await fetchSettings();
  loading.value = false;
});

onActivated(async () => {
  await Promise.allSettled([
    fetchSiteDefault(),
    fetchCLIDefault(),
    fetchInstalledVersions(),
    fetchAvailableVersions(),
  ]);
  await fetchSettings();
});
</script>
