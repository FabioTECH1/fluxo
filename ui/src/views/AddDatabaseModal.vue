<template>
  <BaseModal v-model="visible" title="Add Database" :loading="loading" confirm-text="Add Database" max-width="max-w-md" @submit="formRef?.requestSubmit()">
    <form ref="formRef" @submit.prevent="submit" class="space-y-5">
      <ErrorAlert :message="error" />
      <FormGroup label="Database Name">
        <input v-model="name" type="text" required class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="my_database">
      </FormGroup>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import BaseModal from '../components/BaseModal.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';

const visible = defineModel<boolean>({ required: true });
const emit = defineEmits(['created']);

const formRef = ref<HTMLFormElement | null>(null);
const name = ref('');
const loading = ref(false);
const error = ref('');

const submit = async () => {
  loading.value = true;
  error.value = '';
  try {
    const res = await fetch('/api/v1/databases', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${localStorage.getItem('fluxo_jwt')}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.value })
    });
    if (!res.ok) throw new Error(await res.text());
    emit('created');
  } catch (e: any) {
    error.value = e.message || 'Failed to create database';
  } finally {
    loading.value = false;
  }
};
</script>