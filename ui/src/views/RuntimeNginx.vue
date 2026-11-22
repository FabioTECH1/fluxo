<template>
  <div class="space-y-6">
    <ErrorAlert :message="updates.error.value || infoError" />
    <Card>
      <div class="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Nginx</h2><p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Installed web server and package update availability.</p></div>
        <AppButton variant="secondary" :loading="reloading" :disabled="reloading || confirming" @click="reloadNginx">{{ reloading ? 'Reloading…' : 'Reload Nginx' }}</AppButton>
      </div>
      <SkeletonLoader v-if="loading" type="card" />
      <dl v-else class="grid grid-cols-1 gap-5 sm:grid-cols-2 text-sm text-gray-900 dark:text-gray-100">
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">Binary</dt><dd class="mt-1 break-all font-mono">{{ info.binary || 'Not found' }}</dd></div>
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">On-disk binary version</dt><dd class="mt-1 font-mono">{{ info.version || 'Unknown' }}</dd></div>
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">Config directory</dt><dd class="mt-1 font-mono">{{ info.config_dir || '/etc/nginx' }}</dd></div>
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">Running binary adoption</dt><dd class="mt-1">Not verified</dd></div>
      </dl>
    </Card>
    <Card>
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Package updates</h2><p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ nginxCheckMessage(updates.state.value) }}</p></div>
        <AppButton :loading="updates.busy.value || updates.state.value?.checking" :disabled="updates.busy.value || updates.state.value?.checking" @click="check">Check for updates</AppButton>
      </div>
      <dl class="mt-5 grid grid-cols-1 gap-5 text-sm text-gray-900 sm:grid-cols-2 dark:text-gray-100">
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">Installed package revision · last successful check</dt><dd class="mt-1 break-all font-mono">{{ snapshot ? `${snapshot.package || 'Unknown package'} ${snapshot.installed_version || 'Unknown revision'}` : 'Not checked' }}</dd></div>
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">APT candidate · last successful check</dt><dd class="mt-1 break-all font-mono">{{ snapshot?.candidate_version || 'Not confirmed' }}</dd></div>
        <div><dt class="text-xs text-gray-500 dark:text-gray-400">Last successful check</dt><dd class="mt-1">{{ updates.state.value?.last_successful_check ? new Date(updates.state.value.last_successful_check * 1000).toLocaleString() : 'No successful check' }}{{ updates.state.value?.stale ? ' · stale' : '' }}</dd></div>
      </dl>
      <p v-if="snapshot?.reason" class="mt-4 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ updates.state.value?.stale ? `Previous check: ${snapshot.reason}` : snapshot.reason }}</p>
      <AppButton v-if="snapshot && ['update_available', 'held'].includes(snapshot.state)" variant="secondary" size="sm" class="mt-4" @click="updates.openInstructions">View instructions</AppButton>
      <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-gray-400">Checks use the binary's installed package and effective APT policy, including distro revisions. Fluxo does not install Nginx updates or change repositories. A configuration reload does not confirm that running processes adopted a new executable.</p>
    </Card>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, onMounted, onActivated } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import { useNginxUpdates, nginxCheckMessage } from '../composables/useNginxUpdates';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import Card from '../components/Card.vue';
import AppButton from '../components/AppButton.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
const updates = useNginxUpdates();
const snapshot = computed(() => updates.state.value?.snapshot);
const { addToast } = useToast(); const { confirm } = useConfirm();
const info = ref<any>({}); const loading = ref(true); const infoError = ref(''); const reloading = ref(false); const confirming = ref(false);
async function fetchInfo() { try { info.value = await apiClient.get('/api/v1/server/nginx/info', { bypassCache: true, useCache: false }); infoError.value = ''; } catch (e: any) { infoError.value = e.message || 'Nginx information is unavailable.'; } finally { loading.value = false; } }
async function check() { const status = await updates.check(); await fetchInfo(); if (status && !status.error) addToast(nginxCheckMessage(status), status.snapshot?.state === 'current' ? 'success' : 'info'); }
async function reloadNginx() {
  if (reloading.value || confirming.value) return;
  confirming.value = true;
  const ok = await confirm({ title: 'Reload Nginx?', message: 'Validate and reload the Nginx configuration. This does not perform a process restart or confirm adoption of an updated binary.', confirmText: 'Reload Nginx', cancelText: 'Cancel', variant: 'info' });
  confirming.value = false; if (!ok) return;
  reloading.value = true;
  try { await apiClient.post('/api/v1/server/nginx/restart'); addToast('Nginx configuration reloaded', 'success'); await fetchInfo(); }
  catch (e: any) { addToast(e.message || 'Failed to reload Nginx', 'error'); }
  finally { reloading.value = false; }
}
onMounted(() => { void fetchInfo(); void updates.refresh(); });
onActivated(() => { if (info.value.binary) { void fetchInfo(); void updates.refresh(); } });
</script>
