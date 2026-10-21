<template>
  <div class="space-y-6">
    <Card v-for="failure in phpUpdates.state.value.failures" :key="failure.id">
      <p class="font-semibold text-red-700 dark:text-red-300">{{ failure.kind.startsWith('repository_') ? 'PHP repository setup needs attention' : `PHP ${failure.version} update needs attention` }}</p>
      <p class="mt-2 text-sm text-gray-700 dark:text-gray-300">{{ failure.message }}</p>
      <button type="button" class="mt-3 text-sm font-semibold text-blue-600 hover:underline dark:text-blue-400" @click="report = failure">View report</button>
    </Card>
    <Card v-if="repositoryStatus && repositoryStatus.state !== 'current'">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">PHP repository setup needs attention</h2>
      <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">{{ repositoryStatus.message }}</p>
      <button v-if="repositoryStatus.can_review || repositoryStatus.resume_plan_id" type="button" class="mt-3 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50" :disabled="phpUpdates.busy.value || reviewingRepository" @click="reviewRepository">
        {{ reviewingRepository ? 'Reviewing…' : repositoryStatus.resume_plan_id ? 'Resume repository setup' : 'Review repair' }}
      </button>
    </Card>
    <BaseModal :model-value="!!repositoryReview" title="Repair PHP repository setup" :loading="switchingRepository" @update:model-value="repositoryReview = null">
      <p class="text-sm text-gray-700 dark:text-gray-300">Complete PHP repository setup. Your installed PHP versions and running applications stay unchanged.</p>
      <p v-if="repositoryStatus?.resume_plan_id" class="mt-3 text-sm text-amber-700 dark:text-amber-300">Resume the interrupted setup. Conflicting configuration changes require manual attention.</p>
      <details class="mt-3 text-sm text-gray-600 dark:text-gray-400"><summary>Technical details</summary><p class="mt-2">Use packages.sury.org/php and disable {{ repositoryReview?.disabled_ppa_entries }} legacy PPA entries. Existing Ubuntu PHP branches keep Ubuntu security sources, including ESM.</p><ul class="mt-2 space-y-1 break-all"><li v-for="file in repositoryReview?.files" :key="file">{{ file }}</li></ul><p class="mt-2 break-all">Signing key: {{ repositoryReview?.key_fingerprint }}</p></details>
      <p v-if="repositoryReview?.retained_extensions?.length" class="mt-3 text-sm text-amber-700 dark:text-amber-300">These extensions will stay at their verified installed revisions because the new repository offers older builds: {{ repositoryReview.retained_extensions.join(', ') }}. No extension downgrade is included.</p>
      <ul v-if="repositoryReview?.migration_blockers?.length" class="mt-3 list-disc pl-5 text-sm text-red-700 dark:text-red-300"><li v-for="reason in repositoryReview.migration_blockers" :key="reason">{{ reason }}</li></ul>
      <p v-if="repositoryReview?.rollback_unavailable?.length" class="mt-3 text-sm text-amber-700 dark:text-amber-300">Some current PHP packages could not be retained for rollback. A later PHP update will require acknowledgement if its complete rollback set remains unavailable.</p>
      <div v-for="(reasons, branch) in repositoryReview?.package_blockers" :key="branch" class="mt-3 text-sm text-amber-700 dark:text-amber-300"><template v-if="reasons.length"><p class="font-semibold">PHP {{ branch }} still needs attention before package installation:</p><ul class="mt-1 list-disc pl-5"><li v-for="reason in reasons" :key="reason">{{ reason }}</li></ul></template></div>
      <template #footer><button type="button" class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50" :disabled="switchingRepository || phpUpdates.busy.value || !!repositoryReview?.migration_blockers?.length" @click="switchRepository">{{ switchingRepository ? 'Starting…' : 'Complete setup' }}</button></template>
    </BaseModal>
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
            <span class="font-medium text-gray-900 dark:text-gray-100">PHP {{ item.full_version || item.version }}</span>
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
            <div class="flex items-center justify-end gap-3">
              <button v-if="item.status === 'running'" :disabled="phpUpdates.locked.value" @click="restartFPM(item.version)" class="text-green-600 hover:text-green-900 font-semibold text-xs disabled:opacity-50 dark:text-green-400 dark:hover:text-green-300">Restart FPM</button>
              <TableActionMenu :items="versionMenu(item)" :disabled="installing" :aria-label="`PHP ${item.version} actions`" @select="id => versionAction(id, item)" />
            </div>
          </template>
        </DataTable>
      </div>
    </Card>
    <BaseModal :model-value="!!updatePlan" :title="`Update PHP ${updatePlan?.version || ''}`" confirm-text="Update PHP" :loading="startingUpdate" :confirm-disabled="!updatePlan?.reviewed || !!updatePlan?.impact.blocked_reasons?.length || (!!updatePlan && !updatePlan.rollback?.available && !acknowledgeNoRollback)" @update:model-value="value => { if (!value) updatePlan = null; }" @submit="applyUpdate">
      <p class="text-sm text-gray-700 dark:text-gray-300">Update PHP and restart affected services. Brief downtime may occur.</p>
      <div v-if="updatePlan?.impact.blocked_reasons?.length" class="mt-3 space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100" role="alert">
        <p class="font-semibold">Installation needs attention</p>
        <p v-for="reason in updatePlan.impact.blocked_reasons" :key="reason">{{ reason }}</p>
      </div>
      <p class="mt-3 text-sm font-semibold text-gray-900 dark:text-gray-100">Affected sites</p>
      <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ [...new Set(updatePlan?.impact.sites.map(s => s.domain))].join(', ') || 'No managed sites use this branch.' }}</p>
      <p class="mt-3 text-sm font-semibold text-gray-900 dark:text-gray-100">Affected services</p>
      <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ affectedServices.join(', ') }}</p>
      <p v-if="updatePlan?.impact.phpmyadmin" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Also affected: phpMyAdmin</p>

      <div v-if="updatePlan && !updatePlan.rollback?.available && !updatePlan.impact.blocked_reasons?.length" class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
        <p>Package rollback is unavailable for this update.</p>
        <label class="mt-3 flex items-start gap-2">
          <input v-model="acknowledgeNoRollback" type="checkbox" class="mt-1 shrink-0" />
          <span>I agree to update without package rollback.</span>
        </label>
      </div>
    </BaseModal>
    <BaseModal :model-value="!!recoveryPlan" :title="`${recoveryPlan?.kind === 'rollback' ? 'Roll back' : 'Repair'} PHP ${recoveryPlan?.version || ''}`" :confirm-text="recoveryPlan?.kind === 'rollback' ? 'Roll back PHP' : 'Repair PHP'" :loading="startingRecovery" @update:model-value="value => { if (!value) recoveryPlan = null; }" @submit="applyRecovery">
      <template v-if="recoveryPlan?.kind === 'rollback'">
        <p class="text-sm text-gray-700 dark:text-gray-300">Restore the previous PHP packages and restart affected services. Brief downtime may occur.</p>
        <p class="mt-3 text-sm font-semibold text-gray-900 dark:text-gray-100">Affected sites</p>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ [...new Set(recoveryPlan.impact.sites.map(s => s.domain))].join(', ') || 'No managed sites use this branch.' }}</p>
        <p class="mt-3 text-sm font-semibold text-gray-900 dark:text-gray-100">Affected services</p>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ recoveryAffectedServices.join(', ') }}</p>
        <p v-if="recoveryPlan.impact.phpmyadmin" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Also affected: phpMyAdmin</p>
        <p class="mt-3 text-sm font-semibold text-amber-800 dark:text-amber-200">Older packages may reintroduce security vulnerabilities.</p>
      </template>
      <template v-else>
        <p class="text-sm text-gray-700 dark:text-gray-300">Resume the recorded maintenance attempt using its retained package set, then verify affected services. Sites may be unavailable while services stop and restart. Queue and Horizon jobs remain queued while their workers drain. Nightwatch monitoring pauses and buffered telemetry may be lost. Configured application timeouts still apply.</p>
        <p class="mt-3 text-sm text-gray-700 dark:text-gray-300">{{ recoveryPlan?.impact.sites.map(s => s.domain).join(', ') || 'No managed sites use this branch.' }}</p>
        <p class="mt-3 text-sm text-gray-700 dark:text-gray-300">Your current PHP configuration will be kept. This does not restore application files, databases, Redis data or jobs. Recovery may require SSH if package or configuration damage is outside this recorded update.</p>
        <p v-for="note in recoveryPlan?.impact.notes || []" :key="note" class="mt-3 text-sm text-amber-800 dark:text-amber-200">{{ note }}</p>
        <p v-if="recoveryPlan?.retry_fpm_reload" class="mt-3 text-sm text-gray-700 dark:text-gray-300">Repair will retry an unconfirmed graceful FPM reload only if the recorded master still uses the old runtime. A replacement master will not be signalled.</p>
        <details class="mt-4 text-sm text-gray-700 dark:text-gray-300"><summary class="cursor-pointer font-semibold">Target package revisions</summary><ul class="mt-2 space-y-1 break-words"><li v-for="pkg in recoveryPlan?.packages" :key="`${pkg.name}:${pkg.architecture}`">{{ pkg.name }}: {{ pkg.version }}</li></ul></details>
      </template>
    </BaseModal>
    <BaseModal :model-value="!!report" title="PHP update report" :hide-footer="true" @update:model-value="value => { if (!value) report = null; }">
      <p class="text-sm text-gray-700 dark:text-gray-300">{{ report?.message }}</p>
      <pre class="mt-4 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-950 p-4 text-xs text-gray-100">{{ report?.output }}</pre>
      <div v-if="report && phpUpdates.state.value.recoveries.some(r => r.operation_id === report?.id)" class="mt-4 flex flex-wrap gap-3">
        <template v-for="recovery in phpUpdates.state.value.recoveries.filter(r => r.operation_id === report?.id)" :key="recovery.generation_id">
          <button v-if="recovery.can_repair" type="button" :disabled="phpUpdates.busy.value" class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50" @click="reviewRecovery(recovery.operation_id, 'repair')">Review repair</button>
          <button v-if="recovery.can_rollback" type="button" :disabled="phpUpdates.busy.value" class="rounded-lg border border-gray-300 px-4 py-2 text-sm font-semibold text-gray-900 disabled:opacity-50 dark:border-gray-600 dark:text-gray-100" @click="reviewRecovery(recovery.operation_id, 'rollback')">Review package rollback</button>
        </template>
      </div>
      <button v-else-if="report?.kind === 'apply' && report.result && Object.keys(report.result).length" type="button" :disabled="phpUpdates.busy.value" class="mt-4 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50" @click="verifyUpdate">Retry service verification</button>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onActivated, watch } from 'vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import { apiClient } from '../api/client';
