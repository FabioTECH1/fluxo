<template>
  <div v-if="site" class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 dark:bg-gray-900 dark:border-gray-800">
      <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Deployments</h2>
        <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage build and deployment settings.</p>
      </div>
      <div class="p-6 space-y-5">
        <div class="flex items-start justify-between">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Push to deploy</label>
            <p class="text-xs text-gray-500 dark:text-gray-400">Automatically trigger a new deployment when changes are pushed to the environment's Git branch.</p>
          </div>
          <button @click="togglePushToDeploy" type="button"
            :class="form.push_to_deploy ? 'bg-blue-600' : 'bg-gray-200'"
            class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0">
            <span :class="form.push_to_deploy ? 'translate-x-6' : 'translate-x-1'"
              class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
          </button>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Deploy script</label>
          <p class="text-xs text-gray-500 mb-1 dark:text-gray-400">The commands that will be run to deploy your application. Deployments are limited to 10 minutes. If a deployment takes longer, it will fail automatically.</p>
          <textarea v-model="form.deploy_script" class="w-full h-128 font-mono text-xs border border-gray-200 rounded-lg p-3 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600" placeholder="git pull origin main
composer install --no-dev --optimize-autoloader
php artisan migrate --force"></textarea>
        </div>

        <div class="flex items-start justify-between">
          <div>
            <label class="block text-gray-700 text-sm font-bold mb-1 dark:text-gray-300">Make .env variables available to deployment script</label>
          </div>
          <button @click="toggleExposeEnv" type="button"
            :class="form.expose_env ? 'bg-blue-600' : 'bg-gray-200'"
            class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0">
            <span :class="form.expose_env ? 'translate-x-6' : 'translate-x-1'"
              class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform" />
          </button>
        </div>

        <div class="flex justify-end pt-2 border-t border-gray-100 dark:border-gray-800">
          <button @click="saveSettings" :disabled="saving" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">
            {{ saving ? 'Saving...' : 'Save settings' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';

const route = useRoute();
const siteId = route.params.id as string;
const { addToast } = useToast();

const site = ref<any>(null);
const form = ref({ push_to_deploy: false, deploy_script: '', expose_env: false });
const saving = ref(false);

const token = () => localStorage.getItem('fluxo_jwt');

const fetchSite = async () => {
  try {
    const res = await fetch(`/api/v1/sites/${siteId}`, { headers: { 'Authorization': `Bearer ${token()}` } });
    if (res.ok) {
      site.value = await res.json();
      form.value = {
        push_to_deploy: !!site.value.push_to_deploy,
        deploy_script: site.value.deploy_script || '',
        expose_env: !!site.value.expose_env,
      };
    }
  } catch (e) {}
};

const togglePushToDeploy = () => {
  form.value.push_to_deploy = !form.value.push_to_deploy;
};

const toggleExposeEnv = () => {
  form.value.expose_env = !form.value.expose_env;
};

const saveSettings = async () => {
  saving.value = true;
  try {
    const res = await fetch(`/api/v1/sites/${siteId}`, {
      method: 'PUT',
      headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({
        push_to_deploy: form.value.push_to_deploy,
        deploy_script: form.value.deploy_script,
        expose_env: form.value.expose_env,
      })
    });
    if (!res.ok) throw new Error(await res.text());
    addToast('Settings saved', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save', 'error');
  } finally {
    saving.value = false;
  }
};

onMounted(fetchSite);
</script>
