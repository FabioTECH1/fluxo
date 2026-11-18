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
          <AppButton v-if="$route.path !== '/runtime/php'" to="/runtime/php" variant="secondary" size="sm">Open PHP</AppButton>
        </template>
        <template #modal-actions>
          <AppButton v-if="$route.path !== '/runtime/php'" to="/runtime/php" size="sm">Open PHP</AppButton>
        </template>
      </PersistentFailureBanner>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { phpUpdateError, usePHPUpdates } from '../composables/usePHPUpdates';
import { useToast } from '../composables/useToast';
import PersistentFailureBanner from './PersistentFailureBanner.vue';
import AppButton from './AppButton.vue';
import UpdateNotice from './UpdateNotice.vue';
const updates = usePHPUpdates();
const notices = computed(() => updates.state.value.branches.filter(b => b.notice));
const { showToast } = useToast();
const dismissing = ref(false);
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
