<template>
  <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800" :class="[padding && 'p-6', $attrs.class]">
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  padding?: boolean;
}>(), {
  padding: true,
});
</script>
