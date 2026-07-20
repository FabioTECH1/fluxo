<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-4 sm:px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex flex-wrap justify-between items-center gap-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Certificates</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage your site's SSL certificates.</p>
        </div>
        <div class="relative">
          <button @click="showAddOptions = !showAddOptions" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors">Add certificate</button>
          <div v-if="showAddOptions" class="absolute right-0 mt-2 w-72 max-w-[calc(100vw-2rem)] bg-white rounded-lg shadow-lg border border-gray-200 py-2 z-20 dark:bg-gray-800 dark:border-gray-700">
          <button @click="startLetsEncrypt()" class="flex items-center gap-3 w-full px-4 py-3 text-sm text-gray-700 hover:bg-gray-50 text-left dark:text-gray-300 dark:hover:bg-gray-800">
            <svg class="w-5 h-5 text-green-600 dark:text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" /></svg>
            <div>
              <p class="font-medium">Let's Encrypt</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">Free automated SSL certificate</p>
            </div>
          </button>
          <button @click="startExisting()" class="flex items-center gap-3 w-full px-4 py-3 text-sm text-gray-700 hover:bg-gray-50 text-left dark:text-gray-300 dark:hover:bg-gray-800">
            <svg class="w-5 h-5 text-blue-600 dark:text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" /></svg>
            <div>
              <p class="font-medium">Existing Certificate</p>
              <p class="text-xs text-gray-500">Upload your own certificate</p>
            </div>
          </button>
          <button @click="startClone()" class="flex items-center gap-3 w-full px-4 py-3 text-sm text-gray-700 hover:bg-gray-50 text-left dark:text-gray-300 dark:hover:bg-gray-800">
            <svg class="w-5 h-5 text-cyan-600 dark:text-cyan-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7V5a2 2 0 012-2h9a2 2 0 012 2v9a2 2 0 01-2 2h-2M5 8h9a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2v-9a2 2 0 012-2z" /></svg>
            <div>
              <p class="font-medium">Clone Certificate</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">Reuse a matching custom certificate</p>
            </div>
          </button>
          </div>
        </div>
      </div>

      <div v-if="certs.length === 0" class="px-6 py-12 text-center text-gray-400 dark:text-gray-500 text-sm">
        No certificates installed.
      </div>

      <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <li v-for="cert in certs" :key="cert.id" class="px-4 sm:px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors">
          <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div class="flex items-center gap-3 min-w-0 flex-1">
              <div class="w-8 h-8 rounded-full flex items-center justify-center shrink-0" :class="cert.provider === 'letsencrypt' ? 'bg-green-50 text-green-600 dark:bg-green-900/30 dark:text-green-400' : 'bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400'">
                <svg v-if="cert.provider === 'letsencrypt'" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" /></svg>
                <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" /></svg>
              </div>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ providerLabel(cert.provider) }}</span>
                  <span class="text-xs text-gray-500 dark:text-gray-400 break-all">{{ cert.domain }}</span>
                </div>
                <p class="text-xs text-gray-400 dark:text-gray-500 mt-0.5">
                  {{ formatDate(cert.created_at) }}
                  <span v-if="cert.expires_at" class="ml-2" :class="expiryClass(cert.expires_at)">{{ formatExpiry(cert.expires_at) }}</span>
                  <span v-if="cert.active" class="ml-2 inline-flex items-center px-1.5 py-0.5 rounded text-xs font-semibold bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300">Active</span>
                </p>
              </div>
            </div>
            <div class="flex flex-wrap items-center gap-2 sm:ml-4 shrink-0">
              <AppButton variant="primary" size="sm" @click="activate(cert.id)" :loading="activatingId === cert.id" :disabled="cert.active || hasActiveCert">
                Activate
              </AppButton>
              <AppButton variant="secondary" size="sm" @click="deactivate(cert.id)" :loading="deactivatingId === cert.id" :disabled="!cert.active">
                Deactivate
              </AppButton>
              <AppButton variant="secondary" size="sm" @click="deleteCert(cert.id)" :loading="deletingId === cert.id" :disabled="cert.active" class="text-red-600 dark:text-red-400">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
              </AppButton>
            </div>
          </div>
        </li>
      </ul>
    </div>

    <!-- Let's Encrypt Modal -->
    <div v-if="showLetsEncrypt" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/40" @click="showLetsEncrypt = false"></div>
      <div class="relative bg-white rounded-xl shadow-2xl w-full max-w-md mx-4 overflow-hidden dark:bg-gray-900">
        <div class="px-6 py-4 border-b border-gray-100 bg-gray-50 flex justify-between items-center dark:border-gray-800 dark:bg-gray-800">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Issue Let's Encrypt</h3>
          <button @click="showLetsEncrypt = false" class="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <p class="text-sm text-gray-600 dark:text-gray-400">Issue a free Let's Encrypt certificate for the primary domain and all aliases.</p>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showLetsEncrypt = false" class="px-4 py-2 text-gray-700 bg-white border border-gray-200 rounded-lg shadow-sm hover:bg-gray-50 font-semibold text-sm transition-colors dark:text-gray-300 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-700">Cancel</button>
            <button @click="issueLetsEncrypt" :disabled="issuing" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
              {{ issuing ? 'Issuing...' : 'Issue certificate' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Existing Certificate Modal -->
    <div v-if="showExisting" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/40" @click="showExisting = false"></div>
      <div class="relative bg-white rounded-xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden dark:bg-gray-900">
        <div class="px-6 py-4 border-b border-gray-100 bg-gray-50 flex justify-between items-center dark:border-gray-800 dark:bg-gray-800">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Install Existing Certificate</h3>
          <button @click="showExisting = false" class="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Certificate / CA Bundle</label>
            <p class="text-xs text-gray-500 mb-2 dark:text-gray-400">Paste your certificate and any intermediate CA certificates.</p>
            <textarea v-model="customSSL.certificate" data-gramm="false" class="w-full h-32 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="-----BEGIN CERTIFICATE-----
..."></textarea>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Private Key</label>
            <p class="text-xs text-gray-500 mb-2 dark:text-gray-400">Paste the private key for this certificate.</p>
            <textarea v-model="customSSL.private_key" data-gramm="false" class="w-full h-32 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="-----BEGIN PRIVATE KEY-----
..."></textarea>
          </div>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showExisting = false" class="px-4 py-2 text-gray-700 bg-white border border-gray-200 rounded-lg shadow-sm hover:bg-gray-50 font-semibold text-sm transition-colors dark:text-gray-300 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-700">Cancel</button>
            <button @click="installCustomSSL" :disabled="installing" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
              {{ installing ? 'Installing...' : 'Install certificate' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <BaseModal v-model="showClone" title="Clone Certificate" max-width="max-w-xl" :loading="cloning">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          Only valid custom certificates that cover this site's primary domain and every alias are available.
        </p>

        <div v-if="loadingCloneable" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
          Loading compatible certificates...
        </div>
        <div v-else-if="cloneableCerts.length === 0" class="py-10 text-center">
          <p class="text-sm font-medium text-gray-800 dark:text-gray-200">No compatible certificates found</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">The custom certificate must be unexpired and match all domains attached to this site.</p>
        </div>
        <div v-else class="space-y-2">
          <label
            v-for="candidate in cloneableCerts"
            :key="candidate.id"
            class="flex cursor-pointer items-start gap-3 rounded-lg border p-4 transition-colors"
            :class="selectedCloneId === candidate.id
              ? 'border-blue-500 bg-blue-50 dark:border-blue-400 dark:bg-blue-950/30'
              : 'border-gray-200 hover:border-gray-300 dark:border-gray-700 dark:hover:border-gray-600'"
          >
            <input v-model="selectedCloneId" type="radio" name="clone-certificate" :value="candidate.id" class="mt-1 h-4 w-4 shrink-0 border-gray-300 text-blue-600 focus:ring-blue-500" />
            <span class="min-w-0 flex-1">
              <span class="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span class="text-sm font-semibold text-gray-900 dark:text-gray-100 break-all">{{ candidate.site_domain }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ providerLabel(candidate.provider) }}</span>
              </span>
              <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
                {{ candidate.issuer || 'Unknown issuer' }} · {{ formatExpiry(candidate.expires_at) }}
              </span>
              <span class="mt-2 flex flex-wrap gap-1.5">
                <span v-for="domain in candidate.domains" :key="domain" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300 break-all">{{ domain }}</span>
              </span>
            </span>
          </label>
        </div>
      </div>

      <template #footer>
        <AppButton variant="secondary" :disabled="cloning" @click="showClone = false">Cancel</AppButton>
        <AppButton variant="primary" :loading="cloning" :disabled="selectedCloneId === null || loadingCloneable" @click="cloneCertificate">Clone certificate</AppButton>
      </template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { useConfirm } from '../../composables/useConfirm';
import { apiClient } from '../../api/client';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';

interface CloneableCertificate {
  id: number;
  site_id: number;
  site_domain: string;
  provider: string;
  domains: string[];
  expires_at: string;
  issuer: string;
  fingerprint: string;
  active: boolean;
}

const route = useRoute();
let siteId = route.params.id as string;

const { addToast } = useToast();
const { confirm } = useConfirm();

const certs = ref<any[]>([]);
const hasActiveCert = computed(() => certs.value.some((c: any) => c.active));
const showAddOptions = ref(false);
const showLetsEncrypt = ref(false);
const showExisting = ref(false);
const showClone = ref(false);
const issuing = ref(false);
const installing = ref(false);
const loadingCloneable = ref(false);
const cloning = ref(false);
const cloneableCerts = ref<CloneableCertificate[]>([]);
const selectedCloneId = ref<number | null>(null);
const activatingId = ref<number | null>(null);
const deactivatingId = ref<number | null>(null);
const deletingId = ref<number | null>(null);
const customSSL = ref({ certificate: '', private_key: '' });

const providerLabel = (provider: string) => {
  if (provider === 'letsencrypt') return "Let's Encrypt";
  if (provider === 'cloned') return 'Cloned';
  return 'Custom';
};

const fetchCerts = async (silent = false, bypassCache = false) => {
  try {
    certs.value = await apiClient.getSiteCertificates(siteId, bypassCache) || [];
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to load certificates', 'error');
  }
};

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  if (diff < 2592000) return `${Math.floor(diff / 86400)}d ago`;
  return d.toLocaleDateString();
};

const formatExpiry = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = d.getTime() - now.getTime();
  const days = Math.ceil(diff / 86400000);
  if (days < 0) return `Expired ${Math.abs(days)}d ago`;
  if (days === 0) return 'Expires today';
  if (days === 1) return 'Expires tomorrow';
  if (days <= 7) return `Expires in ${days}d`;
  return `Expires ${d.toLocaleDateString()}`;
};

const expiryClass = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = d.getTime() - now.getTime();
  const days = diff / 86400000;
  if (days < 0) return 'text-red-500 dark:text-red-400 font-semibold';
  if (days < 30) return 'text-amber-500 dark:text-amber-400';
  return 'text-gray-400 dark:text-gray-500';
};

const startLetsEncrypt = () => {
  showAddOptions.value = false;
  showLetsEncrypt.value = true;
};

const startExisting = () => {
  showAddOptions.value = false;
  showExisting.value = true;
};

const startClone = async () => {
  showAddOptions.value = false;
  selectedCloneId.value = null;
  showClone.value = true;
  loadingCloneable.value = true;
  try {
    cloneableCerts.value = await apiClient.getCloneableCertificates(siteId, true) || [];
  } catch (e: any) {
    cloneableCerts.value = [];
    addToast(e.message || 'Failed to load compatible certificates', 'error');
  } finally {
    loadingCloneable.value = false;
  }
};

const cloneCertificate = async () => {
  if (selectedCloneId.value === null) return;
  cloning.value = true;
  try {
    const result = await apiClient.cloneCertificate(siteId, selectedCloneId.value);
    addToast(
      result?.active
        ? 'Certificate cloned and activated.'
        : 'Certificate cloned. Deactivate the current certificate before activating it.',
      'success'
    );
    showClone.value = false;
    selectedCloneId.value = null;
    await fetchCerts(true, true);
  } catch (e: any) {
    addToast(e.message || 'Failed to clone certificate', 'error');
  } finally {
    cloning.value = false;
  }
};

const issueLetsEncrypt = async () => {
  issuing.value = true;
  try {
    await apiClient.installLetsEncryptSSL(siteId, {});
    addToast("Let's Encrypt certificate installed. Activate it to enable HTTPS.", 'success');
    showLetsEncrypt.value = false;
    fetchCerts(true, true);
  } catch (e: any) {
    addToast(e.message || 'Failed to issue certificate', 'error');
  } finally {
    issuing.value = false;
  }
};

const installCustomSSL = async () => {
  installing.value = true;
  try {
    await apiClient.installCustomSSL(siteId, customSSL.value);
    addToast('Certificate installed. Activate it to enable HTTPS.', 'success');
    showExisting.value = false;
    customSSL.value = { certificate: '', private_key: '' };
    fetchCerts(true, true);
  } catch (e: any) {
    addToast(e.message || 'Failed to install certificate', 'error');
  } finally {
    installing.value = false;
  }
};

const activate = async (certId: number) => {
  activatingId.value = certId;
  try {
    await apiClient.activateCert(siteId, certId);
    addToast('Certificate activated', 'success');
    fetchCerts(true, true);
  } catch (e: any) {
    addToast(e.message || 'Failed to activate certificate', 'error');
  } finally {
    activatingId.value = null;
  }
};

const deactivate = async (certId: number) => {
  deactivatingId.value = certId;
  try {
    await apiClient.deactivateCert(siteId, certId);
    addToast('Certificate deactivated', 'success');
    fetchCerts(true, true);
  } catch (e: any) {
    addToast(e.message || 'Failed to deactivate certificate', 'error');
  } finally {
    deactivatingId.value = null;
  }
};

const deleteCert = async (certId: number) => {
  const confirmed = await confirm({
    title: 'Delete Certificate',
    message: 'Are you sure you want to delete this certificate? This will remove the certificate files from the server.',
    confirmText: 'Delete',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!confirmed) return;
  deletingId.value = certId;
  try {
    await apiClient.deleteSiteCertificate(siteId, certId);
    addToast('Certificate deleted', 'success');
    fetchCerts(true, true);
  } catch (e: any) {
    addToast(e.message || 'Failed to delete certificate', 'error');
  } finally {
    deletingId.value = null;
  }
};

const handleClickOutside = (e: MouseEvent) => {
  if (!(e.target as HTMLElement).closest('.relative')) showAddOptions.value = false;
};

let clickListenerActive = false;

const addClickListener = () => {
  if (clickListenerActive) return;
  window.addEventListener('click', handleClickOutside);
  clickListenerActive = true;
};

const removeClickListener = () => {
  if (!clickListenerActive) return;
  window.removeEventListener('click', handleClickOutside);
  clickListenerActive = false;
};

onMounted(() => {
  fetchCerts(true);
  addClickListener();
});

onActivated(() => {
  fetchCerts(true);
  addClickListener();
});

onDeactivated(() => {
  removeClickListener();
});

onUnmounted(() => {
  removeClickListener();
});

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  showClone.value = false;
  cloneableCerts.value = [];
  selectedCloneId.value = null;
  fetchCerts(true);
});
</script>
