<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" />
    <template v-else>
      <Card>
        <div class="flex flex-col sm:flex-row sm:items-start justify-between gap-4 mb-4">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Backup Destinations</h2>
            <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Connect private S3 or Cloudflare R2 buckets and reuse them across sites.</p>
          </div>
          <AppButton size="sm" @click="openDestinationModal()">Add Destination</AppButton>
        </div>

        <DataTable :columns="destinationColumns" :items="destinations" empty-text="No backup destinations connected." aria-label="Backup destinations">
          <template #name="{ item }">
            <div class="flex items-center gap-2">
              <span class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</span>
              <StatusBadge v-if="item.is_default" label="Default" variant="blue" />
            </div>
          </template>
          <template #provider="{ item }"><span class="uppercase text-xs font-semibold text-gray-500 dark:text-gray-400">{{ item.provider }}</span></template>
          <template #location="{ item }">
            <div class="max-w-64">
              <p class="font-mono text-xs text-gray-700 truncate dark:text-gray-300" :title="item.bucket">{{ item.bucket }}</p>
              <p class="font-mono text-[11px] text-gray-400 truncate" :title="item.prefix">/{{ item.prefix }}</p>
            </div>
          </template>
          <template #auth="{ item }"><span class="text-gray-500 dark:text-gray-400">{{ item.use_instance_role ? 'AWS credential chain' : 'Access key' }}</span></template>
          <template #actions="{ item }">
            <div class="relative inline-block">
              <button type="button" class="table-menu-button" :class="isPending('destination', item.id) && 'animate-pulse'" :disabled="isPending('destination', item.id)" aria-label="Destination actions" @click="toggleDestinationMenu(item.id)">{{ isPending('destination', item.id) ? '…' : '•••' }}</button>
              <div v-if="openDestinationMenu === item.id" class="table-menu w-48">
                <button class="table-menu-item" @click="testDestination(item); openDestinationMenu = null">Test connection</button>
                <button class="table-menu-item" @click="openDestinationModal(item); openDestinationMenu = null">Rotate credentials</button>
                <button class="table-menu-item-danger" @click="deleteDestination(item); openDestinationMenu = null">Remove destination</button>
              </div>
            </div>
          </template>
        </DataTable>
      </Card>

      <Card>
        <div class="flex flex-col sm:flex-row sm:items-start justify-between gap-4 mb-4">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Backup Plans</h2>
            <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Choose each site's files and databases, schedule, and retention policy.</p>
            <p class="text-xs text-gray-400 mt-1">Times use {{ timezone || 'the server timezone' }}.</p>
          </div>
          <AppButton size="sm" :disabled="destinations.length === 0" @click="openPlanModal()">Create Plan</AppButton>
        </div>

        <DataTable :columns="planColumns" :items="plans" empty-text="No backup plans configured." aria-label="Backup plans">
          <template #name="{ item }">
            <div>
              <p class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</p>
              <p class="text-xs text-gray-400">{{ item.site_domain }}</p>
            </div>
          </template>
          <template #scope="{ item }"><span class="text-gray-600 dark:text-gray-400">{{ scopeLabel(item) }}</span></template>
          <template #schedule="{ item }">
            <div>
              <p class="text-gray-700 dark:text-gray-300">{{ scheduleLabel(item.schedule) }}</p>
              <p v-if="item.next_run_at && item.enabled" class="text-xs text-gray-400">Next {{ formatDate(item.next_run_at) }}</p>
            </div>
          </template>
          <template #retention="{ item }"><span class="capitalize text-gray-600 dark:text-gray-400">{{ item.retention_profile }}</span></template>
          <template #status="{ item }"><StatusBadge :label="item.enabled ? 'Enabled' : 'Paused'" :variant="item.enabled ? 'green' : 'gray'" /></template>
          <template #actions="{ item }">
            <div class="relative inline-block">
              <button type="button" class="table-menu-button" :class="isPending('plan', item.id) && 'animate-pulse'" :disabled="isPending('plan', item.id)" aria-label="Backup plan actions" @click="togglePlanMenu(item.id)">{{ isPending('plan', item.id) ? '…' : '•••' }}</button>
              <div v-if="openPlanMenu === item.id" class="table-menu w-44">
                <button class="table-menu-item text-blue-600 dark:text-blue-400" @click="runPlan(item); openPlanMenu = null">Back up now</button>
                <button class="table-menu-item" @click="openPlanModal(item); openPlanMenu = null">Edit plan</button>
                <button class="table-menu-item-danger" @click="deletePlan(item); openPlanMenu = null">Delete plan</button>
              </div>
            </div>
          </template>
        </DataTable>
      </Card>

      <Card>
        <div class="flex flex-col sm:flex-row sm:items-start justify-between gap-4 mb-4">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Backup History</h2>
            <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Completed backups use unique object paths and are never overwritten.</p>
          </div>
          <AppButton variant="secondary" size="sm" :loading="refreshing" @click="refreshRuns">Refresh</AppButton>
        </div>

        <DataTable :columns="runColumns" :items="runs" empty-text="No backups have run yet." aria-label="Backup history">
          <template #site="{ item }">
            <div class="max-w-64">
              <p class="font-medium text-gray-900 dark:text-gray-100">{{ item.site_domain }}</p>
              <p class="text-xs text-gray-400">{{ item.plan_name }}</p>
              <p v-if="item.status === 'failed'" class="mt-1 truncate text-xs text-red-600 dark:text-red-400" :title="item.error || 'Backup failed'">
                {{ shortError(item.error) }}
              </p>
            </div>
          </template>
          <template #status="{ item }"><StatusBadge :label="statusLabel(item.status)" :variant="statusVariant(item.status)" /></template>
          <template #contents="{ item }"><span class="text-gray-600 dark:text-gray-400">{{ artifactLabel(item.artifacts) }}</span></template>
          <template #size="{ item }"><span class="text-gray-500 dark:text-gray-400">{{ formatBytes(item.total_size_bytes) }}</span></template>
          <template #created_at="{ item }">
            <div>
              <p class="text-gray-600 dark:text-gray-400">{{ formatDate(item.created_at) }}</p>
              <p class="text-xs text-gray-400 capitalize">{{ item.trigger }}</p>
            </div>
          </template>
          <template #actions="{ item }">
            <div v-if="item.status === 'failed'" class="relative inline-block">
              <button type="button" class="table-menu-button" :class="isPending('run', item.id) && 'animate-pulse'" :disabled="isPending('run', item.id)" aria-label="Failed backup actions" @click="toggleRunMenu(item.id)">{{ isPending('run', item.id) ? '…' : '•••' }}</button>
              <div v-if="openRunMenu === item.id" class="table-menu w-40">
                <button class="table-menu-item-danger" @click="deleteRun(item); openRunMenu = null">Delete record</button>
              </div>
            </div>
            <div v-else-if="item.status === 'completed'" class="relative inline-block">
              <button type="button" class="table-menu-button" :class="isPending('run', item.id) && 'animate-pulse'" :disabled="isPending('run', item.id)" aria-label="Backup actions" @click="toggleRunMenu(item.id)">{{ isPending('run', item.id) ? '…' : '•••' }}</button>
              <div v-if="openRunMenu === item.id" class="table-menu w-52">
                <button v-for="artifact in item.artifacts" :key="artifact.id" class="table-menu-item"
                  @click="downloadArtifact(item, artifact); openRunMenu = null">
                  Download {{ artifact.kind === 'files' ? 'site files' : artifact.database_name }}
                </button>
                <button class="table-menu-item-danger" @click="deleteRun(item); openRunMenu = null">Delete backup</button>
              </div>
            </div>
            <span v-else class="text-xs text-gray-400">Running…</span>
          </template>
        </DataTable>
      </Card>
    </template>

    <BaseModal v-model="showDestinationModal" :title="editingDestinationId ? 'Rotate Destination Credentials' : 'Add Backup Destination'" :confirm-text="editingDestinationId ? 'Test & Save' : 'Connect & Test'" :loading="savingDestination" @submit="destinationFormElement?.requestSubmit()">
      <form ref="destinationFormElement" class="space-y-4" @submit.prevent="saveDestination">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Provider</label>
          <select v-model="destinationForm.provider" :disabled="!!editingDestinationId" class="form-input" @change="onDestinationProviderChanged">
            <option value="r2">Cloudflare R2</option>
            <option value="s3">Amazon S3</option>
          </select>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Name</label>
          <input v-model.trim="destinationForm.name" required maxlength="80" class="form-input" placeholder="Production backups" />
        </div>
        <div class="grid sm:grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Bucket</label>
            <input v-model.trim="destinationForm.bucket" required :disabled="!!editingDestinationId" class="form-input font-mono" placeholder="fluxo-backups" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Prefix</label>
            <input v-model.trim="destinationForm.prefix" :disabled="!!editingDestinationId" class="form-input font-mono" placeholder="fluxo" />
          </div>
        </div>
        <div v-if="destinationForm.provider === 'r2'" class="grid sm:grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Cloudflare Account ID</label>
            <input v-model.trim="destinationForm.account_id" required maxlength="32" :disabled="!!editingDestinationId" class="form-input font-mono" placeholder="32-character account ID" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Jurisdiction</label>
            <select v-model="destinationForm.jurisdiction" :disabled="!!editingDestinationId" class="form-input">
              <option value="default">Default</option>
              <option value="eu">European Union</option>
              <option value="fedramp">FedRAMP</option>
            </select>
          </div>
        </div>
        <template v-else>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">AWS Region</label>
            <input v-model.trim="destinationForm.region" required :disabled="!!editingDestinationId" class="form-input font-mono" placeholder="us-east-1" />
          </div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-gray-700">
            <ToggleSwitch v-model="destinationForm.use_instance_role" label="Use AWS credential chain"
              description="Use environment, shared configuration, container, or instance credentials instead of storing access keys." />
          </div>
        </template>
        <div class="p-4 bg-blue-50 border border-blue-100 rounded-lg dark:bg-blue-950/20 dark:border-blue-900/50">
          <a
            v-if="destinationForm.provider === 'r2'"
            href="https://dash.cloudflare.com/?to=%2F%3Aaccount%2Fr2%2Foverview"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-xs font-bold text-blue-600 underline transition-colors hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
          >
            Open Cloudflare R2 to create an API token ↗
          </a>
          <a
            v-else
            href="https://console.aws.amazon.com/iam/"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-xs font-bold text-blue-600 underline transition-colors hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
          >
            {{ destinationForm.use_instance_role ? 'Open AWS IAM to manage server credentials' : 'Open AWS IAM to create an access key' }} ↗
          </a>
          <p v-if="destinationForm.provider === 'r2'" class="mt-1 text-xs text-blue-700 dark:text-blue-300">
            Choose Object Read & Write and restrict the token to this backup bucket.
          </p>
          <p v-else-if="destinationForm.use_instance_role" class="mt-1 text-xs text-blue-700 dark:text-blue-300">
            Fluxo will use AWS environment, shared configuration, container, or instance credentials. Grant only this bucket and prefix.
          </p>
          <p v-else class="mt-1 text-xs text-blue-700 dark:text-blue-300">
            Create a dedicated IAM user limited to this bucket. Never use AWS root access keys.
          </p>
        </div>
        <div v-if="destinationForm.provider === 'r2' || !destinationForm.use_instance_role" class="grid sm:grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Access Key ID</label>
            <input v-model.trim="destinationForm.access_key" required autocomplete="off" class="form-input font-mono" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Secret Access Key</label>
            <input v-model="destinationForm.secret_key" required type="password" autocomplete="new-password" class="form-input font-mono" />
          </div>
        </div>
        <div class="rounded-lg border border-gray-200 p-3 dark:border-gray-700">
          <ToggleSwitch v-model="destinationForm.is_default" label="Default destination"
            description="Preselect this destination when creating new backup plans." />
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">Use dedicated, least-privilege credentials limited to this bucket and prefix. The connection test writes, reads, and deletes a temporary object. Stored credentials are encrypted and never returned by the API. Rotating credentials does not move existing backups.</p>
      </form>
    </BaseModal>

    <BaseModal v-model="showPlanModal" :title="editingPlanId ? 'Edit Backup Plan' : 'Create Backup Plan'" :confirm-text="editingPlanId ? 'Save Plan' : 'Create Plan'" :loading="savingPlan" max-width="max-w-2xl" @submit="planFormElement?.requestSubmit()">
      <form ref="planFormElement" class="space-y-4" @submit.prevent="savePlan">
        <div class="grid sm:grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Plan Name</label>
            <input v-model.trim="planForm.name" required maxlength="80" class="form-input" placeholder="Daily production backup" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Destination</label>
            <select v-model.number="planForm.destination_id" required class="form-input">
              <option v-for="destination in destinations" :key="destination.id" :value="destination.id">{{ destination.name }}</option>
            </select>
          </div>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Site</label>
          <select v-model.number="planForm.site_id" required class="form-input" @change="onPlanSiteChanged">
            <option :value="0" disabled>Select a site</option>
            <option v-for="site in sites" :key="site.id" :value="site.id">{{ site.domain }}</option>
          </select>
        </div>
        <div class="overflow-hidden rounded-xl border border-gray-200 dark:border-gray-700">
          <div class="border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800/60">
            <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">Backup contents</p>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">Select everything that should be included in this recovery point.</p>
          </div>
          <div class="space-y-4 p-4">
            <ToggleSwitch v-model="planForm.include_files" label="Site files"
              description="Includes configuration and persistent files; skips Git data, Node modules, caches, logs, and old releases." />
            <div class="border-t border-gray-100 pt-4 dark:border-gray-800">
              <p class="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Databases</p>
              <div v-if="siteDatabases.length" class="grid gap-3 sm:grid-cols-2">
                <div v-for="item in siteDatabases" :key="item.id" class="rounded-lg border border-gray-200 p-3 dark:border-gray-700">
                  <ToggleSwitch :model-value="planForm.database_ids.includes(item.id)" :label="item.name"
                    :description="item.engine.toUpperCase()" @update:model-value="toggleDatabaseSelection(item.id, $event)" />
                </div>
              </div>
              <p v-else class="text-xs text-gray-500">No databases are linked to this site.</p>
            </div>
          </div>
        </div>
        <div class="grid sm:grid-cols-3 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Schedule</label>
            <select v-model="planForm.schedule" class="form-input" @change="onPlanScheduleChanged">
              <option value="every_6_hours">Every 6 hours</option>
              <option value="every_12_hours">Every 12 hours</option>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
              <option value="manual">Manual only</option>
            </select>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Start Hour</label>
            <select v-model.number="planForm.backup_hour" :disabled="planForm.schedule === 'manual'" class="form-input">
              <option v-for="hour in 24" :key="hour - 1" :value="hour - 1">{{ String(hour - 1).padStart(2, '0') }}:00</option>
            </select>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Retention</label>
            <select v-model="planForm.retention_profile" class="form-input">
              <option value="minimal">Minimal</option>
              <option value="recommended">Recommended</option>
              <option value="extended">Extended</option>
            </select>
          </div>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ retentionDescription(planForm.retention_profile) }}</p>
        <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-gray-700 dark:bg-gray-800/40">
          <ToggleSwitch v-model="planForm.enabled" label="Automatic backups"
            :description="planForm.schedule === 'manual' ? 'Manual-only plans run when you choose Back up now.' : 'Run this plan automatically on the selected schedule.'"
            :disabled="planForm.schedule === 'manual'" />
        </div>
      </form>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, ref } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AppButton from '../components/AppButton.vue';
