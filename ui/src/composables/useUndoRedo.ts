import { ref, computed, watch, type Ref } from 'vue';

export function useUndoRedo(source: Ref<string>, maxHistory = 100) {
  const past = ref<string[]>([]);
  const future = ref<string[]>([]);
  let ignoreNext = false;
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;
  let pendingSnapshot: string | null = null;

  const commitPendingSnapshot = () => {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
    }
    if (pendingSnapshot === null || pendingSnapshot === source.value) {
      pendingSnapshot = null;
      return;
    }
    past.value.push(pendingSnapshot);
    if (past.value.length > maxHistory) past.value.shift();
    future.value = [];
    pendingSnapshot = null;
  };

  watch(source, (newVal, oldVal) => {
    if (ignoreNext) {
      ignoreNext = false;
      return;
    }
    if (oldVal === undefined || oldVal === newVal) return;
    if (pendingSnapshot === null) pendingSnapshot = oldVal;
    if (saveTimeout) clearTimeout(saveTimeout);
    saveTimeout = setTimeout(commitPendingSnapshot, 300);
  }, { flush: 'sync' });

  const undo = () => {
    commitPendingSnapshot();
    if (past.value.length === 0) return;
    const prev = past.value.pop()!;
    future.value.push(source.value);
    ignoreNext = true;
    source.value = prev;
  };

  const redo = () => {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
      pendingSnapshot = null;
    }
    if (future.value.length === 0) return;
    const next = future.value.pop()!;
    past.value.push(source.value);
    ignoreNext = true;
    source.value = next;
  };

  const resetHistory = () => {
    if (saveTimeout) clearTimeout(saveTimeout);
    saveTimeout = null;
    pendingSnapshot = null;
    ignoreNext = false;
    past.value = [];
    future.value = [];
  };

  const canUndo = computed(() => past.value.length > 0);
  const canRedo = computed(() => future.value.length > 0);

  return { undo, redo, resetHistory, canUndo, canRedo };
}
