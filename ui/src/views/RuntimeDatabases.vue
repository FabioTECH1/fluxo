<template>
  <div class="space-y-6">
    <div class="bg-white rounded-lg shadow-sm border border-gray-100 p-6 dark:bg-gray-900 dark:border-gray-800">
      <div class="flex justify-between items-start mb-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Database Engines</h2>
          <p class="text-sm text-gray-600 mt-1 dark:text-gray-400">Install and manage database engines on your server. You can install additional engines at any time.</p>
        </div>
      </div>

      <div class="overflow-hidden">
        <DataTable :columns="columns" :items="engines" empty-text="No database engines detected.">
          <template #engine="{ item }">
            <span class="font-medium text-gray-900 dark:text-gray-100">{{ item.name }}</span>
          </template>
          <template #version="{ item }">
            <span class="text-sm text-gray-600 dark:text-gray-400 max-w-[200px] truncate block">{{ item.version || 'N/A' }}</span>
          </template>
          <template #status="{ item }">
            <StatusBadge v-if="item.installed && item.status === 'running'" label="Running" variant="green" />
            <StatusBadge v-else-if="item.installed" label="Stopped" variant="red" />
            <StatusBadge v-else label="Not installed" variant="gray" />
          </template>
          <template #actions="{ item }">
            <div class="space-x-3">
              <button v-if="!item.installed" @click="installEngine(item.key)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs dark:text-blue-400 dark:hover:text-blue-300" :disabled="installing === item.key">
                {{ installing === item.key ? 'Installing...' : 'Install' }}
              </button>
              <template v-else>
                <button v-if="item.status === 'running'" @click="stopEngine(item.key)" class="text-yellow-600 hover:text-yellow-900 font-semibold text-xs dark:text-yellow-400 dark:hover:text-yellow-300" :disabled="stopping === item.key">
                  Stop
                </button>
                <button v-else @click="startEngine(item.key)" class="text-blue-600 hover:text-blue-900 font-semibold text-xs dark:text-blue-400 dark:hover:text-blue-300" :disabled="starting === item.key">
                  Start
                </button>
                <button v-if="item.status === 'running'" @click="restartEngine(item.key)" class="text-green-600 hover:text-green-900 font-semibold text-xs dark:text-green-400 dark:hover:text-green-300" :disabled="restarting === item.key">
                  {{ restarting === item.key ? 'Restarting...' : 'Restart' }}
                </button>
              </template>
            </div>
          </template>
        </DataTable>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useToast } from '../composables/useToast';
import { useConfirm } from '../composables/useConfirm';
import DataTable from '../components/DataTable.vue';
import StatusBadge from '../components/StatusBadge.vue';

const columns = [
  { key: 'engine', label: 'Engine' },
  { key: 'version', label: 'Version' },
  { key: 'status', label: 'Status' },
];

const { addToast } = useToast();
const { confirm } = useConfirm();
const token = () => localStorage.getItem('fluxo_jwt');

interface EngineInfo {
  key: string;
  name: string;
  version: string;
  installed: boolean;
  status: string;
}

const engines = ref<EngineInfo[]>([
  { key: 'mysql', name: 'MySQL (MariaDB)', version: '', installed: false, status: 'stopped' },
  { key: 'postgres', name: 'PostgreSQL', version: '', installed: false, status: 'stopped' },
  { key: 'redis', name: 'Redis', version: '', installed: false, status: 'stopped' },
]);

const installing = ref<string | null>(null);
const restarting = ref<string | null>(null);
const starting = ref<string | null>(null);
const stopping = ref<string | null>(null);