import BaseModal from '../components/BaseModal.vue';
import Card from '../components/Card.vue';
import DataTable from '../components/DataTable.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';

const destinationColumns = [
  { key: 'name', label: 'Name' }, { key: 'provider', label: 'Provider' },
  { key: 'location', label: 'Bucket / Prefix' }, { key: 'auth', label: 'Authentication' },
];
const planColumns = [
  { key: 'name', label: 'Plan' }, { key: 'scope', label: 'Contents' },
  { key: 'schedule', label: 'Schedule' }, { key: 'retention', label: 'Retention' }, { key: 'status', label: 'Status' },
];
const runColumns = [
  { key: 'site', label: 'Site' }, { key: 'status', label: 'Status' }, { key: 'contents', label: 'Contents' },
  { key: 'size', label: 'Size' }, { key: 'created_at', label: 'Started' },
];

const { confirm } = useConfirm();
const { addToast } = useToast();
const destinations = ref<any[]>([]);
const plans = ref<any[]>([]);
const runs = ref<any[]>([]);
const sites = ref<any[]>([]);
const databases = ref<any[]>([]);
const timezone = ref('');
const loading = ref(true);
const refreshing = ref(false);
const showDestinationModal = ref(false);
const showPlanModal = ref(false);
const savingDestination = ref(false);
const savingPlan = ref(false);
const openDestinationMenu = ref<number | null>(null);
const openPlanMenu = ref<number | null>(null);
const openRunMenu = ref<string | null>(null);
const pendingAction = ref('');
const destinationFormElement = ref<HTMLFormElement | null>(null);
const planFormElement = ref<HTMLFormElement | null>(null);
const editingDestinationId = ref<number | null>(null);
const editingPlanId = ref<number | null>(null);
let pollTimer: ReturnType<typeof setInterval> | null = null;

