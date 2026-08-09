<template>
  <SkeletonLoader v-if="loading" type="card" />
  <Card v-else>
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Panel Domain</h2>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
          Open this administration panel through a trusted HTTPS hostname.
        </p>
      </div>
      <StatusBadge :label="statusLabel" :variant="statusVariant" />
    </div>

    <ErrorAlert class="mt-4" :message="errorMessage" />

    <div v-if="!loaded" class="mt-5 flex flex-col gap-3 rounded-lg border border-red-200 bg-red-50 p-4 sm:flex-row sm:items-center sm:justify-between dark:border-red-900/60 dark:bg-red-950/30">
      <p class="text-sm text-red-800 dark:text-red-300">Panel-domain status is unavailable. Retry before making changes.</p>
      <AppButton variant="secondary" size="sm" @click="refresh()">Retry</AppButton>
    </div>

    <template v-else>
    <div v-if="panel.domain" class="mt-5 rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-gray-700 dark:bg-gray-800">
      <div class="grid gap-4 sm:grid-cols-3">
        <div class="sm:col-span-2 min-w-0">
          <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Panel URL</p>
          <a :href="panel.url" target="_blank" rel="noopener noreferrer" class="mt-1 block break-all text-sm font-semibold text-blue-600 hover:underline dark:text-blue-400">
            {{ panel.url }}
          </a>
        </div>
        <div>
          <p class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Certificate</p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{{ providerLabel(panel.ssl_provider) }}</p>
          <p v-if="panel.expires_at" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">Expires {{ formatDate(panel.expires_at) }}</p>
        </div>
      </div>
      <p v-if="panel.status_error" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ panel.status_error }}</p>
    </div>

    <div v-if="!panel.domain" class="mt-5 space-y-4">
      <FormGroup
        label="Domain"
        for-attr="panel-domain"
        hint="Create an A or AAAA record for this hostname pointing to the server before connecting it."
        :error="domainTouched ? domainValidationError : ''"
      >
        <input
          id="panel-domain"
          v-model="domain"
          type="text"
          inputmode="url"
          autocomplete="off"
          spellcheck="false"
          placeholder="panel.example.com"
          :disabled="busy"
          :aria-invalid="domainTouched && Boolean(domainValidationError)"
          :aria-describedby="domainTouched && domainValidationError ? 'panel-domain-error' : 'panel-domain-hint'"
          class="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 disabled:opacity-60 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          @blur="domainTouched = true"
        />
      </FormGroup>

      <div class="rounded-lg border border-blue-100 bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300">
        Direct access through the server IP and existing dashboard port remains available for recovery after connecting a domain.
      </div>

      <div class="flex justify-end">
        <AppButton variant="primary" :disabled="busy || !domain.trim() || Boolean(domainValidationError)" @click="openConnect">
          Connect Domain
        </AppButton>
      </div>
    </div>

    <div v-else class="mt-5 space-y-4">
      <div class="rounded-lg border border-blue-100 bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300">
        Direct access through the server IP and existing dashboard port remains available for recovery.
      </div>

      <div v-if="certificateNeedsAttention" class="flex flex-col gap-3 rounded-lg border border-yellow-200 bg-yellow-50 px-4 py-3 sm:flex-row sm:items-center sm:justify-between dark:border-yellow-900/60 dark:bg-yellow-950/30">
        <div>
          <p class="text-sm font-semibold text-yellow-900 dark:text-yellow-200">{{ attentionTitle }}</p>
          <p class="mt-0.5 text-xs text-yellow-800 dark:text-yellow-300">{{ attentionMessage }}</p>
        </div>
        <AppButton variant="primary" size="sm" :disabled="busy" :loading="operation !== '' && operation !== 'remove'" @click="repairCertificate">
          {{ repairActionLabel }}
        </AppButton>
      </div>

      <div class="flex flex-col gap-3 border-t border-gray-100 pt-4 sm:flex-row sm:items-center sm:justify-between dark:border-gray-800">
        <p class="text-xs text-gray-500 dark:text-gray-400">
          Removing this hostname does not stop direct dashboard access.
        </p>
        <div class="flex justify-end sm:block">
          <AppButton variant="danger" size="sm" :disabled="busy" :loading="operation === 'remove'" @click="removeDomain">
            Remove Domain
          </AppButton>
        </div>
      </div>
    </div>
    </template>

    <BaseModal
      v-model="showConnect"
      title="Connect Panel Domain"
      max-width="max-w-xl"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          Choose how Fluxo should secure <span class="font-semibold text-gray-900 dark:text-gray-100">{{ normalizedDomain }}</span>.
        </p>

        <div class="space-y-3">
          <AppButton class="w-full justify-start" variant="primary" :disabled="busy" @click="chooseLetsEncrypt">
            <span class="text-left">
              <span class="block">Let's Encrypt</span>
              <span class="mt-0.5 block text-xs font-normal text-blue-100">Automatic issuance and renewal through Certbot.</span>
            </span>
          </AppButton>
          <AppButton class="w-full justify-start" variant="secondary" :disabled="busy" @click="chooseCustomCertificate">
            <span class="text-left">
              <span class="block">Existing Certificate</span>
              <span class="mt-0.5 block text-xs font-normal text-gray-500 dark:text-gray-400">Upload a certificate and matching private key.</span>
            </span>
          </AppButton>
          <AppButton class="w-full justify-start" variant="secondary" :disabled="busy" @click="chooseCloneCertificate">
            <span class="text-left">
              <span class="block">Clone Certificate</span>
              <span class="mt-0.5 block text-xs font-normal text-gray-500 dark:text-gray-400">Copy a compatible custom certificate from a managed site.</span>
            </span>
          </AppButton>
        </div>
      </div>

      <template #footer>
        <AppButton variant="secondary" :disabled="busy" @click="showConnect = false">Cancel</AppButton>
      </template>
    </BaseModal>

    <BaseModal
      v-model="showCustom"
      :title="customModalTitle"
      max-width="max-w-2xl"
      :confirm-text="customConfirmText"
      :loading="operation === 'custom'"
      :prevent-dismiss="operation === 'custom'"
      :confirm-disabled="!customCertificate.trim() || !customPrivateKey.trim()"
      @submit="connectCustomCertificate"
    >
      <div class="space-y-4">
        <ErrorAlert :message="errorMessage" />
        <p class="text-sm text-gray-600 dark:text-gray-400">
          The certificate must be valid for <span class="font-semibold text-gray-900 dark:text-gray-100">{{ normalizedDomain }}</span>.
        </p>
        <FormGroup label="Certificate / CA Bundle" for-attr="panel-certificate" hint="Paste the leaf certificate followed by any intermediate certificates.">
          <ScriptEditor
            id="panel-certificate"
            aria-describedby="panel-certificate-hint"
            v-model="customCertificate"
            label="Certificate and CA bundle"
            language="plain"
            :visible-lines="8"
            :minimum-lines="8"
            :readonly="operation === 'custom'"
            :busy="operation === 'custom'"
            placeholder="-----BEGIN CERTIFICATE-----"
          />
        </FormGroup>
        <FormGroup label="Private Key" for-attr="panel-private-key" hint="The key stays hidden unless you explicitly reveal it.">
          <ScriptEditor
            id="panel-private-key"
            aria-describedby="panel-private-key-hint"
            v-model="customPrivateKey"
            label="Private key"
            language="plain"
            :visible-lines="6"
            :minimum-lines="6"
            :readonly="operation === 'custom'"
            :busy="operation === 'custom'"
            :masked="!privateKeyRevealed"
            masked-message="Click to reveal private key"
            placeholder="-----BEGIN PRIVATE KEY-----"
            @reveal="privateKeyRevealed = true"
          />
        </FormGroup>
      </div>
    </BaseModal>

    <BaseModal
      v-model="showClone"
      :title="cloneModalTitle"
      max-width="max-w-2xl"
      :confirm-text="cloneConfirmText"
      :loading="operation === 'clone'"
      :prevent-dismiss="operation === 'clone'"
      :confirm-disabled="selectedCloneId === null || loadingCloneable"
      @submit="connectClonedCertificate"
    >
      <ErrorAlert :message="errorMessage" />
      <div v-if="loadingCloneable" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
        Loading compatible certificates…
      </div>
      <div v-else-if="cloneableCertificates.length === 0" class="py-10 text-center">
        <p class="text-sm font-medium text-gray-800 dark:text-gray-200">No compatible certificates found</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">A custom certificate must be unexpired and cover {{ normalizedDomain }}.</p>
      </div>
      <div v-else class="space-y-2">
        <label
          v-for="certificate in cloneableCertificates"
          :key="certificate.id"
          class="flex cursor-pointer items-start gap-3 rounded-lg border p-4 transition-colors"
          :class="selectedCloneId === certificate.id
            ? 'border-blue-500 bg-blue-50 dark:border-blue-400 dark:bg-blue-950/30'
            : 'border-gray-200 hover:border-gray-300 dark:border-gray-700 dark:hover:border-gray-600'"
        >
          <input v-model="selectedCloneId" type="radio" name="panel-clone-certificate" :value="certificate.id" class="mt-1 h-4 w-4 shrink-0 border-gray-300 text-blue-600 focus:ring-blue-500" />
          <span class="min-w-0 flex-1">
            <span class="flex flex-wrap items-center gap-2">
              <span class="break-all text-sm font-semibold text-gray-900 dark:text-gray-100">{{ certificate.site_domain }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ providerLabel(certificate.provider) }}</span>
            </span>
            <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
              {{ certificate.issuer || 'Unknown issuer' }} · expires {{ formatDate(certificate.expires_at) }}
            </span>
            <span class="mt-2 flex flex-wrap gap-1.5">
              <span v-for="name in certificate.domains" :key="name" class="break-all rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">{{ name }}</span>
            </span>
          </span>
        </label>
      </div>
    </BaseModal>
  </Card>
