import { computed, reactive, ref } from 'vue';

type Runtime = 'php' | 'node';
interface Operation { id: string; status: string }
const submissions = ref(0);
const accepted = reactive(new Set<string>());
const observed = reactive<Record<Runtime, string | null>>({ php: null, node: null });
const recovery = reactive<Record<Runtime, boolean>>({ php: false, node: false });
const active = (operation: Operation) => ['queued', 'running'].includes(operation.status);

// An accepted operation remains a claim even if a subsequent GET fails or an
// older in-flight snapshot says there is no operation. Only its terminal record
// clears the accepted claim. The backend remains the admission authority.
export const runtimeMaintenanceBusy = computed(() => submissions.value > 0 || accepted.size > 0 || !!observed.php || !!observed.node);
export const runtimeMaintenanceLocked = computed(() => runtimeMaintenanceBusy.value || recovery.php || recovery.node);

export function beginRuntimeSubmission(): () => void {
  if (runtimeMaintenanceBusy.value) throw new Error('Another runtime maintenance operation is in progress.');
  submissions.value++;
  let released = false;
  return () => { if (!released) { released = true; submissions.value--; } };
}

export function acceptRuntimeOperation(runtime: Runtime, operation: Operation): void {
  if (active(operation)) accepted.add(`${runtime}:${operation.id}`);
}

export function observeRuntimeMaintenance(runtime: Runtime, operation: Operation | null, pendingRecovery: boolean): void {
  observed[runtime] = operation && active(operation) ? operation.id : null;
  recovery[runtime] = pendingRecovery;
}

export function finishRuntimeOperation(runtime: Runtime, id: string): void {
  accepted.delete(`${runtime}:${id}`);
  if (observed[runtime] === id) observed[runtime] = null;
}
