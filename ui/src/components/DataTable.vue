<template>
  <div class="overflow-x-auto border border-gray-200 dark:border-gray-700 rounded-lg">
    <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-800">
      <thead class="bg-gray-50 dark:bg-gray-800">
        <tr>
          <th v-for="col in columns" :key="col.key" scope="col"
            class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider"
            :class="col.class">
            {{ col.label }}
          </th>
          <th v-if="$slots.actions" scope="col" class="relative px-6 py-3">
            <span class="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody class="bg-white dark:bg-gray-900 divide-y divide-gray-200 dark:divide-gray-800">
        <tr v-for="(item, idx) in items" :key="item.id ?? idx" class="hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
          <td v-for="col in columns" :key="col.key" class="px-6 py-4 whitespace-nowrap text-sm" :class="col.cellClass">
            <slot :name="col.key" :item="item" :value="item[col.key]">{{ item[col.key] }}</slot>
          </td>
          <td v-if="$slots.actions" class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
            <slot name="actions" :item="item" />
          </td>
        </tr>
        <EmptyState v-if="items.length === 0" mode="table" :col-span="columns.length + ($slots.actions ? 1 : 0)" :message="emptyText" />
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import EmptyState from './EmptyState.vue';

interface Column {
  key: string;
  label: string;
  class?: string;
  cellClass?: string;
}

withDefaults(defineProps<{
  columns: Column[];
  items: any[];
  emptyText?: string;
}>(), {
  emptyText: 'No items found.',
});
</script>