</template>

<script setup lang="ts">
import { computed, onActivated, onMounted, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AppButton from './AppButton.vue';
import BaseModal from './BaseModal.vue';
import Card from './Card.vue';
import ErrorAlert from './ErrorAlert.vue';
import FormGroup from './FormGroup.vue';
import ScriptEditor from './ScriptEditor.vue';
import SkeletonLoader from './SkeletonLoader.vue';
import StatusBadge from './StatusBadge.vue';

interface PanelDomainState {
  domain: string;
  url: string;
  ssl_provider: string;
  ssl_active: boolean;
  expires_at: string;
  status: 'not_configured' | 'active' | 'error';
  status_error?: string;
  direct_access_preserved: boolean;
}

interface CloneableCertificate {
  id: number;
  site_domain: string;
  provider: string;
  domains: string[];
  expires_at: string;
  issuer: string;
}

const emptyPanel = (): PanelDomainState => ({
  domain: '', url: '', ssl_provider: '', ssl_active: false, expires_at: '',
  status: 'not_configured', direct_access_preserved: true,
});

const panel = ref<PanelDomainState>(emptyPanel());
const domain = ref('');
const loading = ref(true);
const loaded = ref(false);
const domainTouched = ref(false);
const operation = ref('');
const errorMessage = ref('');
const showConnect = ref(false);
const showCustom = ref(false);
const showClone = ref(false);
const customCertificate = ref('');
const customPrivateKey = ref('');
const privateKeyRevealed = ref(false);
const loadingCloneable = ref(false);
const cloneableCertificates = ref<CloneableCertificate[]>([]);
const selectedCloneId = ref<number | null>(null);

const { confirm } = useConfirm();
const { showToast, updateToast, warning, error: showErrorToast } = useToast();

let refreshVersion = 0;
let initialActivation = true;

const busy = computed(() => operation.value !== '');
const normalizedDomain = computed(() => domain.value.trim().toLowerCase().replace(/\.$/, ''));
const domainValidationError = computed(() => {
  const value = normalizedDomain.value;
  if (!value) return '';
  const labels = value.split('.');
  const validHostname = labels.length >= 2 && labels.every(label => (
    /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label)
  ));
  if (!validHostname) return 'Enter a valid hostname such as panel.example.com.';
  const isIPv4 = labels.length === 4 && labels.every(label => /^\d{1,3}$/.test(label) && Number(label) <= 255);
  if (isIPv4) return 'Use a hostname instead of an IP address.';
  return '';
});
const expiringSoon = computed(() => {
  if (panel.value.ssl_provider === 'letsencrypt' || !panel.value.expires_at) return false;
  const expiry = Date.parse(panel.value.expires_at);
  return Number.isFinite(expiry) && expiry - Date.now() <= 30 * 24 * 60 * 60 * 1000;
});
const certificateNeedsAttention = computed(() => panel.value.status === 'error' || expiringSoon.value);
const statusLabel = computed(() => {
  if (!loaded.value) return 'Unavailable';
  if (panel.value.status === 'error') return 'Needs attention';
  if (expiringSoon.value) return 'Expiring soon';
  if (panel.value.status === 'active') return 'Active';
  return 'Not configured';
});
const statusVariant = computed<'green' | 'red' | 'yellow' | 'gray'>(() => {
  if (!loaded.value) return 'red';
  if (panel.value.status === 'error') return 'red';
  if (expiringSoon.value) return 'yellow';
  if (panel.value.status === 'active') return 'green';
  return 'gray';
});
const attentionTitle = computed(() => {
  if (expiringSoon.value) return 'Certificate expires soon';
  return panel.value.ssl_provider === 'letsencrypt' ? 'Panel SSL needs repair' : 'Certificate needs replacement';
});
const attentionMessage = computed(() => {
  if (expiringSoon.value) return `Replace this certificate before ${formatDate(panel.value.expires_at)}.`;
  if (panel.value.ssl_provider === 'letsencrypt') return 'Fluxo can revalidate the hostname and repair its Certbot certificate and Nginx proxy.';
  return 'Install a valid replacement certificate to restore trusted panel access.';
});
const repairActionLabel = computed(() => panel.value.ssl_provider === 'letsencrypt' ? 'Repair SSL' : 'Replace Certificate');
const customModalTitle = computed(() => panel.value.domain ? 'Replace Panel Certificate' : 'Use Existing Certificate');
const cloneModalTitle = computed(() => panel.value.domain ? 'Replace with Cloned Certificate' : 'Clone Certificate');
const customConfirmText = computed(() => panel.value.domain ? 'Replace Certificate' : 'Install and Connect');
const cloneConfirmText = computed(() => panel.value.domain ? 'Replace Certificate' : 'Clone and Connect');

