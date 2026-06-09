<template>
  <tr v-if="mode === 'table'">
    <td :colspan="colSpan" class="px-6 py-8 text-center text-gray-500 dark:text-gray-400 text-sm">{{ message }}</td>
  </tr>
  <div v-else class="py-12 text-center text-gray-500 dark:text-gray-400 text-sm">
    {{ message }}
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  message: string;
  mode?: 'table' | 'standalone';
  colSpan?: number;
}>(), {
  mode: 'standalone',
  colSpan: 4,
});
</script>
