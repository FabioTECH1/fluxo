<template>
  <div class="fixed inset-0 bg-black/60 backdrop-blur-xs flex items-center justify-center z-50 p-4">
    <div class="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden transform transition-all">
      <div class="px-6 py-5 border-b border-gray-100 bg-gray-50 flex justify-between items-center">
        <h3 class="text-lg font-bold text-gray-900">{{ editing ? 'Edit User' : 'Add User' }}</h3>
        <button @click="$emit('close')" class="text-gray-400 hover:text-gray-600 transition-colors">
          <svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
      <form @submit.prevent="submit" class="p-6 space-y-5">
        <div v-if="error" class="text-red-700 bg-red-50 border border-red-200 p-3 rounded-lg text-sm">{{ error }}</div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Username</label>
          <input v-model="form.user" type="text" required :disabled="editing" class="w-full border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow disabled:bg-gray-100" placeholder="username">
        </div>

        <div v-if="!editing">
          <label class="block text-gray-700 text-sm font-bold mb-2">Password</label>
          <div class="relative">
            <input v-model="form.password" :type="showPassword ? 'text' : 'password'" required class="w-full border border-gray-200 rounded-lg pl-3 pr-20 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow font-mono text-sm" placeholder="Enter a password or click Generate">
            <div class="absolute inset-y-0 right-0 flex items-center gap-1 pr-2">
              <button type="button" @click="generatePassword" class="px-2 py-1 text-xs text-blue-600 hover:text-blue-800 font-semibold">Generate</button>
              <button type="button" @click="showPassword = !showPassword" class="text-gray-400 hover:text-gray-600">
                <span v-if="!showPassword" class="text-lg leading-none">&#128065;</span>
                <span v-else class="text-lg leading-none">&#128064;</span>
              </button>
            </div>
          </div>
        </div>

        <div>
          <label class="block text-gray-700 text-sm font-bold mb-2">Database Access</label>
          <p class="text-xs text-gray-500 mb-2">Select which databases this user can access.</p>
          <div class="space-y-2 max-h-48 overflow-y-auto border border-gray-200 rounded-lg p-3">
            <label v-for="db in allDatabases" :key="db" class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" :value="db" v-model="form.databases" class="w-4 h-4 text-blue-600 focus:ring-blue-500 rounded">
              <span class="text-sm text-gray-700 font-mono">{{ db }}</span>
            </label>
            <div v-if="allDatabases.length === 0" class="text-sm text-gray-400 italic text-center py-2">No databases available.</div>
          </div>
        </div>

        <div class="flex justify-end space-x-3 pt-2 border-t border-gray-100">
          <button type="button" @click="$emit('close')" class="px-4 py-2 text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 font-medium transition-colors">Cancel</button>
          <button type="submit" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors disabled:opacity-50" :disabled="loading">{{ loading ? 'Saving...' : (editing ? 'Update User' : 'Add User') }}</button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';

const props = defineProps<{ editing?: boolean; userName?: string; userDatabases?: string[] }>();
const emit = defineEmits(['close', 'created']);

const form = ref({ user: '', password: '', databases: [] as string[] });
const loading = ref(false);
const error = ref('');
const allDatabases = ref<string[]>([]);
const showPassword = ref(false);

const generatePassword = () => {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
  let pwd = '';
  for (let i = 0; i < 20; i++) {
    pwd += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  form.value.password = pwd;
  showPassword.value = true;
};

const token = () => localStorage.getItem('fluxo_jwt');

onMounted(async () => {
  if (props.editing && props.userName) {
    form.value.user = props.userName;
    form.value.databases = props.userDatabases || [];
  }
  try {
    const res = await fetch('/api/v1/databases', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (res.ok) {
      const dbs = await res.json();
      allDatabases.value = dbs.map((d: any) => d.name);
    }
  } catch (e) { console.error(e); }
});

const submit = async () => {
  loading.value = true;
  error.value = '';
  try {
    if (props.editing) {
      await fetch('/api/v1/databases/users/grants', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ user: form.value.user, databases: form.value.databases })
      });
    } else {
      await fetch('/api/v1/databases/users', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ user: form.value.user, password: form.value.password, databases: form.value.databases })
      });
    }
    emit('created');
  } catch (e: any) {
    error.value = e.message || 'Failed';
  } finally {
    loading.value = false;
  }
};
</script>