const emptyDestinationForm = () => ({ provider: 'r2', name: '', bucket: '', region: '', account_id: '', jurisdiction: 'default', prefix: 'fluxo', access_key: '', secret_key: '', use_instance_role: false, is_default: false });
const emptyPlanForm = () => ({ name: '', site_id: 0, destination_id: destinations.value.find((item: any) => item.is_default)?.id || destinations.value[0]?.id || 0, include_files: true, database_ids: [] as number[], schedule: 'daily', backup_hour: 2, retention_profile: 'recommended', enabled: true });
const destinationForm = ref(emptyDestinationForm());
const planForm = ref(emptyPlanForm());
const siteDatabases = computed(() => databases.value.filter((item: any) => item.site_id === planForm.value.site_id));

async function fetchData() {
  try {
    const [destinationData, planData, runData, siteData, databaseData] = await Promise.all([
      apiClient.get('/api/v1/backups/destinations', { bypassCache: true, useCache: false }),
      apiClient.get('/api/v1/backups/plans', { bypassCache: true, useCache: false }),
      apiClient.get('/api/v1/backups/runs?limit=100', { bypassCache: true, useCache: false }),
      apiClient.getSites(true), apiClient.getDatabases(true),
    ]);
    destinations.value = destinationData || [];
    plans.value = planData?.plans || [];
    timezone.value = planData?.timezone || '';
    runs.value = runData || [];
    sites.value = siteData || [];
    databases.value = databaseData || [];
  } catch (error: any) {
    addToast(error.message || 'Failed to load backups', 'error');
  } finally {
    loading.value = false;
  }
}

