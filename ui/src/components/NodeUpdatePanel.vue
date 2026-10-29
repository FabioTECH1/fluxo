<template>
  <Card class="mt-6">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Node.js updates</h2><p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Maintain the installed major version. Updates run only after your approval.</p></div>
      <div class="flex flex-wrap gap-2"><AppButton variant="secondary" size="sm" :disabled="updates.busy.value || updates.locked.value || reviewing" @click="check">Check for updates</AppButton><AppButton size="sm" :disabled="(!snapshot.update_available && !snapshot.migration_available) || updates.busy.value || updates.locked.value || reviewing" :loading="reviewing" @click="review">{{ snapshot.migration_available ? 'Review migration' : 'Update' }}</AppButton></div>
    </div>
    <ErrorAlert v-if="error" class="mt-4" :message="error" />
    <ErrorAlert v-if="snapshot.error" class="mt-4" :message="snapshot.error" />
    <ErrorAlert v-if="updates.attention.value" :key="updates.attention.value.id" class="mt-4" message="Node.js maintenance needs attention." :details="updates.attention.value.message" />
    <p class="mt-4 text-sm text-gray-700 dark:text-gray-300">{{ statusText }}</p>
    <p v-if="snapshot.checked_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">Last successful check: {{ date(snapshot.checked_at) }}. Checks run daily and do not install updates.</p>
    <p v-if="updates.state.value.active_operation" class="mt-4 text-sm font-medium text-blue-700 dark:text-blue-300" role="status">{{ updates.state.value.active_operation.message || 'Preparing maintenance…' }} You can leave this page.</p>
    <div v-for="generation in updates.state.value.recoveries" :key="generation.id" class="mt-5 border-t border-gray-100 pt-4 dark:border-gray-800">
      <p class="text-sm text-gray-700 dark:text-gray-300">{{ ['succeeded', 'rolled_back'].includes(generation.state) ? 'The previous release remains available for recovery.' : 'This maintenance attempt needs recovery. Conflicting changes remain paused.' }}</p>
      <div class="mt-3 flex flex-wrap gap-2"><AppButton v-if="!['succeeded', 'rolled_back'].includes(generation.state)" variant="secondary" size="sm" :disabled="updates.busy.value || reviewing" @click="recover(generation, 'repair')">Review repair</AppButton><AppButton variant="secondary" size="sm" :disabled="updates.busy.value || reviewing" @click="recover(generation, 'rollback')">Review rollback</AppButton></div>
    </div>

  </Card>
  <BaseModal :model-value="!!plan" :title="plan?.kind === 'migrate' ? 'Migrate Node.js' : plan?.kind === 'rollback' ? 'Roll back Node.js' : plan?.kind === 'repair' ? 'Repair Node.js' : 'Update Node.js'" :loading="submitting" :confirm-disabled="!!plan?.impact.blocked_reasons.length" :confirm-text="plan?.kind === 'migrate' ? 'Migrate and verify' : plan?.kind === 'rollback' ? 'Restore and verify' : plan?.kind === 'repair' ? 'Repair and verify' : 'Update and verify'" @update:model-value="v => { if (!v) plan = null }" @submit="apply">
    <template v-if="plan">
      <ErrorAlert v-if="error" class="mb-4" :message="error" />
      <p class="font-semibold text-gray-900 dark:text-gray-100">{{ versionSummary }}</p>
      <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ npmSummary }}.</p>
      <p class="mt-4 text-sm text-gray-700 dark:text-gray-300">Restart affected services. Brief downtime may occur. Stopped apps and workers stay stopped.</p>
      <div v-if="affectedDomains.length" class="mt-4">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Affected sites</h3>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ affectedDomains.join(', ') }}</p>
      </div>
      <div v-if="affectedServices.length" class="mt-4">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Affected services</h3>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ affectedServices.join(', ') }}</p>
      </div>
      <p v-if="plan.impact.scheduled_tasks?.length" class="mt-4 text-sm text-gray-600 dark:text-gray-400">{{ plan.impact.scheduled_tasks.length }} scheduled {{ plan.impact.scheduled_tasks.length === 1 ? 'job pauses' : 'jobs pause' }}. Running jobs finish first; missed launches are not replayed.</p>
      <p v-if="hasNaturalWait" class="mt-3 text-sm text-amber-800 dark:text-amber-300">Running Artisan tasks must finish naturally before maintenance can continue.</p>
      <p v-if="hasCompletedTasks" class="mt-3 text-sm text-gray-600 dark:text-gray-400">Completed tasks will not run again.</p>
      <p v-if="plan.migration && plan.kind !== 'rollback'" class="mt-3 text-sm text-gray-600 dark:text-gray-400">The APT installation stays available for rollback while its files remain unchanged.</p>
      <p v-if="plan.kind === 'rollback'" class="mt-3 text-sm text-amber-800 dark:text-amber-300">Older releases may contain vulnerabilities. Application files and databases are not restored.</p>
      <ErrorAlert v-for="reason in plan.impact.blocked_reasons" :key="reason" class="mt-3" :message="reason" />
    </template>
  </BaseModal>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import Card from './Card.vue'; import AppButton from './AppButton.vue'; import BaseModal from './BaseModal.vue'; import ErrorAlert from './ErrorAlert.vue';
