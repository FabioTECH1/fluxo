<template>
  <div v-if="message" role="alert" class="text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/30 border border-red-200 p-3 rounded-lg text-sm mb-4">
    {{ message }}
  </div>
</template>

<script setup lang="ts">
defineProps<{
  message: string;
}>();
</script>
