<template>
  <div class="space-y-6">
    <ErrorAlert :message="error" />
    <div v-if="loading && !state" class="text-sm text-gray-500 dark:text-gray-400" role="status">Loading network settings…</div>
    <AppButton v-if="!state && !loading" variant="secondary" @click="refresh()">Reload settings</AppButton>
    <template v-if="state">
      <StatusNotice v-if="state.pending" message="A network change was interrupted" tone="warning">
        <p class="mt-1 leading-6">Restore the last confirmed configuration before making further changes.</p>
        <template #actions><AppButton variant="secondary" size="sm" :disabled="busy" :loading="saving" @click="recover">Restore configuration</AppButton></template>
      </StatusNotice>
      <StatusNotice v-if="!state.managed" message="Custom vhost is active" tone="warning">
        <p class="mt-1 leading-6">Restore the Fluxo default in <router-link :to="`/sites/${siteId}/settings/vhost`" class="font-medium text-blue-600 dark:text-blue-400">Settings › Nginx Vhost</router-link> to manage password protection and redirects. Operator-written protection may still apply. Saved redirects are shown below for reference.</p>
      </StatusNotice>
      <Card>
        <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Password Protection</h2>
          <StatusBadge :label="state.pending ? 'Unconfirmed' : !state.managed ? 'Not managed' : protectionEnabled ? 'Enabled' : 'Disabled'" :variant="protectionEnabled ? 'green' : 'gray'" />
        </div>
        <ToggleSwitch :model-value="protectionEnabled" :disabled="!editable" label="Protect this site" label-position="left" description="Require a username and password to access this entire site." @update:model-value="toggleProtection" />
        <div v-if="protectionEnabled" class="mt-6 flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-4 dark:border-gray-800">
          <div><p class="text-xs text-gray-500 dark:text-gray-400">Username</p><p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{{ state.protection.username }}</p></div>
          <AppButton variant="secondary" size="sm" :disabled="!editable" @click="openCredentials">Change credentials</AppButton>
        </div>
        <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-gray-400">Use HTTPS to protect credentials in transit. Redirect responses and ACME challenges remain accessible without a password.</p>
      </Card>
      <Card>
        <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
          <div><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Redirects</h2><p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Send visitors from an exact path to a new destination.</p></div>
          <AppButton :disabled="!editable" @click="openRedirect()">Add redirect</AppButton>
        </div>
        <DataTable :columns="redirectColumns" :items="state.redirects" empty-text="No redirects configured." aria-label="Redirect rules">
          <template #source="{ value }"><span class="font-mono text-xs text-gray-900 dark:text-gray-100">{{ value }}</span></template>
          <template #destination="{ value }"><span class="font-mono text-xs text-gray-900 dark:text-gray-100">{{ value }}</span></template>
          <template #status="{ value }"><StatusBadge :label="value === 301 ? '301 Permanent' : '302 Temporary'" variant="gray" /></template>
          <template #actions="{ item }"><TableActionMenu :disabled="!editable" :items="redirectMenu" :aria-label="`Actions for ${item.source}`" @select="action => action === 'edit' ? openRedirect(item) : deleteRedirect(item)" /></template>
        </DataTable>
        <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-gray-400">301 redirects may be cached by browsers. Use 302 for temporary changes.</p>
      </Card>
    </template>
    <BaseModal v-model="showPassword" :title="protectionEnabled ? 'Change credentials' : 'Enable password protection'" :confirm-text="protectionEnabled ? 'Save credentials' : 'Enable protection'" :confirm-disabled="!credentialsValid || !editable" :loading="saving" @submit="saveCredentials">
      <ErrorAlert :message="error" />
      <p class="mb-6 text-sm leading-6 text-gray-600 dark:text-gray-400">Visitors will need these credentials before accessing content on this site.</p>
      <div class="space-y-5">
        <FormGroup label="Username" for-attr="protection-username" :error="username ? usernameError : ''"><input id="protection-username" v-model="username" class="network-input" autocomplete="off" maxlength="64" :disabled="saving" /></FormGroup>
        <FormGroup label="Password" for-attr="protection-password" :error="password ? passwordError : ''" hint="Use 8–72 UTF-8 bytes. Stored as a password hash; your saved password cannot be viewed."><PasswordInput id="protection-password" v-model="password" :maxlength="72" :disabled="saving" /></FormGroup>
      </div>
    </BaseModal>
    <BaseModal v-model="showRedirect" :title="editingRedirect === null ? 'Add redirect' : 'Edit redirect'" confirm-text="Save redirect" :confirm-disabled="!redirectValid || !editable" :loading="saving" @submit="saveRedirect">
      <ErrorAlert :message="error" />
      <div class="space-y-5">
        <FormGroup label="Source path" for-attr="redirect-source" hint="Matches this exact path. Regular expressions and query strings are not supported." :error="source ? sourceError : ''"><input id="redirect-source" v-model="source" class="network-input" placeholder="/old-pricing" :disabled="saving" /></FormGroup>
        <FormGroup label="Destination" for-attr="redirect-destination" :error="destination ? destinationError : ''"><input id="redirect-destination" v-model="destination" class="network-input" placeholder="/pricing or https://example.com/pricing" :disabled="saving" /></FormGroup>
        <FormGroup label="Redirect type" for-attr="redirect-status"><select id="redirect-status" v-model.number="status" class="network-input" :disabled="saving"><option :value="301">301 · Permanent</option><option :value="302">302 · Temporary</option></select></FormGroup>
      </div>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onMounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';
import Card from '../../components/Card.vue';
import DataTable from '../../components/DataTable.vue';
import ErrorAlert from '../../components/ErrorAlert.vue';
import FormGroup from '../../components/FormGroup.vue';
import PasswordInput from '../../components/PasswordInput.vue';
import StatusBadge from '../../components/StatusBadge.vue';
import StatusNotice from '../../components/StatusNotice.vue';
import TableActionMenu from '../../components/TableActionMenu.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';
import { useSiteNetwork } from '../../composables/useSiteNetwork';
const route = useRoute();
const siteId = computed(() => String(route.params.id));
const { state, loading, saving, busy, error, editable, protectionEnabled, refresh, reset,
  showPassword, username, password, usernameError, passwordError, credentialsValid, openCredentials, saveCredentials, toggleProtection,
  showRedirect, editingRedirect, source, destination, status, sourceError, destinationError, redirectValid, openRedirect, saveRedirect, deleteRedirect, recover } = useSiteNetwork(() => siteId.value);
const redirectColumns = [{ key: 'source', label: 'Source' }, { key: 'destination', label: 'Destination' }, { key: 'status', label: 'Type' }];
const redirectMenu = [{ id: 'edit', label: 'Edit' }, { id: 'delete', label: 'Delete', variant: 'danger' as const }];
onMounted(() => refresh());
onActivated(() => { if (state.value) void refresh(); });
watch(siteId, () => { reset(); void refresh(); });
watch(showPassword, open => { if (!open) password.value = ''; });
</script>

<style scoped>
@reference '../../style.css';
.network-input { @apply w-full min-w-0 rounded-lg border border-gray-200 px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 disabled:opacity-60 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100; }
</style>
