<template>
  <div class="space-y-6">
    <!-- PHP Settings -->
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">PHP Settings</h2>
          <p class="text-sm text-gray-600 mt-1">General settings related to PHP. <a href="https://php.net" class="text-blue-600 hover:underline">Learn more</a></p>
        </div>
        <select v-model="selectedVersion" @change="fetchSettings" class="border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
          <option v-for="v in installedVersions" :key="v" :value="v">PHP {{ v }}</option>
        </select>
      </div>

      <form @submit.prevent="saveSettings" class="space-y-5">
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Max file upload size</label>
            <p class="text-xs text-gray-500 mb-2">Specify the maximum file size allowed for uploads in PHP. Files larger than this limit will be rejected.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.upload_max_filesize" type="number" min="1" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center" placeholder="50">
              <span class="text-sm text-gray-500 font-medium">MB</span>
            </div>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Max execution time</label>
            <p class="text-xs text-gray-500 mb-2">Specify the maximum time a PHP script can run for before it's terminated.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.max_execution_time" type="number" min="1" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center" placeholder="30">
              <span class="text-sm text-gray-500 font-medium">seconds</span>
            </div>
          </div>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Memory Limit</label>
            <p class="text-xs text-gray-500 mb-2">The maximum amount of memory a script may consume.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.memory_limit" type="number" min="8" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center" placeholder="128">
              <span class="text-sm text-gray-500 font-medium">MB</span>
            </div>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-2">Max input time</label>
            <p class="text-xs text-gray-500 mb-2">Maximum time a script can spend parsing request data.</p>
            <div class="flex items-center gap-2">
              <input v-model.number="displayValues.max_input_time" type="number" min="1" class="w-24 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-center" placeholder="60">
              <span class="text-sm text-gray-500 font-medium">seconds</span>
            </div>
          </div>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">OPcache</label>
          <p class="text-xs text-gray-500 mb-2">Optimize PHP OPcache for production by disabling file change detection to significantly boost performance.</p>
          <label class="inline-flex items-center gap-3 cursor-pointer">
            <button type="button" @click="opcacheEnabled = !opcacheEnabled"
              class="relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
              :class="opcacheEnabled ? 'bg-blue-600' : 'bg-gray-200'">
              <span class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="opcacheEnabled ? 'translate-x-5' : 'translate-x-0'"></span>
            </button>
            <span class="text-sm font-medium text-gray-700 select-none">Enable OPcache</span>
          </label>
        </div>

        <div class="flex justify-end pt-2 border-t border-gray-100">
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="saving">
            {{ saving ? 'Saving...' : 'Save Settings' }}
          </button>
        </div>
      </form>
    </div>

    <!-- PHP Versions -->
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Versions</h2>
          <p class="text-sm text-gray-600 mt-1">Manage the available PHP versions on your server. <a href="https://php.net" class="text-blue-600 hover:underline">Learn more</a></p>
        </div>
        <div class="flex gap-3">
          <select v-model="installVersion" class="border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option v-for="v in availableVersions" :key="v.version" :value="v.version" :disabled="v.installed">
              PHP {{ v.version }}{{ v.installed ? ' (installed)' : '' }}
            </option>
          </select>
          <button @click="installVersionAction" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm disabled:opacity-50" :disabled="installing">
            {{ installing ? 'Installing...' : 'Install version' }}
          </button>
        </div>
      </div>

      <div class="overflow-x-auto border rounded-lg max-h-[300px] overflow-y-auto">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50 sticky top-0">
            <tr>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Version</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Site default</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">CLI default</th>
              <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-200">
            <tr v-if="installedVersionsList.length === 0">
              <td colspan="5" class="px-6 py-8 text-center text-gray-500 text-sm">No PHP versions installed.</td>
            </tr>
            <tr v-for="v in installedVersionsList" :key="v.version" class="hover:bg-gray-50 transition-colors">
              <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">PHP {{ v.version }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm">
                <span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-green-100 text-green-800">Installed</span>
              </td>
              <td class="px-6 py-4 whitespace-nowrap text-sm">
                <span v-if="v.version === siteDefault" class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-blue-100 text-blue-800">Default</span>
                <button v-else @click="setSiteDefault(v.version)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs">Set as default</button>
              </td>
              <td class="px-6 py-4 whitespace-nowrap text-sm">
                <button @click="setDefaultCLI(v.version)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs">Set as default</button>
              </td>
              <td class="px-6 py-4 whitespace-nowrap text-right text-sm space-x-3">
                <button @click="restartFPM(v.version)" class="text-green-600 hover:text-green-900 font-semibold text-xs">Restart FPM</button>
                <button @click="removeVersion(v.version)" class="text-red-600 hover:text-red-900 font-semibold text-xs">Remove</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';

