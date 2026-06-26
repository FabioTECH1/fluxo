<template>
  <div ref="containerRef" class="relative">
    <button type="button" @click="toggle"
      :disabled="disabled"
      class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 text-left flex items-center justify-between focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow disabled:opacity-50 disabled:cursor-not-allowed font-mono text-sm"
      :class="open ? 'ring-2 ring-blue-500 border-blue-500' : ''">
      <span class="truncate" :class="selectedLabel ? 'text-gray-900 dark:text-gray-100' : 'text-gray-400 dark:text-gray-500'">{{ selectedLabel || placeholder }}</span>
      <svg class="w-4 h-4 text-gray-400 transition-transform shrink-0 ml-2" :class="open ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>
    </button>

    <div v-if="open" class="absolute z-50 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-600 rounded-lg shadow-lg overflow-hidden">
      <div v-if="searchable" class="border-b border-gray-100 dark:border-gray-700 px-3 py-2">
        <input ref="searchInput" v-model="search" type="text" placeholder="Search..."
          @keydown.down.prevent="moveDown" @keydown.up.prevent="moveUp"
          @keydown.enter.prevent="selectHighlighted"
          @keydown.escape.prevent="close"
          @click.stop
          class="w-full border-0 bg-transparent text-sm focus:outline-none dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500" />
      </div>
      <div class="max-h-52 overflow-y-auto">
        <div v-if="filteredOptions.length === 0" class="px-3 py-3 text-sm text-gray-400 dark:text-gray-500 text-center">
          No results found
        </div>
        <div v-for="(opt, i) in filteredOptions" :key="opt.value"
          @mousedown.prevent="select(opt)"
          @mouseenter="highlightedIndex = i"
          class="px-3 py-2 text-sm cursor-pointer transition-colors font-mono truncate"
          :class="i === highlightedIndex ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300' : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'">
          {{ opt.label }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, watch, onUnmounted } from 'vue';

interface Option {
  label: string;
  value: string;
}

const props = withDefaults(defineProps<{
  modelValue: string;
  options: Option[];
  placeholder?: string;
  disabled?: boolean;
  searchable?: boolean;
}>(), {
  placeholder: 'Select...',
  disabled: false,
  searchable: true,
});

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();

const containerRef = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);
const open = ref(false);
const search = ref('');
const highlightedIndex = ref(0);

const selectedLabel = computed(() => {
  const found = props.options.find(o => o.value === props.modelValue);
  return found ? found.label : '';
});

const filteredOptions = computed(() => {
  if (!search.value.trim()) return props.options;
  const q = search.value.toLowerCase();
  return props.options.filter(o => o.label.toLowerCase().includes(q));
});

const toggle = () => {
  if (props.disabled) return;
  open.value = !open.value;
  if (open.value) {
    search.value = '';
    highlightedIndex.value = 0;
    nextTick(() => {
      searchInput.value?.focus();
    });
  }
};

const close = () => {
  open.value = false;
  search.value = '';
};

const select = (opt: Option) => {
  emit('update:modelValue', opt.value);
  close();
};

const moveDown = () => {
  if (highlightedIndex.value < filteredOptions.value.length - 1) {
    highlightedIndex.value++;
  }
};

const moveUp = () => {
  if (highlightedIndex.value > 0) {
    highlightedIndex.value--;
  }
};

const selectHighlighted = () => {
  const opt = filteredOptions.value[highlightedIndex.value];
  if (opt) select(opt);
};

const handleClickOutside = (e: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(e.target as Node)) {
    close();
  }
};

watch(open, (val) => {
  if (val) {
    document.addEventListener('click', handleClickOutside, true);
  } else {
    document.removeEventListener('click', handleClickOutside, true);
  }
});

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside, true);
});
</script>
