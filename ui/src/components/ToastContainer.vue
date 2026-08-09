<template>
  <div
    class="pointer-events-none fixed inset-x-0 bottom-0 z-[100] flex flex-col items-center gap-3 px-4 pb-[calc(1rem+env(safe-area-inset-bottom))] sm:pb-[calc(1.5rem+env(safe-area-inset-bottom))]"
    aria-label="Notifications"
  >
    <TransitionGroup name="toast" tag="div" class="relative flex w-full flex-col items-center gap-3">
      <section
        v-for="toast in visibleToasts"
        :key="toast.id"
        class="pointer-events-auto relative w-full max-w-[28rem] overflow-hidden rounded-xl border border-gray-200 bg-white text-gray-950 shadow-[0_18px_48px_-18px_rgba(15,23,42,0.45)] dark:border-gray-700/80 dark:bg-gray-900 dark:text-gray-50 dark:shadow-[0_20px_50px_-16px_rgba(0,0,0,0.75)]"
        :role="toast.type === 'error' ? 'alert' : 'status'"
        :aria-live="toast.type === 'error' ? 'assertive' : 'polite'"
        :aria-atomic="true"
        @mouseenter="handleMouseEnter(toast.id)"
        @mouseleave="handleMouseLeave(toast.id)"
        @focusin="handleFocusIn(toast.id)"
        @focusout="handleFocusOut(toast.id, $event)"
      >
        <div class="flex min-h-[4.75rem] items-start gap-4 px-5 py-5 sm:items-center">
          <span
            class="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-lg sm:mt-0"
            :class="iconWellClass(toast.type)"
            aria-hidden="true"
          >
            <ArrowPathIcon v-if="toast.type === 'loading'" class="h-7 w-7 animate-spin stroke-2 motion-reduce:animate-none" />
            <CheckCircleIcon v-else-if="toast.type === 'success'" class="h-5 w-5" />
            <XCircleIcon v-else-if="toast.type === 'error'" class="h-5 w-5" />
            <ExclamationTriangleIcon v-else-if="toast.type === 'warning'" class="h-5 w-5" />
            <InformationCircleIcon v-else class="h-5 w-5" />
          </span>

          <div class="min-w-0 flex-1">
            <div :id="`toast-content-${toast.id}`">
              <p
                class="pr-1 text-sm font-semibold leading-5 tracking-[-0.01em] text-gray-950 [overflow-wrap:anywhere] dark:text-white"
                :class="isExpanded(toast.id) ? 'max-h-32 overflow-y-auto overscroll-contain' : 'line-clamp-3'"
              >
                {{ toast.title }}
              </p>
              <p
                v-if="toast.description"
                class="mt-0.5 pr-1 text-sm leading-5 text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400"
                :class="isExpanded(toast.id) ? 'max-h-48 overflow-y-auto overscroll-contain' : 'line-clamp-3'"
              >
                {{ toast.description }}
              </p>
            </div>

            <div
              v-if="needsExpansion(toast) || toast.action"
              class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1"
            >
              <button
                v-if="needsExpansion(toast)"
                type="button"
                class="inline-flex min-h-8 items-center rounded-md py-1 pr-2 text-sm font-medium text-gray-600 transition-colors hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-300 dark:hover:text-white"
                :aria-expanded="isExpanded(toast.id)"
                :aria-controls="`toast-content-${toast.id}`"
                @click="toggleExpanded(toast.id)"
              >
                {{ isExpanded(toast.id) ? 'Hide details' : 'Show details' }}
              </button>

              <RouterLink
                v-if="toast.action?.to"
                :to="toast.action.to"
                class="inline-flex min-h-8 items-center rounded-md py-1 pr-2 text-sm font-medium text-blue-600 transition-colors hover:text-blue-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-blue-400 dark:hover:text-blue-300 sm:hidden"
                @click="handleAction(toast)"
              >
                {{ toast.action.label }}
              </RouterLink>
              <button
                v-else-if="toast.action"
                type="button"
                class="inline-flex min-h-8 items-center rounded-md py-1 pr-2 text-sm font-medium text-blue-600 transition-colors hover:text-blue-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-blue-400 dark:hover:text-blue-300 sm:hidden"
                @click="handleAction(toast)"
              >
                {{ toast.action.label }}
              </button>
            </div>

            <div
              v-if="toast.type === 'loading' || toast.duration"
              class="mt-2.5 h-0.5 max-w-56 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700"
              aria-hidden="true"
            >
              <span
                v-if="toast.type === 'loading'"
                class="toast-progress-indeterminate block h-full w-1/3 rounded-full bg-blue-500"
                :class="{ 'toast-progress-paused': toast.paused }"
              />
              <span
                v-else
                :key="`${toast.id}-${toast.version}`"
                class="toast-progress-countdown block h-full w-full origin-left rounded-full"
                :class="[progressClass(toast.type), { 'toast-progress-paused': toast.paused }]"
                :style="{ animationDuration: `${toast.duration}ms` }"
              />
            </div>
          </div>

          <RouterLink
            v-if="toast.action?.to"
            :to="toast.action.to"
            class="hidden min-h-8 shrink-0 items-center rounded-md px-1.5 py-1 text-sm font-medium text-blue-600 transition-colors hover:text-blue-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-blue-400 dark:hover:text-blue-300 sm:inline-flex"
            @click="handleAction(toast)"
          >
            {{ toast.action.label }}
          </RouterLink>
          <button
            v-else-if="toast.action"
            type="button"
            class="hidden min-h-8 shrink-0 items-center rounded-md px-1.5 py-1 text-sm font-medium text-blue-600 transition-colors hover:text-blue-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-blue-400 dark:hover:text-blue-300 sm:inline-flex"
            @click="handleAction(toast)"
          >
            {{ toast.action.label }}
          </button>

          <button
            v-if="toast.dismissible"
            type="button"
            class="-mr-1 flex h-8 w-8 shrink-0 items-center justify-center self-start rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-gray-500 dark:hover:bg-gray-800 dark:hover:text-gray-200 sm:self-center"
            :aria-label="`Dismiss ${toast.title}`"
            @click="removeToast(toast.id)"
          >
            <XMarkIcon class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
      </section>
    </TransitionGroup>
  </div>
