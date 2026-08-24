<template>
  <div v-if="modelValue" class="fixed inset-0 z-50 flex items-center justify-center p-4">
    <div class="fixed inset-0 bg-black/60 backdrop-blur-xs" aria-hidden="true" @click="cancel"></div>
    <div
      ref="modalPanel"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
      :aria-busy="loading"
      tabindex="-1"
      class="relative flex min-w-0 w-full flex-col overflow-hidden rounded-xl bg-white shadow-2xl transition-all dark:border dark:border-gray-800 dark:bg-gray-900"
      :class="maxWidth"
    >
      <div class="flex min-w-0 items-center justify-between border-b border-gray-100 bg-gray-50 px-6 py-5 dark:border-gray-800 dark:bg-gray-800">
        <div :id="titleId" class="min-w-0 flex-1">
          <slot name="title">
            <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">{{ title }}</h3>
          </slot>
        </div>
        <button
          v-if="showClose && !preventDismiss && !loading"
          type="button"
          aria-label="Close dialog"
          class="ml-4 shrink-0 rounded-md text-gray-400 transition-colors hover:text-gray-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-500 dark:hover:text-gray-300"
          @click="cancel"
        >
          <XMarkIcon class="h-6 w-6" aria-hidden="true" />
        </button>
      </div>

      <div ref="modalBody" class="max-h-[calc(100vh-10rem)] w-full min-w-0 overflow-y-auto p-6">
        <slot />
      </div>

      <div v-if="!hideFooter" class="flex w-full min-w-0 shrink-0 justify-end space-x-3 border-t border-gray-100 px-6 pb-6 pt-2 dark:border-gray-800">
        <slot name="footer">
          <AppButton variant="secondary" :disabled="loading || preventDismiss" @click="cancel">{{ cancelText }}</AppButton>
          <AppButton variant="primary" :loading="loading" :disabled="confirmDisabled" @click="$emit('submit')">{{ confirmText }}</AppButton>
        </slot>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { XMarkIcon } from '@heroicons/vue/24/outline';
import { nextTick, onMounted, onUnmounted, ref, useId, watch } from 'vue';
import AppButton from './AppButton.vue';

const props = withDefaults(defineProps<{
  modelValue: boolean;
  title: string;
  maxWidth?: string;
  showClose?: boolean;
  loading?: boolean;
  cancelText?: string;
  confirmText?: string;
  confirmDisabled?: boolean;
  preventDismiss?: boolean;
  hideFooter?: boolean;
}>(), {
  maxWidth: 'max-w-lg',
  showClose: true,
  loading: false,
  cancelText: 'Cancel',
  confirmText: 'Submit',
  confirmDisabled: false,
  preventDismiss: false,
  hideFooter: false,
});

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  'submit': [];
}>();

const modalPanel = ref<HTMLDivElement | null>(null);
const modalBody = ref<HTMLDivElement | null>(null);
const titleId = `base-modal-title-${useId()}`;
let previouslyFocused: HTMLElement | null = null;
let previousBodyOverflow = '';

const focusableElements = () => [...(modalPanel.value?.querySelectorAll<HTMLElement>(
  '[autofocus], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
) || [])].filter(element => !element.hasAttribute('aria-hidden'));

watch(() => props.modelValue, async (isOpen) => {
  if (isOpen) {
    previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    previousBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    await nextTick();
    const bodyFocusTarget = modalBody.value?.querySelector<HTMLElement>(
      '[autofocus], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
    );
    (bodyFocusTarget || focusableElements()[0] || modalPanel.value)?.focus();
    return;
  }

  document.body.style.overflow = previousBodyOverflow;
  if (previouslyFocused) {
    await nextTick();
    previouslyFocused.focus();
  }
  previouslyFocused = null;
});

const cancel = () => {
  if (props.preventDismiss) return;
  if (!props.loading) {
    emit('update:modelValue', false);
  }
};

const handleKeyDown = (e: KeyboardEvent) => {
  if (!props.modelValue) return;
  if (e.key === 'Escape') {
    e.preventDefault();
    cancel();
    return;
  }
  if (e.key !== 'Tab' || !modalPanel.value) return;

  const focusable = focusableElements();
  if (!focusable.length) {
    e.preventDefault();
    modalPanel.value.focus();
    return;
  }
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (!modalPanel.value.contains(document.activeElement)) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
  } else if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
};

onMounted(() => window.addEventListener('keydown', handleKeyDown));
onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyDown);
  if (props.modelValue) document.body.style.overflow = previousBodyOverflow;
});
</script>
