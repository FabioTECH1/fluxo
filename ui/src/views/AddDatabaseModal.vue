<template>
  <BaseModal v-model="visible" title="Add Database" :loading="loading" confirm-text="Add Database" max-width="max-w-md" @submit="formRef?.requestSubmit()">
    <form ref="formRef" @submit.prevent="submit" class="space-y-5">
      <ErrorAlert :message="error" />
      <FormGroup label="Database Name">
        <input v-model="name" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="my_database">
      </FormGroup>
      <FormGroup label="Engine">
        <select v-model="engine" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm">
          <option v-for="eng in installedEngines" :key="eng" :value="eng">{{ eng === 'mysql' ? 'MySQL' : 'PostgreSQL' }}</option>
        </select>
      </FormGroup>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';
import BaseModal from '../components/BaseModal.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';

const visible = defineModel<boolean>({ required: true });
const emit = defineEmits(['created']);

const formRef = ref<HTMLFormElement | null>(null);
const name = ref('');
const engine = ref('mysql');
const loading = ref(false);
const error = ref('');
const installedEngines = ref<string[]>(['mysql']);

onMounted(async () => {
  try {
    const engines: string[] = await apiClient.getDatabaseEngines() || [];
    const dbs = engines.filter(e => e === 'mysql' || e === 'postgres');
    if (dbs.length > 0) {
      installedEngines.value = dbs;
      engine.value = dbs[0];
    }
  } catch (e) { console.error(e); }
});

const submit = async () => {
  loading.value = true;
  error.value = '';
  try {
    await apiClient.createDatabase({ name: name.value, engine: engine.value });
    emit('created');
  } catch (e: any) {
    error.value = e.message || 'Failed to create database';
  } finally {
    loading.value = false;
  }
};
</script>
