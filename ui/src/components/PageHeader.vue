<template>
  <div>
    <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100 mb-6">{{ title }}</h1>
    <p v-if="subtitle" class="text-sm text-gray-500 dark:text-gray-400 -mt-4 mb-6">{{ subtitle }}</p>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title: string;
  subtitle?: string;
}>();
</script>
