<template>
  <div class="w-full max-w-full">
    <div
      ref="scrollContainer"
      class="w-full max-w-full overflow-x-auto overscroll-x-contain rounded-lg border border-gray-200 dark:border-gray-700"
      role="region"
      :aria-label="ariaLabel"
      tabindex="0"
    >
    <table ref="tableElement" class="w-full min-w-max divide-y divide-gray-200 dark:divide-gray-800">
      <thead class="bg-gray-50 dark:bg-gray-800">
        <tr>
          <th v-for="col in columns" :key="col.key" scope="col"
            class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider sm:px-6"
            :class="col.class">
            {{ col.label }}
          </th>
          <th v-if="$slots.actions" scope="col" class="sticky right-0 z-10 bg-gray-50 px-4 py-3 shadow-[-1px_0_0_0_var(--color-gray-200)] dark:bg-gray-800 dark:shadow-[-1px_0_0_0_var(--color-gray-700)] sm:px-6">
            <span class="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody class="bg-white dark:bg-gray-900 divide-y divide-gray-200 dark:divide-gray-800">
        <tr v-for="(item, idx) in items" :key="item.id ?? idx" class="group hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
          <td v-for="col in columns" :key="col.key" class="px-4 py-4 whitespace-nowrap text-sm sm:px-6" :class="col.cellClass">
            <slot :name="col.key" :item="item" :value="item[col.key]">{{ item[col.key] }}</slot>
          </td>
          <td v-if="$slots.actions" class="sticky right-0 z-[1] bg-white px-4 py-4 whitespace-nowrap text-right text-sm font-medium shadow-[-1px_0_0_0_var(--color-gray-200)] transition-colors group-hover:bg-gray-50 dark:bg-gray-900 dark:shadow-[-1px_0_0_0_var(--color-gray-700)] dark:group-hover:bg-gray-800 sm:px-6">
            <slot name="actions" :item="item" />
          </td>
        </tr>
        <EmptyState v-if="items.length === 0" mode="table" :col-span="columns.length + ($slots.actions ? 1 : 0)" :message="emptyText" />
      </tbody>
    </table>
    </div>
    <p v-if="isScrollable" class="mt-2 text-right text-[11px] text-gray-400 sm:hidden" aria-hidden="true">Swipe horizontally to view more</p>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import EmptyState from './EmptyState.vue';

interface Column {
  key: string;
  label: string;
  class?: string;
  cellClass?: string;
}

const props = withDefaults(defineProps<{
  columns: Column[];
  items: any[];
  emptyText?: string;
  ariaLabel?: string;
}>(), {
  emptyText: 'No items found.',
  ariaLabel: 'Data table',
});

const scrollContainer = ref<HTMLElement | null>(null);
const tableElement = ref<HTMLTableElement | null>(null);
const isScrollable = ref(false);
let resizeObserver: ResizeObserver | null = null;

const updateScrollable = () => {
  const container = scrollContainer.value;
  isScrollable.value = !!container && container.scrollWidth > container.clientWidth + 1;
};

onMounted(async () => {
  await nextTick();
  updateScrollable();
  resizeObserver = new ResizeObserver(updateScrollable);
  if (scrollContainer.value) resizeObserver.observe(scrollContainer.value);
  if (tableElement.value) resizeObserver.observe(tableElement.value);
});

watch(() => props.items, async () => {
  await nextTick();
  updateScrollable();
}, { deep: true });

onBeforeUnmount(() => resizeObserver?.disconnect());
</script>
