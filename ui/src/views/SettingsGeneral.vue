<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" class="mb-6" />
    <template v-else>
      <!-- Admin Email / Global Config -->
      <Card>
      <h2 class="text-lg font-semibold mb-4 text-gray-900 dark:text-gray-100">Global Configuration</h2>
      <p class="text-sm text-gray-600 dark:text-gray-400 mb-4">
        Required for Let's Encrypt SSL certificate expiration warnings.
      </p>

      <form @submit.prevent="saveSettings">
        <div class="mb-6">
          <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Admin Email</label>
          <input v-model="form.admin_email" type="email" class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="admin@example.com">
        </div>

        <div class="flex justify-end">
          <AppButton variant="primary" type="submit" :loading="saving">
            {{ saving ? 'Saving...' : 'Save' }}
          </AppButton>
        </div>
      </form>
    </Card>



    <!-- Change Password -->
    <Card>
      <h2 class="text-lg font-semibold mb-4 text-gray-900 dark:text-gray-100">Change Admin Password</h2>
      <p class="text-sm text-gray-600 dark:text-gray-400 mb-4">
        Update the admin password / Day Zero token used to access this dashboard.
      </p>

      <form @submit.prevent="changePassword">
        <div v-if="pwError" class="mb-4 text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-900/30 border border-red-200 p-3 rounded-lg text-sm">
          {{ pwError }}
        </div>
        <div v-if="pwSuccess" class="mb-4 text-green-700 dark:text-green-400 bg-green-50 dark:bg-green-900/30 border border-green-200 p-3 rounded-lg text-sm">
          {{ pwSuccess }}
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Current Password / Day Zero Token</label>
            <input v-model="pwdForm.current" type="password" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="••••••••••••••••">
          </div>
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">New Password (min 8 chars)</label>
            <div class="relative">
              <input v-model="pwdForm.new" :type="showNewPassword ? 'text' : 'password'" required class="w-full border border-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:border-gray-600 rounded-lg pl-3 pr-10 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow" placeholder="At least 8 characters">
              <button type="button" @click="showNewPassword = !showNewPassword" class="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 hover:text-gray-600 focus:outline-none">
                <span v-if="!showNewPassword" class="text-lg leading-none">&#128065;</span>
                <span v-else class="text-lg leading-none">&#128064;</span>
              </button>
            </div>

            <div v-if="pwdForm.new" class="mt-2 space-y-1.5">
              <div class="flex justify-between items-center text-xs">
                <span class="text-gray-500 dark:text-gray-400">Password strength:</span>
                <span :class="passwordStrength.textClass">{{ passwordStrength.label }}</span>
              </div>
              <div class="w-full bg-gray-200 dark:bg-gray-700 h-1.5 rounded-full overflow-hidden">
                <div class="h-full rounded-full transition-all duration-300" :class="passwordStrength.colorClass"></div>
              </div>
              <p class="text-[10px] text-gray-400 dark:text-gray-500 leading-normal">
                For a strong password, use 8+ characters combining uppercase, lowercase, numbers, and symbols.
              </p>
            </div>
          </div>
        </div>

        <div class="flex justify-end">
          <AppButton variant="primary" type="submit" :loading="pwLoading">
            {{ pwLoading ? 'Updating...' : 'Change Password' }}
          </AppButton>
        </div>
      </form>
    </Card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onActivated } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import Card from '../components/Card.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import { useToast } from '../composables/useToast';

const { addToast } = useToast();

const form = ref({
  admin_email: ''
});

const saving = ref(false);
const loading = ref(true);

const pwdForm = ref({
  current: '',
  new: ''
});

const showNewPassword = ref(false);
const pwLoading = ref(false);
const pwError = ref('');
const pwSuccess = ref('');

const passwordStrength = computed(() => {
  const password = pwdForm.value.new;
  if (!password) {
    return { score: 0, label: '', colorClass: 'bg-gray-200 w-0', textClass: 'text-gray-400' };
  }

  let score = 0;
  if (password.length >= 8) score++;
  if (/[A-Z]/.test(password)) score++;
  if (/[a-z]/.test(password)) score++;
  if (/[0-9]/.test(password)) score++;
  if (/[^A-Za-z0-9]/.test(password)) score++;

  if (score <= 2) {
    return { score, label: 'Weak', colorClass: 'bg-red-500 w-1/3', textClass: 'text-red-600 font-bold' };
  } else if (score <= 4) {
    return { score, label: 'Okay', colorClass: 'bg-yellow-500 w-2/3', textClass: 'text-yellow-600 font-bold' };
  } else {
    return { score, label: 'Strong', colorClass: 'bg-green-500 w-full', textClass: 'text-green-600 font-bold' };
  }
});

const current = ref<any>({});

const fetchSettings = async () => {
  try {
    loading.value = true;
    const data = await apiClient.getSettings();
    current.value = data;
    form.value.admin_email = data.admin_email || '';
  } catch (e) {
    console.error('Failed to load settings:', e);
  } finally {
    loading.value = false;
  }
};

const saveSettings = async () => {
  saving.value = true;
  try {
    await apiClient.updateSettings({ github_pat: current.value.github_pat, admin_email: form.value.admin_email, default_php: current.value.default_php });
    addToast('Settings saved successfully', 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed to save settings', 'error');
  } finally {
    saving.value = false;
  }
};

const changePassword = async () => {
  pwError.value = '';
  pwSuccess.value = '';

  if (passwordStrength.value.score < 3) {
    pwError.value = 'Password is too weak. Please use at least 8 characters with a mix of letters and numbers/symbols.';
    return;
  }

  pwLoading.value = true;
  try {
    await apiClient.updatePassword(pwdForm.value.current, pwdForm.value.new);
    pwSuccess.value = 'Password updated successfully!';
    pwdForm.value.current = '';
    pwdForm.value.new = '';
  } catch (e: any) {
    pwError.value = e.message || 'Failed to change password. Double check your current password.';
  } finally {
    pwLoading.value = false;
  }
};

onMounted(() => {
  fetchSettings();
});

onActivated(fetchSettings);
</script>