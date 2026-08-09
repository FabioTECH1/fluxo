<template>
  <div v-if="isOpen" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
    <div class="fixed inset-0 bg-black/60" aria-hidden="true" @click="handleCancel"></div>
    <div
      ref="modalPanel"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-modal-title"
      aria-describedby="confirm-modal-message"
      class="relative flex w-full max-w-md flex-col space-y-4 rounded-xl border border-gray-200 bg-white p-6 shadow-xl dark:border-gray-700 dark:bg-gray-900"
    >
      <div class="flex items-start space-x-3">
        <div class="flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center"
             :class="options.variant === 'danger' ? 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400' : 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400'">
          <ExclamationTriangleIcon v-if="options.variant === 'danger'" class="h-6 w-6" aria-hidden="true" />
          <InformationCircleIcon v-else class="h-6 w-6" aria-hidden="true" />
        </div>
        <div class="flex-1 min-w-0">
          <h3 id="confirm-modal-title" class="text-lg font-bold text-gray-900 dark:text-gray-100 leading-6">{{ options.title }}</h3>
          <p id="confirm-modal-message" class="text-sm text-gray-500 dark:text-gray-400 mt-2 whitespace-pre-wrap leading-relaxed">{{ options.message }}</p>
        </div>
      </div>
      <div class="flex justify-end space-x-3 pt-2">
        <button ref="cancelButton" type="button" @click="handleCancel" class="px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-semibold text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500 transition-colors cursor-pointer">
          {{ options.cancelText }}
        </button>
        <button type="button" @click="handleConfirm" class="px-4 py-2 rounded-lg text-sm font-semibold text-white shadow-sm focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-offset-white dark:focus:ring-offset-gray-900 transition-colors cursor-pointer"
                :class="options.variant === 'danger' ? 'bg-red-600 hover:bg-red-700 focus:ring-red-500' : 'bg-blue-600 hover:bg-blue-700 focus:ring-blue-500'">
          {{ options.confirmText }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ExclamationTriangleIcon, InformationCircleIcon } from '@heroicons/vue/24/outline';
import { watch, ref, nextTick, onMounted, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';

const { isOpen, options, handleConfirm, handleCancel } = useConfirm();
const modalPanel = ref<HTMLDivElement | null>(null);
const cancelButton = ref<HTMLButtonElement | null>(null);
let previouslyFocused: HTMLElement | null = null;
let previousBodyOverflow = '';

watch(isOpen, async (newValue) => {
  if (newValue) {
    previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    previousBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    await nextTick();
    // The non-destructive choice receives focus first, especially for danger confirmations.
    cancelButton.value?.focus();
  } else {
    document.body.style.overflow = previousBodyOverflow;
    if (previouslyFocused) {
      await nextTick();
      previouslyFocused.focus();
    }
    previouslyFocused = null;
  }
});

const handleKeyDown = (e: KeyboardEvent) => {
  if (!isOpen.value) return;
  if (e.key === 'Escape') {
    handleCancel();
    return;
  }
  if (e.key !== 'Tab' || !modalPanel.value) return;
  const focusable = [...modalPanel.value.querySelectorAll<HTMLElement>(
    'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )];
  if (!focusable.length) return;
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (!modalPanel.value.contains(document.activeElement)) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
    return;
  }
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
};

onMounted(() => {
  window.addEventListener('keydown', handleKeyDown);
});

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyDown);
  if (isOpen.value) document.body.style.overflow = previousBodyOverflow;
});
</script>
