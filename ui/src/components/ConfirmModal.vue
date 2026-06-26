<template>
  <div v-if="isOpen" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
    <div class="fixed inset-0 bg-black/60" @click="handleCancel"></div>
    <div class="relative bg-white dark:bg-gray-900 rounded-xl shadow-xl max-w-md w-full border border-gray-200 dark:border-gray-700 p-6 flex flex-col space-y-4">
      <div class="flex items-start space-x-3">
        <div class="flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center"
             :class="options.variant === 'danger' ? 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400' : 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400'">
          <svg v-if="options.variant === 'danger'" class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
          </svg>
          <svg v-else class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
          </svg>
        </div>
        <div class="flex-1 min-w-0">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100 leading-6">{{ options.title }}</h3>
          <p class="text-sm text-gray-500 dark:text-gray-400 mt-2 whitespace-pre-wrap leading-relaxed">{{ options.message }}</p>
        </div>
      </div>
      <div class="flex justify-end space-x-3 pt-2">
        <button @click="handleCancel" class="px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-semibold text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500 transition-colors cursor-pointer">
          {{ options.cancelText }}
        </button>
        <button @click="handleConfirm" ref="confirmButton" class="px-4 py-2 rounded-lg text-sm font-semibold text-white shadow-sm focus:outline-none focus:ring-2 focus:ring-offset-2 transition-colors cursor-pointer"
                :class="options.variant === 'danger' ? 'bg-red-600 hover:bg-red-700 focus:ring-red-500' : 'bg-blue-600 hover:bg-blue-700 focus:ring-blue-500'">
          {{ options.confirmText }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { watch, ref, onMounted, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';

const { isOpen, options, handleConfirm, handleCancel } = useConfirm();
const confirmButton = ref<HTMLButtonElement | null>(null);

// Focus confirm button when modal opens
watch(isOpen, (newValue) => {
  if (newValue) {
    setTimeout(() => {
      confirmButton.value?.focus();
    }, 50);
  }
});

const handleKeyDown = (e: KeyboardEvent) => {
  if (!isOpen.value) return;
  if (e.key === 'Escape') {
    handleCancel();
  }
};

onMounted(() => {
  window.addEventListener('keydown', handleKeyDown);
});

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyDown);
});
</script>


