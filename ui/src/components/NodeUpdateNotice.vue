<template>
  <section v-if="updates.state.value.snapshot.notice && !attention" class="border-b border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950/35" role="status">
    <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-6 py-3">
      <p class="flex-1 text-sm font-semibold text-blue-950 dark:text-blue-100">Node.js {{ updates.state.value.snapshot.candidate?.version }} is available.</p>
      <RouterLink to="/runtime/node" class="text-sm font-semibold text-blue-700 hover:underline dark:text-blue-300">Review update</RouterLink>
      <button type="button" aria-label="Dismiss Node.js update notification" class="rounded px-2 text-blue-700 dark:text-blue-300" @click="dismiss">Dismiss</button>
    </div>
  </section>
  <section v-if="attention" aria-label="Node.js maintenance">
    <div class="mx-auto max-w-6xl px-6 pt-3">
      <PersistentFailureBanner
        :key="attention.id"
        title="Node.js maintenance needs attention."
        :reason="attention.message || 'The operation needs attention. Review recovery on the Node.js runtime page.'"
        :output="attention.message"
        modal-title="Node.js maintenance details"
        :dismissible="false"
      >
        <template #actions>
          <AppButton v-if="$route.path !== '/runtime/node'" to="/runtime/node" variant="secondary" size="sm">Open Node.js</AppButton>
        </template>
        <template #modal-actions>
          <AppButton v-if="$route.path !== '/runtime/node'" to="/runtime/node" size="sm">Open Node.js</AppButton>
        </template>
      </PersistentFailureBanner>
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
import PersistentFailureBanner from './PersistentFailureBanner.vue';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
const updates = useNodeUpdates();
const { showToast } = useToast();
const attention = updates.attention;
async function dismiss() { try { await updates.dismiss(); } catch (e) { showToast({ type: 'error', title: 'Couldn’t dismiss Node.js notice', description: nodeUpdateError(e) }); } }
onMounted(updates.start); onUnmounted(updates.stop);
</script>
