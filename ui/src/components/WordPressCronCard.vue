<template>
  <Card>
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-gray-100">WordPress</h3>
    <ToggleSwitch :model-value="Boolean(state?.enabled)" label="WordPress Cron" label-position="left"
      description="Run due tasks every minute, even without visitors." :disabled="busy || !state"
      @update:model-value="change" />
    <ErrorAlert v-if="error || state?.message" class="mt-3" :message="error || state?.message || ''" />
    <div v-if="!state || state.phase === 'enabling' || state.phase === 'disabling' || state.phase === 'attention'" class="mt-3 flex flex-wrap gap-2">
      <AppButton variant="secondary" :disabled="busy" @click="state ? retry() : refresh()">{{ state ? 'Retry setup' : 'Retry' }}</AppButton>
      <AppButton v-if="state?.cron_id" variant="secondary" :disabled="busy" @click="change(false)">Turn off</AppButton>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, onActivated, watch } from 'vue';
import Card from './Card.vue';
import ToggleSwitch from './ToggleSwitch.vue';
import ErrorAlert from './ErrorAlert.vue';
import AppButton from './AppButton.vue';
import { useWordPressCron } from '../composables/useWordPressCron';
const props = defineProps<{ siteId: string }>();
const emit = defineEmits<{ changed: [] }>();
const { state, busy, error, refresh, setEnabled } = useWordPressCron(() => props.siteId);
async function change(enabled: boolean) { if (await setEnabled(enabled)) emit('changed'); }
function retry() { return change(state.value?.phase !== 'disabling'); }
onMounted(() => refresh());
let firstActivation = true;
onActivated(() => { if (firstActivation) firstActivation = false; else refresh(); });
watch(() => props.siteId, () => refresh());
</script>
