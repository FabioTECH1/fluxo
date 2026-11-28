<template>
  <Card class="mt-6">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Node.js updates</h2>
      <div class="flex flex-wrap gap-2"><AppButton variant="secondary" :disabled="updates.busy.value || updates.locked.value || reviewing" @click="check">Check for updates</AppButton><AppButton :disabled="!canReview || updates.busy.value || updates.locked.value || reviewing" :loading="reviewing" @click="review()">{{ snapshot.migration_available ? 'Review migration' : 'Update' }}</AppButton></div>
    </div>
    <ErrorAlert v-if="error" class="mt-4" :message="error" />
    <ErrorAlert v-if="updates.attention.value" class="mt-4" message="Node.js maintenance needs attention." :details="updates.attention.value.message" />
    <div class="mt-5 space-y-3" role="status" aria-live="polite">
      <template v-if="canReview"><dl class="flex flex-wrap items-center gap-5"><div><dt class="text-sm text-gray-500 dark:text-gray-400">Installed</dt><dd class="mt-1 font-mono text-base text-gray-900 dark:text-gray-100">{{ installedVersion }}</dd></div><span class="text-gray-400" aria-label="to">→</span><div><dt class="text-sm text-gray-500 dark:text-gray-400">Available</dt><dd class="mt-1 font-mono text-base text-gray-900 dark:text-gray-100">{{ snapshot.candidate?.version }}</dd></div></dl><button type="button" class="rounded-sm text-sm font-medium text-blue-600 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-blue-400" @click="detailsOpen=true">Show details</button></template>
      <div v-else-if="upToDate" class="flex flex-wrap items-center gap-3"><span class="font-mono text-base text-gray-900 dark:text-gray-100">{{ installedVersion }}</span><StatusBadge label="Up to date" variant="green" /></div>
      <p v-else class="text-sm text-gray-700 dark:text-gray-300">{{ statusText }}</p>
      <p v-if="snapshot.checked_at" class="text-sm text-gray-500 dark:text-gray-400">Last successful check <time>{{ date(snapshot.checked_at) }}</time></p>
    </div>
    <p v-if="updates.state.value.active_operation" class="mt-4 text-sm font-medium text-blue-700 dark:text-blue-300" role="status">{{ updates.state.value.active_operation.message || 'Preparing maintenance…' }}</p>
    <div v-for="generation in updates.state.value.recoveries.filter(g=>!['succeeded','rolled_back'].includes(g.state))" :key="generation.id" class="mt-5 border-t border-gray-100 pt-4 dark:border-gray-800">
      <p class="text-sm text-gray-700 dark:text-gray-300">This maintenance attempt needs recovery. Conflicting changes remain paused.</p>
      <div class="mt-3 flex flex-wrap gap-2"><AppButton variant="secondary" :disabled="updates.busy.value || reviewing" @click="recover(generation, 'repair')">Review repair</AppButton><AppButton variant="secondary" :disabled="updates.busy.value || reviewing" @click="recover(generation, 'rollback')">Review rollback</AppButton></div>
    </div>
    <div v-for="generation in rollbackGenerations" :key="generation.id" class="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-4 dark:border-gray-800"><div class="flex flex-wrap items-center gap-3"><span class="text-sm text-gray-500 dark:text-gray-400">Previous version</span><span class="font-mono text-sm text-gray-900 dark:text-gray-100">{{ rollbackVersion(generation) }}</span></div><AppButton variant="secondary" :disabled="updates.busy.value || updates.locked.value || reviewing" @click="recover(generation, 'rollback')">Review rollback</AppButton></div>

  </Card>
  <RuntimeUpdateDetailsModal v-model="detailsOpen" :summaries="canReview ? [nodeSummary(snapshot)] : []" :reviewing="reviewing" :review-disabled="!canReview || updates.locked.value || updates.busy.value" @review="review(()=>{detailsOpen=false})" />
  <BaseModal :model-value="!!plan" :title="plan?.kind === 'migrate' ? 'Migrate Node.js' : plan?.kind === 'rollback' ? 'Roll back Node.js' : plan?.kind === 'repair' ? 'Repair Node.js' : 'Update Node.js'" :loading="submitting" :confirm-disabled="!!plan?.impact.blocked_reasons.length" :confirm-text="plan?.kind === 'migrate' ? 'Migrate and verify' : plan?.kind === 'rollback' ? 'Restore and verify' : plan?.kind === 'repair' ? 'Repair and verify' : 'Update and verify'" @update:model-value="v => { if (!v) plan = null }" @submit="apply">
    <template v-if="plan">
      <ErrorAlert v-if="error" class="mb-4" :message="error" />
      <p class="font-semibold text-gray-900 dark:text-gray-100">{{ versionSummary }}</p>
      <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ npmSummary }}</p>
      <ErrorAlert v-if="plan.impact.blocked_reasons.length" class="mt-4" :message="`Resolve ${plan.impact.blocked_reasons.length} ${plan.impact.blocked_reasons.length === 1 ? 'issue' : 'issues'} to continue.`" :details="plan.impact.blocked_reasons.join('\n\n')">
        <template v-if="hasScheduledBlocker" #actions>
          <AppButton variant="secondary" size="sm" to="/processes/scheduler">Review jobs</AppButton>
        </template>
      </ErrorAlert>
      <p v-for="job in plan.impact.scheduled_tasks?.filter(task => task.notice) || []" :key="job.identity" class="mt-3 text-sm text-amber-800 dark:text-amber-300" role="status">{{ job.notice }}</p>
      <div v-if="affectedDomains.length || affectedServices.length" class="mt-4 grid gap-3 sm:grid-cols-2">
        <div v-if="affectedDomains.length" class="min-w-0">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Sites</h3>
          <p class="mt-1 text-sm text-gray-600 [overflow-wrap:anywhere] dark:text-gray-400">{{ affectedDomains.join(', ') }}</p>
        </div>
        <div v-if="affectedServices.length" class="min-w-0">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Services</h3>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ affectedServices.join(', ') }}</p>
        </div>
      </div>
      <p class="mt-4 text-sm text-gray-700 dark:text-gray-300">Brief service restart. Stopped apps and workers stay stopped.</p>
      <details v-if="plan.impact.scheduled_tasks?.length || hasNaturalWait || hasCompletedTasks || (plan.migration && plan.kind !== 'rollback')" class="mt-3 text-sm text-gray-600 dark:text-gray-400">
        <summary class="w-fit cursor-pointer rounded-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500">Maintenance details</summary>
        <p v-if="plan.impact.scheduled_tasks?.length" class="mt-2">{{ plan.impact.scheduled_tasks.length }} scheduled {{ plan.impact.scheduled_tasks.length === 1 ? 'job pauses' : 'jobs pause' }}. Running jobs finish first; missed launches are not replayed.</p>
        <p v-if="hasNaturalWait" class="mt-2">Running Artisan tasks must finish before maintenance.</p>
        <p v-if="hasCompletedTasks" class="mt-2">Completed tasks will not run again.</p>
        <p v-if="plan.migration && plan.kind !== 'rollback'" class="mt-2">Rollback retains the APT installation while its files remain unchanged.</p>
      </details>
      <p v-if="plan.kind === 'rollback'" class="mt-3 text-sm text-amber-800 dark:text-amber-300">Older releases may contain vulnerabilities. Application files and databases are not restored.</p>
    </template>
  </BaseModal>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import Card from './Card.vue'; import AppButton from './AppButton.vue'; import BaseModal from './BaseModal.vue'; import ErrorAlert from './ErrorAlert.vue';
