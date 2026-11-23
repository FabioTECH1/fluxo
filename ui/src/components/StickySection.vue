<template>
  <div :class="{ 'sticky-section': active, 'sticky-section-scroll': active && scrollable }" :style="active ? { top: `${top}px`, maxHeight: scrollable ? `${available}px` : undefined } : undefined">
    <div ref="content" :class="contentClass"><slot /></div>
  </div>
</template>
<script setup lang="ts">
import { nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue';
import { stickyGeometry } from '../utils/stickyLayout';
const props = withDefaults(defineProps<{ scrollable?: boolean; minWidth?: number; contentClass?: string }>(), { scrollable: false, minWidth: 768, contentClass: '' });
const content = ref<HTMLElement>();
const active = ref(false), top = ref(0), available = ref(0);
let observer: ResizeObserver | undefined;
let generation = 0;
function measure() {
  const header = document.querySelector<HTMLElement>('[data-app-header]');
  const g = stickyGeometry(window.innerWidth, window.innerHeight, header?.getBoundingClientRect().height || 0, content.value?.scrollHeight || 0, props.minWidth, props.scrollable);
  top.value = g.top; available.value = g.available; active.value = g.active;
}
function stop() { generation++; observer?.disconnect(); observer = undefined; window.removeEventListener('resize', measure); }
async function start() {
  stop(); const current = generation; await nextTick();
  if (current !== generation) return;
  if (!content.value) return;
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measure);
    observer.observe(content.value);
    const header = document.querySelector('[data-app-header]'); if (header) observer.observe(header);
  }
  window.addEventListener('resize', measure); measure();
}
onMounted(start); onActivated(start); onDeactivated(stop); onBeforeUnmount(stop);
</script>
<style scoped>
.sticky-section { position: sticky; align-self: start; }
.sticky-section-scroll { overflow-y: auto; overscroll-behavior-y: contain; }
</style>