import { createRequestID } from '../utils/requestID';
import Card from '../components/Card.vue';
import DataTable from '../components/DataTable.vue';
import StatusBadge from '../components/StatusBadge.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';
import TableActionMenu from '../components/TableActionMenu.vue';
import BaseModal from '../components/BaseModal.vue';
import { phpUpdateError, usePHPUpdates, type PHPUpdatePlan, type PHPUpdateOperation, type PHPRecoveryPlan, type PHPRepositoryReview } from '../composables/usePHPUpdates';

const phpUpdates = usePHPUpdates();
const repositoryStatus = ref<{state: string; message: string; can_review: boolean; resume_plan_id?: string; review?: PHPRepositoryReview} | null>(null);
const repositoryReview = ref<PHPRepositoryReview | null>(null);
const reviewingRepository = ref(false);
const switchingRepository = ref(false);
let repositoryRequestID = '';
async function fetchRepository() {
 try { repositoryStatus.value = await apiClient.get('/api/v1/server/php/updates/repository', { useCache: false }); }
 catch { repositoryStatus.value = null; }
}
async function reviewRepository() {
 reviewingRepository.value = true;
 try {
  repositoryReview.value = repositoryStatus.value?.resume_plan_id && repositoryStatus.value.review ? repositoryStatus.value.review : await phpUpdates.reviewRepository();
  repositoryRequestID = createRequestID();
 } catch (error) { showToast({ type: 'error', title: 'Couldn’t review PHP repository', description: phpUpdateError(error) }); }
 finally { reviewingRepository.value = false; await fetchRepository(); }
}
async function switchRepository() {
 if (!repositoryReview.value) return;
 switchingRepository.value = true;
 try { await phpUpdates.applyRepository(repositoryReview.value.id, repositoryRequestID); repositoryReview.value = null; }
 catch (error) { showToast({ type: 'error', title: 'Couldn’t start PHP repository setup', description: phpUpdateError(error) }); }
 finally { switchingRepository.value = false; await fetchRepository(); }
}
onMounted(fetchRepository);
watch(() => phpUpdates.state.value.active_operation, (current, previous) => { if (!current && previous?.kind.startsWith('repository_')) void fetchRepository(); });
const updatePlan = ref<PHPUpdatePlan | null>(null);
const report = ref<PHPUpdateOperation | null>(null);
const recoveryPlan = ref<PHPRecoveryPlan | null>(null);
const startingRecovery = ref(false);
let recoveryRequestID = '';
async function reviewRecovery(operationID: string, kind: 'repair' | 'rollback') {
  try { recoveryPlan.value = await phpUpdates.reviewRecovery(operationID, kind); recoveryRequestID = createRequestID(); report.value = null; }
  catch (error) { showToast({ type: 'error', title: 'Recovery needs attention', description: phpUpdateError(error) }); }
}
async function applyRecovery() {
  if (!recoveryPlan.value) return;
  startingRecovery.value = true;
  try { await phpUpdates.recover(recoveryPlan.value, recoveryRequestID); recoveryPlan.value = null; }
  catch (error) { showToast({ type: 'error', title: 'Recovery could not be started', description: phpUpdateError(error) }); }
  finally { startingRecovery.value = false; }
}
const startingUpdate = ref(false);
const reviewingUpdate = ref(false);
const acknowledgeNoRollback = ref(false);
function servicesForImpact(impact?: PHPUpdatePlan['impact']) {
 const services = new Set<string>(['PHP-FPM']);
 for (const worker of impact?.workers || []) {
  const labels: Record<string, string> = { laravel_queue: 'Queue workers', laravel_horizon: 'Queue workers', laravel_octane: 'Octane', laravel_nightwatch: 'Nightwatch' };
  services.add(labels[worker.kind || ''] || worker.name);
 }
 if (impact?.schedulers?.length) services.add('Scheduler');
 return [...services];
}
const affectedServices = computed(() => servicesForImpact(updatePlan.value?.impact));
const recoveryAffectedServices = computed(() => servicesForImpact(recoveryPlan.value?.impact));
let updateRequestID = '';
function versionMenu(item: PhpVersion) {
  const branch = phpUpdates.state.value.branches.find(b => b.version === item.version);
  const recovery = phpUpdates.state.value.recoveries.find(r => r.version === item.version);
  return [
    { id: 'update', label: 'Update', disabled: !branch?.update_available || phpUpdates.locked.value || reviewingUpdate.value },
    ...(recovery?.can_repair ? [{ id: 'repair', label: 'Repair', disabled: phpUpdates.busy.value }] : []),
    ...(recovery?.can_rollback ? [{ id: 'rollback', label: 'Roll back PHP packages', disabled: phpUpdates.busy.value }] : []),
    { id: 'check', label: 'Check for updates', disabled: phpUpdates.busy.value },
    { id: 'toggle', label: item.status === 'running' ? 'Stop FPM' : 'Start FPM', disabled: phpUpdates.locked.value },
    { id: 'remove', label: 'Remove', variant: 'danger' as const, disabled: phpUpdates.locked.value },
  ];
}
async function versionAction(action: string, item: PhpVersion) {
  try {
    if (action === 'repair' || action === 'rollback') {
      const recovery = phpUpdates.state.value.recoveries.find(r => r.version === item.version);
      if (recovery) await reviewRecovery(recovery.operation_id, action);
      return;
    }
    if (action === 'check') { await phpUpdates.check(item.version); return; }
    if (action === 'update') {
      const branch = phpUpdates.state.value.branches.find(b => b.version === item.version);
      if (!branch?.update_available || !branch.plan) return;
      updateRequestID = createRequestID();
      acknowledgeNoRollback.value = false;
      reviewingUpdate.value = true;
      const toast = showToast({ type: 'loading', title: `Reviewing PHP ${item.version} installation`, description: 'Checking affected services. Nothing will be stopped or installed.' });
      try {
        updatePlan.value = await apiClient.post(`/api/v1/server/php/updates/plans/${branch.plan.id}/review`, {});
        removeToast(toast);
      } catch (error) {
        updateToast(toast, { type: 'error', title: 'Couldn’t review PHP installation', description: phpUpdateError(error) });
      } finally { reviewingUpdate.value = false; }
      return;
    }
    if (action === 'toggle') { if (item.status === 'running') await stopFPM(item.version); else await startFPM(item.version); }
    if (action === 'remove') await removeVersion(item.version);
  } catch (error) { showToast({ type: 'error', title: 'PHP action could not be completed', description: phpUpdateError(error) }); }
}
async function applyUpdate() {
  if (!updatePlan.value?.reviewed || updatePlan.value.impact.blocked_reasons?.length || (!updatePlan.value.rollback?.available && !acknowledgeNoRollback.value)) return;
  startingUpdate.value = true;
  try { await phpUpdates.apply(updatePlan.value, updateRequestID, acknowledgeNoRollback.value); updatePlan.value = null; }
  catch (error) { showToast({ type: 'error', title: 'PHP update could not be started', description: phpUpdateError(error) }); }
  finally { startingUpdate.value = false; }
}
async function verifyUpdate() {
  if (!report.value) return;
  try { await phpUpdates.verify(report.value); report.value = null; }
  catch (error) { showToast({ type: 'error', title: 'Couldn’t start service verification', description: phpUpdateError(error) }); }
}

