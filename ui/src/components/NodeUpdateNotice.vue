<template>
  <section v-if="updates.state.value.snapshot.notice" class="border-b border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950/35" role="status">
    <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-6 py-3">
      <p class="flex-1 text-sm font-semibold text-blue-950 dark:text-blue-100">Node.js {{ updates.state.value.snapshot.candidate?.version }} is available.</p>
      <RouterLink to="/runtime/node" class="text-sm font-semibold text-blue-700 hover:underline dark:text-blue-300">Review update</RouterLink>
      <button type="button" aria-label="Dismiss Node.js update notification" class="rounded px-2 text-blue-700 dark:text-blue-300" @click="dismiss">Dismiss</button>
    </div>
  </section>
  <section v-if="needsAttention" class="border-b border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/35" role="alert">
    <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-6 py-3"><p class="flex-1 text-sm font-semibold text-red-900 dark:text-red-200">Node.js maintenance needs attention.</p><RouterLink to="/runtime/node" class="text-sm font-semibold text-red-700 hover:underline dark:text-red-300">Review details</RouterLink></div>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import { nodeUpdateError, useNodeUpdates } from '../composables/useNodeUpdates';
import { useToast } from '../composables/useToast';
const updates = useNodeUpdates();
const { showToast } = useToast();
const needsAttention = computed(() => {
  const latest = updates.state.value.operations.find(o => o.kind !== 'check');
  return updates.state.value.recoveries.some(g => !['succeeded', 'rolled_back'].includes(g.state)) || !!latest && (['failed', 'interrupted', 'recovery_required'].includes(latest.status) || latest.kind === 'apply' && latest.status === 'rolled_back');
});
async function dismiss() { try { await updates.dismiss(); } catch (e) { showToast({ type: 'error', title: 'Couldn’t dismiss Node.js notice', description: nodeUpdateError(e) }); } }
onMounted(updates.start); onUnmounted(updates.stop);
</script>
