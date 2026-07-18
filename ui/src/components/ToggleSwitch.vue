<template>
  <label
    class="flex items-start gap-3"
    :class="[
      disabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
      labelPosition === 'left' ? 'w-full justify-between' : 'inline-flex',
    ]"
  >
    <button
      type="button"
      role="switch"
      :aria-checked="modelValue"
      :aria-label="label"
      :disabled="disabled"
      class="relative mt-0.5 inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 dark:focus:ring-offset-gray-900"
      :class="[
        modelValue ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600',
        labelPosition === 'left' ? 'order-2' : 'order-1',
      ]"
      @click="emit('update:modelValue', !modelValue)"
    >
      <span
        class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out dark:bg-gray-100"
        :class="modelValue ? 'translate-x-5' : 'translate-x-0'"
      />
    </button>
    <span class="min-w-0 select-none" :class="labelPosition === 'left' ? 'order-1 pr-4' : 'order-2'">
      <span class="block text-sm font-medium text-gray-800 dark:text-gray-200">{{ label }}</span>
      <span v-if="description" class="mt-0.5 block text-xs leading-5 text-gray-500 dark:text-gray-400">{{ description }}</span>
    </span>
  </label>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  modelValue: boolean;
  label: string;
  description?: string;
  disabled?: boolean;
  labelPosition?: 'left' | 'right';
}>(), {
  description: '',
  disabled: false,
  labelPosition: 'right',
});

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();
</script>
