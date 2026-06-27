<template>
  <BaseModal v-model="visible" :title="editing ? 'Edit User' : 'Add User'" :loading="loading" @submit="formRef?.requestSubmit()">
    <template #footer>
      <AppButton variant="secondary" @click="visible = false">Cancel</AppButton>
      <AppButton variant="primary" type="submit" :loading="loading" :disabled="loading" @click="formRef?.requestSubmit()">
        {{ loading ? 'Saving...' : (editing ? 'Update User' : 'Add User') }}
      </AppButton>
    </template>

    <form ref="formRef" @submit.prevent="submit" class="space-y-5">
      <ErrorAlert :message="error" />

      <FormGroup label="Engine">
        <select v-model="form.engine" required :disabled="editing" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm disabled:bg-gray-100 dark:disabled:bg-gray-700">
          <option v-for="eng in installedDbEngines" :key="eng" :value="eng">{{ eng === 'mysql' ? 'MySQL' : 'PostgreSQL' }}</option>
        </select>
      </FormGroup>

      <FormGroup label="Username">
        <input v-model="form.user" type="text" required :disabled="editing" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow disabled:bg-gray-100 dark:disabled:bg-gray-700" placeholder="username">
      </FormGroup>

      <div>
        <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Password</label>
        <div class="relative">
          <input v-model="form.password" :type="showPassword ? 'text' : 'password'" :required="!editing" class="w-full border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg pl-3 pr-20 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" :placeholder="editing ? 'Leave blank to keep your current password' : 'Enter a password or click Generate'">
          <div class="absolute inset-y-0 right-0 flex items-center gap-1 pr-2">
            <button type="button" @click="generatePassword" class="px-2 py-1 text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 font-semibold">Generate</button>
            <button type="button" @click="showPassword = !showPassword" class="text-gray-400 dark:text-gray-500 dark:text-gray-400 hover:text-gray-600 dark:hover:text-gray-400">
              <span v-if="!showPassword" class="text-lg leading-none">&#128065;</span>
              <span v-else class="text-lg leading-none">&#128064;</span>
            </button>
          </div>
        </div>
      </div>

      <div>
        <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-2">Database Access</label>
        <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">Select which databases this user can access.</p>
        <div class="space-y-2 max-h-48 overflow-y-auto border border-gray-200 dark:border-gray-600 dark:bg-gray-800 rounded-lg p-3">
          <label v-for="db in filteredDatabases" :key="db" class="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" :value="db" v-model="form.databases" class="w-4 h-4 text-blue-600 dark:text-blue-400 focus:ring-blue-500 rounded">
            <span class="text-sm text-gray-700 dark:text-gray-300 font-mono">{{ db }}</span>
          </label>
          <div v-if="filteredDatabases.length === 0" class="text-sm text-gray-400 dark:text-gray-500 italic text-center py-2">No databases available.</div>
        </div>
      </div>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import { apiClient } from '../api/client';
import BaseModal from '../components/BaseModal.vue';
import AppButton from '../components/AppButton.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';

const props = defineProps<{ editing?: boolean; userName?: string; userDatabases?: string[]; userEngine?: string }>();
const visible = defineModel<boolean>({ required: true });
const emit = defineEmits(['created']);

const formRef = ref<HTMLFormElement | null>(null);

const form = ref({ user: '', password: '', databases: [] as string[], engine: 'mysql' });
const loading = ref(false);
const error = ref('');
const allDatabases = ref<{ name: string; engine: string }[]>([]);
const installedDbEngines = ref<string[]>(['mysql']);
const showPassword = ref(false);

const filteredDatabases = computed(() => {
  return allDatabases.value
    .filter(d => d.engine === form.value.engine)
    .map(d => d.name);
});

const generatePassword = () => {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
  let pwd = '';
  for (let i = 0; i < 20; i++) {
    pwd += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  form.value.password = pwd;
  showPassword.value = true;
};

watch(visible, (newVal) => {
  if (newVal) {
    if (props.editing) {
      form.value.user = props.userName || '';
      form.value.password = '';
      form.value.engine = props.userEngine || 'mysql';
      showPassword.value = false;
      if (props.userDatabases && props.userDatabases.includes('*')) {
        form.value.databases = allDatabases.value
          .filter(d => d.engine === form.value.engine)
          .map(d => d.name);
      } else {
        form.value.databases = [...(props.userDatabases || [])];
      }
    } else {
      form.value.user = '';
      form.value.password = '';
      form.value.databases = [];
      form.value.engine = installedDbEngines.value[0] || 'mysql';
      showPassword.value = false;
    }
    error.value = '';
  }
});

onMounted(async () => {
  if (props.editing && props.userName) {
    form.value.user = props.userName;
    form.value.databases = props.userDatabases || [];
    form.value.engine = props.userEngine || 'mysql';
  }
  try {
    const [engines, databases] = await Promise.all([
      apiClient.getDatabaseEngines(),
      apiClient.getDatabases(true)
    ]);
    const dbs = (engines || []).filter((e: string) => e === 'mysql' || e === 'postgres');
    if (dbs.length > 0) {
      installedDbEngines.value = dbs;
      if (!props.editing) form.value.engine = dbs[0];
    }
    allDatabases.value = databases || [];
  } catch (e) { console.error(e); }
});

const submit = async () => {
  loading.value = true;
  error.value = '';
  try {
    if (props.editing) {
      const payload: any = { user: form.value.user, databases: form.value.databases, engine: form.value.engine };
      if (form.value.password) payload.password = form.value.password;
      await apiClient.createDatabaseUserGrant(payload);
    } else {
      await apiClient.createDatabaseUser({ user: form.value.user, password: form.value.password, databases: form.value.databases, engine: form.value.engine });
    }
    emit('created');
  } catch (e: any) {
    error.value = e.message || 'Failed';
  } finally {
    loading.value = false;
  }
};
</script>