async function refreshRuns() {
  refreshing.value = true;
  try {
    runs.value = await apiClient.get('/api/v1/backups/runs?limit=100', { bypassCache: true, useCache: false }) || [];
    if (!runs.value.some((run: any) => run.status === 'queued' || run.status === 'running')) stopPolling();
  } catch { /* preserve current history */ }
  finally { refreshing.value = false; }
}

function startPolling() {
  if (!pollTimer) pollTimer = setInterval(refreshRuns, 5000);
}
function stopPolling() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = null;
}

function openDestinationModal(destination?: any) {
  editingDestinationId.value = destination?.id || null;
  destinationForm.value = destination ? {
    provider: destination.provider, name: destination.name, bucket: destination.bucket,
    region: destination.region || '', account_id: destination.account_id || '',
    jurisdiction: destination.jurisdiction || 'default', prefix: destination.prefix,
    access_key: '', secret_key: '', use_instance_role: destination.use_instance_role,
    is_default: destination.is_default,
  } : emptyDestinationForm();
  if (!destination) destinationForm.value.is_default = destinations.value.length === 0;
  showDestinationModal.value = true;
}

function onDestinationProviderChanged() {
  destinationForm.value.access_key = '';
  destinationForm.value.secret_key = '';
  destinationForm.value.use_instance_role = false;
  if (destinationForm.value.provider === 'r2') {
    destinationForm.value.region = '';
    destinationForm.value.jurisdiction = 'default';
  } else {
    destinationForm.value.account_id = '';
    destinationForm.value.jurisdiction = 'default';
    destinationForm.value.region = 'us-east-1';
  }
}

