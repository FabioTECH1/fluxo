<template>
  <SkeletonLoader v-if="loading" type="card" class="mb-6" />
  <div v-else class="rounded-lg border border-gray-100 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-900">
    <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-5 dark:border-gray-800 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Python Application Support</h2>
          <StatusBadge v-if="info.toolchain_ready" label="Ready" variant="green" />
          <StatusBadge v-else label="Incomplete" variant="yellow" />
        </div>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">System Python, isolated virtual environments, pip, and uv for Python sites.</p>
      </div>
      <div class="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:shrink-0 sm:flex-nowrap sm:justify-end">
        <AppButton v-if="info.toolchain_ready" variant="secondary" size="sm" :loading="restarting" :disabled="removing || installing" @click="restartPython">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h5M20 20v-5h-5M5.5 15a7 7 0 0011.7 2.6L20 15M4 9l2.8-2.6A7 7 0 0118.5 9" /></svg>
          {{ restarting ? 'Restarting...' : 'Restart apps' }}
        </AppButton>
        <AppButton v-if="!info.toolchain_ready" variant="primary" size="sm" :loading="installing" :disabled="removing || restarting" @click="installPython">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3v12m0 0l-4-4m4 4l4-4M5 19h14" /></svg>
          {{ installing ? 'Installing...' : info.installed ? 'Repair support' : 'Install support' }}
        </AppButton>
        <AppButton v-if="info.managed" variant="danger" size="sm" :loading="removing" :disabled="installing || restarting" @click="removePython">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 7h16M9 7V4h6v3m-8 0l1 13h8l1-13" /></svg>
          {{ removing ? 'Removing...' : 'Remove managed tools' }}
        </AppButton>
      </div>
    </div>

    <div v-if="error" class="mx-6 mt-5 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
      {{ error }}
    </div>
    <div v-else-if="!info.toolchain_ready" class="mx-6 mt-5 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300">
      Missing: {{ info.missing?.join(', ') || 'required Python components' }}.
    </div>

    <dl class="grid grid-cols-1 px-6 py-4 sm:grid-cols-2 lg:grid-cols-4">
      <div v-for="component in components" :key="component.label" class="border-b border-gray-100 py-4 last:border-b-0 sm:px-4 sm:[&:nth-last-child(-n+2)]:border-b-0 lg:[&:nth-last-child(-n+4)]:border-b-0 dark:border-gray-800">
        <dt class="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">{{ component.label }}</dt>
        <dd class="mt-1 break-all font-mono text-sm text-gray-900 dark:text-gray-100">{{ component.value }}</dd>
      </div>
    </dl>

    <div class="border-t border-gray-100 px-6 py-4 text-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
      <span v-if="info.binary" class="break-all font-mono">{{ info.binary }}</span>
      <span v-else>Python 3 is not installed.</span>
      <span class="mt-1 block">Minimum supported Python version: {{ info.minimum_python_version || '3.10.0' }}</span>
      <span class="mt-1 block">Removing Fluxo-managed tools never removes Ubuntu's system Python or existing site virtual environments.</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onMounted, ref } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';

interface PythonRuntimeInfo {
  installed: boolean;
  managed: boolean;
  toolchain_ready: boolean;
  python_compatible: boolean;
  minimum_python_version: string;
  binary: string;
  version: string;
  venv: boolean;
  pip: string;
  uv: string;
  missing: string[];
}

const emptyInfo = (): PythonRuntimeInfo => ({
  installed: false,
  managed: false,
  toolchain_ready: false,
  python_compatible: false,
  minimum_python_version: '3.10.0',
  binary: '',
  version: '',
  venv: false,
  pip: '',
  uv: '',
  missing: [],
});

const { confirm } = useConfirm();
const { addToast, showToast, updateToast } = useToast();
const info = ref<PythonRuntimeInfo>(emptyInfo());
const loading = ref(true);
const installing = ref(false);
const removing = ref(false);
const restarting = ref(false);
const error = ref('');
let infoRequest = 0;

const components = computed(() => [
  { label: 'Python', value: info.value.version || 'Not installed' },
  { label: 'Virtual environments', value: info.value.venv ? 'Available' : 'Unavailable' },
  { label: 'pip', value: info.value.pip || 'Available inside site environments' },
  { label: 'uv', value: info.value.uv || 'Not installed' },
]);

const fetchInfo = async (showLoader = true) => {
  const requestID = ++infoRequest;
  try {
    if (showLoader) loading.value = true;
    error.value = '';
    apiClient.invalidate('/api/v1/server/python/info');
    const runtime = await apiClient.get('/api/v1/server/python/info', { bypassCache: true, useCache: false });
    if (requestID === infoRequest) info.value = runtime;
  } catch (e: any) {
    if (requestID === infoRequest) error.value = e.message || 'Failed to load Python application support status.';
  } finally {
    if (requestID === infoRequest) loading.value = false;
  }
};

const restartPython = async () => {
  const ok = await confirm({ title: 'Restart Python Applications', message: 'Restart every Python application process managed by Fluxo?', confirmText: 'Restart', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  restarting.value = true;
  try {
    await apiClient.post('/api/v1/server/python/restart');
    addToast('Python application processes restarted', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to restart Python applications', 'error');
  } finally {
    restarting.value = false;
  }
};

const installPython = async () => {
  const ok = await confirm({
    title: info.value.installed ? 'Repair Python Application Support' : 'Install Python Application Support',
    message: 'Fluxo will install the supported Ubuntu Python packages and a verified uv release. Existing system Python packages will not be replaced.',
    confirmText: info.value.installed ? 'Repair' : 'Install',
    cancelText: 'Cancel',
    variant: 'info',
  });
  if (!ok) return;
  installing.value = true;
  error.value = '';
  const toastId = showToast({ title: 'Installing Python application support', description: 'This may take several minutes.', type: 'loading' });
  try {
    await apiClient.post('/api/v1/server/python/install');
    updateToast(toastId, { title: 'Python application support is ready', description: 'Python sites can now be created and deployed.', type: 'success' });
    await fetchInfo(false);
  } catch (e: any) {
    const message = e.message || 'Failed to install Python application support.';
    updateToast(toastId, { title: 'Python installation failed', description: message, type: 'error' });
    await fetchInfo(false);
    error.value = message;
  } finally {
    installing.value = false;
  }
};

const removePython = async () => {
  const ok = await confirm({
    title: 'Remove Managed Python Tools',
    message: "Fluxo will remove only its managed uv installation. Ubuntu's system Python, site files, and virtual environments remain. Python sites must be removed first.",
    confirmText: 'Remove',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!ok) return;
  removing.value = true;
  error.value = '';
  const toastId = showToast({ title: 'Removing managed Python tools', description: 'System Python will remain installed.', type: 'loading' });
  try {
    await apiClient.post('/api/v1/server/python/remove');
    updateToast(toastId, { title: 'Managed Python tools removed', description: "Ubuntu's system Python was left unchanged.", type: 'success' });
    await fetchInfo(false);
  } catch (e: any) {
    const message = e.message || 'Failed to remove managed Python tools.';
    updateToast(toastId, { title: 'Python tools could not be removed', description: message, type: 'error' });
    await fetchInfo(false);
    error.value = message;
  } finally {
    removing.value = false;
  }
};

let activatedOnce = false;
onMounted(() => void fetchInfo());
onActivated(() => {
  if (activatedOnce) void fetchInfo(false);
  activatedOnce = true;
});
</script>
