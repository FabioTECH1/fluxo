<template>
  <span ref="triggerContainer" class="inline-block">
    <button
      type="button"
      class="rounded-lg px-2.5 py-1.5 text-xs font-bold tracking-wider text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 disabled:cursor-wait disabled:opacity-60 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200"
      :disabled="disabled || loading"
      :aria-label="ariaLabel"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click.stop="toggle"
    >
      {{ loading ? '…' : '•••' }}
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="menuElement"
        class="fixed z-[80] overflow-hidden rounded-xl border border-gray-200 bg-white py-1 shadow-xl dark:border-gray-700 dark:bg-gray-900"
        :style="menuStyle"
        role="menu"
        @click.stop
      >
        <button
          v-for="item in items"
          :key="item.id"
          type="button"
          class="block w-full bg-transparent px-4 py-2 text-left text-[13px] transition-colors disabled:cursor-not-allowed disabled:opacity-50"
          :class="item.variant === 'danger'
            ? 'text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40'
            : item.variant === 'primary'
              ? 'text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40'
              : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
          :disabled="item.disabled"
          role="menuitem"
          @click="select(item.id)"
        >
          {{ item.label }}
        </button>
      </div>
    </Teleport>
  </span>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue';

interface MenuItem {
  id: string;
  label: string;
  variant?: 'default' | 'primary' | 'danger';
  disabled?: boolean;
}

const props = withDefaults(defineProps<{
  items: MenuItem[];
  ariaLabel?: string;
  width?: number;
  disabled?: boolean;
  loading?: boolean;
}>(), {
  ariaLabel: 'Row actions',
  width: 192,
  disabled: false,
  loading: false,
});

const emit = defineEmits<{
  select: [id: string];
}>();

const triggerContainer = ref<HTMLElement | null>(null);
const menuElement = ref<HTMLElement | null>(null);
const menuStyle = ref<Record<string, string>>({});
const open = ref(false);

const updatePosition = () => {
  const trigger = triggerContainer.value;
  if (!trigger) return;

  const rect = trigger.getBoundingClientRect();
  const viewportPadding = 8;
  const gap = 4;
  const width = Math.min(props.width, window.innerWidth - viewportPadding * 2);
  const left = Math.min(
    Math.max(viewportPadding, rect.right - width),
    window.innerWidth - width - viewportPadding,
  );
  const menuHeight = menuElement.value?.offsetHeight || 0;
  const fitsBelow = rect.bottom + gap + menuHeight <= window.innerHeight - viewportPadding;
  const top = fitsBelow
    ? rect.bottom + gap
    : Math.max(viewportPadding, rect.top - menuHeight - gap);

  menuStyle.value = {
    left: `${left}px`,
    top: `${top}px`,
    width: `${width}px`,
  };
};

const close = () => {
  open.value = false;
};

const toggle = async () => {
  open.value = !open.value;
  if (!open.value) return;
  updatePosition();
  await nextTick();
  updatePosition();
};

const select = (id: string) => {
  const item = props.items.find(candidate => candidate.id === id);
  if (item?.disabled) return;
  close();
  emit('select', id);
};

const handlePointerDown = (event: PointerEvent) => {
  const target = event.target as Node;
  if (!triggerContainer.value?.contains(target) && !menuElement.value?.contains(target)) close();
};

const handleKeyDown = (event: KeyboardEvent) => {
  if (event.key === 'Escape') close();
};

watch(open, value => {
  if (value) {
    window.addEventListener('scroll', updatePosition, true);
    window.addEventListener('resize', updatePosition);
    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
  } else {
    window.removeEventListener('scroll', updatePosition, true);
    window.removeEventListener('resize', updatePosition);
    document.removeEventListener('pointerdown', handlePointerDown, true);
    document.removeEventListener('keydown', handleKeyDown);
  }
});

onBeforeUnmount(() => {
  window.removeEventListener('scroll', updatePosition, true);
  window.removeEventListener('resize', updatePosition);
  document.removeEventListener('pointerdown', handlePointerDown, true);
  document.removeEventListener('keydown', handleKeyDown);
});
</script>
