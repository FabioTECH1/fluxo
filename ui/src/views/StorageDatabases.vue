<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" class="mb-6" />
    <template v-else>
      <Card>
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Databases</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage your server's databases.</p>
        </div>
        <button @click="showDbModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add Database</button>
      </div>

      <DataTable :columns="dbColumns" :items="databases" empty-text="No databases found." overflow-visible>
        <template #name="{ item }">
          <span class="font-medium text-gray-900 font-mono dark:text-gray-100">{{ item.name }}</span>
        </template>
        <template #engine="{ item }">
          <span class="text-gray-500 uppercase text-xs font-semibold dark:text-gray-400">{{ item.engine }}</span>
        </template>
        <template #size="{ item }">
          <span class="text-gray-500 dark:text-gray-400">{{ dbSize(item.name) }}</span>
        </template>
        <template #actions="{ item }">
          <div class="relative inline-block">
            <button @click="toggleDbMenu(item.id)" class="px-2.5 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 font-medium transition-colors">···</button>
            <div v-if="openDbMenu === item.id" class="absolute right-0 top-full mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-50 dark:bg-gray-900 dark:border-gray-700">
              <button @click="deleteDatabase(item.id); openDbMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left dark:text-red-400 dark:hover:bg-red-900/30">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                Delete
              </button>
            </div>
          </div>
        </template>
      </DataTable>
    </Card>

    <Card>
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Database Users</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage the database users that may access your server's databases.</p>
        </div>
        <button @click="showUserModal = true; editingUser = null" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add User</button>
      </div>

      <DataTable :columns="userColumns" :items="users" empty-text="No database users found." overflow-visible>
        <template #user="{ item }">
          <span class="font-medium text-gray-900 font-mono dark:text-gray-100">{{ item.user }}</span>
        </template>
        <template #engine="{ item }">
          <span class="text-gray-500 uppercase text-xs font-semibold dark:text-gray-400">{{ item.engine }}</span>
        </template>
        <template #databases="{ item }">
          <span class="text-gray-500 dark:text-gray-400">{{ userDbLabel(item.user, item.engine) }}</span>
        </template>
        <template #actions="{ item }">
          <div class="relative inline-block">
            <button @click="toggleUserMenu(item.engine + '_' + item.user)" class="px-2.5 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 font-medium transition-colors">···</button>
            <div v-if="openUserMenu === (item.engine + '_' + item.user)" class="absolute right-0 top-full mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-50 dark:bg-gray-900 dark:border-gray-700">
              <button @click="editUser(item); openUserMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 text-left dark:text-gray-300 dark:hover:bg-gray-800">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                Edit
              </button>
              <button 
                :disabled="item.user === 'fluxo'"
                @click="item.user === 'fluxo' ? null : (deleteUser(item.user, item.engine), openUserMenu = null)"
                class="flex items-center gap-2 w-full px-4 py-2 text-sm text-left transition-colors"
                :class="item.user === 'fluxo' 
                  ? 'text-gray-400 dark:text-gray-600 cursor-not-allowed opacity-50' 
                  : 'text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/30'"
              >
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                Delete
              </button>
            </div>
          </div>
        </template>
      </DataTable>
    </Card>
    </template>

    <AddDatabaseModal v-model="showDbModal" @created="onDbCreated" />
    <AddUserModal v-model="showUserModal" :editing="!!editingUser" :user-name="editingUser?.name || ''" :user-databases="editingUser?.databases || []" :user-engine="editingUser?.engine || ''" @created="onUserCreated" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import { apiClient } from '../api/client';
import AddDatabaseModal from './AddDatabaseModal.vue';
import AddUserModal from './AddUserModal.vue';
import DataTable from '../components/DataTable.vue';
import Card from '../components/Card.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';

const dbColumns = [
  { key: 'name', label: 'Name' },
  { key: 'engine', label: 'Engine' },
  { key: 'size', label: 'Size' },
];

const userColumns = [
  { key: 'user', label: 'User' },
  { key: 'engine', label: 'Engine' },
  { key: 'databases', label: 'Databases' },
];

const { confirm } = useConfirm();
const { addToast } = useToast();

