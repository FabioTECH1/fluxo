<template>
  <div>
    <label :for="forAttr" class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">
      {{ label }}
      <span v-if="hint" class="text-gray-400 dark:text-gray-500 font-normal">{{ hint }}</span>
    </label>
    <slot />
    <p v-if="error" class="text-xs text-red-600 dark:text-red-400 mt-1">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  label: string;
  forAttr?: string;
  error?: string;
  hint?: string;
}>();
</script>
