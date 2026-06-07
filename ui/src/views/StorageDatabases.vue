<template>
  <div class="space-y-6">
    <!-- Databases -->
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Databases</h2>
          <p class="text-sm text-gray-600 mt-1">Manage your server's databases.</p>
        </div>
        <button @click="showDbModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add Database</button>
      </div>

      <div class="overflow-x-auto border rounded-lg">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Engine</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Size</th>
              <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-200">
            <tr v-for="db in databases" :key="db.id" class="hover:bg-gray-50 transition-colors">
              <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 font-mono">{{ db.name }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 uppercase">{{ db.engine }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ dbSize(db.name) }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-right text-sm">
                <div class="relative inline-block">
                  <button @click="toggleDbMenu(db.id)" class="px-3 py-1.5 text-xs text-gray-600 bg-gray-50 border border-gray-200 rounded-lg hover:bg-gray-100 font-medium transition-colors">Open menu</button>
                  <div v-if="openDbMenu === db.id" class="absolute right-0 mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10">
                    <button @click="deleteDatabase(db.id); openDbMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left">
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                      Delete
                    </button>
                  </div>
                </div>
              </td>
            </tr>
            <tr v-if="databases.length === 0">
              <td colspan="4" class="px-6 py-8 text-center text-gray-500 text-sm">No databases found.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Database Users -->
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900">Database Users</h2>
          <p class="text-sm text-gray-600 mt-1">Manage the database users that may access your server's databases.</p>
        </div>
        <button @click="showUserModal = true; editingUser = null" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add User</button>
      </div>

      <div class="overflow-x-auto border rounded-lg">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">User</th>
              <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Databases</th>
              <th scope="col" class="relative px-6 py-3"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-200">
            <tr v-for="u in users" :key="u.user + u.host" class="hover:bg-gray-50 transition-colors">
              <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 font-mono">{{ u.user }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ userDbLabel(u.user) }}</td>
              <td class="px-6 py-4 whitespace-nowrap text-right text-sm">
                <div class="relative inline-block">
                  <button @click="toggleUserMenu(u.user)" class="px-3 py-1.5 text-xs text-gray-600 bg-gray-50 border border-gray-200 rounded-lg hover:bg-gray-100 font-medium transition-colors">Open menu</button>
                  <div v-if="openUserMenu === u.user" class="absolute right-0 mt-1 w-44 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10">
                    <button @click="editUser(u.user); openUserMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 text-left">
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                      Edit
                    </button>
                    <button @click="deleteUser(u.user); openUserMenu = null" class="flex items-center gap-2 w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50 text-left">
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                      Delete
                    </button>
                  </div>
                </div>
              </td>
            </tr>
            <tr v-if="users.length === 0">
              <td colspan="3" class="px-6 py-8 text-center text-gray-500 text-sm">No database users found.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <AddDatabaseModal v-if="showDbModal" @close="showDbModal = false" @created="onDbCreated" />
    <AddUserModal v-if="showUserModal" :editing="!!editingUser" :user-name="editingUser?.name || ''" :user-databases="editingUser?.databases || []" @close="showUserModal = false" @created="onUserCreated" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import AddDatabaseModal from './AddDatabaseModal.vue';
import AddUserModal from './AddUserModal.vue';

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
const editingUser = ref<{ name: string; databases: string[] } | null>(null);

const token = () => localStorage.getItem('fluxo_jwt');

const dbSize = (name: string) => sizes.value[name] ? sizes.value[name] + ' MB' : '-';

const userDbLabel = (user: string) => {
  const dbs = userGrants.value[user];
  if (!dbs || dbs.length === 0) return 'None';
  const allDbNames = databases.value.map((d: any) => d.name);
  const hasAll = allDbNames.every((n: string) => dbs.includes(n));
  if (hasAll && allDbNames.length > 0) return 'All databases';
  return `${dbs.length} database${dbs.length !== 1 ? 's' : ''}`;
};

const fetchData = async () => {
  const t = token();
  try {
    const [dbRes, sizeRes, userRes] = await Promise.all([
      fetch('/api/v1/databases', { headers: { 'Authorization': `Bearer ${t}` } }),
      fetch('/api/v1/databases/sizes', { headers: { 'Authorization': `Bearer ${t}` } }),
      fetch('/api/v1/databases/users', { headers: { 'Authorization': `Bearer ${t}` } })
    ]);
    if (dbRes.ok) databases.value = await dbRes.json();
    if (sizeRes.ok) {
      const list = await sizeRes.json();
      const map: Record<string, string> = {};
      for (const item of list) map[item.name] = item.size_mb;
      sizes.value = map;
    }
    if (userRes.ok) {
      users.value = await userRes.json();
      fetchAllGrants(users.value);
    }
  } catch (e) { console.error(e); }
};

const fetchAllGrants = async (userList: any[]) => {
  const t = token();
  const map: Record<string, string[]> = {};
  for (const u of userList) {
    try {
      const res = await fetch(`/api/v1/databases/users/grants?user=${encodeURIComponent(u.user)}`, {
        headers: { 'Authorization': `Bearer ${t}` }
      });
      if (res.ok) map[u.user] = await res.json();
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

const editUser = async (user: string) => {
  const dbs = userGrants.value[user] || [];
  editingUser.value = { name: user, databases: dbs };
  showUserModal.value = true;
};

const deleteDatabase = async (id: number) => {
  const db = databases.value.find(d => d.id === id);
  if (!db) return;
  const ok = await confirm({ title: 'Delete Database', message: `Delete database "${db.name}"? This cannot be undone.`, confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    const res = await fetch(`/api/v1/databases/${id}`, { method: 'DELETE', headers: { 'Authorization': `Bearer ${token()}` } });
    if (!res.ok) throw new Error(await res.text());
    addToast('Database deleted', 'success');
    fetchData();
  } catch (e: any) { addToast(e.message || 'Failed to delete', 'error'); }
};

const deleteUser = async (user: string) => {
  const ok = await confirm({ title: 'Delete User', message: `Delete database user "${user}"?`, confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    const res = await fetch(`/api/v1/databases/users?user=${encodeURIComponent(user)}`, { method: 'DELETE', headers: { 'Authorization': `Bearer ${token()}` } });
    if (!res.ok) throw new Error(await res.text());
    addToast('User deleted', 'success');
    fetchData();
  } catch (e: any) { addToast(e.message || 'Failed to delete', 'error'); }
};

const handleClickOutside = (e: MouseEvent) => {
  const t = e.target as HTMLElement;
  if (!t.closest('.relative')) { openDbMenu.value = null; openUserMenu.value = null; }
};

onMounted(() => {
  fetchData();
  window.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  window.removeEventListener('click', handleClickOutside);
});
</script>