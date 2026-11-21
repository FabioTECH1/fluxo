<template>
  <section v-if="updates.state.value.snapshot.notice && updates.state.value.snapshot.update_available && !attention" aria-label="Node.js updates" class="mx-auto w-full max-w-6xl px-6 pt-3">
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
          <AppButton v-if="!isCurrentDestination('/runtime/node')" to="/runtime/node" variant="secondary" size="sm">Open Node.js</AppButton>
        </template>
        <template #modal-actions="{ close }">
          <AppButton v-if="updates.failureAction(attention)" size="sm" :disabled="acting || updates.busy.value || !!updates.repairReviewRequest.value || (updates.failureAction(attention) === 'check' && updates.locked.value)" @click="actOnFailure(attention, close)">{{ updates.failureAction(attention) === 'repair' ? 'Review repair' : 'Check for updates' }}</AppButton>
          <AppButton v-if="!isCurrentDestination('/runtime/node')" to="/runtime/node" variant="secondary" size="sm">Open Node.js</AppButton>
        </template>
      </PersistentFailureBanner>
    </div>
  </section>
  <BaseModal :model-value="!!updates.detailsOperation.value" title="Node.js maintenance details" @update:model-value="value => { if (!value) updates.closeDetails() }">
    <p class="whitespace-pre-wrap break-words text-sm text-gray-700 dark:text-gray-300">{{ updates.detailsOperation.value?.message || 'No additional details were recorded.' }}</p>
    <template #footer>
      <AppButton variant="secondary" @click="updates.closeDetails()">Close</AppButton>
      <AppButton v-if="updates.detailsOperation.value && updates.failureAction(updates.detailsOperation.value)" :disabled="acting || updates.busy.value || !!updates.repairReviewRequest.value || (updates.failureAction(updates.detailsOperation.value) === 'check' && updates.locked.value)" @click="actOnFailure(updates.detailsOperation.value, updates.closeDetails)">{{ updates.failureAction(updates.detailsOperation.value) === 'repair' ? 'Review repair' : 'Check for updates' }}</AppButton>
      <AppButton v-if="!isCurrentDestination('/runtime/node')" to="/runtime/node" @click="updates.closeDetails()">Open Node.js</AppButton>
    </template>
  </BaseModal>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useNotificationNavigation } from '../composables/useNotificationNavigation';
import { nodeUpdateError, useNodeUpdates } from '../composables/useNodeUpdates';
import { useToast } from '../composables/useToast';
import PersistentFailureBanner from './PersistentFailureBanner.vue';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
import UpdateNotice from './UpdateNotice.vue';
const { isCurrentDestination } = useNotificationNavigation();
const updates = useNodeUpdates();
const router = useRouter();
const acting = ref(false);
const { showToast } = useToast();
const attention = updates.attention;
const dismissing = ref(false);
async function actOnFailure(operation: { id: string; kind?: string; status?: string }, close: () => void) {
  const action = updates.failureAction(operation);
  if (!action || acting.value || updates.busy.value || updates.repairReviewRequest.value || (action === 'check' && updates.locked.value)) return;
  acting.value = true;
  try {
    if (action === 'check') await updates.check();
    else {
      if (!isCurrentDestination('/runtime/node')) await router.push('/runtime/node');
      if (!isCurrentDestination('/runtime/node') || !updates.requestRepairReview(operation.id, close)) throw new Error('Recovery state changed. Reopen the error details to review the current attempt.');
      return;
    }
    close();
  } catch (error) { showToast({ type: 'error', title: 'Couldn’t start the next step', description: nodeUpdateError(error) }); }
  finally { acting.value = false; }
}
async function dismiss() {
  if (dismissing.value) return;
  dismissing.value = true;
  try { await updates.dismiss(); }
  catch (e) { showToast({ type: 'error', title: 'Couldn’t dismiss Node.js notice', description: nodeUpdateError(e) }); }
  finally { dismissing.value = false; }
}
onMounted(updates.start); onUnmounted(updates.stop);
</script>
