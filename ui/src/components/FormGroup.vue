<template>
  <div>
    <label :for="forAttr" class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1.5">
      {{ label }}
    </label>
    <slot />
    <p v-if="hint" class="text-xs text-gray-400 dark:text-gray-500 mt-1.5 leading-relaxed">{{ hint }}</p>
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
