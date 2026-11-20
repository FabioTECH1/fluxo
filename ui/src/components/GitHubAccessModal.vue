<template>
  <BaseModal :model-value="modelValue" title="Deployment access" max-width="max-w-2xl" :loading="checking" :prevent-dismiss="busy" @update:model-value="$emit('update:modelValue', $event)">
    <div class="space-y-3" aria-live="polite">
      <div>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <h3 class="break-words text-sm font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</h3>
            <StatusBadge :label="token.label" :variant="token.variant" />
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ deploymentAccessLabel(account) }}<span v-if="account.deployment_access.checked_at"> · {{ formatGitHubDate(account.deployment_access.checked_at) }}</span></p>
        </div>
        <p class="mt-2 text-sm" :class="token.variant === 'red' ? 'text-red-700 dark:text-red-300' : account.token_status === 'error' ? 'text-yellow-700 dark:text-yellow-300' : 'text-gray-600 dark:text-gray-400'">{{ githubAccessNotice(account, now) }}</p>
      </div>
      <div>
        <div v-for="site in sites" :key="site.site_id" class="border-t border-gray-100 py-3 dark:border-gray-800">
          <div class="flex flex-wrap items-center gap-2">
            <span class="break-words text-sm font-semibold text-gray-900 dark:text-gray-100">{{ site.domain }}</span>
            <StatusBadge :label="site.presentation.label" :variant="site.presentation.variant" />
          </div>
          <p class="mt-0.5 break-words text-xs text-gray-500 dark:text-gray-400">{{ site.repository }}</p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ site.presentation.message }}</p>
        </div>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ account.token_status === 'error' || account.token_status === 'unchecked' ? 'Retry check validates the token and SSH access. It does not change keys or deploy.' : 'Token updates retry existing-key registration; deploy-key permissions are required.' }}</p>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap justify-end gap-2">
        <AppButton variant="secondary" :disabled="busy" @click="$emit('update:modelValue', false)">Close</AppButton>
        <AppButton :variant="canUpdate ? 'secondary' : 'primary'" :disabled="busy" :loading="checking" @click="$emit('retry')">Retry check</AppButton>
        <AppButton v-if="canUpdate" :disabled="busy" @click="$emit('update')">{{ githubTokenAction(account, now) }}</AppButton>
      </div>
    </template>
  </BaseModal>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
import StatusBadge from './StatusBadge.vue';
import { type GitHubAccount, tokenPresentation, deploymentAccessLabel, formatGitHubDate, githubAccessNotice, githubTokenAction, githubSiteAccessPresentation } from '../utils/githubConnection';

const props = defineProps<{ modelValue: boolean; account: GitHubAccount; now: number; busy: boolean; checking: boolean }>();
defineEmits<{ 'update:modelValue': [value: boolean]; retry: []; update: [] }>();
const token = computed(() => tokenPresentation(props.account, props.now));
const canUpdate = computed(() => token.value.reconnect || props.account.token_status === 'valid');
const sites = computed(() => props.account.deployment_access.sites.map(site => ({ ...site, presentation: githubSiteAccessPresentation(site) })));
</script>
