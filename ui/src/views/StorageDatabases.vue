<template>
  <div class="space-y-6">
    <SkeletonLoader v-if="loading" type="card" class="mb-6" />
    <template v-else>
      <Card>
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div class="flex items-start gap-3 min-w-0">
            <div class="w-10 h-10 rounded-lg bg-orange-50 text-orange-600 dark:bg-orange-900/30 dark:text-orange-300 flex items-center justify-center shrink-0">
              <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4" /></svg>
            </div>
            <div class="min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">phpMyAdmin</h2>
                <span v-if="phpMyAdmin.installed" class="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-semibold"
                  :class="phpMyAdmin.enabled ? 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'">
                  {{ phpMyAdmin.enabled ? 'Enabled' : 'Disabled' }}
                </span>
              </div>
              <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage MySQL and MariaDB databases through a protected web interface.</p>
              <p v-if="phpMyAdmin.installed" class="text-xs text-gray-500 mt-1 dark:text-gray-500">
                Version {{ phpMyAdmin.version || 'unknown' }}<span v-if="phpMyAdmin.php_version"> · PHP {{ phpMyAdmin.php_version }}</span>
              </p>
              <p v-else-if="!phpMyAdmin.mysql_available" class="text-xs text-amber-600 mt-1 dark:text-amber-400">Install MySQL or MariaDB before installing phpMyAdmin.</p>
            </div>
          </div>

          <div class="flex items-center gap-2 shrink-0 flex-wrap sm:justify-end">
            <AppButton v-if="!phpMyAdmin.installed" size="sm" :loading="phpMyAdminAction === 'install'" :disabled="!phpMyAdmin.mysql_available" @click="installPhpMyAdmin">Install</AppButton>
            <template v-else>
              <AppButton v-if="phpMyAdmin.enabled" size="sm" :loading="phpMyAdminAction === 'open'" @click="openPhpMyAdmin">Open phpMyAdmin</AppButton>
              <AppButton v-else size="sm" :loading="phpMyAdminAction === 'enable'" @click="enablePhpMyAdmin">Enable</AppButton>
              <AppButton v-if="phpMyAdmin.enabled" variant="secondary" size="sm" :loading="phpMyAdminAction === 'disable'" @click="disablePhpMyAdmin">Disable</AppButton>
              <AppButton variant="danger" size="sm" :loading="phpMyAdminAction === 'remove'" @click="removePhpMyAdmin">Uninstall</AppButton>
            </template>
          </div>
        </div>
      </Card>

      <Card>
      <div class="flex justify-between items-center mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Databases</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Manage your server's databases.</p>
        </div>
        <button @click="showDbModal = true" class="px-4 py-2 text-white bg-blue-600 rounded-lg hover:bg-blue-700 font-medium shadow-sm transition-colors text-sm whitespace-nowrap">Add Database</button>
      </div>

      <DataTable :columns="dbColumns" :items="databases" empty-text="No databases found." aria-label="Databases">
        <template #name="{ item }">
          <span class="font-medium text-gray-900 font-mono dark:text-gray-100">{{ item.name }}</span>
        </template>
        <template #engine="{ item }">
          <span class="text-gray-500 uppercase text-xs font-semibold dark:text-gray-400">{{ item.engine }}</span>
        </template>
        <template #size="{ item }">
          <span class="text-gray-500 dark:text-gray-400">{{ dbSize(item.name, item.engine) }}</span>
        </template>
        <template #actions="{ item }">
          <TableActionMenu :items="databaseMenuItems(item)" aria-label="Database actions"
            @select="handleDatabaseAction($event, item)" />
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

      <DataTable :columns="userColumns" :items="users" empty-text="No database users found." aria-label="Database users">
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
          <TableActionMenu :items="userMenuItems(item)" aria-label="Database user actions"
            @select="handleUserAction($event, item)" />
        </template>
      </DataTable>
    </Card>
    </template>

    <AddDatabaseModal v-model="showDbModal" @created="onDbCreated" />
    <AddUserModal v-model="showUserModal" :editing="!!editingUser" :user-name="editingUser?.name || ''" :user-databases="editingUser?.databases || []" :user-engine="editingUser?.engine || ''" @created="onUserCreated" />
    <RotateDatabaseUserPasswordModal
      v-model="showRotatePasswordModal"
      :user-name="rotatingUser?.name || ''"
      :user-engine="rotatingUser?.engine || 'mysql'"
      :affected-databases="rotatingUserDatabases"
      @rotated="onUserPasswordRotated"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onActivated } from 'vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';
