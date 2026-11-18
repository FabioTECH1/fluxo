<template>
  <section v-if="notices.length" class="border-b border-blue-200 bg-blue-50 dark:border-blue-900/70 dark:bg-blue-950/35" role="status">
    <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-6 py-3">
      <p class="flex-1 text-sm font-semibold text-blue-950 dark:text-blue-100">PHP updates are available for {{ notices.map(b => b.version).join(', ') }}.</p>
      <RouterLink to="/runtime/php" class="text-sm font-semibold text-blue-700 hover:underline dark:text-blue-300">Review updates</RouterLink>
      <button type="button" aria-label="Dismiss PHP update notifications" class="rounded-lg p-1.5 text-blue-500 transition-colors hover:bg-blue-100 hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:text-blue-400 dark:hover:bg-blue-900/60 dark:hover:text-blue-200" @click="dismissAll">
        <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
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
import { computed, onMounted, onUnmounted } from 'vue';
import { phpUpdateError, usePHPUpdates } from '../composables/usePHPUpdates';
import { useToast } from '../composables/useToast';
import PersistentFailureBanner from './PersistentFailureBanner.vue';
import AppButton from './AppButton.vue';
const updates = usePHPUpdates();
const notices = computed(() => updates.state.value.branches.filter(b => b.notice));
const { showToast } = useToast();
async function dismissAll() {
  try { for (const branch of notices.value) await updates.dismiss(branch); }
  catch (error) { showToast({ type: 'error', title: 'Couldn’t dismiss PHP update notice', description: phpUpdateError(error) }); }
}
onMounted(updates.start);
onUnmounted(updates.stop);
</script>
