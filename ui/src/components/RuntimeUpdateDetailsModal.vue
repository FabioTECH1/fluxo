<template>
 <BaseModal :model-value="modelValue" :title="`${summary?.label || 'Runtime'} update details`" max-width="max-w-xl" @update:model-value="value => emit('update:modelValue',value)">
  <div v-if="summary" class="space-y-5">
   <FormGroup v-if="summaries.length > 1" label="PHP branch" :for-attr="selectID"><select :id="selectID" v-model="selected" class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800"><option v-for="item in summaries" :key="item.id" :value="item.branch">{{ item.label }}</option></select></FormGroup>
   <div class="flex flex-wrap items-center gap-3"><AppTypeIcon :app-type="summary.runtime" /><div class="min-w-0 flex-1"><p class="flex flex-wrap items-center gap-2 text-lg font-semibold text-gray-900 dark:text-gray-100"><span>{{ summary.before }}</span><ArrowLongRightIcon class="h-5 w-5" aria-label="to" /><span>{{ summary.after }}</span></p><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ summary.kind }}<template v-if="notes?.date"> · Released {{ notes.date }}</template></p></div><StatusBadge v-if="notes?.state === 'available' && notes.security" label="Security fixes" variant="yellow" /></div>
   <dl v-if="summary.runtime==='node'" class="flex flex-wrap justify-between gap-2 text-sm"><dt class="text-gray-500 dark:text-gray-400">Bundled npm</dt><dd class="font-medium">{{ summary.changes[1]?.installed }} → {{ summary.changes[1]?.candidate }}</dd></dl>
   <section v-if="summary.target" class="space-y-2"><h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100">What’s in this release</h4><p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-gray-400">Loading official release highlights…</p><ul v-else-if="notes?.state==='available'" class="list-disc space-y-2 pl-5 text-sm leading-6 text-gray-700 dark:text-gray-300"><li v-for="note in notes.highlights" :key="note">{{ note }}</li></ul><p v-else class="text-sm text-gray-500 dark:text-gray-400">Release highlights are unavailable.</p><a :href="officialURL" target="_blank" rel="noopener noreferrer" class="inline-flex text-sm text-blue-600 hover:underline dark:text-blue-400">Official {{ summary.runtime==='php'?'PHP':'Node.js' }} release notes</a><p class="text-xs leading-5 text-gray-500 dark:text-gray-400">Target-release highlights; intervening releases are not summarized.</p></section>
   <p v-else class="text-sm leading-6 text-gray-600 dark:text-gray-400">{{ !summary.changes.length ? 'Exact package details are unavailable. Check for updates again before reviewing.' : summary.kind==='Package revision update' ? 'The PHP upstream version is unchanged. Exact repository package revisions are listed below.' : 'These changes affect packages or extensions. See their exact versions and sources below.' }}</p>
   <div><button type="button" class="text-sm font-medium text-blue-600 hover:underline dark:text-blue-400" :aria-expanded="packagesOpen" :aria-controls="tableID" @click="packagesOpen=!packagesOpen">{{ packagesOpen?'Hide':'Show' }} {{ summary.runtime==='php'?'package':'component' }} changes ({{ summary.changes.length }})</button>
    <div v-if="packagesOpen" :id="tableID" class="mt-3"><DataTable :columns="columns" :items="summary.changes" aria-label="Installed and available versions" scroll-class="max-h-56 overflow-y-auto" sticky-header><template #name="{item}"><code class="block max-w-48 truncate text-xs" :title="item.name">{{ item.name }}</code><span class="block text-xs text-gray-500 dark:text-gray-400">{{ item.kind }}</span><span class="block max-w-48 truncate text-xs text-gray-500 dark:text-gray-400" :title="item.source">{{ item.source }}</span></template><template #versions="{item}"><span class="block max-w-64 truncate font-mono text-xs text-gray-500 dark:text-gray-400" :title="item.installed">{{ item.installed }}</span><span class="mt-1 block max-w-64 truncate font-mono text-xs" :title="item.candidate">{{ item.candidate }}</span></template></DataTable></div>
   </div>
   <p class="text-sm leading-6 text-gray-500 dark:text-gray-400">Review affected sites and services before installing.</p>
  </div>
  <p v-else class="text-sm">The update information changed. Check for updates again.</p>
  <template #footer><AppButton variant="secondary" @click="emit('update:modelValue',false)">Close</AppButton><AppButton :loading="reviewing" :disabled="reviewDisabled || !summary?.id || !summary.changes.length" @click="summary && emit('review',summary)">Review update</AppButton></template>
 </BaseModal>
</template>
<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';
import { ArrowLongRightIcon } from '@heroicons/vue/24/outline';
import { apiClient } from '../api/client';
import type { RuntimeSummary, ReleaseHighlights } from '../utils/runtimeUpdateDetails';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
import AppTypeIcon from './AppTypeIcon.vue';
import DataTable from './DataTable.vue';
import StatusBadge from './StatusBadge.vue';
import FormGroup from './FormGroup.vue';
const props=defineProps<{modelValue:boolean;summaries:RuntimeSummary[];reviewing?:boolean;reviewDisabled?:boolean}>();
const emit=defineEmits<{'update:modelValue':[value:boolean];review:[summary:RuntimeSummary]}>();
const selected=ref(''), packagesOpen=ref(false), notes=ref<ReleaseHighlights|null>(null), loading=ref(false);
const selectID=`runtime-branch-${useId()}`,tableID=`runtime-changes-${useId()}`;
const summary=computed(()=>props.summaries.find(s=>s.branch===selected.value)||props.summaries[0]);
watch(()=>props.summaries.map(s=>s.branch),branches=>{if(!branches.includes(selected.value))selected.value=branches[0] || '';},{immediate:true});
const officialURL=computed(()=>!summary.value?.target?'':summary.value.runtime==='php'?`https://www.php.net/ChangeLog-${summary.value.target.split('.')[0]}.php#${summary.value.target}`:`https://nodejs.org/en/blog/release/v${summary.value.target}`);
const columns=[{key:'name',label:'Component / source'},{key:'versions',label:'Installed / Available'}];
let request=0;
watch([()=>props.modelValue,()=>summary.value?.runtime,()=>summary.value?.target],async()=>{
 const seq=++request;notes.value=null;loading.value=false;packagesOpen.value=false;
 if(!props.modelValue||!summary.value?.target)return;
 const {runtime,target}=summary.value;loading.value=true;
 try{const result=await apiClient.get(`/api/v1/server/runtime-release-notes?runtime=${runtime}&version=${encodeURIComponent(target)}`);if(seq===request)notes.value=result;}catch{ /* Optional context never changes update authority. */ }finally{if(seq===request)loading.value=false;}
},{immediate:true});
</script>
