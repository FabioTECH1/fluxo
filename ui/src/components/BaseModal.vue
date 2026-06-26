<template>
  <div v-if="modelValue" class="fixed inset-0 z-50 flex items-center justify-center p-4">
    <div class="fixed inset-0 bg-black/60 backdrop-blur-xs" @click="cancel"></div>
    <div class="relative bg-white dark:bg-gray-900 rounded-xl shadow-2xl dark:border dark:border-gray-800 w-full overflow-hidden transform transition-all" :class="maxWidth">
      <div class="px-6 py-5 border-b border-gray-100 dark:border-gray-800 bg-gray-50 dark:bg-gray-800 flex justify-between items-center">
        <slot name="title">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">{{ title }}</h3>
        </slot>
        <button v-if="showClose && !preventDismiss" @click="cancel" class="text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400 transition-colors ml-4 shrink-0">
          <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <div class="p-6 overflow-y-auto max-h-[calc(100vh-10rem)]">
        <slot />
      </div>

      <div v-if="!hideFooter" class="flex justify-end space-x-3 px-6 pb-6 pt-2 border-t border-gray-100 dark:border-gray-800">
        <slot name="footer">
          <AppButton variant="secondary" @click="cancel">{{ cancelText }}</AppButton>
          <AppButton variant="primary" :loading="loading" @click="$emit('submit')">{{ confirmText }}</AppButton>
        </slot>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';
import AppButton from './AppButton.vue';

const props = withDefaults(defineProps<{
  modelValue: boolean;
  title: string;
  maxWidth?: string;
  showClose?: boolean;
  loading?: boolean;
  cancelText?: string;
  confirmText?: string;
  preventDismiss?: boolean;
  hideFooter?: boolean;
}>(), {
  maxWidth: 'max-w-lg',
  showClose: true,
  loading: false,
  cancelText: 'Cancel',
  confirmText: 'Submit',
  preventDismiss: false,
  hideFooter: false,
});

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  'submit': [];
}>();

const cancel = () => {
  if (props.preventDismiss) return;
  if (!props.loading) {
    emit('update:modelValue', false);
  }
};

const handleKeyDown = (e: KeyboardEvent) => {
  if (e.key === 'Escape' && props.modelValue) cancel();
};

onMounted(() => window.addEventListener('keydown', handleKeyDown));
onUnmounted(() => window.removeEventListener('keydown', handleKeyDown));
</script>