const databases = ref<any[]>([]);
const users = ref<any[]>([]);
const userGrants = ref<Record<string, string[]>>({});
const sizes = ref<Record<string, string>>({});
const openDbMenu = ref<number | null>(null);
const openUserMenu = ref<string | null>(null);
const showDbModal = ref(false);
const showUserModal = ref(false);
const editingUser = ref<{ name: string; databases: string[]; engine: string } | null>(null);
const loading = ref(true);

const dbSize = (name: string) => sizes.value[name] ? sizes.value[name] + ' MB' : '-';

const userDbLabel = (user: string, engine: string) => {
  const dbs = userGrants.value[user];
  if (!dbs || dbs.length === 0) return 'None';
  if (dbs.includes('*')) return 'All databases';
  const sameEngine = databases.value.filter((d: any) => d.engine === engine).map((d: any) => d.name);
  const hasAll = sameEngine.every((n: string) => dbs.includes(n));
  if (hasAll && sameEngine.length > 0) return 'All databases';
  return `${dbs.length} database${dbs.length !== 1 ? 's' : ''}`;
};

const fetchData = async () => {
  try {
    const [dbRes, sizeRes, userRes] = await Promise.allSettled([
      apiClient.get('/api/v1/databases'),
      apiClient.get('/api/v1/databases/sizes'),
      apiClient.get('/api/v1/databases/users')
    ]);

    if (dbRes.status === 'fulfilled') databases.value = dbRes.value;
    
    if (sizeRes.status === 'fulfilled') {
      const list = sizeRes.value;
      const map: Record<string, string> = {};
      for (const item of list) map[item.name] = item.size_mb;
      sizes.value = map;
    }
    
    if (userRes.status === 'fulfilled') {
      users.value = userRes.value;
      fetchAllGrants(users.value);
    }
  } catch (e) { console.error(e); }
};

const fetchAllGrants = async (userList: any[]) => {
  const map: Record<string, string[]> = {};
  for (const u of userList) {
    try {
      const engine = u.engine || 'mysql';
      map[u.user] = await apiClient.get(`/api/v1/databases/users/grants?user=${encodeURIComponent(u.user)}&engine=${engine}`);
    } catch (e) { /* skip */ }
  }
  userGrants.value = map;
};

const toggleDbMenu = (id: number) => {
  openDbMenu.value = openDbMenu.value === id ? null : id;
};

const toggleUserMenu = (user: string) => {
  openUserMenu.value = openUserMenu.value === user ? null : user;
};

const onDbCreated = () => {
  showDbModal.value = false;
  fetchData();
};

const onUserCreated = () => {
  showUserModal.value = false;
  editingUser.value = null;
  fetchData();
};

const editUser = async (item: any) => {
  const dbs = userGrants.value[item.user] || [];
  editingUser.value = { name: item.user, databases: dbs, engine: item.engine || 'mysql' };
  showUserModal.value = true;
};

const deleteDatabase = async (id: number) => {
  const db = databases.value.find(d => d.id === id);
  if (!db) return;
  const ok = await confirm({ title: 'Delete Database', message: `Delete database "${db.name}"? This cannot be undone.`, confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    await apiClient.delete(`/api/v1/databases/${id}`);
    apiClient.invalidate('/api/v1/databases');
    addToast('Database deleted', 'success');
    fetchData();
  } catch (e: any) { addToast(e.message || 'Failed to delete', 'error'); }
};

const deleteUser = async (user: string, engine: string) => {
  const ok = await confirm({ title: 'Delete User', message: `Delete database user "${user}" from ${engine}?`, confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    await apiClient.delete(`/api/v1/databases/users?user=${encodeURIComponent(user)}&engine=${engine}`);
    apiClient.invalidate('/api/v1/databases/users');
    addToast('User deleted', 'success');
    fetchData();
  } catch (e: any) { addToast(e.message || 'Failed to delete', 'error'); }
};

const handleClickOutside = (e: MouseEvent) => {
  const t = e.target as HTMLElement;
  if (!t.closest('.relative')) { openDbMenu.value = null; openUserMenu.value = null; }
};

onMounted(async () => {
  loading.value = true;
  await fetchData();
  loading.value = false;
  window.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  window.removeEventListener('click', handleClickOutside);
});
</script>
