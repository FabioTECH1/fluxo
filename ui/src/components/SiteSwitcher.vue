<template>
 <div ref="root" class="relative min-w-0 max-w-full">
  <h1><button ref="trigger" type="button" class="flex max-w-full items-center gap-2 rounded text-left text-2xl font-bold text-gray-900 focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-gray-100" aria-haspopup="listbox" :aria-expanded="open" :aria-controls="listID" @click="toggle" @keydown.down.prevent="show">
   <span class="truncate" :title="site?.domain">{{ site?.domain || 'Site details' }}</span><ChevronDownIcon class="h-5 w-5 shrink-0 text-gray-400" aria-hidden="true" />
  </button></h1>
  <Teleport to="body">
   <div v-if="open" ref="menu" :style="position" class="fixed z-50 flex max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900" @keydown.esc.prevent.stop="close()" @keydown.tab="leaveMenu">
    <div class="flex shrink-0 items-center gap-2 border-b border-gray-200 px-4 py-3 dark:border-gray-700"><MagnifyingGlassIcon class="h-4 w-4 text-gray-400" aria-hidden="true" /><input ref="input" v-model="query" role="combobox" aria-expanded="true" aria-autocomplete="list" :aria-controls="listID" :aria-activedescendant="filtered[highlighted] ? `${listID}-${filtered[highlighted]!.id}` : undefined" aria-label="Search sites" placeholder="Search sites…" class="min-w-0 flex-1 bg-transparent text-sm text-gray-900 outline-none dark:text-gray-100" @keydown.down.prevent="move(1)" @keydown.up.prevent="move(-1)" @keydown.enter.prevent="choose(filtered[highlighted])" /></div>
    <div :id="listID" role="listbox" aria-label="Switch site" class="min-h-0 overflow-y-auto overscroll-contain">
     <button v-for="(item,index) in filtered" :id="`${listID}-${item.id}`" :key="item.id" type="button" role="option" tabindex="-1" :aria-selected="String(item.id) === String(site?.id)" :title="`${item.domain}\n${siteSummary(item)}`" class="flex w-full items-center gap-3 border-b border-gray-100 px-4 py-3 text-left last:border-0 dark:border-gray-800" :class="index === highlighted ? 'bg-blue-50 dark:bg-blue-950' : 'hover:bg-gray-50 dark:hover:bg-gray-800'" @pointerenter="highlighted=index" @click="choose(item)">
      <AppTypeIcon :app-type="item.app_type || ''" /><span class="min-w-0 flex-1"><span class="block truncate text-sm font-semibold text-gray-900 dark:text-gray-100">{{ item.domain }}</span><span class="mt-1 block truncate text-sm text-gray-500 dark:text-gray-400">{{ siteSummary(item) }}</span></span><CheckIcon v-if="String(item.id) === String(site?.id)" class="h-4 w-4 shrink-0 text-blue-600" aria-label="Current site" />
     </button>
     <p v-if="error" class="p-4 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
     <p v-if="!filtered.length && !error" class="p-4 text-sm text-gray-500" role="status">{{ loading ? 'Loading sites…' : error || 'No matching sites.' }}</p>
    </div>
   </div>
  </Teleport>
 </div>
</template>
<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onMounted, onUnmounted, ref, useId, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { CheckIcon, ChevronDownIcon, MagnifyingGlassIcon } from '@heroicons/vue/24/outline';
import AppTypeIcon from './AppTypeIcon.vue';
import { apiClient } from '../api/client';
import { apiErrorMessage } from '../utils/apiError';
import { siteSummary, switchedSitePath, type SwitchableSite } from '../utils/siteSwitcher';
const props = defineProps<{site: SwitchableSite | null}>();
const route = useRoute(), router = useRouter(), listID = `site-switcher-${useId()}`;
let loadSequence = 0;
const open = ref(false), query = ref(''), highlighted = ref(0), loading = ref(false), error = ref('');
const sites = ref<SwitchableSite[]>([]), root = ref<HTMLElement>(), trigger = ref<HTMLElement>(), input = ref<HTMLInputElement>(), menu = ref<HTMLElement>();
const position = ref<Record<string,string>>({});
const filtered = computed(() => sites.value.filter(s => query.value.trim().toLowerCase().split(/\s+/).every(term=>`${s.domain} ${siteSummary(s)}`.toLowerCase().includes(term))));
function place() {
 const rect = trigger.value?.getBoundingClientRect(); if (!rect) return;
 const width = Math.min(520, Math.max(0, window.innerWidth - 32));
 const top = Math.max(16,Math.min(rect.top,window.innerHeight-16)), bottom = Math.max(16,Math.min(rect.bottom,window.innerHeight-16));
 const below = Math.max(0,window.innerHeight-bottom-24), above = Math.max(0,top-24);
 const useBelow = below >= Math.min(280,above);
 position.value = { left: `${Math.max(16,Math.min(rect.left,window.innerWidth-width-16))}px`, width: `${width}px`, maxHeight: `${Math.min(440,useBelow?below:above)}px`, ...(useBelow ? { top: `${bottom+8}px` } : { bottom: `${window.innerHeight-top+8}px` }) };
}
async function show() {
 if (open.value) { input.value?.focus(); return; }
 const sequence = ++loadSequence;
 open.value = true; query.value = ''; error.value = ''; place();
 await nextTick(); input.value?.focus();
 loading.value = true;
 try { const result = await apiClient.getSites(); if (sequence !== loadSequence) return; sites.value = result; highlighted.value = Math.max(0,filtered.value.findIndex(s => String(s.id) === String(props.site?.id))); await nextTick(); document.getElementById(`${listID}-${props.site?.id}`)?.scrollIntoView({block:'nearest'}); }
 catch(e) { if (sequence === loadSequence) error.value = apiErrorMessage(e); }
 finally { if (sequence === loadSequence) loading.value = false; }
}
function close(focus = true) { open.value = false; ++loadSequence; loading.value = false; if (focus) void nextTick(() => trigger.value?.focus()); }
function toggle() { if(open.value) close(); else void show(); }
function move(delta: number) { if (!filtered.value.length) return; highlighted.value = (highlighted.value+delta+filtered.value.length)%filtered.value.length; void nextTick(() => document.getElementById(`${listID}-${filtered.value[highlighted.value]!.id}`)?.scrollIntoView({block:'nearest'})); }
async function choose(site?: SwitchableSite) { if (!site) return; close(); if (String(site.id) !== String(props.site?.id)) { try { await router.push(switchedSitePath(route.path,site)); } catch(e) { error.value=apiErrorMessage(e); open.value=true; await nextTick(); input.value?.focus(); } } }
function leaveMenu() { close(false); trigger.value?.focus(); }
function outside(e: PointerEvent) { if (!root.value?.contains(e.target as Node) && !menu.value?.contains(e.target as Node)) close(false); }
watch(query,() => {highlighted.value=0;});
watch(() => route.fullPath,() => close(false));
onMounted(() => {document.addEventListener('pointerdown',outside);window.addEventListener('resize',place);window.addEventListener('scroll',place,true);});
onDeactivated(() => close(false));
onActivated(() => { if(open.value) place(); });
onUnmounted(() => {document.removeEventListener('pointerdown',outside);window.removeEventListener('resize',place);window.removeEventListener('scroll',place,true);});
</script>
