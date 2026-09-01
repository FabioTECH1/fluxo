<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
    <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Domains</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage your site's domains and aliases.</p>
      </div>
    </div>

    <div class="px-6 pt-4">
      <DnsRecordNotice :address="hostAddress" />
    </div>

    <div class="p-6">
      <h3 class="text-sm font-semibold text-gray-500 uppercase tracking-wider mb-4 dark:text-gray-400">Custom domains</h3>
      <p class="text-sm text-gray-500 mb-4 dark:text-gray-400">Add custom domains and aliases that you own.</p>

      <ul class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-700">
        <li v-for="d in domains" :key="d.id" class="flex items-center justify-between gap-3 px-4 py-3 transition-colors first:rounded-t-lg last:rounded-b-lg hover:bg-gray-50 dark:hover:bg-gray-800">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium text-gray-900 dark:text-gray-100 break-all">{{ d.domain }}</span>
              <span v-if="d.primary" class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">Primary</span>
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold" :class="d.ssl_active ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'">
                {{ d.ssl_active ? 'SSL active' : 'SSL not active' }}
              </span>
            </div>
            <p v-if="d.ssl_inherited" class="mt-1 text-xs text-gray-500 dark:text-gray-400">Covered by the site certificate</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ wwwRedirectSummary(d.www_redirect || 'none') }}</p>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <TableActionMenu
              :items="domainMenuItems(d)"
              :loading="sslDomainId === d.id || deletingDomainId === d.id || promotingDomainId === d.id || configuringDomainId === d.id"
              :aria-label="`Actions for ${d.domain}`"
              @select="handleDomainAction($event, d)"
            />
          </div>
        </li>
      </ul>

      <div class="flex gap-3 mt-3">
        <input v-model="newDomain" type="text" class="flex-1 border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="your-domain.com" @keyup.enter="openAddDomainConfiguration" />
        <button @click="openAddDomainConfiguration" :disabled="adding || classifyingDomain" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">Add domain</button>
      </div>
    </div>
  </div>

  <DomainConfigurationModal
    v-model="showDomainConfiguration"
    :domain="configurationDomain?.domain || ''"
    :behavior="configurationDomain?.www_redirect || 'none'"
    :title="configurationMode === 'add' ? `Add ${configurationDomain?.domain || 'domain'}` : `Edit ${configurationDomain?.domain || 'domain'}`"
    :confirm-text="configurationMode === 'add' ? 'Add domain' : 'Save'"
    :loading="configurationLoading"
    @save="saveDomainConfiguration"
  />
</template>

<script setup lang="ts">
import { ref, onMounted, onActivated, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useConfirm } from '../../composables/useConfirm';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';
import { useSiteStore } from '../../stores/site';
import DnsRecordNotice from '../../components/DnsRecordNotice.vue';
import TableActionMenu from '../../components/TableActionMenu.vue';
import DomainConfigurationModal from '../../components/DomainConfigurationModal.vue';
import { classifyWWWDomain, wwwRedirectSummary } from '../../types/domain';
import type { WWWRedirectBehavior } from '../../types/domain';

const route = useRoute();
let siteId = route.params.id as string;

const { confirm } = useConfirm();
const { addToast } = useToast();
const siteStore = useSiteStore();

const domains = ref<any[]>([]);
const newDomain = ref('');
const hostAddress = ref('');
const adding = ref(false);
const sslDomainId = ref<number | null>(null);
const deletingDomainId = ref<number | null>(null);
const promotingDomainId = ref<number | null>(null);
const configuringDomainId = ref<number | null>(null);
const showDomainConfiguration = ref(false);
const configurationLoading = ref(false);
const classifyingDomain = ref(false);
const configurationMode = ref<'add' | 'edit'>('add');
const configurationDomain = ref<any | null>(null);

type DomainMenuItem = {
  id: string;
  label: string;
  variant?: 'default' | 'primary' | 'danger';
};

const fetchDomains = async () => {
  try {
    domains.value = await apiClient.getSiteDomains(siteId) || [];
  } catch (e) {}
};

const fetchMetrics = async () => {
  try {
    const m = await apiClient.getMetrics();
    hostAddress.value = m?.host_address || '';
  } catch (e) {}
};

const openAddDomainConfiguration = async () => {
  const domain = newDomain.value.trim().toLowerCase();
  if (!domain) return;
  classifyingDomain.value = true;
  try {
    const classification = await classifyWWWDomain(domain);
    configurationMode.value = 'add';
    configurationDomain.value = { id: -1, domain, www_redirect: classification.defaultRedirect };
    showDomainConfiguration.value = true;
  } catch (e: any) {
    addToast(e.message || 'Failed to inspect the domain', 'error');
  } finally {
    classifyingDomain.value = false;
  }
};

