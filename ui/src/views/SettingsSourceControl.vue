<template>
  <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-6">
    <h2 class="text-lg font-semibold mb-4 text-gray-900 dark:text-gray-100">Source Control Providers</h2>
    <p class="text-sm text-gray-600 dark:text-gray-400 mb-4">
      Connect your GitHub account to allow Fluxo to automatically list your repositories and inject deploy keys.
    </p>

    <div v-if="!editing && hasToken" class="space-y-4">
      <!-- Configured State -->
      <div class="flex items-center gap-3 p-4 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-lg">
        <svg class="w-5 h-5 text-green-600 dark:text-green-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <div class="flex-1">
          <p class="text-sm font-semibold text-green-800 dark:text-green-300">GitHub is connected</p>
          <p class="text-xs text-green-600 dark:text-green-400">A Personal Access Token has been configured.</p>
        </div>
        <span class="px-2 py-0.5 text-xs font-semibold rounded-full bg-green-100 dark:bg-green-900/40 text-green-800 dark:text-green-300">Configured</span>
      </div>

      <div class="flex justify-end gap-3">
        <AppButton variant="danger" @click="removeToken">Remove Token</AppButton>
        <AppButton variant="primary" @click="editing = true">Change Token</AppButton>
      </div>
    </div>

    <form v-else @submit.prevent="saveSettings" class="space-y-4">
      <div>
        <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">GitHub Personal Access Token</label>
        <input v-model="form.github_pat" type="password" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono" placeholder="ghp_xxxxxxxxxxxxxxx">
        
        <div class="mt-4 p-4 bg-blue-50 dark:bg-blue-950/20 border border-blue-100 dark:border-blue-900/50 rounded-lg space-y-2">
          <p class="text-xs font-semibold text-blue-800 dark:text-blue-300">Quick Setup Instructions:</p>
          <p class="text-xs text-blue-600 dark:text-blue-400 leading-relaxed">
            Fluxo requires a Classic Personal Access Token with the <code class="font-mono bg-blue-100 dark:bg-blue-900/40 px-1 py-0.5 rounded text-blue-700 dark:text-blue-300">repo</code> and <code class="font-mono bg-blue-100 dark:bg-blue-900/40 px-1 py-0.5 rounded text-blue-700 dark:text-blue-300">admin:public_key</code> scopes so it can clone private repositories and register deploy SSH keys.
          </p>
          <a 
            href="https://github.com/settings/tokens/new?scopes=repo,admin:public_key&description=Fluxo%20Server" 
            target="_blank" 
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-xs font-bold text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 transition-colors mt-1 underline"
          >
            Generate pre-configured token on GitHub ↗
          </a>
        </div>
      </div>

      <div class="flex justify-end gap-3">
        <AppButton v-if="hasToken" variant="secondary" type="button" @click="cancelEditing">Cancel</AppButton>
        <AppButton variant="primary" type="submit" :loading="loading">
          {{ loading ? 'Saving...' : 'Save Token' }}
        </AppButton>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import { useToast } from '../composables/useToast';

const { addToast } = useToast();

const form = ref({
  github_pat: '',
  admin_email: ''
});

const savedToken = ref('');
const fullSettings = ref<any>({});
const editing = ref(false);
const loading = ref(false);

const hasToken = computed(() => !!savedToken.value);

const fetchSettings = async () => {
  try {
    const data = await apiClient.getSettings();
    fullSettings.value = data;
    savedToken.value = data.github_pat || '';
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
    savedToken.value = form.value.github_pat;
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
    savedToken.value = '';
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