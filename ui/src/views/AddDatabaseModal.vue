<template>
  <div class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
    <div class="bg-white rounded-xl shadow-2xl w-full max-w-md overflow-hidden transform transition-all">
      <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
        <h3 class="text-lg font-bold text-gray-900">Add Database</h3>
        <button @click="$emit('close')" class="text-gray-400 hover:text-gray-600 transition-colors">
          <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
      <form @submit.prevent="submit" class="p-6 space-y-5">
        <div v-if="error" class="text-red-700 bg-red-50 border border-red-200 p-3 rounded-lg text-sm">{{ error }}</div>
        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Database Name</label>
          <input v-model="name" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="my_database">
        </div>
        <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100">
          <button type="button" @click="$emit('close')" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">Cancel</button>
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">{{ loading ? 'Creating...' : 'Add Database' }}</button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const emit = defineEmits(['close', 'created']);

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