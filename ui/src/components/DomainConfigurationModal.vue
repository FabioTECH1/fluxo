<template>
  <BaseModal
    v-model="visible"
    :title="title"
    max-width="max-w-lg"
    :loading="loading"
    :confirm-disabled="!normalizedDomain || (showWildcard && !managed)"
    :confirm-text="confirmText"
    @submit="save"
  >
    <div>
      <h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100">WWW redirects</h4>

      <div v-if="!normalizedDomain" class="mt-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800/50 dark:text-gray-400">
        Enter a domain before configuring its www redirect behavior.
      </div>

      <div v-else-if="supportsWWW" class="mt-3 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
        <label
          v-for="option in options"
          :key="option.value"
          class="flex cursor-pointer items-center gap-3 border-b border-gray-200 px-4 py-3 last:border-b-0 dark:border-gray-700"
          :class="draft === option.value ? 'bg-blue-50 dark:bg-blue-900/20' : 'bg-white hover:bg-gray-50 dark:bg-gray-900 dark:hover:bg-gray-800'"
        >
          <input v-model="draft" type="radio" :disabled="loading || (showWildcard && !managed)" name="www-redirect" :value="option.value" class="h-4 w-4 text-blue-600 focus:ring-blue-500">
          <span class="flex min-w-0 flex-1 items-center justify-between gap-3">
            <span class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ option.label }}</span>
            <span v-if="option.recommended" class="rounded border border-green-500/50 px-2 py-0.5 text-xs font-semibold text-green-700 dark:text-green-400">Recommended</span>
          </span>
        </label>
      </div>

      <div v-else class="mt-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800/50 dark:text-gray-400">
        This hostname already starts with www, so no additional www redirect will be created.
      </div>
    </div>
    <div v-if="showWildcard" class="mt-5 border-t border-gray-100 pt-5 dark:border-gray-800">
      <ToggleSwitch v-model="draftWildcard" :disabled="!managed || loading" label="Allow wildcard subdomains" label-position="left" description="Serve unmatched subdomains using this site." />
      <p v-if="!managed" class="mt-3 text-xs leading-5 text-gray-500 dark:text-gray-400">Restore the Fluxo default in <router-link :to="vhostTo" class="font-medium text-blue-600 dark:text-blue-400">Settings › Nginx Vhost</router-link> to manage wildcard subdomains.</p>
      <div v-else-if="draftWildcard" class="mt-3">
        <p class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ normalizedDomain }} · *.{{ normalizedDomain }}</p>
        <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">Existing subdomain sites keep priority. Configure wildcard DNS and manage certificates in <router-link :to="sslTo" class="font-medium text-blue-600 dark:text-blue-400" @click="visible = false">SSL</router-link>.</p>
      </div>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { classifyWWWDomain } from '../types/domain';
import type { WWWRedirectBehavior } from '../types/domain';
import BaseModal from './BaseModal.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const props = withDefaults(defineProps<{
  modelValue: boolean;
  domain: string;
  behavior: WWWRedirectBehavior;
  title?: string;
  confirmText?: string;
  loading?: boolean;
  wildcard?: boolean;
  managed?: boolean;
  showWildcard?: boolean;
  sslTo?: string;
  vhostTo?: string;
}>(), {
  title: 'Configure domain',
  confirmText: 'Save',
  loading: false,
  wildcard: false,
  managed: false,
  showWildcard: false,
  sslTo: '',
  vhostTo: '',
});

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  save: [behavior: WWWRedirectBehavior, wildcard: boolean];
}>();

const visible = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value),
});
const normalizedDomain = computed(() => props.domain.trim().toLowerCase().replace(/^\*\./, ''));
const supportsWWW = computed(() => Boolean(normalizedDomain.value) && !normalizedDomain.value.startsWith('www.'));
const domainIsSubdomain = ref(false);
const classifyingDomain = ref(false);
let classificationRequest = 0;
const draft = ref<WWWRedirectBehavior>(props.behavior);
const draftWildcard = ref(false);
watch(() => [props.modelValue, props.wildcard, props.managed] as const, ([open, wildcard, managed]) => {
  if (open) draftWildcard.value = managed && wildcard;
}, { immediate: true });
const options = computed<Array<{ value: WWWRedirectBehavior; label: string; recommended?: boolean }>>(() => [
  { value: 'from_www', label: 'Redirect from www.', recommended: !classifyingDomain.value && !domainIsSubdomain.value },
  { value: 'to_www', label: 'Redirect to www.' },
  { value: 'none', label: 'No redirect', recommended: !classifyingDomain.value && domainIsSubdomain.value },
]);

watch(() => [props.modelValue, normalizedDomain.value] as const, async ([open, domain]) => {
  const requestID = ++classificationRequest;
  if (!open || !domain) {
    domainIsSubdomain.value = false;
    classifyingDomain.value = false;
    return;
  }
  classifyingDomain.value = true;
  try {
    const classification = await classifyWWWDomain(domain);
    if (requestID === classificationRequest) domainIsSubdomain.value = classification.isSubdomain;
  } catch {
    if (requestID === classificationRequest) domainIsSubdomain.value = false;
  } finally {
    if (requestID === classificationRequest) classifyingDomain.value = false;
  }
}, { immediate: true });

watch(() => [props.modelValue, props.behavior, normalizedDomain.value] as const, ([open, behavior]) => {
  if (!open) return;
  draft.value = supportsWWW.value ? behavior : 'none';
}, { immediate: true });

const save = () => emit('save', supportsWWW.value ? draft.value : 'none', props.managed && draftWildcard.value);
</script>
