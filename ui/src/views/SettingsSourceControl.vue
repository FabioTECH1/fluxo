<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
    <h2 class="text-lg font-semibold mb-4 text-gray-900">Source Control Providers</h2>
    <p class="text-sm text-gray-600 mb-4">
      Connect your GitHub account to allow Fluxo to automatically list your repositories and inject deploy keys.
    </p>

    <div v-if="!editing && hasToken" class="space-y-4">
      <!-- Configured State -->
      <div class="flex items-center gap-3 p-4 bg-green-50 border border-green-200 rounded-lg">
        <svg class="w-5 h-5 text-green-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <div class="flex-1">
          <p class="text-sm font-semibold text-green-800">GitHub is connected</p>
          <p class="text-xs text-green-600">A Personal Access Token has been configured.</p>
        </div>
        <span class="px-2 py-0.5 text-xs font-semibold rounded-full bg-green-100 text-green-800">Configured</span>
      </div>

      <div class="flex justify-end gap-3">
        <button @click="removeToken" class="px-4 py-2 text-red-600 bg-white border border-red-300 rounded-lg hover:bg-red-50 font-medium transition-colors">
          Remove Token
        </button>
        <button @click="editing = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors">
          Change Token
        </button>
      </div>
    </div>

    <form v-else @submit.prevent="saveSettings" class="space-y-4">
      <div>
        <label class="block text-gray-700 text-sm font-bold mb-2">GitHub Personal Access Token</label>
        <input v-model="form.github_pat" type="password" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="ghp_xxxxxxxxxxxxxxx">
        <p class="text-xs text-gray-500 mt-1">Requires 'repo' scope to list private repositories and add deploy keys.</p>
      </div>

      <div class="flex justify-end gap-3">
        <button v-if="hasToken" type="button" @click="cancelEditing" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">
          Cancel
        </button>
        <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">
          {{ loading ? 'Saving...' : 'Save Token' }}
        </button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { apiClient } from '../api/client';
import { useToast } from '../composables/useToast';

const { addToast } = useToast();

const form = ref({
  github_pat: '',
  admin_email: ''
});

const fullSettings = ref<any>({});
const editing = ref(false);
const loading = ref(false);

const hasToken = computed(() => !!form.value.github_pat);

const fetchSettings = async () => {
  try {
    const data = await apiClient.getSettings();
    fullSettings.value = data;
    form.value.github_pat = data.github_pat || '';
    form.value.admin_email = data.admin_email || '';
  } catch (e) {
    console.error('Failed to load settings:', e);
  }
};

const saveSettings = async () => {
  loading.value = true;
  try {
    await apiClient.updateSettings({ ...fullSettings.value, ...form.value });
    addToast('GitHub token saved successfully', 'success');
    editing.value = false;
  } catch (e: any) {
    addToast(e.message || 'Failed to save token', 'error');
  } finally {
    loading.value = false;
  }
};

const removeToken = async () => {
  loading.value = true;
  try {
    await apiClient.updateSettings({ ...fullSettings.value, github_pat: '' });
    form.value.github_pat = '';
    addToast('GitHub token removed', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to remove token', 'error');
  } finally {
    loading.value = false;
  }
};

const cancelEditing = () => {
  editing.value = false;
  fetchSettings();
};

onMounted(() => {
  fetchSettings();
});
</script>