<template>
  <Card>
    <h2 class="text-lg font-semibold text-gray-900 mb-4 dark:text-gray-100">Database Users</h2>
    <p class="text-sm text-gray-600 mb-4 dark:text-gray-400">Manage the database users that may access your server's databases.</p>

    <DataTable :columns="columns" :items="users" empty-text="No database users found.">
      <template #user="{ item }">
        <span class="font-medium text-gray-900 font-mono dark:text-gray-100">{{ item.user }}</span>
      </template>
      <template #host="{ item }">
        <span class="text-gray-500 font-mono dark:text-gray-400">{{ item.host }}</span>
      </template>
      <template #databases="{ item }">
        <span v-if="item.user === 'fluxo'" class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300">All databases</span>
        <span v-else class="text-gray-500 dark:text-gray-400">{{ userDbCount(item.user) }}</span>
      </template>
    </DataTable>
  </Card>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import DataTable from '../components/DataTable.vue';
import Card from '../components/Card.vue';

const columns = [
  { key: 'user', label: 'User' },
  { key: 'host', label: 'Host' },
  { key: 'databases', label: 'Databases' },
];

const users = ref<any[]>([]);
const databases = ref<any[]>([]);

const token = () => localStorage.getItem('fluxo_jwt');

const userDbCount = (user: string) => {
  const count = databases.value.filter((d: any) => d.username === user).length;
  return count ? `${count} database${count !== 1 ? 's' : ''}` : 'None';
};

onMounted(async () => {
  try {
    const [uRes, dRes] = await Promise.all([
      fetch('/api/v1/databases/users', { headers: { 'Authorization': `Bearer ${token()}` } }),
      fetch('/api/v1/databases', { headers: { 'Authorization': `Bearer ${token()}` } })
    ]);
    if (uRes.ok) users.value = await uRes.json();
    if (dRes.ok) databases.value = await dRes.json();
  } catch (e) {
    console.error('Failed to load data:', e);
  }
});
</script>