async function saveDestination() {
  savingDestination.value = true;
  try {
    if (destinationForm.value.provider === 'r2') destinationForm.value.use_instance_role = false;
    if (editingDestinationId.value) await apiClient.put(`/api/v1/backups/destinations/${editingDestinationId.value}`, destinationForm.value);
    else await apiClient.post('/api/v1/backups/destinations', destinationForm.value);
    addToast(editingDestinationId.value ? 'Destination credentials rotated' : 'Backup destination connected', 'success');
    showDestinationModal.value = false;
    await fetchData();
  } catch (error: any) { addToast(error.message || 'Failed to connect destination', 'error'); }
  finally { savingDestination.value = false; }
}

async function testDestination(item: any) {
  pendingAction.value = `destination-${item.id}`;
  try {
    await apiClient.post(`/api/v1/backups/destinations/${item.id}/test`);
    addToast('Destination connection is healthy', 'success');
  } catch (error: any) { addToast(error.message || 'Destination test failed', 'error'); }
  finally { pendingAction.value = ''; }
}

async function deleteDestination(item: any) {
  const ok = await confirm({ title: 'Remove backup destination', message: `Remove ${item.name}? Destinations referenced by plans or backup history cannot be removed.`, confirmText: 'Remove', variant: 'danger' });
  if (!ok) return;
  pendingAction.value = `destination-${item.id}`;
  try {
    await apiClient.delete(`/api/v1/backups/destinations/${item.id}`);
    addToast('Backup destination removed', 'success');
    await fetchData();
  } catch (error: any) { addToast(error.message || 'Failed to remove destination', 'error'); }
  finally { pendingAction.value = ''; }
}