const openEditDomainConfiguration = (domain: any) => {
  configurationMode.value = 'edit';
  configurationDomain.value = { ...domain, www_redirect: domain.www_redirect || 'none' };
  showDomainConfiguration.value = true;
};

const saveDomainConfiguration = async (behavior: WWWRedirectBehavior) => {
  if (!configurationDomain.value) return;
  configurationLoading.value = true;
  try {
    if (configurationMode.value === 'add') {
      adding.value = true;
      await apiClient.addSiteDomain(siteId, {
        domain: configurationDomain.value.domain,
        www_redirect: behavior,
      });
      addToast('Domain added', 'success');
      newDomain.value = '';
    } else {
      configuringDomainId.value = configurationDomain.value.id;
      await apiClient.updateSiteDomain(siteId, configurationDomain.value.id, { www_redirect: behavior });
      addToast(`Domain configuration updated for ${configurationDomain.value.domain}`, 'success');
    }
    showDomainConfiguration.value = false;
    await fetchDomains();
  } catch (e: any) {
    addToast(e.message || 'Failed to save domain configuration', 'error');
  } finally {
    adding.value = false;
    configurationLoading.value = false;
    configuringDomainId.value = null;
  }
};

const issueDomainSSL = async (domain: any) => {
  sslDomainId.value = domain.id;
  try {
    const result = await apiClient.installDomainLetsEncryptSSL(siteId, domain.id);
    addToast(
      result?.active ? `SSL activated for ${domain.domain}` : `Certificate installed, but could not be activated for ${domain.domain}`,
      result?.active ? 'success' : 'info'
    );
    await fetchDomains();
  } catch (e: any) {
    addToast(e.message || 'Failed to issue SSL certificate', 'error');
  } finally {
    sslDomainId.value = null;
  }
};

const removeDomainSSL = async (domain: any) => {
  const confirmed = await confirm({
    title: 'Deactivate Domain SSL',
    message: `Deactivate SSL for ${domain.domain}?`,
    confirmText: 'Deactivate',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  sslDomainId.value = domain.id;
  try {
    await apiClient.deactivateDomainSSL(siteId, domain.id);
    addToast(`SSL deactivated for ${domain.domain}`, 'success');
    await fetchDomains();
  } catch (e: any) {
    addToast(e.message || 'Failed to deactivate SSL', 'error');
  } finally {
    sslDomainId.value = null;
  }
};

const deleteDomain = async (id: number) => {
  const confirmed = await confirm({
    title: 'Delete Domain',
    message: 'Are you sure you want to delete this domain alias?',
    confirmText: 'Delete',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  deletingDomainId.value = id;
  try {
    await apiClient.deleteSiteDomain(siteId, id);
    addToast('Domain deleted', 'success');
    fetchDomains();
  } catch (e: any) {
    addToast(e.message || 'Failed to delete', 'error');
  } finally {
    deletingDomainId.value = null;
  }
};

const promoteDomain = async (domain: any) => {
  const confirmed = await confirm({
    title: 'Make Primary Domain',
    message: `${domain.domain} will become the primary domain. The current primary domain will become an alias.`,
    confirmText: 'Make primary',
    cancelText: 'Cancel'
  });
  if (!confirmed) return;

  promotingDomainId.value = domain.id;
  try {
    await apiClient.promoteSiteDomain(siteId, domain.id);
    addToast(`${domain.domain} is now the primary domain`, 'success');
    await Promise.allSettled([
      fetchDomains(),
      siteStore.fetchSite(siteId, true)
    ]);
  } catch (e: any) {
    addToast(e.message || 'Failed to change primary domain', 'error');
  } finally {
    promotingDomainId.value = null;
  }
};

const domainMenuItems = (domain: any) => {
  const items: DomainMenuItem[] = [{ id: 'configure', label: 'Configure domain' }];
  if (domain.primary) return items;
  items.push({ id: 'primary', label: 'Make primary' });
  if (!domain.ssl_active) {
    items.push({ id: 'ssl', label: "Secure with Let's Encrypt", variant: 'primary' as const });
  } else {
    items.push({ id: 'remove-ssl', label: 'Deactivate domain SSL' });
  }
  items.push({ id: 'delete', label: 'Delete domain', variant: 'danger' as const });
  return items;
};

const handleDomainAction = (action: string, domain: any) => {
  if (action === 'configure') openEditDomainConfiguration(domain);
  if (action === 'primary') promoteDomain(domain);
  if (action === 'ssl') issueDomainSSL(domain);
  if (action === 'remove-ssl') removeDomainSSL(domain);
  if (action === 'delete') deleteDomain(domain.id);
};

onMounted(() => {
  fetchDomains();
  fetchMetrics();
});

onActivated(() => {
  fetchDomains();
  fetchMetrics();
});

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  fetchDomains();
});
</script>