import { nodeUpdateError, useNodeUpdates, type NodePlan, type NodeGeneration } from '../composables/useNodeUpdates';
import { createRequestID } from '../utils/requestID';
const emit = defineEmits<{ changed: [] }>();
const updates = useNodeUpdates(); const plan = ref<NodePlan | null>(null); const reviewing = ref(false); const submitting = ref(false); const error = ref(''); let requestID = '';
const snapshot = computed(() => updates.state.value.snapshot);
const statusText = computed(() => snapshot.value.migration_available ? `This older APT installation needs an approved migration to Node.js ${snapshot.value.candidate?.version} before Fluxo-managed updates are enabled.` : snapshot.value.update_available ? `Node.js ${snapshot.value.candidate?.version} is available for the installed ${snapshot.value.installed?.release?.version.split('.')[0]}.x line.` : snapshot.value.state === 'current' ? 'No newer release was found for the installed Node.js major.' : snapshot.value.state === 'check_failed' ? 'The last check failed. Check again after resolving the reported issue.' : snapshot.value.state === 'stale' ? 'The stored check is out of date. Check again before maintenance.' : 'Check for updates to verify this installation and its supported release line.');
const versionSummary = computed(() => {
  const p = plan.value; if (!p) return '';
  return p.migration ? p.kind === 'rollback' ? `Restore original APT Node.js ${p.migration.version}` : `APT Node.js ${p.migration.version} → Fluxo Node.js ${p.after.version}` : `Node.js ${p.before.release.version} → ${p.after.version}`;
});
const npmSummary = computed(() => {
  const p = plan.value; if (!p) return '';
  return p.migration && p.kind === 'rollback' ? `Restore original npm ${p.migration.npm}` : `Bundled npm: ${p.migration?.npm || p.before.release.npm || 'Unavailable'} → ${p.after.npm}`;
});
const affectedDomains = computed(() => [...new Set([
  ...(plan.value?.impact.units ?? []).map(unit => unit.domain || 'Site domain unavailable'),
  ...(plan.value?.impact.php_consumers ?? []).map(consumer => consumer.domain || 'Site domain unavailable'),
])]);
const restartingConsumers = computed(() => (plan.value?.impact.php_consumers ?? []).filter(consumer =>
  consumer.units.some(unit => unit.runtime.active && !plan.value?.impact.php_resumes?.[unit.runtime.name]?.completed)));
const affectedServices = computed(() => {
  const services = new Set<string>();
  for (const unit of plan.value?.impact.units ?? []) {
    if (unit.active) services.add(unit.bun_only ? 'Bun apps' : 'Node.js apps');
  }
  const labels: Record<string, string> = {
    laravel_queue: 'Queue workers', laravel_horizon: 'Horizon', laravel_nightwatch: 'Nightwatch',
    laravel_octane: 'Octane', artisan_natural: 'Artisan tasks',
  };
  for (const consumer of restartingConsumers.value) services.add(labels[consumer.consumer.protocol || consumer.consumer.kind] || 'PHP background processes');
  return [...services];
});
const hasNaturalWait = computed(() => restartingConsumers.value.some(consumer => consumer.units.some(unit =>
  unit.runtime.active && unit.runtime.natural && !plan.value?.impact.php_resumes?.[unit.runtime.name]?.completed)));
const hasCompletedTasks = computed(() => Object.values(plan.value?.impact.php_resumes ?? {}).some(receipt => receipt.completed));
const date = (unix: number) => new Date(unix * 1000).toLocaleString();
async function check() { error.value = ''; try { await updates.check(); } catch (e) { error.value = nodeUpdateError(e); } }
async function review() { reviewing.value = true; error.value = ''; try { plan.value = await updates.review(); requestID = createRequestID(); } catch (e) { error.value = nodeUpdateError(e); } finally { reviewing.value = false; } }
async function recover(g: NodeGeneration, kind: 'repair' | 'rollback') { reviewing.value = true; error.value = ''; try { plan.value = await updates.reviewRecovery(g, kind); requestID = createRequestID(); } catch (e) { error.value = nodeUpdateError(e); } finally { reviewing.value = false; } }
async function apply() { if (!plan.value) return; submitting.value = true; error.value = ''; try { await updates.apply(plan.value, requestID); plan.value = null; } catch (e) { error.value = nodeUpdateError(e); } finally { submitting.value = false; } }
watch(() => updates.state.value.active_operation, (current, previous) => { if (!current && previous && previous.kind !== 'check') emit('changed'); });
onMounted(() => { void updates.refresh().catch(e => { error.value = nodeUpdateError(e); }); });
</script>
