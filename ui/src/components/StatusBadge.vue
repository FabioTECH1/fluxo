<template>
  <span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full" :class="colorClass">{{ label }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  label: string;
  variant: 'green' | 'red' | 'blue' | 'yellow' | 'gray';
}>();

const colorClass = computed(() => ({
  green: 'bg-green-100 dark:bg-green-900/40 text-green-800 dark:text-green-300',
  red: 'bg-red-100 dark:bg-red-900/40 text-red-800 dark:text-red-300',
  blue: 'bg-blue-100 dark:bg-blue-900/40 text-blue-800 dark:text-blue-300',
  yellow: 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-800 dark:text-yellow-300',
  gray: 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-300',
}[props.variant]));
</script>
