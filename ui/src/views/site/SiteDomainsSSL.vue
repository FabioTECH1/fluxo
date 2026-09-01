<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-4 sm:px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex flex-wrap justify-between items-center gap-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Certificates</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage installed certificates and choose which one is active.</p>
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

      <div v-if="activeCert" class="px-4 sm:px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <p class="text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">Current Active</p>
        <div class="mt-2 flex flex-wrap items-center gap-2 text-sm text-gray-900 dark:text-gray-100">
          <span class="font-semibold">{{ providerLabel(activeCert.provider) }}</span>
          <span class="text-green-600 dark:text-green-400">Active</span>
          <span class="text-gray-500 dark:text-gray-400 break-all">{{ activeCert.domain }}</span>
        </div>
      </div>

      <div v-if="certs.length === 0" class="px-6 py-12 text-center text-gray-400 dark:text-gray-500 text-sm">
        No certificates installed.
      </div>

      <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <li class="px-4 sm:px-6 py-3 text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">
          Available Certificates
        </li>
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
                  <span class="ml-2 inline-flex items-center px-1.5 py-0.5 rounded text-xs font-semibold" :class="certificateInUse(cert) ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'">
                    {{ certificateStatus(cert) }}
                  </span>
                </p>
                <p v-if="cert.active_domains?.length" class="mt-1 text-xs text-gray-500 dark:text-gray-400 break-all">
                  Active for {{ cert.active_domains.join(', ') }}
                </p>
              </div>
            </div>
            <div class="flex flex-wrap items-center gap-2 sm:ml-4 shrink-0">
              <AppButton variant="primary" size="sm" @click="activate(cert.id)" :loading="activatingId === cert.id" :disabled="cert.active">
                {{ activateLabel(cert) }}
              </AppButton>
              <AppButton variant="secondary" size="sm" @click="deactivate(cert.id)" :loading="deactivatingId === cert.id" :disabled="!cert.active">
                Deactivate
              </AppButton>
              <AppButton v-if="eligibleAliases(cert).length" variant="secondary" size="sm" @click="openDomainAssignment(cert)">
                Use for alias
              </AppButton>
              <AppButton variant="secondary" size="sm" @click="deleteCert(cert.id)" :loading="deletingId === cert.id" :disabled="certificateInUse(cert)" class="text-red-600 dark:text-red-400">
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
          <div v-if="letsEncryptWWWHostnames.length" class="rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200">
            <p class="font-semibold">WWW DNS records required</p>
            <p class="mt-1 text-xs leading-5">
              A www redirect is enabled, so Let's Encrypt must also validate:
              <span class="font-mono">{{ letsEncryptWWWHostnames.join(', ') }}</span>.
              Create DNS records for these hostnames first, or choose <span class="font-semibold">No redirect</span> on the Domains tab.
            </p>
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
      <div class="absolute inset-0 bg-black/40" @click="closeExisting"></div>
      <div class="relative bg-white rounded-xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden dark:bg-gray-900">
        <div class="px-6 py-4 border-b border-gray-100 bg-gray-50 flex justify-between items-center dark:border-gray-800 dark:bg-gray-800">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Install Existing Certificate</h3>
          <button @click="closeExisting" class="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Use for</label>
            <select v-model.number="customSSL.domain_id" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600">
              <option :value="0">Primary domain{{ primaryDomain ? ` (${primaryDomain})` : '' }}</option>
              <option v-for="domain in aliasDomains" :key="domain.id" :value="domain.id">Alias ({{ domain.domain }})</option>
            </select>
          </div>
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
            <button @click="closeExisting" class="px-4 py-2 text-gray-700 bg-white border border-gray-200 rounded-lg shadow-sm hover:bg-gray-50 font-semibold text-sm transition-colors dark:text-gray-300 dark:bg-gray-800 dark:border-gray-700 dark:hover:bg-gray-700">Cancel</button>
            <button @click="installCustomSSL" :disabled="installing" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
              {{ installing ? 'Installing...' : 'Install certificate' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <BaseModal v-model="showClone" title="Clone Certificate" max-width="max-w-xl" :loading="cloning || updatingCloneDomain">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          Only valid custom certificates that cover every hostname required by the selected domain are available.
        </p>

        <div>
          <label class="block text-sm font-semibold text-gray-700 mb-1 dark:text-gray-300">Use for</label>
          <select v-model.number="cloneDomainId" :disabled="loadingCloneable || cloning || updatingCloneDomain" class="w-full border border-gray-200 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 disabled:opacity-50 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" @change="fetchCloneableCertificates">
            <option :value="0">Primary domain{{ primaryDomain ? ` (${primaryDomain})` : '' }}</option>
            <option v-for="domain in aliasDomains" :key="domain.id" :value="domain.id">Alias ({{ domain.domain }})</option>
          </select>
        </div>

        <div v-if="loadingCloneable" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
          Loading compatible certificates...
        </div>
        <div v-else-if="cloneableCerts.length === 0" class="py-10 text-center">
          <p class="text-sm font-medium text-gray-800 dark:text-gray-200">No compatible certificates found</p>
          <template v-if="cloneTargetUsesWWW">
            <p class="mx-auto mt-2 max-w-md text-xs leading-5 text-gray-500 dark:text-gray-400">
              {{ cloneTargetDomain?.domain }} currently uses a www redirect, so its certificate must cover both
              <span class="font-mono text-gray-700 dark:text-gray-300">{{ cloneTargetHostnames.join(' and ') }}</span>.
              A wildcard that covers the first hostname may not cover the added www hostname.
            </p>
            <AppButton class="mt-4" variant="secondary" size="sm" :loading="updatingCloneDomain" @click="removeCloneWWWRedirect">
              Use without www redirect
            </AppButton>
          </template>
          <p v-else class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            No unexpired custom certificate from another site covers {{ cloneTargetDomain?.domain || 'the selected hostname' }}.
          </p>
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
        <AppButton variant="secondary" :disabled="cloning || updatingCloneDomain" @click="showClone = false">Cancel</AppButton>
        <AppButton variant="primary" :loading="cloning" :disabled="selectedCloneId === null || loadingCloneable || updatingCloneDomain" @click="cloneCertificate">Clone certificate</AppButton>
      </template>
    </BaseModal>

    <BaseModal v-model="showDomainAssignment" title="Use Certificate for Alias" max-width="max-w-lg" :loading="assigningDomain">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">Choose an alias covered by this certificate.</p>
        <div class="space-y-2">
          <label
            v-for="domain in assignmentAliases"
            :key="domain.id"
            class="flex cursor-pointer items-center gap-3 rounded-lg border border-gray-200 px-4 py-3 text-sm text-gray-800 dark:border-gray-700 dark:text-gray-200"
          >
            <input v-model="selectedDomainId" type="radio" name="certificate-domain" :value="domain.id" class="h-4 w-4 border-gray-300 text-blue-600 focus:ring-blue-500" />
            <span class="break-all">{{ domain.domain }}</span>
          </label>
        </div>
      </div>

      <template #footer>
        <AppButton variant="secondary" :disabled="assigningDomain" @click="showDomainAssignment = false">Cancel</AppButton>
        <AppButton variant="primary" :loading="assigningDomain" :disabled="selectedDomainId === null" @click="assignCertificateToDomain">Activate</AppButton>
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
import type { WWWRedirectBehavior } from '../../types/domain';

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

interface DomainItem {
  id: number;
  domain: string;
  primary: boolean;
  ssl_active: boolean;
  certificate_id?: number;
  www_redirect: WWWRedirectBehavior;
}

const route = useRoute();
let siteId = route.params.id as string;

const { addToast, showToast, updateToast } = useToast();
const { confirm } = useConfirm();

const certs = ref<any[]>([]);
const domains = ref<DomainItem[]>([]);
const primaryDomain = computed(() => domains.value.find(domain => domain.primary)?.domain || '');
const aliasDomains = computed(() => domains.value.filter(domain => !domain.primary));
const hasActiveCert = computed(() => certs.value.some((c: any) => c.active));
const activeCert = computed(() => certs.value.find((c: any) => c.active) || null);
const showAddOptions = ref(false);
const showLetsEncrypt = ref(false);
const showExisting = ref(false);
const showClone = ref(false);
const showDomainAssignment = ref(false);
const issuing = ref(false);
const installing = ref(false);
const loadingCloneable = ref(false);
const cloning = ref(false);
const cloneableCerts = ref<CloneableCertificate[]>([]);
const selectedCloneId = ref<number | null>(null);
const cloneDomainId = ref(0);
const updatingCloneDomain = ref(false);
const cloneTargetDomain = computed(() => domains.value.find(domain => domain.id === cloneDomainId.value) || null);
const cloneTargetHostnames = computed(() => {
  const target = cloneTargetDomain.value;
  if (!target) return [];
  const hostname = target.domain.trim().toLowerCase();
  if (!hostname || hostname.startsWith('www.') || target.www_redirect === 'none') return hostname ? [hostname] : [];
  return [hostname, `www.${hostname}`];
});
const cloneTargetUsesWWW = computed(() => cloneTargetHostnames.value.length === 2);
const letsEncryptWWWHostnames = computed(() => domains.value.flatMap(domain => {
  const hostname = domain.domain.trim().toLowerCase();
  if (!hostname || hostname.startsWith('www.') || domain.www_redirect === 'none') return [];
  return [`www.${hostname}`];
}));
const activatingId = ref<number | null>(null);
const deactivatingId = ref<number | null>(null);
const deletingId = ref<number | null>(null);
const assigningDomain = ref(false);
const assignmentCert = ref<any | null>(null);
const selectedDomainId = ref<number | null>(null);
const customSSL = ref({ certificate: '', private_key: '', domain_id: 0 });

const providerLabel = (provider: string) => {
  if (provider === 'letsencrypt') return "Let's Encrypt";
  if (provider === 'cloned') return 'Cloned';
  return 'Custom';
};

const activateLabel = (cert: any) => {
  if (cert.active) return 'Active';
  return hasActiveCert.value ? 'Switch' : 'Activate';
};

const certificateInUse = (cert: any) => cert.active || (cert.active_domains?.length || 0) > 0;

const certificateStatus = (cert: any) => {
  if (cert.active) return 'Site active';
  const count = cert.active_domains?.length || 0;
  if (count > 0) return count === 1 ? 'Active for 1 alias' : `Active for ${count} aliases`;
  return 'Installed';
};

const eligibleAliases = (cert: any) => {
  const covered = new Set((cert.covered_domains || []).map((domain: string) => domain.toLowerCase()));
  return domains.value.filter(domain =>
    !domain.primary &&
    covered.has(domain.domain.toLowerCase()) &&
    domain.certificate_id !== cert.id
  );
};

const assignmentAliases = computed(() => assignmentCert.value ? eligibleAliases(assignmentCert.value) : []);

const fetchCerts = async (silent = false, bypassCache = false) => {
  try {
    certs.value = await apiClient.getSiteCertificates(siteId, bypassCache) || [];
  } catch (e: any) {
    if (!silent) addToast(e.message || 'Failed to load certificates', 'error');
  }
};

const fetchDomains = async (bypassCache = false) => {
  try {
    domains.value = await apiClient.getSiteDomains(siteId, bypassCache) || [];
  } catch {
    domains.value = [];
  }
};

const refreshSSLState = () => Promise.all([fetchCerts(true, true), fetchDomains(true)]);

const openDomainAssignment = (cert: any) => {
  assignmentCert.value = cert;
  selectedDomainId.value = null;
  showDomainAssignment.value = true;
};

const assignCertificateToDomain = async () => {
  if (!assignmentCert.value || selectedDomainId.value === null) return;
  assigningDomain.value = true;
  try {
    await apiClient.activateDomainCert(siteId, selectedDomainId.value, assignmentCert.value.id);
    addToast('Certificate activated for alias', 'success');
    showDomainAssignment.value = false;
    await refreshSSLState();
  } catch (e: any) {
    addToast(e.message || 'Failed to activate certificate for alias', 'error');
  } finally {
    assigningDomain.value = false;
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
  resetCustomSSL();
  showExisting.value = true;
};

const resetCustomSSL = () => {
  customSSL.value = { certificate: '', private_key: '', domain_id: 0 };
};

const closeExisting = () => {
  if (installing.value) return;
  showExisting.value = false;
};

const startClone = async () => {
  showAddOptions.value = false;
  cloneDomainId.value = 0;
  selectedCloneId.value = null;
  showClone.value = true;
  await fetchCloneableCertificates();
};

const fetchCloneableCertificates = async () => {
  selectedCloneId.value = null;
  loadingCloneable.value = true;
  try {
    cloneableCerts.value = await apiClient.getCloneableCertificates(siteId, true, cloneDomainId.value) || [];
  } catch (e: any) {
    cloneableCerts.value = [];
    addToast(e.message || 'Failed to load compatible certificates', 'error');
  } finally {
    loadingCloneable.value = false;
  }
};

const removeCloneWWWRedirect = async () => {
  const target = cloneTargetDomain.value;
  if (!target || !cloneTargetUsesWWW.value) return;
  updatingCloneDomain.value = true;
  try {
    await apiClient.updateSiteDomain(siteId, target.id, { www_redirect: 'none' });
    await fetchDomains(true);
    addToast(`WWW redirect removed for ${target.domain}. Rechecking certificates.`, 'success');
    await fetchCloneableCertificates();
  } catch (e: any) {
    addToast(e.message || 'Failed to update the domain configuration', 'error');
  } finally {
    updatingCloneDomain.value = false;
  }
};

const cloneCertificate = async () => {
  if (selectedCloneId.value === null) return;
  cloning.value = true;
  try {
    const result = await apiClient.cloneCertificate(siteId, selectedCloneId.value, cloneDomainId.value);
    addToast(
      result?.active
        ? 'Certificate cloned and activated.'
        : 'Certificate cloned and installed. The current certificate remains active.',
      'success'
    );
    showClone.value = false;
    selectedCloneId.value = null;
    await refreshSSLState();
  } catch (e: any) {
    addToast(e.message || 'Failed to clone certificate', 'error');
  } finally {
    cloning.value = false;
  }
};

const issueLetsEncrypt = async () => {
  issuing.value = true;
  const toastId = showToast({
    title: "Issuing Let's Encrypt certificate",
    description: 'Validating the domain and requesting a certificate.',
    type: 'loading',
  });
  try {
    const result = await apiClient.installLetsEncryptSSL(siteId, {});
    updateToast(toastId, result?.active
      ? {
          title: "Let's Encrypt certificate activated",
          description: 'HTTPS is now active for this domain.',
          type: 'success',
        }
      : {
          title: 'Certificate installed with a warning',
          description: result?.activation_error || 'Activate it manually when ready.',
          type: 'warning',
        });
    showLetsEncrypt.value = false;
    await refreshSSLState();
  } catch (e: any) {
    updateToast(toastId, {
      title: 'Certificate could not be issued',
      description: e.message || 'Domain validation or certificate issuance failed.',
      type: 'error',
    });
  } finally {
    issuing.value = false;
  }
};

const installCustomSSL = async () => {
  installing.value = true;
  const toastId = showToast({
    title: 'Installing SSL certificate',
    description: 'Validating the certificate and private key.',
    type: 'loading',
  });
  try {
    const result = await apiClient.installCustomSSL(siteId, customSSL.value);
    updateToast(toastId, {
      title: result?.active ? 'SSL certificate activated' : 'SSL certificate installed',
      description: result?.active
        ? 'HTTPS is now using the new certificate.'
        : 'The current certificate remains active.',
      type: 'success',
    });
    showExisting.value = false;
    await refreshSSLState();
  } catch (e: any) {
    updateToast(toastId, {
      title: 'SSL certificate could not be installed',
      description: e.message || 'Check the certificate and private key, then try again.',
      type: 'error',
    });
  } finally {
    installing.value = false;
  }
};

const activate = async (certId: number) => {
  activatingId.value = certId;
  try {
    await apiClient.activateCert(siteId, certId);
    addToast('Certificate activated', 'success');
    await refreshSSLState();
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
    await refreshSSLState();
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
    await refreshSSLState();
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
  fetchDomains();
  addClickListener();
});

onActivated(() => {
  fetchCerts(true);
  fetchDomains();
  addClickListener();
});

onDeactivated(() => {
  removeClickListener();
});

onUnmounted(() => {
  removeClickListener();
});

watch(showExisting, (isOpen) => {
  if (!isOpen) resetCustomSSL();
});

watch(() => route.params.id, (newId) => {
  siteId = newId as string;
  showExisting.value = false;
  showClone.value = false;
  cloneableCerts.value = [];
  cloneDomainId.value = 0;
  selectedCloneId.value = null;
  fetchCerts(true);
  fetchDomains();
});
</script>
