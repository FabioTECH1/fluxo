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

        <div class="mb-3 flex flex-col gap-2 sm:flex-row">
          <select v-model="destinationSiteFilter" class="filter-input flex-1" aria-label="Filter destinations by site">
            <option value="all">All sites</option>
            <option v-for="site in sites" :key="site.id" :value="String(site.id)">{{ site.domain }}</option>
          </select>
          <select v-model="destinationProviderFilter" class="filter-input sm:w-48" aria-label="Filter destinations by provider">
            <option value="all">All providers</option>
            <option value="r2">Cloudflare R2</option>
            <option value="s3">Amazon S3</option>
          </select>
        </div>

        <DataTable :columns="destinationColumns" :items="paginatedDestinations" :empty-text="destinationEmptyText" aria-label="Backup destinations">
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
            <TableActionMenu :items="destinationMenuItems" aria-label="Destination actions"
              :loading="isPending('destination', item.id)" @select="handleDestinationAction($event, item)" />
          </template>
        </DataTable>
        <TablePagination v-model:page="destinationPage" :total-items="filteredDestinations.length" :page-size="pageSize" />
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

        <div class="mb-3 flex flex-col gap-2 sm:flex-row">
          <select v-model="planSiteFilter" class="filter-input flex-1" aria-label="Filter backup plans by site">
            <option value="all">All sites</option>
            <option v-for="site in sites" :key="site.id" :value="String(site.id)">{{ site.domain }}</option>
          </select>
          <select v-model="planStatusFilter" class="filter-input sm:w-48" aria-label="Filter backup plans by status">
            <option value="all">All statuses</option>
            <option value="enabled">Enabled</option>
            <option value="paused">Paused</option>
          </select>
        </div>

        <DataTable :columns="planColumns" :items="paginatedPlans" :empty-text="planEmptyText" aria-label="Backup plans">
          <template #name="{ item }">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <p class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</p>
                <StatusBadge v-if="item.encryption_enabled" label="Encrypted" variant="blue" />
              </div>
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
            <TableActionMenu :items="planMenuItems" aria-label="Backup plan actions"
              :loading="isPending('plan', item.id)" @select="handlePlanAction($event, item)" />
          </template>
        </DataTable>
        <TablePagination v-model:page="planPage" :total-items="filteredPlans.length" :page-size="pageSize" />
      </Card>

      <Card>
        <div class="flex flex-col sm:flex-row sm:items-start justify-between gap-4 mb-4">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Backup History</h2>
            <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Completed backups use unique object paths and are never overwritten. Failed records are kept for 30 days.</p>
          </div>
          <AppButton variant="secondary" size="sm" :loading="refreshing" @click="refreshRuns">Refresh</AppButton>
        </div>

        <div class="mb-3 flex flex-col gap-2 sm:flex-row">
          <select v-model="runSiteFilter" class="filter-input flex-1" aria-label="Filter backup history by site">
            <option value="all">All sites</option>
            <option v-for="site in sites" :key="site.id" :value="String(site.id)">{{ site.domain }}</option>
          </select>
          <select v-model="runStatusFilter" class="filter-input sm:w-48" aria-label="Filter backup history by status">
            <option value="all">All statuses</option>
            <option value="completed">Completed</option>
            <option value="failed">Failed</option>
            <option value="running">Running</option>
            <option value="queued">Queued</option>
          </select>
        </div>

        <DataTable :columns="runColumns" :items="paginatedRuns" :empty-text="runEmptyText" aria-label="Backup history">
          <template #site="{ item }">
            <div class="max-w-64">
              <div class="flex flex-wrap items-center gap-2">
                <p class="font-medium text-gray-900 dark:text-gray-100">{{ item.site_domain }}</p>
                <StatusBadge v-if="item.encrypted" label="Encrypted" variant="blue" />
              </div>
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
            <TableActionMenu v-if="item.status === 'failed' || item.status === 'completed'"
              :items="runMenuItems(item)" aria-label="Backup actions" :width="208"
              :loading="isPending('run', item.id)" @select="handleRunAction($event, item)" />
            <span v-else class="text-xs text-gray-400">Running…</span>
          </template>
        </DataTable>
        <TablePagination v-model:page="runPage" :total-items="filteredRuns.length" :page-size="pageSize" />
      </Card>
    </template>

    <BaseModal v-model="showDestinationModal" :title="editingDestinationId ? 'Edit Backup Destination' : 'Add Backup Destination'" :confirm-text="editingDestinationId ? 'Test & Save' : 'Connect & Test'" :loading="savingDestination" @submit="destinationFormElement?.requestSubmit()">
      <form ref="destinationFormElement" class="space-y-4" @submit.prevent="saveDestination">
        <fieldset :disabled="!!editingDestinationId">
          <legend class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">Provider</legend>
          <div class="grid grid-cols-1 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700 sm:grid-cols-2">
            <label class="flex cursor-pointer items-start gap-3 border-b border-gray-200 px-4 py-3 transition-colors dark:border-gray-700 sm:border-b-0 sm:border-r"
              :class="destinationForm.provider === 'r2' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
              <input v-model="destinationForm.provider" type="radio" value="r2" class="mt-0.5 text-blue-600 focus:ring-blue-500" @change="onDestinationProviderChanged" />
              <span>
                <span class="block text-sm font-semibold text-gray-900 dark:text-gray-100">Cloudflare R2</span>
                <span class="block text-xs text-gray-500 dark:text-gray-400">S3-compatible R2 bucket</span>
              </span>
            </label>
            <label class="flex cursor-pointer items-start gap-3 px-4 py-3 transition-colors"
              :class="destinationForm.provider === 's3' ? 'bg-blue-50 dark:bg-blue-900/30' : 'bg-white dark:bg-gray-900'">
              <input v-model="destinationForm.provider" type="radio" value="s3" class="mt-0.5 text-blue-600 focus:ring-blue-500" @change="onDestinationProviderChanged" />
              <span>
                <span class="block text-sm font-semibold text-gray-900 dark:text-gray-100">Amazon S3</span>
                <span class="block text-xs text-gray-500 dark:text-gray-400">AWS private bucket</span>
              </span>
            </label>
          </div>
        </fieldset>
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
            <input v-model.trim="destinationForm.prefix" class="form-input font-mono" placeholder="fluxo-backups" />
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
        <div v-if="destinationForm.provider === 'r2' || !destinationForm.use_instance_role" class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Access Key ID</label>
            <input v-model.trim="destinationForm.access_key" required autocomplete="off" class="form-input font-mono" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1 dark:text-gray-300">Secret Access Key</label>
            <div class="relative">
              <input v-model="destinationForm.secret_key" required :type="showSecretAccessKey ? 'text' : 'password'"
                autocomplete="new-password" class="form-input secret-input font-mono" />
              <button type="button" class="absolute inset-y-0 right-0 flex items-center pr-3 text-gray-400 transition-colors hover:text-gray-600 focus:outline-none dark:text-gray-500 dark:hover:text-gray-300"
                :aria-label="showSecretAccessKey ? 'Hide secret access key' : 'Show secret access key'"
                :title="showSecretAccessKey ? 'Hide secret access key' : 'Show secret access key'"
                @click="showSecretAccessKey = !showSecretAccessKey">
                <span v-if="!showSecretAccessKey" class="text-lg leading-none">&#128065;</span>
                <span v-else class="text-lg leading-none">&#128064;</span>
              </button>
            </div>
          </div>
        </div>
        <div class="rounded-lg border border-gray-200 p-3 dark:border-gray-700">
          <ToggleSwitch v-model="destinationForm.is_default" label="Default destination"
            description="Preselect this destination when creating new backup plans." />
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">Use dedicated, least-privilege credentials limited to this bucket and prefix. The connection test writes, reads, and deletes a temporary object. Stored credentials are encrypted and never returned by the API. Prefix changes affect future backups only; keep access to previous prefixes until their backups expire.</p>
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
        <div class="rounded-xl border border-gray-200 p-4 dark:border-gray-700">
          <ToggleSwitch
            v-model="planForm.encryption_enabled"
            label="Encrypt backup artifacts"
            description="Protect every file archive and database dump with a password using OpenPGP AES-256 encryption."
          />
          <div v-if="planForm.encryption_enabled" class="mt-4 border-t border-gray-100 pt-4 dark:border-gray-800">
            <label for="backup-encryption-password" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
              Encryption password
            </label>
            <PasswordInput
              id="backup-encryption-password"
              v-model="planForm.encryption_password"
              :required="!editingPlanId || !editingPlanEncryptionEnabled"
              :minlength="12"
              :maxlength="256"
              :placeholder="editingPlanEncryptionEnabled ? 'Leave blank to keep the current password' : 'At least 12 characters'"
            />
            <p class="mt-2 text-xs text-amber-700 dark:text-amber-300">
              Keep this password in an independent password manager. Downloaded encrypted backups cannot be recovered without it.
              <span v-if="editingPlanEncryptionEnabled">Leave it blank to keep the current password, or enter a new one for future runs.</span>
            </p>
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
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AppButton from '../components/AppButton.vue';
import BaseModal from '../components/BaseModal.vue';
import Card from '../components/Card.vue';
import DataTable from '../components/DataTable.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import StatusBadge from '../components/StatusBadge.vue';
import TableActionMenu from '../components/TableActionMenu.vue';
import TablePagination from '../components/TablePagination.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';
import PasswordInput from '../components/PasswordInput.vue';

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
const showSecretAccessKey = ref(false);
const pendingAction = ref('');
const pageSize = 5;
const destinationSiteFilter = ref('all');
const destinationProviderFilter = ref('all');
const destinationPage = ref(1);
const planSiteFilter = ref('all');
const planStatusFilter = ref('all');
const planPage = ref(1);
const runSiteFilter = ref('all');
const runStatusFilter = ref('all');
const runPage = ref(1);
const destinationFormElement = ref<HTMLFormElement | null>(null);
const planFormElement = ref<HTMLFormElement | null>(null);
const editingDestinationId = ref<number | null>(null);
const editingPlanId = ref<number | null>(null);
const editingPlanEncryptionEnabled = ref(false);
let pollTimer: ReturnType<typeof setInterval> | null = null;