import { apiClient } from '../api/client';
import AddDatabaseModal from './AddDatabaseModal.vue';
import AddUserModal from './AddUserModal.vue';
import RotateDatabaseUserPasswordModal from './RotateDatabaseUserPasswordModal.vue';
import DataTable from '../components/DataTable.vue';
import Card from '../components/Card.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import AppButton from '../components/AppButton.vue';
import TableActionMenu from '../components/TableActionMenu.vue';

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
const { addToast, showToast, updateToast } = useToast();

const databases = ref<any[]>([]);
const users = ref<any[]>([]);
const userGrants = ref<Record<string, string[]>>({});
const sizes = ref<Record<string, string>>({});
const showDbModal = ref(false);
const showUserModal = ref(false);
const showRotatePasswordModal = ref(false);
const editingUser = ref<{ name: string; databases: string[]; engine: string } | null>(null);
const rotatingUser = ref<{ name: string; engine: string } | null>(null);
const loading = ref(true);
const phpMyAdminAction = ref('');
const phpMyAdmin = ref({
  installed: false,
  enabled: false,
  version: '',
  php_version: '',
  mysql_available: false,
});

const dbSize = (name: string, engine: string) => sizes.value[engine + ':' + name] ? sizes.value[engine + ':' + name] + ' MB' : '-';
const userGrantKey = (user: string, engine: string) => `${engine}:${user}`;

const userDbLabel = (user: string, engine: string) => {
  const dbs = userGrants.value[userGrantKey(user, engine)];
  if (!dbs || dbs.length === 0) return 'None';
  if (dbs.includes('*')) return 'All databases';
  const sameEngine = databases.value.filter((d: any) => d.engine === engine).map((d: any) => d.name);
  const hasAll = sameEngine.every((n: string) => dbs.includes(n));
  if (hasAll && sameEngine.length > 0) return 'All databases';
  return `${dbs.length} database${dbs.length !== 1 ? 's' : ''}`;
};

const databasesForUser = (user: string, engine: string) => {
  const grants = userGrants.value[userGrantKey(user, engine)] || [];
  if (grants.includes('*')) {
    return databases.value.filter((db: any) => db.engine === engine).map((db: any) => db.name);
  }
  return grants;
};

const rotatingUserDatabases = computed(() => {
  if (!rotatingUser.value) return [];
  return databasesForUser(rotatingUser.value.name, rotatingUser.value.engine);
});

const fetchData = async () => {
  try {
    const [dbRes, sizeRes, userRes, phpMyAdminRes] = await Promise.allSettled([
      apiClient.get('/api/v1/databases'),
      apiClient.get('/api/v1/databases/sizes'),
      apiClient.get('/api/v1/databases/users'),
      apiClient.get('/api/v1/tools/phpmyadmin', { bypassCache: true, useCache: false })
    ]);

    if (dbRes.status === 'fulfilled') databases.value = dbRes.value;
    
    if (sizeRes.status === 'fulfilled') {
      const list = sizeRes.value;
      const map: Record<string, string> = {};
      for (const item of list) map[item.engine + ':' + item.name] = item.size_mb;
      sizes.value = map;
    }
    
    if (userRes.status === 'fulfilled') {
      users.value = userRes.value;
      await fetchAllGrants(users.value);
    }

    if (phpMyAdminRes.status === 'fulfilled') phpMyAdmin.value = phpMyAdminRes.value;
  } catch (e) { console.error(e); }
};

