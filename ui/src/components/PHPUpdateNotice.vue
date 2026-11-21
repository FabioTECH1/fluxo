<template>
  <section v-if="notices.length" aria-label="PHP updates" class="mx-auto w-full max-w-6xl px-6 pt-3">
    <UpdateNotice
      :message="`PHP updates are available for ${notices.map(b => b.version).join(', ')}.`"
      to="/runtime/php"
      action-label="Review updates"
      dismiss-label="Dismiss PHP update notifications"
      :dismissing="dismissing"
      @dismiss="dismissAll"
    />
  </section>
  <section v-if="updates.state.value.failures.length" aria-label="PHP update maintenance">
    <div class="mx-auto max-w-6xl px-6 pt-3 space-y-3">
      <PersistentFailureBanner
        v-for="failure in updates.state.value.failures"
        :key="failure.id"
        :title="failure.kind.startsWith('repository_') ? 'PHP repository setup needs attention.' : `PHP ${failure.version} update needs attention.`"
        :reason="failure.message || 'The operation needs attention. Review recovery on the PHP runtime page.'"
        :output="failure.output || failure.message"
        modal-title="PHP maintenance details"
        :dismissible="false"
      >
        <template #actions>
          <AppButton v-if="!isCurrentDestination('/runtime/php')" to="/runtime/php" variant="secondary" size="sm">Open PHP</AppButton>
        </template>
        <template #modal-actions="{ close }">
          <AppButton v-if="updates.failureAction(failure)" size="sm" :disabled="acting || updates.busy.value || !!updates.repairReviewRequest.value || (updates.failureAction(failure) === 'check' && updates.locked.value)" @click="actOnFailure(failure, close)">{{ updates.failureAction(failure) === 'repair' ? 'Review repair' : 'Check for updates' }}</AppButton>
          <AppButton v-if="!isCurrentDestination('/runtime/php')" to="/runtime/php" variant="secondary" size="sm">Open PHP</AppButton>
        </template>
      </PersistentFailureBanner>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useNotificationNavigation } from '../composables/useNotificationNavigation';
import { phpUpdateError, usePHPUpdates, type PHPUpdateOperation } from '../composables/usePHPUpdates';
import { useToast } from '../composables/useToast';
import PersistentFailureBanner from './PersistentFailureBanner.vue';
import AppButton from './AppButton.vue';
import UpdateNotice from './UpdateNotice.vue';
const { isCurrentDestination } = useNotificationNavigation();
const updates = usePHPUpdates();
const router = useRouter();
const acting = ref(false);
const notices = computed(() => updates.state.value.branches.filter(b => b.notice && b.update_available));
const { showToast } = useToast();
const dismissing = ref(false);
async function actOnFailure(failure: PHPUpdateOperation, close: () => void) {
  const action = updates.failureAction(failure);
  if (!action || acting.value || updates.busy.value || updates.repairReviewRequest.value || (action === 'check' && updates.locked.value)) return;
  acting.value = true;
  try {
    if (action === 'check') await updates.check(failure.version);
    else {
      if (!isCurrentDestination('/runtime/php')) await router.push('/runtime/php');
      if (!isCurrentDestination('/runtime/php') || !updates.requestRepairReview(failure.id, close)) throw new Error('Recovery state changed. Reopen the error details to review the current attempt.');
      return;
    }
    close();
  } catch (error) { showToast({ type: 'error', title: 'Couldn’t start the next step', description: phpUpdateError(error) }); }
  finally { acting.value = false; }
}
async function dismissAll() {
  if (dismissing.value) return;
  dismissing.value = true;
  try { for (const branch of [...notices.value]) await updates.dismiss(branch); }
  catch (error) { showToast({ type: 'error', title: 'Couldn’t dismiss PHP update notice', description: phpUpdateError(error) }); }
  finally { dismissing.value = false; }
}
onMounted(updates.start);
onUnmounted(updates.stop);
</script>