const providerLabel = (provider: string) => {
  if (provider === 'letsencrypt') return "Let's Encrypt";
  if (provider === 'cloned') return 'Cloned';
  if (provider === 'custom') return 'Custom';
  return 'Not configured';
};

const formatDate = (value: string) => value ? new Date(value).toLocaleDateString() : '';

const refresh = async (silent = false) => {
  const request = ++refreshVersion;
  if (!silent) loading.value = true;
  try {
    const response = await apiClient.getPanelDomain(true);
    if (request !== refreshVersion) return;
    panel.value = { ...emptyPanel(), ...response };
    domain.value = panel.value.domain;
    loaded.value = true;
    errorMessage.value = '';
  } catch (error: any) {
    if (request === refreshVersion && !silent) {
      loaded.value = false;
      errorMessage.value = error.message || 'Failed to load the panel domain.';
    }
  } finally {
    if (request === refreshVersion) loading.value = false;
  }
};

const operationSucceeded = async (response: any) => {
  if (response?.status) {
    panel.value = { ...emptyPanel(), ...response };
    domain.value = panel.value.domain;
  }
  if (response?.warning) warning(response.warning);
  await refresh(true);
};

const openConnect = () => {
  domainTouched.value = true;
  if (!normalizedDomain.value || domainValidationError.value) return;
  errorMessage.value = '';
  showConnect.value = true;
};

