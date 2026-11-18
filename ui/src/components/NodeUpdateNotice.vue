<template>
  <section v-if="updates.state.value.snapshot.notice && !attention" aria-label="Node.js updates" class="mx-auto w-full max-w-6xl px-6 pt-3">
    <UpdateNotice
      :message="`Node.js ${updates.state.value.snapshot.candidate?.version || 'update'} is available.`"
      to="/runtime/node"
      action-label="Review update"
      dismiss-label="Dismiss Node.js update notification"
      :dismissing="dismissing"
      @dismiss="dismiss"
    />
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
import { onMounted, onUnmounted, ref } from 'vue';
import { nodeUpdateError, useNodeUpdates } from '../composables/useNodeUpdates';
import { useToast } from '../composables/useToast';
import PersistentFailureBanner from './PersistentFailureBanner.vue';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
import UpdateNotice from './UpdateNotice.vue';
const updates = useNodeUpdates();
const { showToast } = useToast();
const attention = updates.attention;
const dismissing = ref(false);
async function dismiss() {
  if (dismissing.value) return;
  dismissing.value = true;
  try { await updates.dismiss(); }
  catch (e) { showToast({ type: 'error', title: 'Couldn’t dismiss Node.js notice', description: nodeUpdateError(e) }); }
  finally { dismissing.value = false; }
}
onMounted(updates.start); onUnmounted(updates.stop);
</script>