function openPlanModal(plan?: any) {
  editingPlanId.value = plan?.id || null;
  planForm.value = plan ? {
    name: plan.name, site_id: plan.site_id, destination_id: plan.destination_id,
    include_files: plan.include_files, database_ids: [...(plan.database_ids || [])],
    schedule: plan.schedule, backup_hour: plan.backup_hour,
    retention_profile: plan.retention_profile, enabled: plan.schedule === 'manual' ? false : plan.enabled,
  } : emptyPlanForm();
  showPlanModal.value = true;
}

function onPlanScheduleChanged() {
  if (planForm.value.schedule === 'manual') planForm.value.enabled = false;
}

function toggleDatabaseSelection(databaseId: number, selected: boolean) {
  planForm.value.database_ids = selected
    ? [...new Set([...planForm.value.database_ids, databaseId])]
    : planForm.value.database_ids.filter((id: number) => id !== databaseId);
}

const toggleDestinationMenu = (id: number) => {
  openDestinationMenu.value = openDestinationMenu.value === id ? null : id;
};

const togglePlanMenu = (id: number) => {
  openPlanMenu.value = openPlanMenu.value === id ? null : id;
};

const toggleRunMenu = (id: string) => {
  openRunMenu.value = openRunMenu.value === id ? null : id;
};

function onPlanSiteChanged() {
  planForm.value.database_ids = siteDatabases.value.map((item: any) => item.id);
  if (!planForm.value.name) {
    const site = sites.value.find((item: any) => item.id === planForm.value.site_id);
    if (site) planForm.value.name = `${site.domain} backup`;
  }
}