const chooseLetsEncrypt = async () => {
  showConnect.value = false;
  await connectLetsEncrypt();
};

const chooseCustomCertificate = () => {
  showConnect.value = false;
  openCustomCertificate();
};

const chooseCloneCertificate = async () => {
  showConnect.value = false;
  await openCloneCertificate();
};

const connectLetsEncrypt = async () => {
  if (!normalizedDomain.value || domainValidationError.value) return;
  const repairing = panel.value.domain !== '' && panel.value.ssl_provider === 'letsencrypt';
  const approved = await confirm({
    title: repairing ? 'Repair Panel SSL' : 'Connect Panel Domain',
    message: repairing
      ? `Fluxo will revalidate ${normalizedDomain.value}, repair its Let's Encrypt certificate, and reactivate the Nginx proxy.`
      : `Fluxo will validate ${normalizedDomain.value}, issue a Let's Encrypt certificate, and activate its Nginx proxy. Direct IP access on the existing dashboard port will remain available.`,
    confirmText: repairing ? 'Repair SSL' : 'Connect Domain',
    cancelText: 'Cancel',
    variant: 'info',
  });
  if (!approved) return;
  operation.value = 'letsencrypt';
  errorMessage.value = '';
  const toastId = showToast({
    title: repairing ? 'Repairing panel SSL' : 'Connecting panel domain',
    description: repairing
      ? `Revalidating ${normalizedDomain.value} and repairing its Nginx proxy.`
      : `Validating ${normalizedDomain.value} and issuing its certificate. This may take a few minutes.`,
    type: 'loading',
  });
  try {
    const response = await apiClient.connectPanelDomainLetsEncrypt(normalizedDomain.value);
    await operationSucceeded(response);
    updateToast(toastId, {
      title: repairing ? 'Panel SSL repaired' : 'Panel domain connected',
      description: normalizedDomain.value,
      type: 'success',
    });
  } catch (error: any) {
    errorMessage.value = error.message || 'Failed to connect the panel domain.';
    updateToast(toastId, {
      title: repairing ? 'Panel SSL repair failed' : 'Panel domain connection failed',
      description: errorMessage.value,
      type: 'error',
    });
  } finally {
    operation.value = '';
  }
};