const emptyDestinationForm = () => ({ provider: 'r2', name: '', bucket: '', region: '', account_id: '', jurisdiction: 'default', prefix: 'fluxo-backups', access_key: '', secret_key: '', use_instance_role: false, is_default: false });
const emptyPlanForm = () => ({ name: '', site_id: 0, destination_id: destinations.value.find((item: any) => item.is_default)?.id || destinations.value[0]?.id || 0, include_files: true, database_ids: [] as number[], schedule: 'daily', backup_hour: 2, retention_profile: 'recommended', enabled: true, encryption_enabled: false, encryption_password: '' });
const destinationForm = ref(emptyDestinationForm());
const planForm = ref(emptyPlanForm());
const siteDatabases = computed(() => databases.value.filter((item: any) => item.site_id === planForm.value.site_id));
const pageItems = <T,>(items: T[], page: number) => items.slice((page - 1) * pageSize, page * pageSize);
const filteredDestinations = computed(() => {
  return destinations.value.filter((item: any) => {
    const siteMatches = destinationSiteFilter.value === 'all' || plans.value.some((plan: any) =>
      plan.site_id === Number(destinationSiteFilter.value) && plan.destination_id === item.id,
    );
    const providerMatches = destinationProviderFilter.value === 'all' || item.provider === destinationProviderFilter.value;
    return siteMatches && providerMatches;
  });
});
const filteredPlans = computed(() => {
  return plans.value.filter((item: any) => {
    const siteMatches = planSiteFilter.value === 'all' || item.site_id === Number(planSiteFilter.value);
    const statusMatches = planStatusFilter.value === 'all' || (planStatusFilter.value === 'enabled' ? item.enabled : !item.enabled);
    return siteMatches && statusMatches;
  });
});
const filteredRuns = computed(() => {
  return runs.value.filter((item: any) => {
    const siteMatches = runSiteFilter.value === 'all' || item.site_id === Number(runSiteFilter.value);
    const statusMatches = runStatusFilter.value === 'all' || item.status === runStatusFilter.value;
    return siteMatches && statusMatches;
  });
});
const paginatedDestinations = computed(() => pageItems(filteredDestinations.value, destinationPage.value));
const paginatedPlans = computed(() => pageItems(filteredPlans.value, planPage.value));
const paginatedRuns = computed(() => pageItems(filteredRuns.value, runPage.value));
const destinationEmptyText = computed(() => destinationSiteFilter.value !== 'all' || destinationProviderFilter.value !== 'all' ? 'No destinations match these filters.' : 'No backup destinations connected.');
const planEmptyText = computed(() => planSiteFilter.value !== 'all' || planStatusFilter.value !== 'all' ? 'No plans match these filters.' : 'No backup plans configured.');
const runEmptyText = computed(() => runSiteFilter.value !== 'all' || runStatusFilter.value !== 'all' ? 'No backup runs match these filters.' : 'No backups have run yet.');
const destinationMenuItems = [
  { id: 'test', label: 'Test connection' },
  { id: 'rotate', label: 'Edit destination' },
  { id: 'remove', label: 'Remove destination', variant: 'danger' as const },
];
const planMenuItems = [
  { id: 'run', label: 'Back up now', variant: 'primary' as const },
  { id: 'edit', label: 'Edit plan' },
  { id: 'delete', label: 'Delete plan', variant: 'danger' as const },
];