async function savePlan() {
  if (!planForm.value.include_files && planForm.value.database_ids.length === 0) {
    addToast('Select site files, at least one database, or both', 'error');
    return;
  }
  savingPlan.value = true;
  try {
    if (editingPlanId.value) await apiClient.put(`/api/v1/backups/plans/${editingPlanId.value}`, planForm.value);
    else await apiClient.post('/api/v1/backups/plans', planForm.value);
    addToast(editingPlanId.value ? 'Backup plan updated' : 'Backup plan created', 'success');
    showPlanModal.value = false;
    await fetchData();
  } catch (error: any) { addToast(error.message || 'Failed to save backup plan', 'error'); }
  finally { savingPlan.value = false; }
}

async function runPlan(item: any) {
  pendingAction.value = `plan-${item.id}`;
  try {
    await apiClient.post(`/api/v1/backups/plans/${item.id}/run`);
    addToast('Backup queued', 'success');
    await refreshRuns();
    startPolling();
  } catch (error: any) { addToast(error.message || 'Failed to queue backup', 'error'); }
  finally { pendingAction.value = ''; }
}

async function deletePlan(item: any) {
  const ok = await confirm({ title: 'Delete backup plan', message: `Delete ${item.name}? Existing backup history and remote objects will be preserved.`, confirmText: 'Delete', variant: 'danger' });
  if (!ok) return;
  pendingAction.value = `plan-${item.id}`;
  try {
    await apiClient.delete(`/api/v1/backups/plans/${item.id}`);
    addToast('Backup plan deleted', 'success');
    await fetchData();
  } catch (error: any) { addToast(error.message || 'Failed to delete backup plan', 'error'); }
  finally { pendingAction.value = ''; }
}

async function downloadArtifact(run: any, artifact: any) {
  const popup = window.open('', '_blank');
  if (popup) popup.opener = null;
  pendingAction.value = `run-${run.id}`;
  try {
    const result = await apiClient.post(`/api/v1/backups/runs/${run.id}/artifacts/${artifact.id}/download`);
    if (!result?.url) throw new Error('Download link was not returned');
    if (popup) popup.location.href = result.url;
    else window.location.href = result.url;
  } catch (error: any) {
    popup?.close();
    addToast(error.message || 'Failed to create download link', 'error');
  } finally {
    pendingAction.value = '';
  }
}

async function deleteRun(item: any) {
  const ok = await confirm({ title: 'Delete backup', message: `Permanently delete this backup for ${item.site_domain} from ${item.destination_name}?`, confirmText: 'Delete backup', variant: 'danger' });
  if (!ok) return;
  pendingAction.value = `run-${item.id}`;
  try {
    await apiClient.delete(`/api/v1/backups/runs/${item.id}`);
    addToast('Backup deleted', 'success');
    await refreshRuns();
  } catch (error: any) { addToast(error.message || 'Failed to delete backup', 'error'); }
  finally { pendingAction.value = ''; }
}