const openCustomCertificate = () => {
  errorMessage.value = '';
  showCustom.value = true;
};

const connectCustomCertificate = async () => {
  if (!customCertificate.value.trim() || !customPrivateKey.value.trim()) return;
  const replacing = Boolean(panel.value.domain);
  operation.value = 'custom';
  errorMessage.value = '';
  const toastId = showToast({
    title: replacing ? 'Replacing panel certificate' : 'Installing panel certificate',
    description: `Validating the certificate for ${normalizedDomain.value}.`,
    type: 'loading',
  });
  try {
    const response = await apiClient.connectPanelDomainCustom(normalizedDomain.value, customCertificate.value, customPrivateKey.value);
    showCustom.value = false;
    customCertificate.value = '';
    customPrivateKey.value = '';
    privateKeyRevealed.value = false;
    await operationSucceeded(response);
    updateToast(toastId, {
      title: replacing ? 'Panel certificate replaced' : 'Panel domain connected',
      description: normalizedDomain.value,
      type: 'success',
    });
  } catch (error: any) {
    errorMessage.value = error.message || 'Failed to install the panel certificate.';
    updateToast(toastId, {
      title: replacing ? 'Certificate replacement failed' : 'Certificate installation failed',
      description: errorMessage.value,
      type: 'error',
    });
  } finally {
    operation.value = '';
  }
};

