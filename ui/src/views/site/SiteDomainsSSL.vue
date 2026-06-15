<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Certificates</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage your site's SSL certificates.</p>
        </div>
      </div>

      <div class="p-6">
        <div v-if="site && site.ssl_provider && site.ssl_provider !== 'none'" class="mb-4 border border-gray-200 rounded-lg p-4 flex items-center justify-between dark:border-gray-700">
          <div>
            <p class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ site.domain }}</p>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ site.ssl_provider === 'letsencrypt' ? "Let's Encrypt" : "Custom" }} &middot; <span :class="site.ssl_active ? 'text-green-600 font-semibold' : 'text-yellow-600 font-semibold'">{{ site.ssl_active ? 'Active' : 'Installed · Inactive' }}</span></p>
          </div>
          <div class="flex gap-2">
            <button v-if="!site.ssl_active" @click="activateSSL" :disabled="activating" class="px-3 py-1.5 text-xs text-white bg-green-600 rounded-lg hover:bg-green-700 font-semibold transition-colors disabled:opacity-50">
              {{ activating ? 'Activating...' : 'Activate' }}
            </button>
            <button v-if="site.ssl_active" @click="deactivateSSL" :disabled="deactivating" class="px-3 py-1.5 text-xs text-red-600 bg-red-50 border border-red-200 rounded-lg hover:bg-red-100 font-semibold transition-colors disabled:opacity-50 dark:text-red-400 dark:bg-red-900/30 dark:border-red-800 dark:hover:bg-red-900/40">
              {{ deactivating ? 'Deactivating...' : 'Deactivate' }}
            </button>
          </div>
        </div>

        <div class="relative">
          <button @click="showAddOptions = !showAddOptions" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors">Add certificate</button>
          <div v-if="showAddOptions" class="absolute left-0 mt-2 w-72 bg-white rounded-lg shadow-lg border border-gray-200 py-2 z-20 dark:bg-gray-800 dark:border-gray-700">
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
          </div>
        </div>
      </div>
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
          <p class="text-sm text-gray-600 dark:text-gray-400">Issue a free Let's Encrypt certificate for one of your domains.</p>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Domain</label>
            <select v-model="leDomain" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
              <option v-for="d in domains" :key="d.id" :value="d.domain">{{ d.domain }}{{ d.primary ? ' (primary)' : '' }}</option>
            </select>
          </div>
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
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Domain</label>
            <select v-model="customDomain" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
              <option v-for="d in domains" :key="d.id" :value="d.domain">{{ d.domain }}{{ d.primary ? ' (primary)' : '' }}</option>
            </select>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Certificate / CA Bundle</label>
            <textarea v-model="customSSL.certificate" class="w-full h-32 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
          </div>
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Private Key</label>
            <textarea v-model="customSSL.private_key" class="w-full h-32 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="-----BEGIN PRIVATE KEY-----"></textarea>
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, inject } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { apiClient } from '../../api/client';

const route = useRoute();
const siteId = route.params.id as string;
const { addToast } = useToast();

const site = ref<any>(null);
const parentSite = inject<any>('site', null);
const domains = ref<any[]>([]);
const showAddOptions = ref(false);
const showLetsEncrypt = ref(false);
const showExisting = ref(false);
const issuing = ref(false);
const installing = ref(false);
const activating = ref(false);
const deactivating = ref(false);
const leDomain = ref('');
const customDomain = ref('');
const customSSL = ref({ certificate: '', private_key: '' });

const fetchSite = async () => {
  if (parentSite?.value?.id) { site.value = parentSite.value; return; }
  try { site.value = await apiClient.getSite(siteId); } catch (e) {}
};

const fetchDomains = async () => {
  try {
    domains.value = await apiClient.getSiteDomains(siteId) || [];
    if (domains.value.length > 0) {
      leDomain.value = domains.value[0].domain;
      customDomain.value = domains.value[0].domain;
    }
  } catch (e) {}
};

const startLetsEncrypt = () => {
  showAddOptions.value = false;
  showLetsEncrypt.value = true;
};

const startExisting = () => {
  showAddOptions.value = false;
  showExisting.value = true;
};

const issueLetsEncrypt = async () => {
  issuing.value = true;
  try {
    await apiClient.installLetsEncryptSSL(siteId, {});
    addToast('Certificate installed. Activate it to enable HTTPS.', 'success');
    showLetsEncrypt.value = false;
    fetchSite();
  } catch (e: any) {
    showLetsEncrypt.value = false;
    addToast(e.message || 'Failed to issue certificate', 'error');
  } finally {
    issuing.value = false;
  }
};

const installCustomSSL = async () => {
  installing.value = true;
  try {
    await apiClient.installCustomSSL(siteId, customSSL.value);
    addToast('Certificate installed! Activate it to enable HTTPS.', 'success');
    showExisting.value = false;
    customSSL.value = { certificate: '', private_key: '' };
    fetchSite();
  } catch (e: any) {
    showExisting.value = false;
    addToast(e.message || 'Failed to install certificate', 'error');
  } finally {
    installing.value = false;
  }
};

const activateSSL = async () => {
  activating.value = true;
  try {
    await apiClient.activateSSL(siteId);
    addToast('SSL activated', 'success');
    fetchSite();
  } catch (e: any) {
    addToast(e.message || 'Failed to activate SSL', 'error');
  } finally {
    activating.value = false;
  }
};

const deactivateSSL = async () => {
  deactivating.value = true;
  try {
    await apiClient.deactivateSSL(siteId);
    addToast('SSL deactivated', 'success');
    fetchSite();
  } catch (e: any) {
    addToast(e.message || 'Failed to deactivate SSL', 'error');
  } finally {
    deactivating.value = false;
  }
};

const handleClickOutside = (e: MouseEvent) => {
  if (!(e.target as HTMLElement).closest('.relative')) showAddOptions.value = false;
};

onMounted(() => {
  fetchSite();
  fetchDomains();
  window.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  window.removeEventListener('click', handleClickOutside);
});
</script>
