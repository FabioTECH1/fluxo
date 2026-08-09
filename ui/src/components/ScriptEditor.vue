<template>
  <div
    class="relative flex w-full overflow-hidden rounded-lg border border-gray-200 bg-gray-50 transition-shadow focus-within:border-blue-500 focus-within:ring-2 focus-within:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
    :style="editorStyle"
  >
    <div
      ref="lineNumbersRef"
      class="w-12 shrink-0 select-none overflow-hidden border-r border-gray-200 bg-gray-100 py-2 dark:border-gray-600 dark:bg-gray-800"
      aria-hidden="true"
    >
      <div
        v-for="line in gutterLineCount"
        :key="line"
        class="px-2 text-right font-mono text-xs leading-5 text-gray-400 dark:text-gray-500"
      >
        {{ line }}
      </div>
    </div>

    <div class="relative h-full min-w-0 flex-1 overflow-hidden">
      <div
        v-if="language !== 'plain'"
        ref="highlightRef"
        class="pointer-events-none absolute inset-0 overflow-hidden whitespace-pre p-2 font-mono text-sm leading-5 text-gray-900 dark:text-gray-100"
        aria-hidden="true"
        v-html="highlightedContent"
      ></div>
      <textarea
        ref="textareaRef"
        :id="id"
        :value="modelValue"
        class="block h-full w-full resize-none whitespace-pre bg-transparent p-2 font-mono text-sm leading-5 caret-gray-900 outline-none placeholder:text-gray-400 dark:caret-gray-100 dark:placeholder:text-gray-500"
        :class="language === 'plain' ? 'text-gray-900 dark:text-gray-100' : 'text-transparent'"
        :placeholder="placeholder"
        :readonly="readonly || masked"
        :aria-label="label"
        :aria-describedby="ariaDescribedby"
        :aria-readonly="readonly || masked"
        :aria-busy="busy"
        :aria-hidden="masked ? 'true' : undefined"
        :tabindex="masked ? -1 : undefined"
        data-gramm="false"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        wrap="off"
        @input="handleInput"
        @keydown="handleKeyDown"
        @scroll="syncScroll"
      ></textarea>

      <button
        v-if="masked"
        type="button"
        class="absolute inset-0 flex w-full cursor-pointer flex-col items-center justify-center gap-3 bg-gray-50/60 backdrop-blur-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500 dark:bg-gray-800/60"
        :aria-label="maskedMessage"
        :aria-describedby="ariaDescribedby"
        @click="handleReveal"
      >
        <EyeSlashIcon class="h-8 w-8 text-gray-400 dark:text-gray-500" aria-hidden="true" />
        <span class="text-xs font-semibold text-gray-500 dark:text-gray-400">{{ maskedMessage }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { EyeSlashIcon } from '@heroicons/vue/24/outline';
import { computed, nextTick, ref } from 'vue';

type EditorLanguage = 'env' | 'shell' | 'plain';

const props = withDefaults(defineProps<{
  modelValue: string;
  id?: string;
  ariaDescribedby?: string;
  language?: EditorLanguage;
  placeholder?: string;
  label: string;
  visibleLines?: number;
  minimumLines?: number;
  readonly?: boolean;
  busy?: boolean;
  masked?: boolean;
  maskedMessage?: string;
}>(), {
  language: 'plain',
  id: undefined,
  ariaDescribedby: undefined,
  placeholder: '',
  visibleLines: 20,
  minimumLines: 20,
  readonly: false,
  busy: false,
  masked: false,
  maskedMessage: 'Click to reveal content',
});

const emit = defineEmits<{
  'update:modelValue': [value: string];
  keydown: [event: KeyboardEvent, textarea: HTMLTextAreaElement];
  reveal: [];
}>();

const lineNumbersRef = ref<HTMLDivElement | null>(null);
const highlightRef = ref<HTMLDivElement | null>(null);
const textareaRef = ref<HTMLTextAreaElement | null>(null);

const actualLineCount = computed(() => props.modelValue.split('\n').length);
const minimumLineCount = computed(() => Math.max(1, Math.floor(props.minimumLines)));
const visibleLineCount = computed(() => Math.max(1, Math.floor(props.visibleLines)));
const gutterLineCount = computed(() => Math.max(actualLineCount.value, minimumLineCount.value));
const editorStyle = computed(() => ({ height: `${visibleLineCount.value * 20 + 10}px` }));

const escapeHtml = (value: string) => value
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;');

const highlightedContent = computed(() => {
  const lines = escapeHtml(props.modelValue || '').split('\n');

  return lines.map((line) => {
    const trimmed = line.trim();
    if (props.language !== 'plain' && trimmed.startsWith('#')) {
      return `<span class="text-gray-400 dark:text-gray-500 font-normal italic">${line}</span>`;
    }

    if (props.language === 'env') {
      const equalsIndex = line.indexOf('=');
      if (equalsIndex !== -1) {
        const key = line.substring(0, equalsIndex);
        const value = line.substring(equalsIndex);
        return `<span class="text-blue-600 dark:text-blue-400 font-semibold">${key}</span><span class="text-emerald-600 dark:text-emerald-400">${value}</span>`;
      }
    }

    if (props.language === 'shell') {
      return line.replace(
        /\b(git|composer|npm|php|artisan|sudo|systemctl|mkdir|chown|chmod|cd|cp|mv|rm|echo|export|set|if|then|fi|else|elif)\b/g,
        '<span class="text-blue-600 dark:text-blue-400 font-semibold">$1</span>',
      );
    }

    return line;
  }).join('\n');
});

const handleInput = (event: Event) => {
  emit('update:modelValue', (event.target as HTMLTextAreaElement).value);
};

const handleKeyDown = (event: KeyboardEvent) => {
  emit('keydown', event, event.currentTarget as HTMLTextAreaElement);
};

const handleReveal = async () => {
  emit('reveal');
  await nextTick();
  textareaRef.value?.focus();
};

const syncScroll = (event: Event) => {
  const textarea = event.currentTarget as HTMLTextAreaElement;
  if (lineNumbersRef.value) lineNumbersRef.value.scrollTop = textarea.scrollTop;
  if (highlightRef.value) {
    highlightRef.value.scrollTop = textarea.scrollTop;
    highlightRef.value.scrollLeft = textarea.scrollLeft;
  }
};
</script>
