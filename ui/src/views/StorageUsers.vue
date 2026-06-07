<template>
  <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6">
    <h2 class="text-lg font-semibold text-gray-900 mb-4">Database Users</h2>
    <p class="text-sm text-gray-600 mb-4">Manage the database users that may access your server's databases.</p>

    <div class="overflow-x-auto border rounded-lg">
      <table class="min-w-full divide-y divide-gray-200">
        <thead class="bg-gray-50">
          <tr>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">User</th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Host</th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Databases</th>
          </tr>
        </thead>
        <tbody class="bg-white divide-y divide-gray-200">
          <tr v-for="u in users" :key="u.user + u.host" class="hover:bg-gray-50 transition-colors">
            <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 font-mono">{{ u.user }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 font-mono">{{ u.host }}</td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ userDbCount(u.user) }}</td>
          </tr>
          <tr v-if="users.length === 0">
            <td colspan="3" class="px-6 py-8 text-center text-gray-500 text-sm">No database users found.</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';

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