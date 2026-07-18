<template>
  <div v-if="totalItems > 0" class="mt-3 flex flex-col gap-3 text-sm sm:flex-row sm:items-center sm:justify-between">
    <p class="text-xs text-gray-500 dark:text-gray-400">
      Showing {{ firstItem }}–{{ lastItem }} of {{ totalItems }}
    </p>
    <div v-if="totalPages > 1" class="flex items-center justify-between gap-3 sm:justify-end">
      <AppButton variant="secondary" size="sm" :disabled="safePage <= 1" @click="changePage(safePage - 1)">Previous</AppButton>
      <span class="whitespace-nowrap text-xs text-gray-600 dark:text-gray-400">Page {{ safePage }} of {{ totalPages }}</span>
      <AppButton variant="secondary" size="sm" :disabled="safePage >= totalPages" @click="changePage(safePage + 1)">Next</AppButton>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import AppButton from './AppButton.vue';

const props = withDefaults(defineProps<{
  page: number;
  totalItems: number;
  pageSize?: number;
}>(), {
  pageSize: 5,
});

const emit = defineEmits<{
  'update:page': [page: number];
}>();

const totalPages = computed(() => Math.max(1, Math.ceil(props.totalItems / props.pageSize)));
const safePage = computed(() => Math.min(Math.max(1, props.page), totalPages.value));
const firstItem = computed(() => (safePage.value - 1) * props.pageSize + 1);
const lastItem = computed(() => Math.min(safePage.value * props.pageSize, props.totalItems));

const changePage = (page: number) => {
  emit('update:page', Math.min(Math.max(1, page), totalPages.value));
};
</script>
