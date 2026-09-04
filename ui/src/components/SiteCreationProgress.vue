<template>
  <BaseModal
    v-model="visible"
    :title="profile.title"
    :prevent-dismiss="true"
    :show-close="false"
    :hide-footer="true"
    max-width="max-w-md"
  >
    <div class="flex min-w-0 items-center gap-3 border-b border-gray-100 pb-5 dark:border-gray-800">
      <AppTypeIcon :app-type="appType" size="lg" />
      <div class="min-w-0 flex-1">
        <p class="truncate text-base font-semibold text-gray-900 dark:text-gray-100">{{ domain }}</p>
        <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ profile.summary }}</p>
      </div>
    </div>

    <p class="sr-only" role="status" aria-live="polite">{{ progressAnnouncement }}</p>
    <div class="space-y-1 pt-4">
      <div v-for="step in steps" :key="step.label" class="flex min-h-9 items-center gap-3 py-1">
        <div
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full transition-colors duration-300"
          :class="step.complete ? 'bg-green-500' : step.active ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
          aria-hidden="true"
        >
          <svg v-if="step.complete" class="h-4 w-4 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
          </svg>
          <span v-else-if="step.active" class="h-2 w-2 animate-pulse rounded-full bg-white"></span>
        </div>
        <span
          class="min-w-0 text-sm transition-colors duration-300"
          :class="step.complete ? 'text-green-700 dark:text-green-400' : step.active ? 'font-medium text-gray-900 dark:text-gray-100' : 'text-gray-400 dark:text-gray-500'"
        >{{ step.label }}</span>
      </div>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { apiClient } from '../api/client';
import AppTypeIcon from './AppTypeIcon.vue';
import BaseModal from './BaseModal.vue';

interface Step {
  label: string;
  active: boolean;
  complete: boolean;
}

interface ProvisioningProfile {
  title: string;
  summary: string;
  steps: string[];
}

const props = defineProps<{
  payload: Record<string, any>;
}>();

const emit = defineEmits<{
  'created': [site: any];
  'error': [message: string];
}>();

const visible = ref(true);
const appType = computed(() => String(props.payload.app_type || 'php').toLowerCase().trim());
const domain = computed(() => String(props.payload.domain || '').trim() || 'New site');

const sourceStep = (payload: Record<string, any>, fallback: string) => {
  return String(payload.repository || '').trim() ? 'Cloning Git repository' : fallback;
};

const packageManagerLabel = (value: unknown) => {
  const manager = String(value || 'npm').toLowerCase();
  if (manager === 'none') return '';
  if (manager === 'yarn') return 'Yarn';
  if (manager === 'bun') return 'Bun';
  return manager;
};