const versionColumns = [
  { key: 'version', label: 'Version' },
  { key: 'status', label: 'Status' },
  { key: 'site_default', label: 'Site default' },
  { key: 'cli_default', label: 'CLI default' },
];

const { addToast, showToast, updateToast, removeToast } = useToast();
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
  full_version?: string;
  installed: boolean;
  status: string;
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
watch(() => phpUpdates.state.value.active_operation, (current, previous) => {
  if (!current && previous?.kind !== 'check') { void fetchAvailableVersions(); }
});

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
  const toastId = showToast({
    title: 'Saving PHP settings',
    description: 'This may take a moment.',
    type: 'loading',
  });
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
    updateToast(toastId, {
      title: 'PHP settings saved',
      description: null,
      type: 'success',
    });
  } catch (e: any) {
    updateToast(toastId, {
      title: 'PHP settings could not be saved',
      description: e.message || 'Please try again.',
      type: 'error',
    });
  } finally {
    saving.value = false;
  }
};

const installVersionAction = async () => {
  installing.value = true;
  const version = installVersion.value;
  const toastId = showToast({
    title: `Installing PHP ${version}`,
    description: 'This may take several minutes.',
    type: 'loading',
  });
  try {
    await apiClient.post('/api/v1/server/php/versions/install', { version });
    apiClient.invalidate('/api/v1/server/php');
    updateToast(toastId, {
      title: `PHP ${version} installed`,
      description: 'The new PHP version is ready to use.',
      type: 'success',
    });
    fetchInstalledVersions();
    fetchAvailableVersions();
  } catch (e: any) {
    updateToast(toastId, {
      title: `PHP ${version} installation failed`,
      description: e.message || 'The PHP version could not be installed.',
      type: 'error',
    });
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
  const toastId = showToast({
    title: `Removing PHP ${version}`,
    description: 'This may take a moment.',
    type: 'loading',
  });
  try {
    await apiClient.post('/api/v1/server/php/versions/remove', { version });
    apiClient.invalidate('/api/v1/server/php');
    updateToast(toastId, {
      title: `PHP ${version} removed`,
      description: null,
      type: 'success',
    });
    fetchInstalledVersions();
    fetchAvailableVersions();
  } catch (e: any) {
    updateToast(toastId, {
      title: `PHP ${version} could not be removed`,
      description: e.message || 'Please try again.',
      type: 'error',
    });
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
    phpUpdates.refresh(),
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
    phpUpdates.refresh(),
  ]);
  await fetchSettings();
});
</script>
