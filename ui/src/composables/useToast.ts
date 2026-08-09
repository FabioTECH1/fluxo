import { computed, ref } from 'vue';

export type ToastType = 'success' | 'error' | 'info' | 'warning' | 'loading';

export interface ToastAction {
  label: string;
  to?: string;
  onClick?: () => void;
  dismissOnClick?: boolean;
}

export interface ToastOptions {
  title: string;
  description?: string;
  type?: ToastType;
  duration?: number | null;
  dismissible?: boolean;
  action?: ToastAction;
}

export interface Toast extends ToastOptions {
  id: number;
  type: ToastType;
  duration: number | null;
  dismissible: boolean;
  paused: boolean;
  version: number;
}

export interface ToastUpdate extends Omit<Partial<ToastOptions>, 'description' | 'action'> {
  description?: string | null;
  action?: ToastAction | null;
}

type ToastContent = string | Omit<ToastOptions, 'type'>;

export interface ToastPromiseMessages<T> {
  loading: ToastContent;
  success: ToastContent | ((value: T) => ToastContent);
  error: ToastContent | ((reason: unknown) => ToastContent);
}

interface ToastTimer {
  handle: ReturnType<typeof setTimeout> | null;
  remaining: number;
  startedAt: number;
}

const DEFAULT_DURATIONS: Record<ToastType, number | null> = {
  success: 4500,
  error: 8000,
  info: 5000,
  warning: 6500,
  loading: null,
};

const MAX_VISIBLE_TOASTS = 4;
const MAX_RETAINED_TOASTS = 32;
const toasts = ref<Toast[]>([]);
const visibleToasts = computed(() => toasts.value.slice(-MAX_VISIBLE_TOASTS));
const timers = new Map<number, ToastTimer>();
let nextId = 1;

const clearTimer = (id: number) => {
  const timer = timers.get(id);
  if (timer?.handle !== null && timer?.handle !== undefined) clearTimeout(timer.handle);
  timers.delete(id);
};

const removeToast = (id: number) => {
  clearTimer(id);
  toasts.value = toasts.value.filter(toast => toast.id !== id);
};

const scheduleToast = (toast: Toast, remaining = toast.duration) => {
  clearTimer(toast.id);
  if (remaining === null || remaining <= 0) return;

  const timer: ToastTimer = {
    handle: null,
    remaining,
    startedAt: Date.now(),
  };
  timer.handle = setTimeout(() => removeToast(toast.id), remaining);
  timers.set(toast.id, timer);
};

const trimRetainedOverflow = () => {
  while (toasts.value.length >= MAX_RETAINED_TOASTS) {
    const removable = toasts.value.find(toast => toast.duration !== null) || toasts.value[0];
    if (!removable) return;
    removeToast(removable.id);
  }
};

const showToast = (input: string | ToastOptions, legacyType: ToastType = 'info') => {
  trimRetainedOverflow();
  const options: ToastOptions = typeof input === 'string'
    ? { title: input, type: legacyType }
    : input;
  const type = options.type || legacyType;
  const toast: Toast = {
    ...options,
    id: nextId++,
    type,
    duration: options.duration === undefined ? DEFAULT_DURATIONS[type] : options.duration,
    // Indefinite loading notifications represent active work. Keeping them in
    // place guarantees that their success or failure update remains visible.
    dismissible: options.dismissible ?? type !== 'loading',
    paused: false,
    version: 0,
  };

  toasts.value.push(toast);
  scheduleToast(toast);
  return toast.id;
};

const updateToast = (id: number, update: ToastUpdate) => {
  const index = toasts.value.findIndex(toast => toast.id === id);
  if (index === -1) return false;

  const current = toasts.value[index];
  const type = update.type || current.type;
  const duration = update.duration !== undefined
    ? update.duration
    : update.type && update.type !== current.type
      ? DEFAULT_DURATIONS[type]
      : current.duration;
  const dismissible = update.dismissible !== undefined
    ? update.dismissible
    : update.type && update.type !== current.type
      ? type !== 'loading'
      : current.dismissible;

  const next: Toast = {
    ...current,
    ...update,
    description: update.description === null ? undefined : update.description ?? current.description,
    action: update.action === null ? undefined : update.action ?? current.action,
    type,
    duration,
    dismissible,
    paused: false,
    version: current.version + 1,
  };

  // A lifecycle update is the newest information. Moving it to the end also
  // brings a previously hidden operation back into the four-toast viewport.
  toasts.value.splice(index, 1);
  toasts.value.push(next);
  scheduleToast(next);
  return true;
};

const pauseToast = (id: number) => {
  const toast = toasts.value.find(item => item.id === id);
  const timer = timers.get(id);
  if (!toast || !timer || toast.paused) return;

  if (timer.handle !== null) clearTimeout(timer.handle);
  timer.remaining = Math.max(0, timer.remaining - (Date.now() - timer.startedAt));
  timer.handle = null;
  toast.paused = true;
};

const resumeToast = (id: number) => {
  const toast = toasts.value.find(item => item.id === id);
  const timer = timers.get(id);
  if (!toast || !timer || !toast.paused) return;

  toast.paused = false;
  if (timer.remaining <= 0) {
    removeToast(id);
    return;
  }
  timer.startedAt = Date.now();
  timer.handle = setTimeout(() => removeToast(id), timer.remaining);
};

const contentOptions = (content: ToastContent, type: ToastType): ToastOptions => (
  typeof content === 'string' ? { title: content, type } : { ...content, type }
);

export function useToast() {
  // Kept for every existing caller. Returning the id is additive and backwards-compatible.
  const addToast = (message: string, type: Exclude<ToastType, 'loading' | 'warning'> = 'info') => (
    showToast(message, type)
  );

  const loading = (title: string, options: Omit<ToastOptions, 'title' | 'type'> = {}) => (
    showToast({ ...options, title, type: 'loading' })
  );
  const success = (title: string, options: Omit<ToastOptions, 'title' | 'type'> = {}) => (
    showToast({ ...options, title, type: 'success' })
  );
  const error = (title: string, options: Omit<ToastOptions, 'title' | 'type'> = {}) => (
    showToast({ ...options, title, type: 'error' })
  );
  const info = (title: string, options: Omit<ToastOptions, 'title' | 'type'> = {}) => (
    showToast({ ...options, title, type: 'info' })
  );
  const warning = (title: string, options: Omit<ToastOptions, 'title' | 'type'> = {}) => (
    showToast({ ...options, title, type: 'warning' })
  );

  const promise = async <T>(operation: Promise<T> | (() => Promise<T>), messages: ToastPromiseMessages<T>) => {
    const id = showToast(contentOptions(messages.loading, 'loading'));
    try {
      const value = await (typeof operation === 'function' ? operation() : operation);
      const content = typeof messages.success === 'function' ? messages.success(value) : messages.success;
      updateToast(id, contentOptions(content, 'success'));
      return value;
    } catch (reason) {
      const content = typeof messages.error === 'function' ? messages.error(reason) : messages.error;
      updateToast(id, contentOptions(content, 'error'));
      throw reason;
    }
  };

  return {
    toasts,
    visibleToasts,
    addToast,
    showToast,
    updateToast,
    removeToast,
    dismissToast: removeToast,
    pauseToast,
    resumeToast,
    loading,
    success,
    error,
    info,
    warning,
    promise,
  };
}