const buildProvisioningProfile = (payload: Record<string, any>): ProvisioningProfile => {
  const type = String(payload.app_type || 'php').toLowerCase().trim();

  if (type === 'wordpress') {
    return {
      title: 'Provisioning WordPress',
      summary: 'WordPress with MySQL and PHP-FPM',
      steps: [
        'Preparing WordPress directory',
        'Checking WP-CLI',
        'Downloading WordPress',
        'Creating WordPress configuration',
        'Configuring Nginx',
        'Configuring PHP-FPM',
        'Finalizing WordPress installation',
      ],
    };
  }

  if (type === 'node') {
    const presetLabels: Record<string, string> = {
      next: 'Next.js',
      nuxt: 'Nuxt',
      generic: 'Node.js',
    };
    const preset = presetLabels[String(payload.node_preset || 'generic').toLowerCase()] || 'Node.js';
    const manager = packageManagerLabel(payload.package_manager);
    const mode = String(payload.node_mode || 'server').toLowerCase();
    const hasApplicationPackage = Boolean(String(payload.repository || '').trim()) || mode !== 'static';
    const activeManager = hasApplicationPackage ? manager : '';
    const steps = [
      'Preparing application directory',
      sourceStep(payload, 'Creating starter application'),
      'Creating environment file',
    ];
    if (manager && hasApplicationPackage) steps.push(`Installing dependencies with ${manager}`);
    if (hasApplicationPackage && String(payload.build_command || '').trim()) steps.push('Building Node.js application');
    if (mode === 'static') steps.push('Preparing static output');
    steps.push('Configuring Nginx');
    if (mode !== 'static') steps.push('Configuring application process');
    steps.push('Finalizing Node.js application');

    return {
      title: 'Provisioning Node.js',
      summary: [preset, mode === 'static' ? 'static build' : 'application server', activeManager].filter(Boolean).join(', '),
      steps,
    };
  }

  if (type === 'python') {
    const presetLabels: Record<string, string> = {
      django: 'Django',
      flask: 'Flask',
      fastapi: 'FastAPI',
      generic: 'Python',
    };
    const preset = presetLabels[String(payload.python_preset || 'generic').toLowerCase()] || 'Python';
    const manager = String(payload.package_manager || 'pip').toLowerCase() === 'uv' ? 'uv' : 'pip';
    const steps = [
      'Preparing application directory',
      sourceStep(payload, 'Creating starter application'),
      'Creating environment file',
      'Creating isolated virtual environment',
      `Installing dependencies with ${manager}`,
    ];
    if (String(payload.build_command || '').trim()) steps.push('Running Python build command');
    steps.push('Configuring Nginx', 'Configuring application process', 'Finalizing Python application');

    return {
      title: 'Provisioning Python',
      summary: `${preset} application server, ${manager}`,
      steps,
    };
  }

  if (type === 'html') {
    return {
      title: 'Provisioning HTML Site',
      summary: 'Static files served directly by Nginx',
      steps: [
        'Preparing web directory',
        sourceStep(payload, 'Creating starter page'),
        'Configuring Nginx',
        'Finalizing static site',
      ],
    };
  }

  const framework = type === 'laravel' ? 'Laravel' : 'PHP';
  const steps = [
    'Preparing application directory',
    sourceStep(payload, 'Creating application files'),
    'Creating environment file',
  ];
  if (String(payload.repository || '').trim() && payload.install_composer !== false) {
    steps.push('Checking Composer dependencies');
  }
  if (String(payload.repository || '').trim()) steps.push('Checking frontend assets');
  steps.push('Configuring Nginx', 'Configuring PHP-FPM', 'Finalizing PHP application');

  return {
    title: `Provisioning ${framework}`,
    summary: `${framework} application with PHP-FPM`,
    steps,
  };
};

const profile = computed(() => buildProvisioningProfile(props.payload));
const steps = ref<Step[]>(profile.value.steps.map(label => ({ label, active: false, complete: false })));
const progressAnnouncement = computed(() => {
  if (steps.value.length > 0 && steps.value.every(step => step.complete)) return 'Site provisioning complete';
  const activeStep = steps.value.find(step => step.active);
  return activeStep ? `${activeStep.label} in progress` : 'Preparing site provisioning';
});

let progressTimer: ReturnType<typeof setInterval> | null = null;
let completeTimeout: ReturnType<typeof setTimeout> | null = null;
let disposed = false;

const startProgress = () => {
  let index = 0;

  const advance = () => {
    if (index >= steps.value.length) {
      if (progressTimer) {
        clearInterval(progressTimer);
        progressTimer = null;
      }
      return;
    }
    if (index > 0) {
      steps.value[index - 1].active = false;
      steps.value[index - 1].complete = true;
    }
    steps.value[index].active = true;
    index++;
  };

  advance();
  progressTimer = setInterval(advance, 4000);
};

const completeAll = () => {
  if (progressTimer) {
    clearInterval(progressTimer);
    progressTimer = null;
  }
  steps.value.forEach(step => {
    step.active = false;
    step.complete = true;
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
    if (disposed) return;
    completeAll();
    completeTimeout = setTimeout(() => {
      completeTimeout = null;
      if (disposed) return;
      visible.value = false;
      emit('created', site);
    }, 600);
  } catch (error: any) {
    if (disposed) return;
    steps.value.forEach(step => {
      step.active = false;
      step.complete = false;
    });
    visible.value = false;
    emit('error', error.message || 'Failed to create site');
  }
});

onUnmounted(() => {
  disposed = true;
  cleanup();
});
</script>
