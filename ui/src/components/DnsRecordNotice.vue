<template>
  <div
    v-if="address"
    class="rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300"
  >
    <p>Point your domain to this server by adding an <strong>A record</strong> at your DNS provider to:</p>
    <div class="mt-1 flex items-center gap-2">
      <code class="rounded bg-blue-100 px-2 py-0.5 font-mono text-blue-900 dark:bg-blue-900/40 dark:text-blue-200">{{ address }}</code>
      <button
        type="button"
        class="rounded text-blue-500 transition-colors hover:text-blue-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-blue-400 dark:hover:text-blue-200"
        title="Copy IP address"
        aria-label="Copy server IP address"
        @click="copyAddress"
      >
        <ClipboardDocumentIcon class="h-4 w-4" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ClipboardDocumentIcon } from '@heroicons/vue/24/outline';
import { useToast } from '../composables/useToast';

const props = defineProps<{
  address: string;
}>();

const { success, error } = useToast();

const copyAddress = async () => {
  try {
    await navigator.clipboard.writeText(props.address);
    success('IP copied to clipboard');
  } catch {
    error('IP address could not be copied');
  }
};
</script>