watch([destinationSiteFilter, destinationProviderFilter], () => { destinationPage.value = 1; });
watch([planSiteFilter, planStatusFilter], () => { planPage.value = 1; });
watch([runSiteFilter, runStatusFilter], () => { runPage.value = 1; });
watch(() => filteredDestinations.value.length, length => { destinationPage.value = Math.min(destinationPage.value, Math.max(1, Math.ceil(length / pageSize))); });
watch(() => filteredPlans.value.length, length => { planPage.value = Math.min(planPage.value, Math.max(1, Math.ceil(length / pageSize))); });
watch(() => filteredRuns.value.length, length => { runPage.value = Math.min(runPage.value, Math.max(1, Math.ceil(length / pageSize))); });
watch(showPlanModal, isOpen => {
  if (!isOpen) planForm.value.encryption_password = '';
});
watch(() => planForm.value.encryption_enabled, enabled => {
  if (!enabled) planForm.value.encryption_password = '';
});

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
  showSecretAccessKey.value = false;
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
    addToast(editingDestinationId.value ? 'Backup destination updated' : 'Backup destination connected', 'success');
    showDestinationModal.value = false;
    await fetchData();
  } catch (error: any) { addToast(error.message || (editingDestinationId.value ? 'Failed to update destination' : 'Failed to connect destination'), 'error'); }
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
  editingPlanEncryptionEnabled.value = !!plan?.encryption_enabled;
  planForm.value = plan ? {
    name: plan.name, site_id: plan.site_id, destination_id: plan.destination_id,
    include_files: plan.include_files, database_ids: [...(plan.database_ids || [])],
    schedule: plan.schedule, backup_hour: plan.backup_hour,
    retention_profile: plan.retention_profile, enabled: plan.schedule === 'manual' ? false : plan.enabled,
    encryption_enabled: !!plan.encryption_enabled, encryption_password: '',
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

function handleDestinationAction(action: string, item: any) {
  if (action === 'test') testDestination(item);
  else if (action === 'rotate') openDestinationModal(item);
  else if (action === 'remove') deleteDestination(item);
}

function handlePlanAction(action: string, item: any) {
  if (action === 'run') runPlan(item);
  else if (action === 'edit') openPlanModal(item);
  else if (action === 'delete') deletePlan(item);
}

function runMenuItems(run: any) {
  const downloads = run.status === 'completed'
    ? (run.artifacts || []).map((artifact: any) => ({
        id: `download:${artifact.id}`,
        label: `Download ${artifact.kind === 'files' ? 'site files' : artifact.database_name}`,
      }))
    : [];
  return [
    ...downloads,
    { id: 'delete', label: run.status === 'failed' ? 'Delete record' : 'Delete backup', variant: 'danger' as const },
  ];
}

function handleRunAction(action: string, run: any) {
  if (action === 'delete') {
    deleteRun(run);
    return;
  }
  if (!action.startsWith('download:')) return;
  const artifactId = Number(action.slice('download:'.length));
  const artifact = run.artifacts?.find((candidate: any) => candidate.id === artifactId);
  if (artifact) downloadArtifact(run, artifact);
}

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
.secret-input { padding-right: 2.75rem; }
.filter-input {
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid var(--color-gray-300);
  background: white;
  padding: 0.5rem 0.75rem;
  font-size: 0.8125rem;
  color: var(--color-gray-900);
}
.filter-input:focus {
  border-color: var(--color-blue-500);
  outline: none;
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-blue-500) 25%, transparent);
}
:global(.dark) .form-input {
  border-color: var(--color-gray-600);
  background: var(--color-gray-800);
  color: var(--color-gray-100);
}
:global(.dark) .filter-input {
  border-color: var(--color-gray-600);
  background: var(--color-gray-800);
  color: var(--color-gray-100);
}
</style>
