<template>
  <BaseModal v-model="visible" title="Creating Site..." :prevent-dismiss="true" :show-close="false" :hide-footer="true" max-width="max-w-md">
    <div class="space-y-1 py-2">
      <p class="text-sm text-gray-500 dark:text-gray-400 mb-5">We are preparing to install your application. This may take a few moments.</p>

      <div v-for="(step, i) in steps" :key="i" class="flex items-center gap-3 py-1.5">
        <div class="shrink-0 w-6 h-6 rounded-full flex items-center justify-center transition-all duration-300"
          :class="step.complete ? 'bg-green-500' : step.active ? 'bg-blue-600 animate-pulse' : 'bg-gray-200 dark:bg-gray-700'">
          <svg v-if="step.complete" class="w-4 h-4 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7"/></svg>
          <div v-else-if="step.active" class="w-2 h-2 bg-white rounded-full"></div>
        </div>
        <span class="text-sm transition-colors duration-300" :class="step.complete ? 'text-green-700 dark:text-green-400' : step.active ? 'text-gray-900 dark:text-gray-100 font-medium' : 'text-gray-400 dark:text-gray-500'">{{ step.label }}</span>
      </div>
    </div>

  </BaseModal>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { apiClient } from '../api/client';
import BaseModal from './BaseModal.vue';

interface Step {
  label: string;
  active: boolean;
  complete: boolean;
}

const props = defineProps<{
  payload: any;
}>();

const emit = defineEmits<{
  'created': [site: any];
  'error': [message: string];
}>();

const visible = ref(true);

const steps = ref<Step[]>([
  { label: 'Configuring Nginx', active: false, complete: false },
  { label: 'Cloning Git repository', active: false, complete: false },
  { label: 'Creating environment file', active: false, complete: false },
  { label: 'Installing dependencies', active: false, complete: false },
  { label: 'Building frontend assets', active: false, complete: false },
  { label: 'Making final touches', active: false, complete: false },
]);

let progressTimer: ReturnType<typeof setInterval> | null = null;
let completeTimeout: ReturnType<typeof setTimeout> | null = null;

const startProgress = () => {
  let idx = 0;
  const s = steps.value;

  const advance = () => {
    if (idx >= s.length) {
      if (progressTimer) {
        clearInterval(progressTimer);
        progressTimer = null;
      }
      return;
    }
    if (idx > 0) {
      s[idx - 1].active = false;
      s[idx - 1].complete = true;
    }
    s[idx].active = true;
    idx++;
  };

  advance();
  progressTimer = setInterval(advance, 4000);
};

const completeAll = () => {
  if (progressTimer) {
    clearInterval(progressTimer);
    progressTimer = null;
  }
  steps.value.forEach(s => {
    s.active = false;
    s.complete = true;
  });
};

const cleanup = () => {
  if (progressTimer) {
    clearInterval(progressTimer);
    progressTimer = null;
  }
  if (completeTimeout) {
    clearTimeout(completeTimeout);
    completeTimeout = null;
  }
};

onMounted(async () => {
  startProgress();
  try {
    const site = await apiClient.createSite(props.payload);
    completeAll();
    completeTimeout = setTimeout(() => {
      visible.value = false;
      emit('created', site);
    }, 600);
  } catch (e: any) {
    steps.value.forEach(s => { s.active = false; s.complete = false; });
    visible.value = false;
    emit('error', e.message || 'Failed to create site');
  }
});

onUnmounted(cleanup);
</script>