const { addToast } = useToast();
const { confirm } = useConfirm();

const token = () => localStorage.getItem('fluxo_jwt');

const installedVersions = ref<string[]>([]);
const selectedVersion = ref('8.4');
const siteDefault = ref('8.4');
const saving = ref(false);
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
    const res = await fetch('/api/v1/server/php', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    installedVersions.value = await res.json();
    if (installedVersions.value.length > 0) {
      selectedVersion.value = installedVersions.value[installedVersions.value.length - 1];
    }
  } catch (e) {
    console.error('Failed to fetch PHP versions:', e);
  }
};

const fetchSiteDefault = async () => {
  const tokenVal = token();
  try {
    const res = await fetch('/api/v1/settings', {
      headers: { 'Authorization': `Bearer ${tokenVal}` }
    });
    if (res.ok) {
      const data = await res.json();
      siteDefault.value = data.default_php || '8.4';
    }
  } catch (e) {
    console.error('Failed to fetch site default:', e);
  }
};

const setSiteDefault = async (version: string) => {
  const tokenVal = token();
  try {
    const getRes = await fetch('/api/v1/settings', {
      headers: { 'Authorization': `Bearer ${tokenVal}` }
    });
    const current = await getRes.json();
    await fetch('/api/v1/settings', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${tokenVal}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...current, default_php: version })
    });
    siteDefault.value = version;
    addToast(`PHP ${version} set as site default`, 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to set site default', 'error');
  }
};

const fetchAvailableVersions = async () => {
  try {
    const res = await fetch('/api/v1/server/php/versions/available', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    availableVersions.value = await res.json();
    const firstNotInstalled = availableVersions.value.find(v => !v.installed);
    if (firstNotInstalled) installVersion.value = firstNotInstalled.version;
  } catch (e) {
    console.error('Failed to fetch available versions:', e);
  }
};

const fetchSettings = async () => {
  try {
    const res = await fetch(`/api/v1/server/php/settings?version=${selectedVersion.value}`, {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    displayValues.upload_max_filesize = stripSuffix(data.upload_max_filesize || '50');
    displayValues.max_execution_time = stripSuffix(data.max_execution_time || '30');
    displayValues.memory_limit = stripSuffix(data.memory_limit || '128');
    displayValues.post_max_size = stripSuffix(data.post_max_size || '50');
    displayValues.max_input_time = stripSuffix(data.max_input_time || '60');
    opcacheEnabled.value = data.opcache_enable === '1';
  } catch (e) {
    console.error('Failed to fetch PHP settings:', e);
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
    const res = await fetch('/api/v1/server/php/settings', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    if (!res.ok) throw new Error(await res.text());
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
    const res = await fetch('/api/v1/server/php/versions/install', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: installVersion.value })
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`PHP ${installVersion.value} installed successfully`, 'success');
    fetchInstalledVersions();
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to install PHP version', 'error');
  } finally {
    installing.value = false;
  }
};

const restartFPM = async (version: string) => {
  const ok = await confirm({ title: 'Restart PHP-FPM', message: `Restart PHP ${version} FPM? This will briefly reload the PHP service.`, confirmText: 'Restart', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  try {
    const res = await fetch(`/api/v1/server/php/restart/${version}`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`PHP ${version} FPM restarted`, 'success');
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
    const res = await fetch('/api/v1/server/php/versions/remove', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ version })
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`PHP ${version} removed`, 'success');
    fetchInstalledVersions();
    fetchAvailableVersions();
  } catch (e: any) {
    addToast(e.message || 'Failed to remove PHP version', 'error');
  }
};

const setDefaultCLI = async (version: string) => {
  try {
    const res = await fetch('/api/v1/server/php/versions/default', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ version })
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`PHP ${version} set as CLI default`, 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to set default', 'error');
  }
};

onMounted(() => {
  fetchInstalledVersions();
  fetchAvailableVersions();
  fetchSiteDefault();
});
</script>