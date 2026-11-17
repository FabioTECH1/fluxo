<template><div ref="container" class="h-[32rem] min-w-0 overflow-hidden rounded-lg bg-[#101418] p-3" aria-label="Interactive Laravel Tinker console" /></template>
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { terminalOutput } from '../utils/terminalOutput';
const emit = defineEmits<{ input: [data: string]; resize: [rows: number, cols: number]; overflow: []; rendered: [bytes: number] }>();
const container = ref<HTMLElement>();
let term: Terminal | undefined;
let observer: ResizeObserver | undefined;
let fit: FitAddon;
const output = terminalOutput((data, complete) => term?.write(data, complete), () => emit('overflow'), 256 * 1024, bytes => emit('rendered', bytes));
onMounted(() => {
  term = new Terminal({ cursorBlink: true, scrollback: 1000, fontSize: 14, fontFamily: 'monospace', theme: { background: '#101418', foreground: '#e5e7eb' }, allowProposedApi: false });
  fit = new FitAddon(); term.loadAddon(fit); term.open(container.value!);
  // No clipboard, links, images, HTML or download addons. Block OSC 52 even if
  // terminal capabilities change in a dependency update.
  term.parser.registerOscHandler(52, () => true);
  term.parser.registerOscHandler(8, () => true);
  term.onData(data => emit('input', data));
  term.onResize(({ rows, cols }) => emit('resize', rows, cols));
  observer = new ResizeObserver(() => { fit.fit(); }); observer.observe(container.value!);
  fit.fit(); term.focus();
});
onBeforeUnmount(() => { output.close(); observer?.disconnect(); term?.dispose(); term = undefined; });
defineExpose({ write: output.write, focus: () => term?.focus(), resize: () => { fit?.fit(); if (term) emit('resize', term.rows, term.cols); } });
</script>
