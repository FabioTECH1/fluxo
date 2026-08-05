<template>
  <nav class="w-full md:w-56 shrink-0">
    <div ref="navScroller" class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 overflow-hidden flex flex-row md:flex-col overflow-x-auto scrollbar-none">
      <router-link v-for="item in items" :key="item.to" :to="item.to"
        class="px-4 py-3 text-sm font-medium border-b-2 md:border-b-0 md:border-l-4 transition-colors whitespace-nowrap flex-1 md:flex-initial text-center md:text-left"
        :class="itemClasses[item.to]" :aria-current="activeItems[item.to] ? 'page' : undefined">
        <span class="flex items-center justify-center md:justify-start gap-2">
          <span v-if="item.icon" v-html="item.icon" class="[&>svg]:w-4 [&>svg]:h-4 shrink-0" />
          {{ item.label }}
        </span>
      </router-link>
    </div>
  </nav>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

interface SidebarItem {
  to: string;
  label: string;
  icon?: string;
  match?: string;
  also?: string[];
}

const props = defineProps<{
  items: SidebarItem[];
}>();

const route = useRoute();
const navScroller = ref<HTMLElement | null>(null);

const activeItems = computed(() => {
  const map: Record<string, boolean> = {};
  for (const item of props.items) {
    const match = item.match || item.to;
    let active = route.path === match || route.path.startsWith(match + '/');
    if (!active && item.also) active = item.also.some(path => route.path === path);
    map[item.to] = active;
  }
  return map;
});

const itemClasses = computed(() => {
  const activeCls = 'border-blue-600 bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300';
  const inactiveCls = 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200';
  const map: Record<string, string> = {};

  for (const item of props.items) {
    map[item.to] = activeItems.value[item.to] ? activeCls : inactiveCls;
  }
  return map;
});

const revealActiveItem = async () => {
  await nextTick();
  const scroller = navScroller.value;
  if (!scroller || window.matchMedia('(min-width: 768px)').matches) return;
  const active = scroller.querySelector<HTMLElement>('[aria-current="page"]');
  if (!active) return;
  const left = active.offsetLeft - (scroller.clientWidth - active.clientWidth) / 2;
  scroller.scrollTo({ left: Math.max(0, left), behavior: 'smooth' });
};

onMounted(revealActiveItem);
watch(() => route.path, revealActiveItem);
</script>
