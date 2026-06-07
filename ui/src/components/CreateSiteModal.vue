<template>
  <div class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
    <div class="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden transform transition-all">
      <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
        <h3 class="text-lg font-bold text-gray-900">Create New Site</h3>
        <button @click="$emit('close')" class="text-gray-400 hover:text-gray-600 transition-colors">
          <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <form @submit.prevent="submit" class="p-6">
        <div v-if="error" class="mb-4 text-red-700 bg-red-50 border border-red-200 p-3 rounded-lg text-sm">
          {{ error }}
        </div>

        <div class="mb-5">
          <label class="block text-gray-700 text-sm font-bold mb-2">Domain Name</label>
          <input v-model="form.domain" type="text" required class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="example.com">
        </div>

        <div class="mb-5">
          <label class="block text-gray-700 text-sm font-bold mb-2">Application Type</label>
          <div class="flex gap-4">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.app_type" value="php" class="text-blue-600 focus:ring-blue-500">
              <span class="text-sm font-medium text-gray-700">PHP / Laravel</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.app_type" value="node" class="text-blue-600 focus:ring-blue-500">
              <span class="text-sm font-medium text-gray-700">Node.js</span>
            </label>
          </div>
        </div>

        <div class="mb-5" v-if="form.app_type === 'node'">
          <label class="block text-gray-700 text-sm font-bold mb-2">Application Port</label>
          <input v-model="form.app_port" type="number" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="3000">
          <p class="text-xs text-gray-500 mt-1">The internal port Nginx will proxy traffic to.</p>
        </div>

        <div class="mb-5" v-if="form.app_type === 'php'">
          <label class="block text-gray-700 text-sm font-bold mb-2">PHP Version</label>
          <select v-model="form.php_version" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option v-for="v in phpVersions" :key="v" :value="v">{{ v }}</option>
          </select>
          <p class="text-xs text-gray-500 mt-1">Need a different PHP version? Install additional runtimes via Server Settings.</p>
        </div>

        <div class="mb-5">
          <label class="block text-gray-700 text-sm font-bold mb-2">GitHub Repository</label>
          <select v-model="form.repository" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow">
            <option value="">None (Static Directory)</option>
            <option v-for="repo in repos" :key="repo.full_name" :value="repo.full_name">{{ repo.full_name }}</option>
          </select>
        </div>

        <div class="mb-5" v-if="form.repository">
          <label class="block text-gray-700 text-sm font-bold mb-2">Branch</label>
          <input v-model="form.branch" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="main">
        </div>

        <div class="mb-5" v-if="form.repository">
          <label class="block text-gray-700 text-sm font-bold mb-2">Deployment Strategy</label>
          <div class="space-y-2">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.deployment_strategy" value="standard" class="text-blue-600 focus:ring-blue-500">
              <span class="text-sm text-gray-700">Standard (Git Pull + Composer)</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.deployment_strategy" value="zero-downtime" class="text-blue-600 focus:ring-blue-500">
              <span class="text-sm text-gray-700">Zero-Downtime (Symlink Swapping)</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="form.deployment_strategy" value="octane" class="text-blue-600 focus:ring-blue-500">
              <span class="text-sm text-gray-700">Octane (Laravel Octane Reload)</span>
            </label>
          </div>
        </div>

        <div class="mb-6">
          <label class="block text-gray-700 text-sm font-bold mb-2">Web Directory</label>
          <input v-model="form.web_root" type="text" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="/public">
        </div>

        <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100 mt-6">
          <button type="button" @click="$emit('close')" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">
            Cancel
          </button>
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">
            {{ loading ? 'Creating...' : 'Create Site' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { apiClient } from '../api/client';

const emit = defineEmits(['close', 'created']);

const form = ref({
  domain: '',
  php_version: '8.4',
  web_root: '/public',
  repository: '',
  branch: 'main',
  deployment_strategy: 'standard',
  app_type: 'php',
  app_port: null
});

const error = ref('');
const loading = ref(false);
const phpVersions = ref<string[]>(['8.4']);
const repos = ref<any[]>([]);

const fetchVersionsAndRepos = async () => {
  try {
    const versions = await apiClient.getPhpVersions();
    if (versions && versions.length > 0) {
      phpVersions.value = versions;
      form.value.php_version = versions[versions.length - 1];
    }
  } catch (e) {
    console.error('Failed to load PHP versions:', e);
  }

  try {
    repos.value = await apiClient.getGithubRepos();
  } catch(e) {
    console.error('Failed to load GitHub repos:', e);
  }
};

const submit = async () => {
  error.value = '';
  loading.value = true;
  try {
    await apiClient.createSite(form.value);
    emit('created');
  } catch (e: any) {
    error.value = e.message;
  } finally {
    loading.value = false;
  }
};

onMounted(fetchVersionsAndRepos);
</script>