const fetchEngines = async () => {
  try {
    const res = await fetch('/api/v1/server/engines', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    const installedList: string[] = await res.json();

    for (const engine of engines.value) {
      engine.installed = installedList.includes(engine.key);
    }
  } catch (e) {
    console.error('Failed to fetch engines:', e);
  }
};

const fetchMySQLInfo = async () => {
  try {
    const res = await fetch('/api/v1/server/mysql/info', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (res.ok) {
      const data = await res.json();
      const eng = engines.value.find(e => e.key === 'mysql');
      if (eng) {
        eng.version = data.version || '';
        eng.status = data.status || 'stopped';
      }
    }
  } catch (e) {
    console.error('Failed to fetch MySQL info:', e);
  }
};

const fetchRedisInfo = async () => {
  try {
    const res = await fetch('/api/v1/server/redis/info', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (res.ok) {
      const data = await res.json();
      const eng = engines.value.find(e => e.key === 'redis');
      if (eng) {
        eng.version = data.version || '';
        eng.status = data.status || 'stopped';
      }
    }
  } catch (e) {
    console.error('Failed to fetch Redis info:', e);
  }
};

const fetchPostgresInfo = async () => {
  try {
    const res = await fetch('/api/v1/server/postgres/info', {
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (res.ok) {
      const data = await res.json();
      const eng = engines.value.find(e => e.key === 'postgres');
      if (eng) {
        eng.version = data.version || '';
        eng.status = data.status || 'stopped';
      }
    }
  } catch (e) {
    console.error('Failed to fetch Postgres info:', e);
  }
};

const installEngine = async (engineKey: string) => {
  const engineName = engineKey === 'mysql' ? 'MySQL' : engineKey === 'postgres' ? 'PostgreSQL' : 'Redis';
  const ok = await confirm({
    title: `Install ${engineName}`,
    message: `Install ${engineName} on this server? This will download and install the ${engineName} package via apt-get.`,
    confirmText: 'Install',
    cancelText: 'Cancel',
    variant: 'info'
  });
  if (!ok) return;

  installing.value = engineKey;
  try {
    const endpoint = engineKey === 'mysql'
      ? '/api/v1/server/engines/mysql/install'
      : engineKey === 'postgres'
      ? '/api/v1/server/engines/postgres/install'
      : '/api/v1/server/engines/redis/install';

    const res = await fetch(endpoint, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}` }
    });

    if (res.status === 200) {
      addToast(`${engineName} is already installed`, 'info');
    } else if (res.status === 202) {
      addToast(`${engineName} installation started. This may take a few minutes.`, 'success');
    } else {
      throw new Error(await res.text());
    }

    setTimeout(() => {
      fetchEngines();
      if (engineKey === 'mysql') fetchMySQLInfo();
      if (engineKey === 'postgres') fetchPostgresInfo();
      if (engineKey === 'redis') fetchRedisInfo();
    }, 3000);
  } catch (e: any) {
    addToast(e.message || 'Failed to install engine', 'error');
  } finally {
    installing.value = null;
  }
};

const startEngine = async (engineKey: string) => {
  const engineName = engineKey === 'mysql' ? 'MySQL' : engineKey === 'postgres' ? 'PostgreSQL' : 'Redis';
  starting.value = engineKey;
  try {
    const res = await fetch(`/api/v1/server/${engineKey}/start`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`${engineName} started successfully`, 'success');
    setTimeout(() => {
      if (engineKey === 'mysql') fetchMySQLInfo();
      if (engineKey === 'postgres') fetchPostgresInfo();
      if (engineKey === 'redis') fetchRedisInfo();
    }, 2000);
  } catch (e: any) {
    addToast(e.message || `Failed to start ${engineName}`, 'error');
  } finally {
    starting.value = null;
  }
};

const stopEngine = async (engineKey: string) => {
  const engineName = engineKey === 'mysql' ? 'MySQL' : engineKey === 'postgres' ? 'PostgreSQL' : 'Redis';
  const ok = await confirm({
    title: `Stop ${engineName}`,
    message: `Stop ${engineName}? Any applications connecting to it will lose connection.`,
    confirmText: 'Stop',
    cancelText: 'Cancel',
    variant: 'danger'
  });
  if (!ok) return;

  stopping.value = engineKey;
  try {
    const res = await fetch(`/api/v1/server/${engineKey}/stop`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`${engineName} stopped successfully`, 'success');
    setTimeout(() => {
      if (engineKey === 'mysql') fetchMySQLInfo();
      if (engineKey === 'postgres') fetchPostgresInfo();
      if (engineKey === 'redis') fetchRedisInfo();
    }, 2000);
  } catch (e: any) {
    addToast(e.message || `Failed to stop ${engineName}`, 'error');
  } finally {
    stopping.value = null;
  }
};

const restartEngine = async (engineKey: string) => {
  const engineName = engineKey === 'mysql' ? 'MySQL' : engineKey === 'postgres' ? 'PostgreSQL' : 'Redis';
  const ok = await confirm({
    title: `Restart ${engineName}`,
    message: `Restart ${engineName}? Active connections may be briefly interrupted.`,
    confirmText: 'Restart',
    cancelText: 'Cancel',
    variant: 'info'
  });
  if (!ok) return;

  restarting.value = engineKey;
  try {
    const res = await fetch(`/api/v1/server/${engineKey}/restart`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token()}` }
    });
    if (!res.ok) throw new Error(await res.text());
    addToast(`${engineName} restarted successfully`, 'success');
    setTimeout(() => {
      if (engineKey === 'mysql') fetchMySQLInfo();
      if (engineKey === 'postgres') fetchPostgresInfo();
      if (engineKey === 'redis') fetchRedisInfo();
    }, 2000);
  } catch (e: any) {
    addToast(e.message || `Failed to restart ${engineName}`, 'error');
  } finally {
    restarting.value = null;
  }
};

onMounted(() => {
  fetchEngines();
  fetchMySQLInfo();
  fetchPostgresInfo();
  fetchRedisInfo();
});
</script>
