<template>
  <SkeletonLoader v-if="loading" type="card" class="mb-6" />
  <div v-else class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
    <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-5 dark:border-gray-800 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Node.js Toolchain</h2>
          <StatusBadge v-if="info.toolchain_ready" label="Ready" variant="green" />
          <StatusBadge v-else label="Incomplete" variant="yellow" />
        </div>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Node.js and the package managers available to application deployments.</p>
      </div>
      <div class="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:shrink-0 sm:flex-nowrap sm:justify-end">
        <AppButton v-if="info.toolchain_ready" variant="secondary" size="sm" :loading="restarting" :disabled="removing || installing" @click="restartNode">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h5M20 20v-5h-5M5.5 15a7 7 0 0011.7 2.6L20 15M4 9l2.8-2.6A7 7 0 0118.5 9" /></svg>
          {{ restarting ? 'Restarting...' : 'Restart apps' }}
        </AppButton>
        <AppButton v-if="!info.toolchain_ready" variant="primary" size="sm" :loading="installing" :disabled="removing || restarting" @click="installNode">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3v12m0 0l-4-4m4 4l4-4M5 19h14" /></svg>
          {{ installing ? 'Installing...' : info.installed ? 'Repair toolchain' : 'Install toolchain' }}
        </AppButton>
        <AppButton v-if="info.managed" variant="danger" size="sm" :loading="removing" :disabled="installing || restarting" @click="removeNode">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 7h16M9 7V4h6v3m-8 0l1 13h8l1-13" /></svg>
          {{ removing ? 'Removing...' : 'Remove' }}
        </AppButton>
      </div>
    </div>

    <div v-if="error" class="mx-6 mt-5 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
      {{ error }}
    </div>
    <div v-else-if="!info.toolchain_ready" class="mx-6 mt-5 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300">
      Missing: {{ info.missing?.join(', ') || 'required toolchain components' }}.
    </div>

    <dl class="grid grid-cols-1 px-6 py-4 sm:grid-cols-2 lg:grid-cols-3">
      <div v-for="component in components" :key="component.label" class="border-b border-gray-100 py-4 last:border-b-0 sm:px-4 sm:[&:nth-last-child(-n+2)]:border-b-0 lg:[&:nth-last-child(-n+3)]:border-b-0 dark:border-gray-800">
        <dt class="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">{{ component.label }}</dt>
        <dd class="mt-1 break-all font-mono text-sm text-gray-900 dark:text-gray-100">{{ component.version || 'Not installed' }}</dd>
      </div>
    </dl>

    <div class="border-t border-gray-100 px-6 py-4 text-xs text-gray-500 dark:border-gray-800 dark:text-gray-400">
      <span v-if="info.binary" class="break-all font-mono">{{ info.binary }}</span>
      <span v-else>Node.js is not installed.</span>
      <span v-if="info.minimum_node_version" class="mt-1 block">Minimum supported Node.js version: {{ info.minimum_node_version }}</span>
      <span v-if="info.installed && !info.managed" class="mt-1 block">This Node.js installation is managed outside Fluxo and will not be removed by Fluxo.</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onActivated } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import AppButton from '../components/AppButton.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';

const { addToast, showToast, updateToast } = useToast();
const { confirm } = useConfirm();

interface NodeRuntimeInfo {
  installed: boolean;
  managed: boolean;
  toolchain_ready: boolean;
  minimum_node_version: string;
  binary: string;
  version: string;
  npm: string;
  pnpm: string;
  yarn: string;
  corepack: string;
  bun: string;
  missing: string[];
}

const info = ref<NodeRuntimeInfo>({
  installed: false,
  managed: false,
  toolchain_ready: false,
  minimum_node_version: '',
  binary: '',
  version: '',
  npm: '',
  pnpm: '',
  yarn: '',
  corepack: '',
  bun: '',
  missing: [],
});
const restarting = ref(false);
const installing = ref(false);
const removing = ref(false);
const loading = ref(true);
const error = ref('');
let infoRequest = 0;

