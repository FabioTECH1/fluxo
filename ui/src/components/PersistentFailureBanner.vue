<template>
  <div
    role="alert"
    class="flex flex-col gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-red-900 dark:border-red-900/70 dark:bg-red-950/40 dark:text-red-200 sm:flex-row sm:items-center sm:justify-between"
  >
    <div class="flex min-w-0 items-start gap-3">
      <svg
        class="mt-0.5 h-5 w-5 shrink-0 text-red-600 dark:text-red-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M12 9v4m0 4h.01M10.29 3.86l-8.82 15.3A1 1 0 002.34 20h19.32a1 1 0 00.87-1.5l-8.82-15.3a1 1 0 00-1.74 0z"
        />
      </svg>
      <div class="min-w-0">
        <p class="text-sm font-semibold break-words">{{ title }}</p>
        <p v-if="reason" class="mt-0.5 text-xs text-red-700 dark:text-red-300 break-words">{{ reason }}</p>
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-2 self-start shrink-0 sm:self-auto">
      <slot name="actions" />
      <AppButton
        v-if="hasDetails"
        variant="secondary"
        size="sm"
        @click="modalOpen = true"
      >
        {{ viewErrorText }}
      </AppButton>
    </div>

    <BaseModal v-model="modalOpen" :title="modalTitle || title" maxWidth="max-w-3xl">
      <div class="space-y-4">
        <div v-if="reason" class="flex items-start gap-3 text-red-700 dark:text-red-300">
          <svg
            class="mt-0.5 h-5 w-5 shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M12 9v4m0 4h.01M10.29 3.86l-8.82 15.3A1 1 0 002.34 20h19.32a1 1 0 00.87-1.5l-8.82-15.3a1 1 0 00-1.74 0z"
            />
          </svg>
          <p class="text-sm font-semibold break-words">{{ reason }}</p>
        </div>

        <div>
          <div class="mb-2 flex items-center justify-between gap-3">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Error output</h4>
            <button
              type="button"
              title="Copy error output"
              aria-label="Copy error output"
              class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-gray-300 text-gray-500 hover:bg-gray-50 hover:text-gray-800 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200"
              @click="copyOutput"
            >
              <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
              </svg>
            </button>
          </div>
          <pre class="max-h-[45vh] w-full overflow-auto whitespace-pre-wrap break-words rounded-md bg-gray-950 p-4 text-xs leading-5 text-gray-100">{{ outputText }}</pre>
        </div>
      </div>

      <template #footer>
        <slot name="modal-actions" />
        <AppButton
          v-if="dismissible"
          variant="secondary"
          :loading="dismissing"
          @click="handleDismiss"
        >
          Dismiss
        </AppButton>
        <AppButton
          variant="primary"
          :disabled="dismissing"
          @click="modalOpen = false"
        >
          Close
        </AppButton>
      </template>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import AppButton from './AppButton.vue';
import BaseModal from './BaseModal.vue';
import { useToast } from '../composables/useToast';

const props = withDefaults(
  defineProps<{
    title: string;
    reason?: string;
    output?: string;
    viewErrorText?: string;
    modalTitle?: string;
    dismissible?: boolean;
    dismissing?: boolean;
  }>(),
  {
    reason: '',
    output: '',
    viewErrorText: 'View error',
    modalTitle: '',
    dismissible: true,
    dismissing: false,
  },
);

const emit = defineEmits<{
  dismiss: [];
}>();

const modalOpen = ref(false);
const { addToast } = useToast();

const hasDetails = computed(() => !!props.output?.trim() || !!props.reason?.trim());
const outputText = computed(() => props.output?.trim() || props.reason?.trim() || 'No detailed error output was recorded.');

const copyOutput = async () => {
  try {
    await navigator.clipboard.writeText(outputText.value);
    addToast('Error output copied', 'success');
  } catch {
    addToast('Unable to copy error output', 'error');
  }
};

const handleDismiss = () => {
  emit('dismiss');
};

defineExpose({
  openModal: () => { modalOpen.value = true; },
  closeModal: () => { modalOpen.value = false; },
});
</script>