</template>

<script setup lang="ts">
import {
  ArrowPathIcon,
  CheckCircleIcon,
  ExclamationTriangleIcon,
  InformationCircleIcon,
  XCircleIcon,
  XMarkIcon,
} from '@heroicons/vue/24/outline';
import { nextTick, ref, watch } from 'vue';
import { RouterLink } from 'vue-router';
import { useToast, type Toast, type ToastType } from '../composables/useToast';

const { visibleToasts, removeToast, pauseToast, resumeToast } = useToast();
const expandedToastIds = ref(new Set<number>());
const hoveredToastIds = new Set<number>();
const focusedToastIds = new Set<number>();

watch(
  () => visibleToasts.value.map(toast => toast.id),
  ids => {
    const visibleIds = new Set(ids);
    expandedToastIds.value = new Set([...expandedToastIds.value].filter(id => visibleIds.has(id)));
    for (const id of hoveredToastIds) {
      if (!visibleIds.has(id)) hoveredToastIds.delete(id);
    }
    for (const id of focusedToastIds) {
      if (!visibleIds.has(id)) focusedToastIds.delete(id);
    }
  },
);

watch(
  () => visibleToasts.value.map(toast => `${toast.id}:${toast.version}`),
  async keys => {
    await nextTick();
    for (const key of keys) {
      const id = Number(key.split(':', 1)[0]);
      if (hoveredToastIds.has(id) || focusedToastIds.has(id)) pauseToast(id);
    }
  },
);

const needsExpansion = (toast: Toast) => (
  toast.title.length > 80
  || (toast.title.match(/\n/g)?.length || 0) >= 3
  || (toast.description?.length || 0) > 160
  || (toast.description?.match(/\n/g)?.length || 0) >= 3
);

const isExpanded = (id: number) => expandedToastIds.value.has(id);

const toggleExpanded = (id: number) => {
  const next = new Set(expandedToastIds.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  expandedToastIds.value = next;
};

const iconWellClass = (type: ToastType) => ({
  loading: 'bg-transparent text-blue-600 dark:text-blue-400',
  success: 'bg-green-50 text-green-600 dark:bg-green-500/10 dark:text-green-400',
  error: 'bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-400',
  warning: 'bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-400',
  info: 'bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-400',
}[type]);

const progressClass = (type: ToastType) => ({
  loading: 'bg-blue-500',
  success: 'bg-green-500',
  error: 'bg-red-500',
  warning: 'bg-amber-500',
  info: 'bg-blue-500',
}[type]);

const handleAction = (toast: Toast) => {
  try {
    toast.action?.onClick?.();
  } finally {
    if (toast.action?.dismissOnClick !== false) removeToast(toast.id);
  }
};

const handleMouseEnter = (id: number) => {
  hoveredToastIds.add(id);
  pauseToast(id);
};

const handleMouseLeave = (id: number) => {
  hoveredToastIds.delete(id);
  if (!focusedToastIds.has(id)) resumeToast(id);
};

const handleFocusIn = (id: number) => {
  focusedToastIds.add(id);
  pauseToast(id);
};

const handleFocusOut = (id: number, event: FocusEvent) => {
  const current = event.currentTarget as HTMLElement | null;
  if (current?.contains(event.relatedTarget as Node | null)) return;
  focusedToastIds.delete(id);
  if (!hoveredToastIds.has(id)) resumeToast(id);
};
</script>

<style scoped>
.toast-enter-active,
.toast-leave-active,
.toast-move {
  transition:
    opacity 240ms ease,
    transform 300ms cubic-bezier(0.16, 1, 0.3, 1);
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(14px) scale(0.98);
}

.toast-leave-active {
  position: absolute;
}

.toast-progress-indeterminate {
  animation: toast-indeterminate 1.35s ease-in-out infinite;
}

.toast-progress-countdown {
  animation-name: toast-countdown;
  animation-timing-function: linear;
  animation-fill-mode: forwards;
}

.toast-progress-paused {
  animation-play-state: paused;
}

@keyframes toast-indeterminate {
  from { transform: translateX(-120%); }
  to { transform: translateX(360%); }
}

@keyframes toast-countdown {
  from { transform: scaleX(1); }
  to { transform: scaleX(0); }
}

@media (prefers-reduced-motion: reduce) {
  .toast-enter-active,
  .toast-leave-active,
  .toast-move {
    transition-duration: 1ms;
  }

  .toast-progress-indeterminate {
    animation: none;
    transform: translateX(100%);
  }

  .toast-progress-countdown {
    animation: none;
    transform: scaleX(1);
  }
}
</style>