import { nodeUpdateError, useNodeUpdates, type NodePlan, type NodeGeneration } from '../composables/useNodeUpdates';
import StatusBadge from './StatusBadge.vue';
import RuntimeUpdateDetailsModal from './RuntimeUpdateDetailsModal.vue';
import { nodeSummary } from '../utils/runtimeUpdateDetails';
import { createRequestID } from '../utils/requestID';
const emit = defineEmits<{ changed: [] }>();
const updates = useNodeUpdates(); const plan = ref<NodePlan | null>(null); const reviewing = ref(false); const submitting = ref(false); const error = ref(''); let requestID = '';
const detailsOpen=ref(false);
const snapshot = computed(() => updates.state.value.snapshot);
const installedVersion=computed(()=>snapshot.value.installed?.release?.version || snapshot.value.legacy_source?.version || 'Unavailable');
const canReview=computed(()=>!!snapshot.value.candidate && !snapshot.value.error && (snapshot.value.state==='available' && snapshot.value.update_available || snapshot.value.state==='migration_available' && snapshot.value.migration_available));
const upToDate=computed(()=>snapshot.value.state==='current' && snapshot.value.checked_at>0 && !snapshot.value.error && !snapshot.value.update_available && !snapshot.value.migration_available && installedVersion.value!=='Unavailable' && !updates.locked.value && !updates.attention.value);
const rollbackVersion=(g:NodeGeneration)=>g.plan.migration ? `APT Node.js ${g.plan.migration.version}` : g.retained?.before.version || g.plan.retained?.before.version || g.plan.before.release.version || 'Unavailable';
const rollbackGenerations=computed(()=>updates.state.value.recoveries.filter(g=>g.state==='succeeded' && g.plan?.after.version===installedVersion.value && rollbackVersion(g)!=='Unavailable' && rollbackVersion(g)!==installedVersion.value));
const statusText = computed(() => {
  const s = snapshot.value;
  if(updates.locked.value)return 'Runtime maintenance is pending. Review the current operation or recovery.';
  if (s.state === 'check_failed') return 'The last check failed. Check again after resolving the reported issue.';
  if (s.state === 'stale') return 'The stored check is out of date. Check again before maintenance.';
  if (s.state === 'migration_available' && s.migration_available) return `This older APT installation needs an approved migration to Node.js ${s.candidate?.version} before Fluxo-managed updates are enabled.`;
  if (s.state === 'available' && s.update_available) return `Node.js ${s.candidate?.version} is available for the installed ${s.installed?.release?.version.split('.')[0]}.x line.`;
  if (upToDate.value) return 'No Node.js updates available for the installed major version.';
  return 'Check for updates to verify this installation and its supported release line.';
});
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
const hasScheduledBlocker = computed(() => plan.value?.impact.blocked_reasons.some(reason => reason.startsWith('Scheduled task ')) ?? false);
const date = (unix: number) => new Date(unix * 1000).toLocaleString();
async function check() { error.value = ''; try { await updates.check(); } catch (e) { error.value = nodeUpdateError(e); } }
async function review(beforeOpen?:()=>void) { if(reviewing.value || updates.busy.value || updates.locked.value || !canReview.value)return; reviewing.value = true; error.value = ''; try { const next=await updates.review(); beforeOpen?.(); plan.value=next; requestID = createRequestID(); } catch (e) { error.value = nodeUpdateError(e); } finally { reviewing.value = false; } }
async function recover(g: NodeGeneration, kind: 'repair' | 'rollback', beforeOpen?: () => void) { if(reviewing.value||updates.busy.value)return; reviewing.value = true; error.value = ''; try { const next = await updates.reviewRecovery(g, kind); beforeOpen?.(); plan.value = next; requestID = createRequestID(); } catch (e) { error.value = nodeUpdateError(e); } finally { reviewing.value = false; } }
watch(() => updates.repairReviewRequest.value, async operationID => {
  if (!operationID) return;
  try {
    const generation = updates.repairForOperation(operationID);
    if (generation && !updates.busy.value && !reviewing.value) await recover(generation, 'repair', () => updates.repairReviewReady(operationID));
  } finally { updates.finishRepairReview(operationID); }
}, { immediate: true });
async function apply() { if (!plan.value || submitting.value || updates.busy.value || plan.value.impact.blocked_reasons.length) return; submitting.value = true; error.value = ''; try { await updates.apply(plan.value, requestID); plan.value = null; } catch (e) { error.value = nodeUpdateError(e); } finally { submitting.value = false; } }
watch(() => updates.state.value.active_operation, (current, previous) => { if (!current && previous && previous.kind !== 'check') emit('changed'); });
watch(()=>updates.updateReviewRequest.value,async discoveryID=>{
 if(!discoveryID)return;
 try{
  await updates.refresh();
  if(updates.locked.value||reviewing.value||!updates.state.value.snapshot.update_available||updates.state.value.snapshot.discovery_id!==discoveryID)throw Error('The update information changed. Check for updates and review it again.');
  await review(updates.updateReviewReady);
 }catch(e){error.value=nodeUpdateError(e);}finally{updates.finishUpdateReview();}
},{immediate:true});
onMounted(() => { void updates.refresh().catch(e => { error.value = nodeUpdateError(e); }); });
</script>