const openCloneCertificate = async () => {
  showClone.value = true;
  selectedCloneId.value = null;
  cloneableCertificates.value = [];
  loadingCloneable.value = true;
  errorMessage.value = '';
  try {
    cloneableCertificates.value = await apiClient.getPanelCloneableCertificates(normalizedDomain.value, true) || [];
  } catch (error: any) {
    showClone.value = false;
    errorMessage.value = error.message || 'Failed to load compatible certificates.';
    showErrorToast('Compatible certificates could not be loaded', { description: errorMessage.value });
  } finally {
    loadingCloneable.value = false;
  }
};

const connectClonedCertificate = async () => {
  if (selectedCloneId.value === null) return;
  const replacing = Boolean(panel.value.domain);
  operation.value = 'clone';
  errorMessage.value = '';
  const toastId = showToast({
    title: replacing ? 'Replacing panel certificate' : 'Cloning panel certificate',
    description: `Creating an independent certificate copy for ${normalizedDomain.value}.`,
    type: 'loading',
  });
  try {
    const response = await apiClient.connectPanelDomainClone(normalizedDomain.value, selectedCloneId.value);
    showClone.value = false;
    selectedCloneId.value = null;
    cloneableCertificates.value = [];
    await operationSucceeded(response);
    updateToast(toastId, {
      title: replacing ? 'Panel certificate replaced' : 'Panel domain connected',
      description: normalizedDomain.value,
      type: 'success',
    });
  } catch (error: any) {
    errorMessage.value = error.message || 'Failed to clone the panel certificate.';
    updateToast(toastId, {
      title: replacing ? 'Certificate replacement failed' : 'Certificate cloning failed',
      description: errorMessage.value,
      type: 'error',
    });
  } finally {
    operation.value = '';
  }
};

const repairCertificate = async () => {
  if (panel.value.ssl_provider === 'letsencrypt') {
    await connectLetsEncrypt();
    return;
  }
  if (panel.value.ssl_provider === 'custom') {
    openCustomCertificate();
    return;
  }
  if (panel.value.ssl_provider === 'cloned') {
    await openCloneCertificate();
  }
};

const removeDomain = async () => {
  const approved = await confirm({
    title: 'Remove Panel Domain',
    message: `Remove ${panel.value.domain} from Fluxo? The server IP and existing dashboard port will remain available for access.`,
    confirmText: 'Remove Domain',
    cancelText: 'Cancel',
    variant: 'danger',
  });
  if (!approved) return;
  operation.value = 'remove';
  errorMessage.value = '';
  const removedDomain = panel.value.domain;
  const toastId = showToast({
    title: 'Removing panel domain',
    description: `Disabling the managed proxy for ${removedDomain}.`,
    type: 'loading',
  });
  try {
    const response = await apiClient.removePanelDomain();
    await operationSucceeded(response);
    domainTouched.value = false;
    updateToast(toastId, {
      title: 'Panel domain removed',
      description: 'Direct dashboard access remains available.',
      type: 'success',
    });
  } catch (error: any) {
    errorMessage.value = error.message || 'Failed to remove the panel domain.';
    updateToast(toastId, {
      title: 'Panel domain removal failed',
      description: errorMessage.value,
      type: 'error',
    });
  } finally {
    operation.value = '';
  }
};

watch(showCustom, (isOpen) => {
  if (isOpen) {
    privateKeyRevealed.value = false;
    return;
  }
  if (operation.value !== 'custom') {
    customCertificate.value = '';
    customPrivateKey.value = '';
    privateKeyRevealed.value = false;
    errorMessage.value = '';
  }
});

watch(showClone, (isOpen) => {
  if (!isOpen && operation.value !== 'clone') {
    selectedCloneId.value = null;
    cloneableCertificates.value = [];
    errorMessage.value = '';
  }
});

onMounted(() => void refresh());
onActivated(() => {
  if (initialActivation) {
    initialActivation = false;
    return;
  }
  void refresh(true);
});
</script>
