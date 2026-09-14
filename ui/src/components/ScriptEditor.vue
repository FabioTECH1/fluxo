<template>
  <div class="script-editor relative w-full overflow-hidden rounded-lg border border-gray-200 bg-gray-50 transition-shadow focus-within:border-blue-500 focus-within:ring-2 focus-within:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" :style="editorStyle">
    <div ref="host" class="h-full min-w-0" :inert="masked" :aria-hidden="masked ? 'true' : undefined"></div>
    <div v-if="loadError" class="absolute inset-0 flex items-center justify-center gap-2 text-sm" role="alert">
      Editor could not load.
      <button type="button" class="underline" @click="loadEditor">Retry</button>
    </div>
    <div v-else-if="!ready" class="absolute inset-0 flex items-center justify-center text-sm text-gray-500" role="status">Loading editor…</div>
    <button v-if="masked" type="button" class="absolute inset-0 flex w-full cursor-pointer flex-col items-center justify-center gap-3 bg-gray-50/60 backdrop-blur-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500 dark:bg-gray-800/60" :aria-label="maskedMessage" :aria-describedby="ariaDescribedby" @click="handleReveal">
      <EyeSlashIcon class="h-8 w-8 text-gray-400 dark:text-gray-500" aria-hidden="true" />
      <span class="text-xs font-semibold text-gray-500 dark:text-gray-400">{{ maskedMessage }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { EyeSlashIcon } from '@heroicons/vue/24/outline';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import type { createScriptEditor } from '../utils/scriptEditor';

const props = withDefaults(defineProps<{
  modelValue: string;
  id?: string;
  ariaDescribedby?: string;
  language?: 'env' | 'shell' | 'plain';
  placeholder?: string;
  label: string;
  visibleLines?: number;
  minimumLines?: number;
  readonly?: boolean;
  busy?: boolean;
  masked?: boolean;
  maskedMessage?: string;
}>(), {
  language: 'plain', placeholder: '', visibleLines: 20, minimumLines: 20,
  readonly: false, busy: false, masked: false, maskedMessage: 'Click to reveal content',
});
const emit = defineEmits<{
  'update:modelValue': [value: string];
  keydown: [event: KeyboardEvent];
  reveal: [];
}>();
const host = ref<HTMLDivElement | null>(null);
const ready = ref(false);
const loadError = ref(false);
let editor: ReturnType<typeof createScriptEditor> | undefined;
let disposed = false;
let loading = false;
const editorStyle = computed(() => ({ height: `${Math.max(1, Math.floor(props.visibleLines)) * 20 + 48}px` }));
const loadEditor = async () => {
  if (loading || editor) return;
  loading = true;
  loadError.value = false;
  try {
    const { createScriptEditor } = await import('../utils/scriptEditor');
    if (disposed || !host.value) return;
    editor = createScriptEditor(host.value, { ...props }, value => emit('update:modelValue', value), event => emit('keydown', event));
    ready.value = true;
  } catch {
    if (!disposed) loadError.value = true;
  } finally {
    loading = false;
  }
};
const handleReveal = async () => {
  emit('reveal');
  await nextTick();
  editor?.focus();
};
watch(() => props.modelValue, value => editor?.setValue(value));
watch(() => [props.language, props.label, props.id, props.ariaDescribedby, props.placeholder, props.readonly, props.masked, props.busy, props.minimumLines], () => editor?.configure({ ...props }));
onMounted(loadEditor);
onBeforeUnmount(() => { disposed = true; editor?.destroy(); });
</script>

<style>
.script-editor {
  --editor-text: #111827;
  --editor-bg: #f9fafb;
  --editor-gutter: #f3f4f6;
  --editor-border: #e5e7eb;
  --editor-muted: #6b7280;
  --editor-key: #2563eb;
  --editor-value: #059669;
  --editor-selection: #2563eb38;
}
.dark .script-editor {
  --editor-text: #f3f4f6;
  --editor-bg: #1f2937;
  --editor-gutter: #1f2937;
  --editor-border: #4b5563;
  --editor-muted: #9ca3af;
  --editor-key: #60a5fa;
  --editor-value: #34d399;
  --editor-selection: #3b82f660;
}
</style>