const scheduleLabel = (schedule: string) => ({ every_6_hours: 'Every 6 hours', every_12_hours: 'Every 12 hours', daily: 'Daily', weekly: 'Weekly', manual: 'Manual only' }[schedule] || schedule);
const scopeLabel = (plan: any) => plan.include_files && plan.database_ids?.length ? `Files + ${plan.database_ids.length} database${plan.database_ids.length === 1 ? '' : 's'}` : plan.include_files ? 'Site files' : `${plan.database_ids?.length || 0} database${plan.database_ids?.length === 1 ? '' : 's'}`;
const artifactLabel = (items: any[]) => !items?.length ? '—' : items.map(item => item.kind === 'files' ? 'Files' : item.database_name).join(', ');
const retentionDescription = (profile: string) => ({ minimal: 'Keeps the 7 most recent and 7 daily recovery points.', recommended: 'Keeps recent runs plus 14 daily, 8 weekly, and 6 monthly recovery points.', extended: 'Keeps recent runs plus 30 daily, 12 weekly, and 12 monthly recovery points.' }[profile] || '');
const statusLabel = (status: string) => status === 'completed' ? 'Completed' : status === 'running' ? 'Running' : status === 'queued' ? 'Queued' : 'Failed';
const statusVariant = (status: string): 'green' | 'red' | 'blue' | 'yellow' | 'gray' => status === 'completed' ? 'green' : status === 'failed' ? 'red' : status === 'running' ? 'blue' : 'yellow';
const formatDate = (value: string) => value ? new Date(value).toLocaleString() : '—';
const formatBytes = (value: number) => {
  if (!value) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / Math.pow(1024, index)).toFixed(index > 1 ? 1 : 0)} ${units[index]}`;
};
const shortError = (value: string) => !value ? 'Backup failed' : value.length > 70 ? value.slice(0, 67) + '…' : value;
const isPending = (kind: string, id: string | number) => pendingAction.value === `${kind}-${id}`;

onMounted(async () => { await fetchData(); if (runs.value.some((run: any) => ['queued', 'running'].includes(run.status))) startPolling(); });
onActivated(() => { if (runs.value.some((run: any) => ['queued', 'running'].includes(run.status))) startPolling(); });
onDeactivated(stopPolling);
onUnmounted(stopPolling);
</script>

<style scoped>
.form-input {
  width: 100%;
  border-radius: 0.5rem;
  border-width: 1px;
  border-color: var(--color-gray-300);
  background: white;
  padding: 0.5rem 0.75rem;
  font-size: 0.875rem;
  color: var(--color-gray-900);
}
.form-input:focus {
  border-color: var(--color-blue-500);
  outline: none;
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-blue-500) 25%, transparent);
}
.form-input:disabled { opacity: 0.5; }
:global(.dark) .form-input {
  border-color: var(--color-gray-600);
  background: var(--color-gray-800);
  color: var(--color-gray-100);
}
.table-menu-button {
  border-radius: 0.5rem;
  padding: 0.375rem 0.625rem;
  color: var(--color-gray-500);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  transition: color 150ms, background-color 150ms;
}
.table-menu-button:hover {
  background: var(--color-gray-100);
  color: var(--color-gray-800);
}
.table-menu-button:focus-visible {
  outline: 2px solid var(--color-blue-500);
  outline-offset: 2px;
}
.table-menu-button:disabled {
  cursor: wait;
  opacity: 0.6;
}
.table-menu {
  position: absolute;
  right: 0;
  top: 100%;
  z-index: 50;
  margin-top: 0.25rem;
  overflow: hidden;
  border: 1px solid var(--color-gray-200);
  border-radius: 0.625rem;
  background: white;
  padding: 0.25rem 0;
  box-shadow: 0 10px 25px rgb(15 23 42 / 18%);
}
.table-menu-item,
.table-menu-item-danger {
  display: block;
  width: 100%;
  padding: 0.5rem 0.875rem;
  background: transparent;
  text-align: left;
  font-size: 0.8125rem;
  transition: color 150ms, background-color 150ms;
}
.table-menu-item {
  color: var(--color-gray-700);
}
.table-menu-item:hover {
  background: var(--color-gray-50);
}
.table-menu-item-danger {
  color: var(--color-red-600);
}
.table-menu-item-danger:hover {
  background: var(--color-red-50);
}
:global(.dark) .table-menu-button:hover {
  background: var(--color-gray-800);
  color: var(--color-gray-200);
}
:global(.dark) .table-menu {
  border-color: var(--color-gray-700);
  background: var(--color-gray-900);
}
:global(.dark) .table-menu-item {
  color: var(--color-gray-300);
}
:global(.dark) .table-menu-item:hover {
  background: var(--color-gray-800);
}
:global(.dark) .table-menu-item-danger {
  color: var(--color-red-400);
}
:global(.dark) .table-menu-item-danger:hover {
  background: color-mix(in srgb, var(--color-red-900) 35%, transparent);
}
</style>
