<template>
  <Card class="mt-6">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div><h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Node.js updates</h2><p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Maintain the installed major version. Updates run only after your approval.</p></div>
      <div class="flex flex-wrap gap-2"><AppButton variant="secondary" size="sm" :disabled="updates.busy.value || updates.locked.value || reviewing" @click="check">Check for updates</AppButton><AppButton size="sm" :disabled="(!snapshot.update_available && !snapshot.migration_available) || updates.busy.value || updates.locked.value || reviewing" :loading="reviewing" @click="review">{{ snapshot.migration_available ? 'Review migration' : 'Update' }}</AppButton></div>
    </div>
    <ErrorAlert v-if="error" class="mt-4" :message="error" />
    <ErrorAlert v-if="snapshot.error" class="mt-4" :message="snapshot.error" />
    <p class="mt-4 text-sm text-gray-700 dark:text-gray-300">{{ statusText }}</p>
    <p v-if="snapshot.checked_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">Last successful check: {{ date(snapshot.checked_at) }}. Checks run daily and do not install updates.</p>
    <p v-if="updates.state.value.active_operation" class="mt-4 text-sm font-medium text-blue-700 dark:text-blue-300" role="status">{{ updates.state.value.active_operation.message || 'Preparing maintenance…' }} You can leave this page.</p>
    <div v-for="generation in updates.state.value.recoveries" :key="generation.id" class="mt-5 border-t border-gray-100 pt-4 dark:border-gray-800">
      <p class="text-sm text-gray-700 dark:text-gray-300">{{ ['succeeded', 'rolled_back'].includes(generation.state) ? 'The previous release remains available for recovery.' : 'This maintenance attempt needs recovery. Conflicting changes remain paused.' }}</p>
      <div class="mt-3 flex flex-wrap gap-2"><AppButton v-if="!['succeeded', 'rolled_back'].includes(generation.state)" variant="secondary" size="sm" :disabled="updates.busy.value || reviewing" @click="recover(generation, 'repair')">Review repair</AppButton><AppButton variant="secondary" size="sm" :disabled="updates.busy.value || reviewing" @click="recover(generation, 'rollback')">Review rollback</AppButton></div>
    </div>
    <div v-if="updates.state.value.operations.length" class="mt-5 border-t border-gray-100 pt-4 dark:border-gray-800">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Recent maintenance</h3>
      <ol class="mt-3 space-y-3"><li v-for="op in updates.state.value.operations.slice(0, 8)" :key="op.id" class="text-sm"><div class="flex flex-wrap items-center gap-2"><span class="capitalize text-gray-900 dark:text-gray-100">{{ op.kind }}</span><StatusBadge :label="op.status.replaceAll('_', ' ')" :variant="op.status === 'succeeded' ? 'green' : ['queued', 'running'].includes(op.status) ? 'blue' : 'yellow'" /><span class="text-xs text-gray-500 dark:text-gray-400">{{ date(op.created_at) }}</span></div><p class="mt-1 break-words text-gray-600 dark:text-gray-400">{{ op.message }}</p></li></ol>
    </div>
  </Card>
  <BaseModal :model-value="!!plan" :title="plan?.kind === 'migrate' ? 'Migrate older Node.js installation' : plan?.kind === 'rollback' ? 'Restore previous Node.js release' : plan?.kind === 'repair' ? 'Repair Node.js maintenance' : 'Update Node.js'" :loading="submitting" :confirm-disabled="!!plan?.impact.blocked_reasons.length" :confirm-text="plan?.kind === 'migrate' ? 'Migrate and verify' : plan?.kind === 'rollback' ? 'Restore and verify' : plan?.kind === 'repair' ? 'Repair and verify' : 'Update and verify'" @update:model-value="v => { if (!v) plan = null }" @submit="apply">
    <template v-if="plan">
      <ErrorAlert v-if="error" class="mb-4" :message="error" />
      <p class="font-semibold text-gray-900 dark:text-gray-100">{{ versionSummary }}</p><p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ npmSummary }}. Other package-manager versions stay the same.</p>
      <div v-if="plan.migration" class="mt-4 space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
        <p>This older APT installation is usable, but needs migration to enable Fluxo-managed updates. Package: {{ plan.migration.package_version }} ({{ plan.migration.architecture }}), {{ plan.migration.node.path }}.</p>
        <p>The destination is an official release verified against Node.js signatures. The existing APT runtime is retained as a local rollback baseline; it is not certified as an official archive. APT packages, sources and other package managers stay unchanged.</p>
        <p>Rollback restores the original APT runtime and command links only while that retained package and its files remain unchanged. Changes made outside Fluxo after an interruption can require manual recovery.</p>
      </div>
      <p class="mt-4 text-sm text-gray-700 dark:text-gray-300">Running affected applications will stop and restart. Expect a brief interruption. Stopped apps stay stopped; static files keep serving. Application files and dependencies are not rebuilt.</p>
      <ul class="mt-4 space-y-2 text-sm text-gray-700 dark:text-gray-300"><li v-for="unit in plan.impact.units" :key="unit.id">{{ unit.domain }} — {{ unit.active ? 'Stop, restart and verify' : 'Keep stopped' }}<span v-if="unit.bun"> (Bun {{ unit.bun.version }} stays installed)</span><span v-if="!['apply', 'migrate'].includes(plan.kind) && unit.state" class="mt-1 block text-xs text-gray-500 dark:text-gray-400">Current state: {{ unit.state }}{{ unit.sub_state ? ` (${unit.sub_state})` : '' }}{{ unit.listener_present === false ? '; no listener on its assigned port' : '' }}. Recovery preserves its state from before the original update.</span></li></ul>
      <p v-if="!plan.impact.units.length" class="mt-3 text-sm text-gray-600 dark:text-gray-400">No managed Node.js server applications need restarting.</p>
      <div v-if="plan.impact.php_consumers?.length" class="mt-5 border-t border-gray-100 pt-4 dark:border-gray-800">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">PHP background processes</h3>
        <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">These processes pause because they can launch Node.js. PHP packages and PHP-FPM stay unchanged. Each independent worker keeps its settings and enabled state.</p>
        <div v-for="consumer in plan.impact.php_consumers" :key="consumer.consumer.id" class="mt-4 text-sm text-gray-700 dark:text-gray-300">
          <p class="font-medium">{{ consumer.domain || 'Site domain unavailable' }} — {{ consumer.consumer.name || 'Unnamed process' }}</p>
          <p class="mt-1 break-all font-mono text-xs">{{ consumer.consumer.command }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">PHP {{ consumer.php.version }} stays installed. Protocol: {{ consumer.consumer.protocol || consumer.consumer.kind }}.</p>
          <ul class="mt-2 space-y-1"><li v-for="(unit, index) in consumer.units" :key="unit.runtime.name">Instance {{ index + 1 }} — {{ plan.impact.php_resumes?.[unit.runtime.name]?.completed ? 'Completed; recovery will not run it again' : !unit.runtime.active ? 'Keep stopped' : unit.runtime.natural ? 'Wait for natural exit, then resume once' : 'Drain, restart and verify' }} ({{ unit.enabled }})</li></ul>
          <p v-if="consumer.units.some(unit => unit.runtime.natural)" class="mt-2 text-amber-800 dark:text-amber-300">Fluxo sends no stop signal to this Artisan command. Let it finish or use its application's documented graceful exit. A continuous command may leave maintenance waiting for recovery. Uncertain or failed executions are never automatically replayed.</p>
          <p v-if="consumer.consumer.kind === 'laravel_nightwatch'" class="mt-2 text-xs text-gray-500 dark:text-gray-400">Monitoring pauses; buffered telemetry may be lost. Remote delivery is not verified.</p>
          <p v-if="consumer.consumer.kind === 'laravel_octane'" class="mt-2 text-xs text-gray-500 dark:text-gray-400">Octane uses its verified native graceful shutdown deadlines. Requests exceeding those deadlines may end.</p>
        </div>
      </div>
      <p class="mt-4 text-sm text-gray-600 dark:text-gray-400">Future builds using the shared Node.js runtime will use this release. Verification checks process and connection health; check your important application pages afterward.</p>
      <p v-if="['apply', 'migrate'].includes(plan.kind)" class="mt-3 text-sm text-gray-600 dark:text-gray-400">If activation fails, Fluxo attempts to restore and verify the retained previous runtime. An incomplete recovery stays visible.</p>
      <p v-else class="mt-3 text-sm text-gray-600 dark:text-gray-400">Recovery requires official Node.js downloads to authenticate the retained target again before applications stop.</p>
      <p v-if="plan.kind === 'rollback'" class="mt-3 text-sm text-amber-800 dark:text-amber-300">Older releases may contain vulnerabilities. Runtime rollback does not restore application files, databases or jobs.</p>
      <ErrorAlert v-for="reason in plan.impact.blocked_reasons" :key="reason" class="mt-3" :message="reason" />
    </template>
  </BaseModal>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import Card from './Card.vue'; import AppButton from './AppButton.vue'; import BaseModal from './BaseModal.vue'; import ErrorAlert from './ErrorAlert.vue'; import StatusBadge from './StatusBadge.vue';
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
const date = (unix: number) => new Date(unix * 1000).toLocaleString();
async function check() { error.value = ''; try { await updates.check(); } catch (e) { error.value = nodeUpdateError(e); } }
async function review() { reviewing.value = true; error.value = ''; try { plan.value = await updates.review(); requestID = createRequestID(); } catch (e) { error.value = nodeUpdateError(e); } finally { reviewing.value = false; } }
async function recover(g: NodeGeneration, kind: 'repair' | 'rollback') { reviewing.value = true; error.value = ''; try { plan.value = await updates.reviewRecovery(g, kind); requestID = createRequestID(); } catch (e) { error.value = nodeUpdateError(e); } finally { reviewing.value = false; } }
async function apply() { if (!plan.value) return; submitting.value = true; error.value = ''; try { await updates.apply(plan.value, requestID); plan.value = null; } catch (e) { error.value = nodeUpdateError(e); } finally { submitting.value = false; } }
watch(() => updates.state.value.active_operation, (current, previous) => { if (!current && previous && previous.kind !== 'check') emit('changed'); });
onMounted(() => { void updates.refresh().catch(e => { error.value = nodeUpdateError(e); }); });
</script>
