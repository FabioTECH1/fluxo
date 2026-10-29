<template>
  <section v-if="updates.state.value.snapshot.notice && !attention" class="border-b border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950/35" role="status">
    <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-6 py-3">
      <p class="flex-1 text-sm font-semibold text-blue-950 dark:text-blue-100">Node.js {{ updates.state.value.snapshot.candidate?.version }} is available.</p>
      <RouterLink to="/runtime/node" class="text-sm font-semibold text-blue-700 hover:underline dark:text-blue-300">Review update</RouterLink>
      <button type="button" aria-label="Dismiss Node.js update notification" class="rounded px-2 text-blue-700 dark:text-blue-300" @click="dismiss">Dismiss</button>
    </div>
  </section>
  <section v-if="attention" class="border-b border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/35">
    <div class="mx-auto max-w-6xl px-6 py-3">
      <ErrorAlert :key="attention.id" class="mb-0" message="Node.js maintenance needs attention." :details="attention.message || 'The operation needs attention. Review recovery on the Node.js runtime page.'" />
      <RouterLink to="/runtime/node" class="mt-2 inline-block text-sm font-semibold text-red-700 hover:underline dark:text-red-300">Open Node.js recovery</RouterLink>
    </div>
  </section>
  <BaseModal :model-value="!!updates.detailsOperation.value" title="Node.js maintenance details" @update:model-value="value => { if (!value) updates.closeDetails() }">
    <p class="whitespace-pre-wrap break-words text-sm text-gray-700 dark:text-gray-300">{{ updates.detailsOperation.value?.message || 'No additional details were recorded.' }}</p>
    <template #footer>
      <AppButton variant="secondary" @click="updates.closeDetails()">Close</AppButton>
      <AppButton to="/runtime/node" @click="updates.closeDetails()">Open Node.js</AppButton>
    </template>
  </BaseModal>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';
import { nodeUpdateError, useNodeUpdates } from '../composables/useNodeUpdates';
import { useToast } from '../composables/useToast';
import ErrorAlert from './ErrorAlert.vue';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
const updates = useNodeUpdates();
const { showToast } = useToast();
const attention = updates.attention;
async function dismiss() { try { await updates.dismiss(); } catch (e) { showToast({ type: 'error', title: 'Couldn’t dismiss Node.js notice', description: nodeUpdateError(e) }); } }
onMounted(updates.start); onUnmounted(updates.stop);
</script>
