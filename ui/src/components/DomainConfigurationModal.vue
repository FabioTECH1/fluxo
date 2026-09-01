<template>
  <BaseModal
    v-model="visible"
    :title="title"
    max-width="max-w-lg"
    :loading="loading"
    :confirm-disabled="!normalizedDomain"
    :confirm-text="confirmText"
    @submit="save"
  >
    <p class="text-sm text-gray-600 dark:text-gray-400">
      <span class="font-semibold text-gray-900 dark:text-gray-100">{{ normalizedDomain || 'This domain' }}</span>
      will be used to access your site and can be configured with redirect behavior.
    </p>

    <div class="mt-6">
      <h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Redirects</h4>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Manage how this domain handles its www. hostname.</p>

      <div v-if="!normalizedDomain" class="mt-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800/50 dark:text-gray-400">
        Enter a domain before configuring its www redirect behavior.
      </div>

      <div v-else-if="supportsWWW" class="mt-3 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
        <label
          v-for="option in options"
          :key="option.value"
          class="flex cursor-pointer items-center gap-3 border-b border-gray-200 px-4 py-4 last:border-b-0 dark:border-gray-700"
          :class="draft === option.value ? 'bg-blue-50 dark:bg-blue-900/20' : 'bg-white hover:bg-gray-50 dark:bg-gray-900 dark:hover:bg-gray-800'"
        >
          <input v-model="draft" type="radio" name="www-redirect" :value="option.value" class="h-4 w-4 text-blue-600 focus:ring-blue-500">
          <span class="flex min-w-0 flex-1 items-center justify-between gap-3">
            <span class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ option.label }}</span>
            <span v-if="option.recommended" class="rounded border border-green-500/50 px-2 py-0.5 text-xs font-semibold text-green-700 dark:text-green-400">Recommended</span>
          </span>
        </label>
      </div>

      <div v-else class="mt-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800/50 dark:text-gray-400">
        This hostname already starts with www, so no additional www redirect will be created.
      </div>

      <div v-if="supportsWWW && draft !== 'none'" class="mt-3 rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-xs leading-5 text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200">
        This creates <span class="font-mono font-semibold">www.{{ normalizedDomain }}</span>. Configure DNS for that hostname and use a certificate that covers both hostnames.
        <span v-if="domainIsSubdomain" class="mt-1 block">Because this is already a subdomain, <span class="font-semibold">No redirect</span> is usually the appropriate choice.</span>
      </div>

      <div v-else-if="supportsWWW && domainIsSubdomain" class="mt-3 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-xs leading-5 text-blue-800 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300">
        This subdomain will use only its exact hostname. No nested www DNS record is required.
      </div>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { classifyWWWDomain } from '../types/domain';
import type { WWWRedirectBehavior } from '../types/domain';
import BaseModal from './BaseModal.vue';

const props = withDefaults(defineProps<{
  modelValue: boolean;
  domain: string;
  behavior: WWWRedirectBehavior;
  title?: string;
  confirmText?: string;
  loading?: boolean;
}>(), {
  title: 'Configure domain',
  confirmText: 'Save',
  loading: false,
});

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  save: [behavior: WWWRedirectBehavior];
}>();

const visible = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value),
});
const normalizedDomain = computed(() => props.domain.trim().toLowerCase());
const supportsWWW = computed(() => Boolean(normalizedDomain.value) && !normalizedDomain.value.startsWith('www.'));
const domainIsSubdomain = ref(false);
const classifyingDomain = ref(false);
let classificationRequest = 0;
const draft = ref<WWWRedirectBehavior>(props.behavior);
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

const save = () => emit('save', supportsWWW.value ? draft.value : 'none');
</script>
