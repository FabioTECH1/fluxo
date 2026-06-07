import { ref } from 'vue';

export interface ConfirmOptions {
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  variant?: 'danger' | 'info';
}

const isOpen = ref(false);
const options = ref<ConfirmOptions>({
  title: 'Confirm',
  message: 'Are you sure?',
  confirmText: 'Confirm',
  cancelText: 'Cancel',
  variant: 'info'
});

let resolvePromise: ((value: boolean) => void) | null = null;

export function useConfirm() {
  const confirm = (opts: ConfirmOptions | string) => {
    isOpen.value = true;
    if (typeof opts === 'string') {
      options.value = {
        title: 'Confirm',
        message: opts,
        confirmText: 'Confirm',
        cancelText: 'Cancel',
        variant: 'info'
      };
    } else {
      options.value = {
        title: opts.title || 'Confirm',
        message: opts.message,
        confirmText: opts.confirmText || 'Confirm',
        cancelText: opts.cancelText || 'Cancel',
        variant: opts.variant || 'info'
      };
    }

    return new Promise<boolean>((resolve) => {
      resolvePromise = resolve;
    });
  };

  const handleConfirm = () => {
    isOpen.value = false;
    if (resolvePromise) {
      resolvePromise(true);
      resolvePromise = null;
    }
  };

  const handleCancel = () => {
    isOpen.value = false;
    if (resolvePromise) {
      resolvePromise(false);
      resolvePromise = null;
    }
  };

  return {
    isOpen,
    options,
    confirm,
    handleConfirm,
    handleCancel
  };
}
