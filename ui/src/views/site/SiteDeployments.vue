<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100">
      <div class="px-6 py-4 border-b border-gray-100 flex justify-between items-center">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Deployments</h2>
          <p class="text-sm text-gray-600 mt-1">
            Deployment history for this site. Each deployment runs the configured deploy script (git pull, composer install, etc.).
          </p>
        </div>
        <button @click="() => fetchDeployments()" class="p-2 text-gray-600 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors" title="Refresh">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </button>
      </div>

      <div v-if="deployments.length === 0" class="text-center text-gray-500 text-sm py-12">
        No deployments yet. Trigger a deployment from the Overview tab.
      </div>

      <ul v-else class="divide-y divide-gray-100">
        <li v-for="dep in deployments" :key="dep.id" class="px-6 py-4 hover:bg-gray-50 transition-colors">
          <div class="flex items-start justify-between">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-3">
                <span v-if="dep.commit_hash" class="font-mono text-sm font-medium text-blue-600">{{ dep.commit_hash.slice(0, 7) }}</span>
                <span v-else class="font-mono text-sm text-gray-400">No commit</span>
                <span :class="statusBadge(dep.status)">{{ dep.status }}</span>
              </div>
              <p class="text-xs text-gray-500 mt-1">Deployed {{ timeAgo(dep.created_at) }} &middot; Deployment #{{ dep.id }}</p>
            </div>
          </div>
        </li>
      </ul>
    </div>

    <div v-if="selectedDeployment" class="bg-white rounded-lg shadow-sm border border-gray-100">
      <div class="px-6 py-4 border-b border-gray-100">
        <h2 class="text-lg font-semibold text-gray-900">Deployment #{{ selectedDeployment.id }} Output</h2>
      </div>
      <div class="p-6">
        <pre class="bg-gray-900 text-green-400 p-4 rounded-lg text-sm font-mono overflow-x-auto max-h-96 whitespace-pre-wrap">{{ selectedDeployment.output || 'No output captured.' }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { router } from '../../router';

const route = useRoute();
const id = route.params.id as string;

const deployments = ref<any[]>([]);
const selectedDeployment = ref<any>(null);

const authedFetch = async (url: string, init?: RequestInit) => {
  const token = localStorage.getItem('fluxo_jwt');
  const headers: Record<string, string> = {};
  if (init?.headers) Object.assign(headers, init.headers as Record<string, string>);
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (!headers['Content-Type'] && !(init?.body instanceof FormData)) headers['Content-Type'] = 'application/json';
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

const statusBadge = (status: string) => {
  const base = 'inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold';
  if (status === 'success') return `${base} bg-green-100 text-green-800`;
  if (status === 'failed') return `${base} bg-red-100 text-red-800`;
  if (status === 'running') return `${base} bg-yellow-100 text-yellow-800`;
  return `${base} bg-gray-100 text-gray-600`;
};

const timeAgo = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)} minute${Math.floor(diff / 60) > 1 ? 's' : ''} ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} hour${Math.floor(diff / 3600) > 1 ? 's' : ''} ago`;
  if (diff < 604800) return `${Math.floor(diff / 86400)} day${Math.floor(diff / 86400) > 1 ? 's' : ''} ago`;
  return `${Math.floor(diff / 604800)} week${Math.floor(diff / 604800) > 1 ? 's' : ''} ago`;
};

onMounted(() => fetchDeployments());
</script>
