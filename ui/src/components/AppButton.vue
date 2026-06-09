<template>
  <component v-if="to" :is="'router-link'" :to="to" :class="computedClass">
    <slot />
  </component>
  <button v-else :type="type" :disabled="disabled || loading" :class="computedClass" @click="$emit('click')">
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{
  variant?: 'primary' | 'secondary' | 'danger';
  size?: 'sm' | 'md';
  loading?: boolean;
  disabled?: boolean;
  to?: string;
  type?: 'button' | 'submit';
}>(), {
  variant: 'primary',
  size: 'md',
  loading: false,
  disabled: false,
  type: 'button',
});

defineEmits<{
  click: [];
}>();

const computedClass = computed(() => {
  const sizes = props.size === 'sm' ? 'px-3 py-1.5 text-xs' : 'px-4 py-2 text-sm';
  const base = {
    primary: 'text-white bg-blue-600 hover:bg-blue-700 shadow-sm',
    secondary: 'text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700',
    danger: 'text-white bg-red-600 hover:bg-red-700 shadow-sm',
  }[props.variant];

  return `${sizes} ${base} rounded-lg font-semibold transition-colors disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-offset-2 inline-flex items-center gap-1.5`;
});
</script>
