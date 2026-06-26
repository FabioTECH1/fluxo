import { ref, computed, watch, type Ref } from 'vue';

export function useUndoRedo(source: Ref<string>, maxHistory = 100) {
  const past = ref<string[]>([]);
  const future = ref<string[]>([]);
  let ignoreNext = false;
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;

  watch(source, (newVal, oldVal) => {
    if (ignoreNext || oldVal === undefined || oldVal === newVal) return;
    if (saveTimeout) clearTimeout(saveTimeout);
    saveTimeout = setTimeout(() => {
      past.value.push(oldVal);
      if (past.value.length > maxHistory) past.value.shift();
      future.value = [];
      saveTimeout = null;
    }, 300);
  });

  const undo = () => {
    if (saveTimeout) clearTimeout(saveTimeout);
    if (past.value.length === 0) return;
    const prev = past.value.pop()!;
    future.value.push(source.value);
    ignoreNext = true;
    source.value = prev;
  };

  const redo = () => {
    if (saveTimeout) clearTimeout(saveTimeout);
    if (future.value.length === 0) return;
    const next = future.value.pop()!;
    past.value.push(source.value);
    ignoreNext = true;
    source.value = next;
  };

  const canUndo = computed(() => past.value.length > 0);
  const canRedo = computed(() => future.value.length > 0);

  return { undo, redo, canUndo, canRedo };
}