const fetchAllGrants = async (userList: any[]) => {
  const map: Record<string, string[]> = {};
  for (const u of userList) {
    try {
      const engine = u.engine || 'mysql';
      map[userGrantKey(u.user, engine)] = await apiClient.get(`/api/v1/databases/users/grants?user=${encodeURIComponent(u.user)}&engine=${engine}`);
    } catch (e) { /* skip */ }
  }
  userGrants.value = map;
};

const databaseMenuItems = (item: any) => [
  ...(item.engine === 'mysql' ? [{ id: 'manage', label: 'Manage with phpMyAdmin' }] : []),
  { id: 'delete', label: 'Delete database', variant: 'danger' as const },
];

const userMenuItems = (item: any) => [
  { id: 'edit', label: 'Edit user' },
  { id: 'rotate-password', label: 'Rotate password' },
  { id: 'delete', label: 'Delete user', variant: 'danger' as const, disabled: item.user === 'fluxo' },
];

const handleDatabaseAction = (action: string, item: any) => {
  if (action === 'manage') openPhpMyAdmin();
  else if (action === 'delete') deleteDatabase(item.id);
};

const handleUserAction = (action: string, item: any) => {
  if (action === 'edit') editUser(item);
  else if (action === 'rotate-password') rotateUserPassword(item);
  else if (action === 'delete' && item.user !== 'fluxo') deleteUser(item.user, item.engine);
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

const onUserPasswordRotated = () => {
  showRotatePasswordModal.value = false;
  rotatingUser.value = null;
  addToast('Database password rotated. Update the affected site environment files.', 'success');
};

const editUser = async (item: any) => {
  const engine = item.engine || 'mysql';
  const dbs = userGrants.value[userGrantKey(item.user, engine)] || [];
  editingUser.value = { name: item.user, databases: dbs, engine };
  showUserModal.value = true;
};

const rotateUserPassword = (item: any) => {
  rotatingUser.value = { name: item.user, engine: item.engine || 'mysql' };
  showRotatePasswordModal.value = true;
};

const deleteDatabase = async (id: number) => {
  const db = databases.value.find(d => d.id === id);
  if (!db) return;
  const ok = await confirm({ title: 'Delete Database', message: `Delete database "${db.name}"? This cannot be undone.`, confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    await apiClient.delete(`/api/v1/databases/${id}`);
    apiClient.invalidate('/api/v1/databases');
    apiClient.invalidate('/api/v1/databases/users/grants');
    addToast('Database deleted', 'success');
    fetchData();
  } catch (e: any) { addToast(e.message || 'Failed to delete', 'error'); }
};

const deleteUser = async (user: string, engine: string) => {
  const affected = databasesForUser(user, engine);
  const affectedNames = affected.map((name: string) => `"${name}"`).join(', ');
  const impact = affected.length > 0
    ? ` Fluxo currently associates this user with ${affectedNames}. Their site environment files will not be changed; update those applications manually before deleting the user. Fluxo will use its managed database account for future administration.`
    : '';
  const ok = await confirm({ title: 'Delete User', message: `Delete database user "${user}" from ${engine}?${impact}`, confirmText: 'Delete', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  try {
    await apiClient.delete(`/api/v1/databases/users?user=${encodeURIComponent(user)}&engine=${engine}`);
    apiClient.invalidate('/api/v1/databases/users');
    apiClient.invalidate('/api/v1/databases/users/grants');
    addToast('User deleted', 'success');
    fetchData();
  } catch (e: any) { addToast(e.message || 'Failed to delete', 'error'); }
};

const refreshPhpMyAdmin = async () => {
  phpMyAdmin.value = await apiClient.get('/api/v1/tools/phpmyadmin', { bypassCache: true, useCache: false });
};

const installPhpMyAdmin = async () => {
  const ok = await confirm({ title: 'Install phpMyAdmin', message: 'Install phpMyAdmin and make it available through Fluxo? The release will be downloaded from phpmyadmin.net.', confirmText: 'Install', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  phpMyAdminAction.value = 'install';
  const toastId = showToast({
    title: 'Installing phpMyAdmin',
    description: 'Downloading and verifying the latest supported release.',
    type: 'loading',
  });
  try {
    await apiClient.post('/api/v1/tools/phpmyadmin/install');
    await refreshPhpMyAdmin();
    updateToast(toastId, {
      title: 'phpMyAdmin installed',
      description: 'The database tool is ready to enable and use.',
      type: 'success',
    });
  } catch (e: any) {
    updateToast(toastId, {
      title: 'phpMyAdmin installation failed',
      description: e.message || 'phpMyAdmin could not be installed.',
      type: 'error',
    });
  }
  finally { phpMyAdminAction.value = ''; }
};

const enablePhpMyAdmin = async () => {
  phpMyAdminAction.value = 'enable';
  try {
    await apiClient.post('/api/v1/tools/phpmyadmin/enable');
    await refreshPhpMyAdmin();
    addToast('phpMyAdmin enabled', 'success');
  } catch (e: any) { addToast(e.message || 'Failed to enable phpMyAdmin', 'error'); }
  finally { phpMyAdminAction.value = ''; }
};

const disablePhpMyAdmin = async () => {
  const ok = await confirm({ title: 'Disable phpMyAdmin', message: 'Disable browser access while keeping phpMyAdmin installed?', confirmText: 'Disable', cancelText: 'Cancel', variant: 'info' });
  if (!ok) return;
  phpMyAdminAction.value = 'disable';
  try {
    await apiClient.post('/api/v1/tools/phpmyadmin/disable');
    await refreshPhpMyAdmin();
    addToast('phpMyAdmin disabled', 'success');
  } catch (e: any) { addToast(e.message || 'Failed to disable phpMyAdmin', 'error'); }
  finally { phpMyAdminAction.value = ''; }
};

const removePhpMyAdmin = async () => {
  const ok = await confirm({ title: 'Uninstall phpMyAdmin', message: 'Remove phpMyAdmin and its Fluxo-managed configuration? Your databases and database users will not be changed.', confirmText: 'Uninstall', cancelText: 'Cancel', variant: 'danger' });
  if (!ok) return;
  phpMyAdminAction.value = 'remove';
  const toastId = showToast({
    title: 'Removing phpMyAdmin',
    description: 'Your databases and database users will not be changed.',
    type: 'loading',
  });
  try {
    await apiClient.delete('/api/v1/tools/phpmyadmin');
    await refreshPhpMyAdmin();
    updateToast(toastId, {
      title: 'phpMyAdmin removed',
      description: null,
      type: 'success',
    });
  } catch (e: any) {
    updateToast(toastId, {
      title: 'phpMyAdmin could not be removed',
      description: e.message || 'Please try again.',
      type: 'error',
    });
  }
  finally { phpMyAdminAction.value = ''; }
};

const openPhpMyAdmin = async () => {
  if (!phpMyAdmin.value.installed || !phpMyAdmin.value.enabled) {
    addToast('Enable phpMyAdmin before opening it', 'error');
    return;
  }
  const popup = window.open('', '_blank');
  phpMyAdminAction.value = 'open';
  try {
    const result = await apiClient.post('/api/v1/tools/phpmyadmin/access');
    if (!result?.url) throw new Error('phpMyAdmin is unavailable in the demo');
    if (popup) {
      popup.opener = null;
      popup.location.href = result.url;
    } else {
      window.location.href = result.url;
    }
  } catch (e: any) {
    if (popup) popup.close();
    addToast(e.message || 'Failed to open phpMyAdmin', 'error');
  } finally { phpMyAdminAction.value = ''; }
};

onMounted(async () => {
  loading.value = true;
  await fetchData();
  loading.value = false;
});

onActivated(async () => {
  loading.value = true;
  await fetchData();
  loading.value = false;
});
</script>