const components = computed(() => [
  { label: 'Node.js', version: info.value.version },
  { label: 'npm', version: info.value.npm },
  { label: 'pnpm', version: info.value.pnpm },
  { label: 'Yarn', version: info.value.yarn },
  { label: 'Corepack', version: info.value.corepack },
  { label: 'Bun', version: info.value.bun },
]);

const fetchInfo = async (showLoader = true) => {
  const requestID = ++infoRequest;
  try {
    if (showLoader) loading.value = true;
    error.value = '';
    apiClient.invalidate('/api/v1/server/node/info');
    const runtime = await apiClient.get('/api/v1/server/node/info', { bypassCache: true, useCache: false });
    if (requestID === infoRequest) info.value = runtime;
  } catch (e: any) {
    if (requestID === infoRequest) error.value = e.message || 'Failed to load the Node.js toolchain status.';
  } finally {
    if (requestID === infoRequest) loading.value = false;
  }
};

const restartNode = async () => {
  const ok = await confirm({ title: 'Restart Applications', message: 'Restart all Node.js and Bun application processes managed on this server?', confirmText: 'Restart', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  restarting.value = true;
  try {
    await apiClient.post('/api/v1/server/node/restart');
    addToast('Node.js and Bun application processes restarted', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to restart applications', 'error');
  } finally {
    restarting.value = false;
  }
};

const installNode = async () => {
  const ok = await confirm({
    title: info.value.installed ? 'Repair Node.js Toolchain' : 'Install Node.js Toolchain',
    message: 'Fluxo will install a compatible Node.js LTS release, npm, pnpm, Yarn, Corepack, and Bun. This may take several minutes.',
    confirmText: info.value.installed ? 'Repair' : 'Install',
    cancelText: 'Cancel',
    variant: 'info',
  });
  if (!ok) return;
  installing.value = true;
  error.value = '';
  const toastId = showToast({
    title: info.value.installed ? 'Repairing Node.js toolchain' : 'Installing Node.js toolchain',
    description: 'This may take several minutes.',
    type: 'loading',
  });
  try {
    await apiClient.post('/api/v1/server/node/install');
    apiClient.invalidate('/api/v1/server/node/info');
    updateToast(toastId, {
      title: info.value.installed ? 'Node.js toolchain repaired' : 'Node.js toolchain installed',
      description: 'Node.js, package managers, and Bun are ready to use.',
      type: 'success',
    });
    await fetchInfo(false);
  } catch (e: any) {
    const installError = e.message || 'Failed to install the Node.js toolchain.';
    updateToast(toastId, {
      title: 'Node.js toolchain installation failed',
      description: installError,
      type: 'error',
    });
    await fetchInfo(false);
    error.value = installError;
  } finally {
    installing.value = false;
  }
};

const removeNode = async () => {
  const ok = await confirm({
    title: 'Remove Node.js Toolchain',
    message: 'Fluxo will remove only the toolchain it manages. Site files, dependency caches, and externally installed tools will remain. Node.js sites must be removed first.',
    confirmText: 'Remove',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!ok) return;
  removing.value = true;
  error.value = '';
  const toastId = showToast({
    title: 'Removing Node.js toolchain',
    description: 'This may take a moment.',
    type: 'loading',
  });
  try {
    await apiClient.post('/api/v1/server/node/remove');
    apiClient.invalidate('/api/v1/server/node/info');
    updateToast(toastId, {
      title: 'Node.js toolchain removed',
      description: 'Site files and dependency caches were left untouched.',
      type: 'success',
    });
    await fetchInfo(false);
  } catch (e: any) {
    const removeError = e.message || 'Failed to remove the Node.js toolchain.';
    await fetchInfo(false);
    error.value = removeError;
    updateToast(toastId, {
      title: 'Node.js toolchain could not be removed',
      description: removeError,
      type: 'error',
    });
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
