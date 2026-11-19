<template>
  <Card>
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div class="flex min-w-0 items-center gap-3">
        <div class="rounded-lg bg-gray-50 p-2 dark:bg-gray-800"><svg class="w-6 h-6 text-gray-700 dark:text-gray-300" fill="currentColor" viewBox="0 0 24 24">
                <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.603-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.579.688.481C19.138 20.162 22 16.418 22 12c0-5.523-4.477-10-10-10z" />
              </svg></div>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100 break-words">{{ account.name }}</h3>
            <StatusBadge :label="token.label" :variant="token.variant" />
          </div>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            <span v-if="account.username">@{{ account.username }} · </span>{{ account.linked_sites }} linked {{ account.linked_sites === 1 ? 'site' : 'sites' }}
          </p>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <AppButton variant="secondary" size="sm" :loading="checking" :disabled="busy" @click="$emit('check')">Check access</AppButton>
        <AppButton size="sm" :disabled="busy" @click="$emit('update')">{{ token.reconnect ? 'Reconnect' : 'Update token' }}</AppButton>
        <TableActionMenu :items="[{ id: 'disconnect', label: 'Disconnect', variant: 'danger' }]" :aria-label="`More actions for ${account.name}`" :disabled="busy" @select="$emit('disconnect')" />
      </div>
    </div>
    <p v-if="!account.github_user_id" class="mt-4 text-sm text-amber-700 dark:text-amber-300">This legacy connection has no verified GitHub account ID. Use Check access to verify its saved token. If GitHub rejects the old token, reconnect offers password-confirmed administrator recovery while preserving site links and keys.</p>
    <dl class="mt-5 grid grid-cols-1 gap-4 border-t border-gray-100 pt-4 dark:border-gray-800 sm:grid-cols-3">
      <div><dt class="text-xs text-gray-500 dark:text-gray-400">Connected</dt><dd class="mt-1 text-sm text-gray-700 dark:text-gray-300">{{ formatGitHubDate(account.created_at) }}</dd></div>
      <div><dt class="text-xs text-gray-500 dark:text-gray-400">Token expiry</dt><dd class="mt-1 text-sm text-gray-700 dark:text-gray-300">{{ formatGitHubDate(account.token_expires_at) }} <span v-if="token.remaining" class="text-xs" :class="token.variant === 'red' ? 'text-red-700 dark:text-red-300' : token.variant === 'yellow' ? 'text-yellow-700 dark:text-yellow-300' : 'text-gray-500 dark:text-gray-400'">· {{ token.remaining }}</span></dd></div>
      <div><dt class="text-xs text-gray-500 dark:text-gray-400">Deployment access</dt><dd class="mt-1 text-sm" :class="accessOK ? 'text-green-700 dark:text-green-300' : 'text-gray-500 dark:text-gray-400'">{{ deploymentAccessLabel(account) }}</dd><dd v-if="account.deployment_access?.checked_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">Checked {{ formatGitHubDate(account.deployment_access.checked_at) }}</dd></div>
    </dl>
  </Card>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import Card from './Card.vue';
import AppButton from './AppButton.vue';
import StatusBadge from './StatusBadge.vue';
import TableActionMenu from './TableActionMenu.vue';
import { type GitHubAccount, tokenPresentation, deploymentAccessLabel, formatGitHubDate } from '../utils/githubConnection';
const props = defineProps<{ account: GitHubAccount; now: number; busy: boolean; checking: boolean }>();
defineEmits<{ check: []; update: []; disconnect: [] }>();
const token = computed(() => tokenPresentation(props.account, props.now));
const accessOK = computed(() => !!props.account.linked_sites && !!props.account.deployment_access?.checked_at && props.account.deployment_access.sites.length === props.account.linked_sites && props.account.deployment_access.sites.every(site => site.status === 'verified'));
</script>
