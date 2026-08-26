<template>
  <div class="flex flex-col lg:flex-row gap-6">
    <!-- Left Column -->
    <div class="flex-1 space-y-6">
      <!-- Deployments -->
      <SkeletonLoader v-if="loading" type="table" :rows="3" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Deployments</h2>
        </div>
        <div v-if="deployments.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No deployments yet.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="dep in deployments.slice(0, 5)" :key="dep.id" class="px-6 py-4 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
            <div class="flex items-center gap-3 min-w-0 flex-1">
              <span :class="statusBadge(dep.status)">{{ dep.status }}</span>
              <span v-if="dep.trigger_source === 'github_webhook'" class="flex items-center gap-1 text-[10px] uppercase font-bold text-purple-600 bg-purple-100 dark:bg-purple-900/30 dark:text-purple-300 px-1.5 py-0.5 rounded shrink-0" title="Auto-deployed via GitHub Push">
                <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clip-rule="evenodd" /></svg>
                Auto
              </span>
              <div v-if="dep.branch" class="flex items-center gap-1 text-xs text-gray-500 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded shrink-0">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
                <span>{{ dep.branch }}</span>
              </div>
              <span v-if="dep.commit_hash" class="font-mono text-xs font-medium text-blue-600 dark:text-blue-400 shrink-0">{{ dep.commit_hash.slice(0, 7) }}</span>
              <span v-else class="font-mono text-xs text-gray-400 dark:text-gray-500 shrink-0">No commit</span>
            </div>
            <div class="mt-2 flex items-baseline gap-2 min-w-0">
              <span class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ dep.commit_message || 'Manual Deployment' }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400 shrink-0">&middot; {{ timeAgo(dep.created_at) }}</span>
            </div>
          </li>
        </ul>
      </div>
 
      <!-- Background Processes -->
      <SkeletonLoader v-if="loading" type="table" :rows="2" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Background Processes</h2>
          <button type="button" @click="showAddDaemon = true" class="bg-blue-600 text-white h-7 w-7 rounded-lg shadow-sm hover:bg-blue-700 flex items-center justify-center font-bold transition-colors" title="Add background process" aria-label="Add background process"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4" /></svg></button>
        </div>
        <div v-if="daemons.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No background processes.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="d in daemons.slice(0, 5)" :key="d.id" class="px-6 py-4 hover:bg-gray-50/50 dark:hover:bg-gray-800/50 transition-all">
            <div class="flex items-center justify-between">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ d.name || d.command.split(' ').slice(0, 2).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ d.command }} &middot; {{ d.directory || site?.path || '' }}</p>
              </div>
              <div class="flex items-center gap-4 shrink-0">
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ d.instances || 1 }} {{ (d.instances || 1) > 1 ? 'Processes' : 'Process' }}</span>
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border"
                      :class="overviewDaemonStatusClass(d)">
                  <span class="h-1.5 w-1.5 rounded-full" :class="overviewDaemonStatusDotClass(d)"></span>
                  {{ overviewDaemonStatusLabel(d) }}
                </span>
              </div>
            </div>
          </li>
        </ul>
      </div>
 
      <!-- Scheduled Jobs -->
      <SkeletonLoader v-if="loading" type="table" :rows="2" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 flex justify-between items-center">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Scheduled Jobs</h2>
          <button type="button" @click="showAddCron = true" class="bg-blue-600 text-white h-7 w-7 rounded-lg shadow-sm hover:bg-blue-700 flex items-center justify-center font-bold transition-colors" title="Add scheduled job" aria-label="Add scheduled job"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4" /></svg></button>
        </div>
        <div v-if="crons.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No scheduled jobs.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="c in crons.slice(0, 5)" :key="c.id" class="px-6 py-4 hover:bg-gray-50/50 dark:hover:bg-gray-800/50 transition-all">
            <div class="flex items-center justify-between">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ c.name || c.command.split(' ').slice(0, 3).join(' ') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5 truncate">{{ c.user || 'fluxo' }} &middot; {{ c.command }}</p>
              </div>
              <div class="flex items-center gap-4 shrink-0">
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ frequencyLabel(c.expression) || c.expression }}</span>
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40">
                  <span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
                  Installed
                </span>
              </div>
            </div>
          </li>
        </ul>
      </div>
 
      <!-- Activity -->
      <SkeletonLoader v-if="loading" type="table" :rows="3" />
      <div v-else class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Activity</h2>
        </div>
        <div v-if="activity.length === 0" class="px-6 py-8 text-center text-gray-400 dark:text-gray-500 text-sm">
          No recent activity.
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="(a, i) in activity.slice(0, 5)" :key="i" class="px-6 py-3">
            <div class="flex items-center gap-3">
              <span class="h-7 w-7 rounded-full bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 flex items-center justify-center text-xs font-bold flex-shrink-0">
                {{ a.type === 'deployment' ? 'D' : 'S' }}
              </span>
              <div>
                <p class="text-sm text-gray-700 dark:text-gray-300">{{ a.summary }}</p>
                <p class="text-xs text-gray-400 dark:text-gray-500">{{ timeAgo(a.created_at) }}</p>
              </div>
            </div>
          </li>
        </ul>
      </div>
 
      <!-- Terminal -->
      <div v-if="logs.length > 0" ref="terminalBox" class="bg-gray-900 rounded-lg shadow-sm p-4 text-green-400 font-mono text-sm h-72 overflow-y-auto">
        <div v-for="(line, idx) in logs" :key="idx" class="whitespace-pre-wrap">{{ line }}</div>
      </div>
    </div>
 
    <!-- Sidebar -->
    <div v-if="loading" class="w-full lg:w-72 shrink-0 space-y-4">
      <SkeletonLoader type="card" />
    </div>
    <div v-else-if="site" class="w-full lg:w-72 shrink-0 space-y-4">
      <div class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">Details</h3>
        <div class="space-y-2.5">
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Daemon PID</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ metrics.daemon_pid }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Site ID</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ padId(site.id) }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Site User</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">fluxo</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Framework</p>
            <p class="text-sm text-gray-700 dark:text-gray-300 capitalize">{{ frameworkLabel }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500 mb-1">Deployment</p>
            <StatusBadge
              :label="site.deployment_strategy === 'zero-downtime' ? 'Zero-downtime' : 'Standard'"
              :variant="site.deployment_strategy === 'zero-downtime' ? 'blue' : 'gray'" />
          </div>
          <div v-if="['laravel', 'php', 'wordpress'].includes(site.app_type || 'php')">
            <p class="text-xs text-gray-400 dark:text-gray-500">PHP</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ site.php_version || 'Not set' }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Public IP</p>
            <p class="text-sm font-mono text-gray-700 dark:text-gray-300">{{ metrics.host_address || '—' }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-400 dark:text-gray-500">Created</p>
            <p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDate(site.created_at) }}</p>
          </div>
        </div>
      </div>
 
      <div v-if="showLaravelFeatures" class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5 space-y-3">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Laravel Features</h3>
        <div class="space-y-3">
          <ToggleSwitch v-if="schedulerAvailable || schedulerEnabled" :model-value="schedulerEnabled" label="Scheduler" label-position="left"
            :description="!schedulerAvailable && schedulerEnabled ? missingPackageDescription : ''"
            :disabled="!schedulerEnabled && !schedulerAvailable" @update:model-value="toggleScheduler" />
          <ToggleSwitch v-if="nightwatchInstalled || nightwatchEnabled" :model-value="nightwatchEnabled" label="Nightwatch" label-position="left"
            :description="!nightwatchInstalled && nightwatchEnabled ? missingPackageDescription : ''"
            :disabled="nightwatchToggling || (!nightwatchEnabled && !nightwatchAvailable)" @update:model-value="toggleNightwatch" />
          <div v-if="queueWorkerAvailable || queueWorkerEnabled" class="space-y-1">
            <ToggleSwitch :model-value="queueWorkerEnabled" label="Queue Worker" label-position="left"
              :description="queueWorkerDescription" :disabled="queueWorkerToggling || horizonToggling || horizonEnabled"
              @update:model-value="toggleQueueWorker" />
            <button v-if="queueWorkerEnabled && !horizonEnabled" type="button" class="text-xs font-semibold text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
              @click="openQueueWorkerModal">
              Configure worker
            </button>
          </div>
          <ToggleSwitch v-if="horizonInstalled || horizonEnabled" :model-value="horizonEnabled" label="Horizon" label-position="left"
            :description="!horizonInstalled && horizonEnabled ? missingPackageDescription : ''"
            :disabled="horizonToggling || queueWorkerToggling || (!horizonEnabled && !horizonAvailable)" @update:model-value="toggleHorizon" />
          <ToggleSwitch v-if="octaneInstalled || octaneEnabled" :model-value="octaneEnabled" label="Octane" label-position="left"
            :description="octaneDescription"
            :disabled="octaneToggling || (!octaneEnabled && !octaneAvailable)" @update:model-value="toggleOctane" />
          <ToggleSwitch v-if="maintenanceAvailable || !siteUp" :model-value="!siteUp" label="Maintenance mode" label-position="left"
            :description="!maintenanceAvailable && !siteUp ? missingPackageDescription : ''"
            :disabled="maintenanceToggling || (siteUp && !maintenanceAvailable)" @update:model-value="toggleMaintenance" />
        </div>
      </div>
 
      <div v-if="site.app_type !== 'html' && site.app_type !== 'wordpress'"
        class="bg-white dark:bg-gray-900 rounded-lg shadow-sm border border-gray-100 dark:border-gray-800 p-5">
        <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">Environment</h3>
        <div class="space-y-2">
          <button @click="$router.push(`/sites/${site.id}/settings`)" class="w-full text-left text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 font-medium">Edit .env file</button>
        </div>
      </div>
    </div>

    <AddDaemonModal v-model="showAddDaemon" :site-id="id" @created="onDaemonCreated" />
    <AddCronModal v-model="showAddCron" :site-id="id" @created="onCronCreated" />

    <BaseModal v-model="showQueueWorkerModal" :title="queueWorkerEnabled ? 'Configure Queue Worker' : 'Enable Queue Worker'" max-width="max-w-xl"
      :loading="queueWorkerToggling" :confirm-text="queueWorkerEnabled ? 'Save and restart worker' : 'Save and start worker'" :confirm-disabled="!queueWorkerFormValid"
      @submit="saveQueueWorker">
      <div class="space-y-5">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          Fluxo will update <code class="font-mono text-xs">QUEUE_CONNECTION</code>, start the worker with systemd, and reload it gracefully after deployments.
        </p>

        <FormGroup label="Queue connection" for-attr="queue-worker-connection" hint="Choose the connection your application dispatches queued jobs to.">
          <select id="queue-worker-connection" v-model="queueConnectionChoice" class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
            <option value="database">Database</option>
            <option value="redis">Redis</option>
            <option value="sqs">Amazon SQS</option>
            <option value="beanstalkd">Beanstalkd</option>
            <option value="custom">Custom connection</option>
          </select>
        </FormGroup>

        <FormGroup v-if="queueConnectionChoice === 'custom'" label="Custom connection name" for-attr="queue-worker-custom-connection">
          <input id="queue-worker-custom-connection" v-model.trim="queueCustomConnection" type="text" maxlength="64" placeholder="e.g. redis-long-running"
            class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 font-mono text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
        </FormGroup>

        <div class="grid gap-4 sm:grid-cols-2">
          <FormGroup label="Queues" for-attr="queue-worker-queues" hint="Comma-separated in priority order.">
            <input id="queue-worker-queues" v-model.trim="queueWorkerForm.queues" type="text" placeholder="default"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 font-mono text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
          <FormGroup label="Processes" for-attr="queue-worker-processes" hint="Concurrent worker processes.">
            <input id="queue-worker-processes" v-model.number="queueWorkerForm.processes" type="number" min="1" max="16"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
          <FormGroup label="Tries" for-attr="queue-worker-tries" hint="Use 0 to retry indefinitely.">
            <input id="queue-worker-tries" v-model.number="queueWorkerForm.tries" type="number" min="0" max="100"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
          <FormGroup label="Timeout" for-attr="queue-worker-timeout" hint="Maximum seconds for one job.">
            <input id="queue-worker-timeout" v-model.number="queueWorkerForm.timeout_seconds" type="number" min="1" max="86400"
              class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800">
          </FormGroup>
        </div>

        <button type="button" class="text-sm font-semibold text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200" @click="showQueueAdvanced = !showQueueAdvanced">
          {{ showQueueAdvanced ? 'Hide advanced settings' : 'Advanced settings' }}
        </button>
        <div v-if="showQueueAdvanced" class="space-y-4 border-l-2 border-gray-200 pl-4 dark:border-gray-700">
          <div class="grid gap-4 sm:grid-cols-2">
            <FormGroup label="Sleep (seconds)" for-attr="queue-worker-sleep">
              <input id="queue-worker-sleep" v-model.number="queueWorkerForm.sleep_seconds" type="number" min="0" max="60"
                class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
            </FormGroup>
            <FormGroup label="Backoff (seconds)" for-attr="queue-worker-backoff">
              <input id="queue-worker-backoff" v-model.number="queueWorkerForm.backoff_seconds" type="number" min="0" max="86400"
                class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
            </FormGroup>
            <FormGroup label="Memory (MB)" for-attr="queue-worker-memory">
              <input id="queue-worker-memory" v-model.number="queueWorkerForm.memory_mb" type="number" min="32" max="4096"
                class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
            </FormGroup>
            <FormGroup label="Max runtime (seconds)" for-attr="queue-worker-max-time" hint="Use 0 to disable lifetime recycling.">
              <input id="queue-worker-max-time" v-model.number="queueWorkerForm.max_time_seconds" type="number" min="0" max="86400"
                class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800">
            </FormGroup>
          </div>
          <ToggleSwitch v-model="queueWorkerForm.force" label="Process during maintenance mode"
            description="Continue processing jobs while the application is in maintenance mode." />
        </div>

        <p v-if="customQueueWorkers > 0" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
          {{ customQueueWorkers }} custom queue process{{ customQueueWorkers === 1 ? '' : 'es' }} already exists. Fluxo will not remove or modify custom processes.
        </p>
      </div>
    </BaseModal>

    <!-- Nightwatch Modal -->
    <div v-if="showNightwatchModal" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/40" @click="showNightwatchModal = false"></div>
      <div class="relative bg-white dark:bg-gray-900 rounded-xl shadow-2xl w-full max-w-md mx-4 overflow-hidden">
        <div class="px-6 py-4 border-b border-gray-100 dark:border-gray-800 bg-gray-50 dark:bg-gray-800 flex justify-between items-center">
          <h3 class="text-lg font-bold text-gray-900 dark:text-gray-100">Enable Nightwatch</h3>
          <button @click="showNightwatchModal = false" class="text-gray-400 dark:text-gray-500 hover:text-gray-600">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div>
            <label class="block text-gray-700 dark:text-gray-300 text-sm font-bold mb-1">Nightwatch Token</label>
            <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">Enter your Laravel Nightwatch ingestion token. The port will be assigned automatically.</p>
            <input v-model="nightwatchToken" type="text" class="w-full bg-white dark:bg-gray-800 dark:border-gray-700 border border-gray-200 rounded-lg px-3 py-2 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 transition-shadow text-sm font-mono" placeholder="nw_...">
          </div>
          <div class="flex justify-end gap-3 pt-2">
            <button @click="showNightwatchModal = false" class="px-4 py-2 text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-sm hover:bg-gray-50 dark:hover:bg-gray-800 font-semibold text-sm transition-colors">Cancel</button>
            <button @click="enableNightwatch" :disabled="!nightwatchToken" class="px-4 py-2 text-white bg-blue-600 rounded-lg shadow-sm hover:bg-blue-700 font-semibold text-sm transition-colors disabled:opacity-50">Enable</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, onActivated, onDeactivated, nextTick, watch, inject } from 'vue';
import { useRoute } from 'vue-router';
import { useToast } from '../../composables/useToast';
import { useConfirm } from '../../composables/useConfirm';
import { useWebSocket } from '../../composables/useWebSocket';
import { apiClient } from '../../api/client';
import SkeletonLoader from '../../components/SkeletonLoader.vue';
import StatusBadge from '../../components/StatusBadge.vue';
import ToggleSwitch from '../../components/ToggleSwitch.vue';
import BaseModal from '../../components/BaseModal.vue';
import FormGroup from '../../components/FormGroup.vue';
import AddDaemonModal from '../AddDaemonModal.vue';
import AddCronModal from '../AddCronModal.vue';
import { siteTypeLabel } from '../../utils/sitePresentation';

const route = useRoute();
let id = route.params.id as string;

const { addToast } = useToast();
const { confirm } = useConfirm();
const refreshStatuses = inject<() => void>('refreshStatuses', () => {});


const site = ref<any>(null);
const deployments = ref<any[]>([]);
const daemons = ref<any[]>([]);
const crons = ref<any[]>([]);
const activity = ref<any[]>([]);
const terminalBox = ref<HTMLElement | null>(null);
const metrics = ref<any>({});

const { logs, connect: wsConnect, disconnect: wsDisconnect } = useWebSocket();

watch(logs, () => {
  nextTick(() => {
    terminalBox.value?.scrollTo({ top: terminalBox.value.scrollHeight });
  });
});

const loading = ref(true);

const showAddDaemon = ref(false);
const showAddCron = ref(false);

const showNightwatchModal = ref(false);
const nightwatchToken = ref('');
const nightwatchToggling = ref(false);

const siteUp = ref(true);
const maintenanceToggling = ref(false);

const schedulerEnabled = ref(false);
const schedulerAvailable = ref(false);
const nightwatchEnabled = ref(false);
const nightwatchInstalled = ref(false);
const nightwatchAvailable = ref(false);
const horizonEnabled = ref(false);
const horizonInstalled = ref(false);
const horizonAvailable = ref(false);
const horizonToggling = ref(false);
const queueWorkerEnabled = ref(false);
const queueWorkerAvailable = ref(false);
const queueWorkerToggling = ref(false);
const showQueueWorkerModal = ref(false);
const showQueueAdvanced = ref(false);
const queueConnectionChoice = ref('database');
const queueCustomConnection = ref('');
const customQueueWorkers = ref(0);
const queueWorkerForm = ref({
  queues: 'default', processes: 1, sleep_seconds: 3, tries: 3,
  timeout_seconds: 60, backoff_seconds: 0, memory_mb: 128,
  max_time_seconds: 3600, force: false,
});
const savedQueueWorkerConfig = ref<any>({});
const octaneEnabled = ref(false);
const octaneInstalled = ref(false);
const octaneAvailable = ref(false);
const octaneToggling = ref(false);
const laravelDetected = ref(false);
const laravelVersion = ref('');
const maintenanceAvailable = ref(false);

const overviewDaemonRunning = (daemon: any) => daemon.status === 'active' || daemon.status === 'running';
const overviewDaemonStatusLabel = (daemon: any) => daemon.status === 'degraded' ? 'Degraded' : (overviewDaemonRunning(daemon) ? 'Running' : 'Stopped');
const overviewDaemonStatusClass = (daemon: any) => daemon.status === 'degraded'
  ? 'bg-yellow-50 dark:bg-yellow-950/20 text-yellow-700 dark:text-yellow-400 border-yellow-200 dark:border-yellow-900/40'
  : (overviewDaemonRunning(daemon)
    ? 'bg-green-50 dark:bg-green-950/20 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900/40'
    : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-700');
const overviewDaemonStatusDotClass = (daemon: any) => daemon.status === 'degraded' ? 'bg-yellow-500' : (overviewDaemonRunning(daemon) ? 'bg-green-500' : 'bg-gray-400');

const missingPackageDescription = 'Package no longer detected. Disable to remove the managed process.';
const showLaravelFeatures = computed(() => laravelDetected.value || schedulerEnabled.value || nightwatchEnabled.value || queueWorkerEnabled.value || horizonEnabled.value || octaneEnabled.value || !siteUp.value);
const queueWorkerDescription = computed(() => {
  if (horizonEnabled.value) return 'Queue processing is managed by Horizon.';
  if (customQueueWorkers.value > 0 && !queueWorkerEnabled.value) return 'A custom queue process is already configured.';
  return queueWorkerEnabled.value ? 'Managed by Fluxo and restarted gracefully after deployments.' : '';
});
const resolvedQueueConnection = computed(() => queueConnectionChoice.value === 'custom'
  ? queueCustomConnection.value.trim()
  : queueConnectionChoice.value);
const integerInRange = (value: unknown, min: number, max: number) => typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max;
const queueWorkerFormValid = computed(() => Boolean(
  resolvedQueueConnection.value
  && queueWorkerForm.value.queues.trim()
  && integerInRange(queueWorkerForm.value.processes, 1, 16)
  && integerInRange(queueWorkerForm.value.sleep_seconds, 0, 60)
  && integerInRange(queueWorkerForm.value.tries, 0, 100)
  && integerInRange(queueWorkerForm.value.timeout_seconds, 1, 86400)
  && integerInRange(queueWorkerForm.value.backoff_seconds, 0, 86400)
  && integerInRange(queueWorkerForm.value.memory_mb, 32, 4096)
  && integerInRange(queueWorkerForm.value.max_time_seconds, 0, 86400)
));
const frameworkLabel = computed(() => laravelDetected.value
  ? `Laravel${laravelVersion.value ? ` ${laravelVersion.value}` : ''}`
  : siteTypeLabel(site.value || {}));
const octaneDescription = computed(() => {
  if (!octaneInstalled.value && octaneEnabled.value) return missingPackageDescription;
  if (site.value?.deployment_strategy === 'zero-downtime') return 'Unavailable with zero-downtime deployments.';
  return '';
});

const fetchFeatures = async () => {
  try {
    const data = await apiClient.getSiteFeatures(id);
    laravelDetected.value = Boolean(data.laravel_detected);
    laravelVersion.value = data.laravel_version || '';
    schedulerEnabled.value = data.scheduler_enabled;
    schedulerAvailable.value = Boolean(data.scheduler_available);
    nightwatchEnabled.value = data.nightwatch_enabled;
    nightwatchInstalled.value = Boolean(data.nightwatch_installed);
    nightwatchAvailable.value = Boolean(data.nightwatch_available);
    horizonEnabled.value = data.horizon_enabled;
    horizonInstalled.value = Boolean(data.horizon_installed);
    horizonAvailable.value = Boolean(data.horizon_available);
    queueWorkerEnabled.value = Boolean(data.queue_worker_enabled);
    queueWorkerAvailable.value = Boolean(data.queue_worker_available);
    customQueueWorkers.value = Number(data.custom_queue_workers || 0);
    savedQueueWorkerConfig.value = data.queue_worker_config || {};
    octaneEnabled.value = data.octane_enabled;
    octaneInstalled.value = Boolean(data.octane_installed);
    octaneAvailable.value = Boolean(data.octane_available);
    maintenanceAvailable.value = Boolean(data.maintenance_available);
    siteUp.value = !data.in_maintenance;
  } catch (e) {}
};

const fetchSite = async () => {
  try {
    site.value = await apiClient.getSite(id);
  } catch (e) {}
};

const fetchDeployments = async () => {
  try {
    const data = await apiClient.getSiteDeployments(id, 1);
    deployments.value = data.data || [];
  } catch (e) {}
};

const fetchDaemons = async () => {
  try {
    daemons.value = await apiClient.getSiteDaemons(id) || [];
  } catch (e) {}
};

const fetchCrons = async () => {
  try {
    crons.value = await apiClient.getSiteCrons(id) || [];
  } catch (e) {}
};

const fetchActivity = async () => {
  try {
    const data = await apiClient.getSiteActivity(id, 5);
    activity.value = data.items || [];
  } catch (e) {}
};

const fetchMetrics = async () => {
  try {
    metrics.value = await apiClient.getMetrics() || {};
  } catch (e) {}
};

const onDaemonCreated = () => {
  showAddDaemon.value = false;
  addToast('Background process created', 'success');
  fetchDaemons();
};

const onCronCreated = () => {
  showAddCron.value = false;
  addToast('Scheduled job added', 'success');
  fetchCrons();
};

const toggleScheduler = async () => {
  const enabling = !schedulerEnabled.value;
  if (enabling && !schedulerAvailable.value) {
    addToast('Laravel was not found in the active composer.lock', 'error');
    return;
  }
  const confirmed = await confirm({
    title: enabling ? 'Enable Scheduler' : 'Disable Scheduler',
    message: enabling ? 'Enable the Laravel Scheduler? Cron jobs will run on the defined schedule.' : 'Disable the Laravel Scheduler? Cron jobs will stop running.',
    confirmText: enabling ? 'Enable' : 'Disable',
    variant: enabling ? 'info' : 'danger'
  });
  if (!confirmed) return;
  try {
    await apiClient.toggleSiteScheduler(id, enabling);
    fetchFeatures();
    fetchCrons();
  } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
};

const toggleNightwatch = async () => {
  if (nightwatchEnabled.value) {
    const confirmed = await confirm({
      title: 'Disable Nightwatch',
      message: 'Disable Nightwatch monitoring? The daemon and ingestion endpoint will be removed.',
      confirmText: 'Disable',
      variant: 'danger'
    });
    if (!confirmed) return;
    try {
      await apiClient.toggleSiteNightwatch(id, false);
      fetchFeatures();
      fetchDaemons();
    } catch (e: any) { addToast(e.message || 'Failed', 'error'); }
  } else {
    if (!nightwatchAvailable.value) {
      addToast('laravel/nightwatch was not found in the active composer.lock', 'error');
      return;
    }
    showNightwatchModal.value = true;
  }
};

const enableNightwatch = async () => {
  const token = nightwatchToken.value.trim();
  if (!token) return;
  nightwatchToggling.value = true;
  try {
    await apiClient.toggleSiteNightwatch(id, true, token);
    addToast('Nightwatch enabled', 'success');
    showNightwatchModal.value = false;
    nightwatchToken.value = '';
    fetchFeatures();
    fetchDaemons();
  } catch (e: any) {
    addToast(e.message || 'Failed to enable Nightwatch', 'error');
  } finally {
    nightwatchToggling.value = false;
  }
};

const openQueueWorkerModal = () => {
  const defaults = {
    connection: 'database', queues: 'default', processes: 1, sleep_seconds: 3,
    tries: 3, timeout_seconds: 60, backoff_seconds: 0, memory_mb: 128,
    max_time_seconds: 3600, force: false,
  };
  const config = { ...defaults, ...(savedQueueWorkerConfig.value || {}) };
  const commonConnections = ['database', 'redis', 'sqs', 'beanstalkd'];
  if (commonConnections.includes(config.connection)) {
    queueConnectionChoice.value = config.connection;
    queueCustomConnection.value = '';
  } else {
    queueConnectionChoice.value = 'custom';
    queueCustomConnection.value = config.connection || '';
  }
  queueWorkerForm.value = {
    queues: config.queues,
    processes: config.processes,
    sleep_seconds: config.sleep_seconds,
    tries: config.tries,
    timeout_seconds: config.timeout_seconds,
    backoff_seconds: config.backoff_seconds,
    memory_mb: config.memory_mb,
    max_time_seconds: config.max_time_seconds,
    force: Boolean(config.force),
  };
  showQueueAdvanced.value = false;
  showQueueWorkerModal.value = true;
};

const toggleQueueWorker = async (enabled: boolean) => {
  if (enabled) {
    if (horizonEnabled.value) {
      addToast('Disable Horizon before enabling the standard queue worker', 'error');
      return;
    }
    openQueueWorkerModal();
    return;
  }

  const confirmed = await confirm({
    title: 'Disable Queue Worker',
    message: 'Disable the managed Laravel queue worker? Queued jobs will stop processing unless another worker is running.',
    confirmText: 'Disable',
    variant: 'danger',
  });
  if (!confirmed) return;
  queueWorkerToggling.value = true;
  try {
    await apiClient.toggleSiteQueueWorker(id, false);
    addToast('Queue Worker disabled', 'success');
    await Promise.allSettled([fetchFeatures(), fetchDaemons()]);
  } catch (e: any) {
    addToast(e.message || 'Failed to disable Queue Worker', 'error');
  } finally {
    queueWorkerToggling.value = false;
  }
};

const saveQueueWorker = async () => {
  if (!queueWorkerFormValid.value) return;
  queueWorkerToggling.value = true;
  try {
    await apiClient.toggleSiteQueueWorker(id, true, {
      ...queueWorkerForm.value,
      connection: resolvedQueueConnection.value,
    });
    addToast(queueWorkerEnabled.value ? 'Queue Worker updated' : 'Queue Worker enabled', 'success');
    showQueueWorkerModal.value = false;
    await Promise.allSettled([fetchFeatures(), fetchDaemons()]);
  } catch (e: any) {
    addToast(e.message || 'Failed to enable Queue Worker', 'error');
  } finally {
    queueWorkerToggling.value = false;
  }
};

const toggleHorizon = async () => {
  const enabling = !horizonEnabled.value;
  if (enabling && !horizonAvailable.value) {
    addToast('laravel/horizon was not found in the active composer.lock', 'error');
    return;
  }
  const confirmed = await confirm({
    title: enabling ? 'Enable Horizon' : 'Disable Horizon',
    message: enabling
      ? queueWorkerEnabled.value
        ? 'Enable Laravel Horizon? Horizon will replace the standard Queue Worker. Fluxo will restore the worker automatically if Horizon cannot start.'
        : 'Enable Laravel Horizon? Fluxo will create a managed Horizon process and restart it gracefully after deployments.'
      : 'Disable Laravel Horizon? Fluxo will remove the managed process and deployment restart hook.',
    confirmText: enabling ? 'Enable' : 'Disable',
    variant: enabling ? 'info' : 'danger'
  });
  if (!confirmed) return;
  horizonToggling.value = true;
  try {
    await apiClient.toggleSiteHorizon(id, enabling);
    addToast(`Horizon ${enabling ? 'enabled' : 'disabled'}`, 'success');
    await Promise.allSettled([fetchFeatures(), fetchDaemons(), fetchSite()]);
  } catch (e: any) {
    addToast(e.message || 'Failed to update Horizon', 'error');
  } finally {
    horizonToggling.value = false;
  }
};

const toggleOctane = async () => {
  const enabling = !octaneEnabled.value;
  if (enabling && !octaneInstalled.value) {
    addToast('laravel/octane was not found in the active composer.lock', 'error');
    return;
  }
  if (enabling && site.value?.deployment_strategy === 'zero-downtime') {
    addToast('Octane is unavailable with zero-downtime deployments', 'error');
    return;
  }
  const confirmed = await confirm({
    title: enabling ? 'Enable Octane' : 'Disable Octane',
    message: enabling ? 'Enable Laravel Octane? Fluxo will create an Octane daemon, proxy traffic to it, and add an Octane reload to the deployment script.' : 'Disable Laravel Octane? Fluxo will remove the daemon and restore normal PHP-FPM routing.',
    confirmText: enabling ? 'Enable' : 'Disable',
    variant: enabling ? 'info' : 'danger'
  });
  if (!confirmed) return;
  octaneToggling.value = true;
  try {
    await apiClient.toggleSiteOctane(id, enabling);
    addToast(`Octane ${enabling ? 'enabled' : 'disabled'}`, 'success');
    await Promise.allSettled([fetchFeatures(), fetchDaemons(), fetchSite()]);
  } catch (e: any) {
    addToast(e.message || 'Failed to update Octane', 'error');
  } finally {
    octaneToggling.value = false;
  }
};

const toggleMaintenance = async () => {
  const enabling = siteUp.value;
  if (enabling && !maintenanceAvailable.value) {
    addToast('Laravel was not found in the active composer.lock', 'error');
    return;
  }
  const confirmed = await confirm({
    title: enabling ? 'Enable Maintenance Mode' : 'Disable Maintenance Mode',
    message: enabling ? 'Enable maintenance mode? The site will display a 503 page to visitors.' : 'Disable maintenance mode? The site will return to normal operation.',
    confirmText: enabling ? 'Enable' : 'Disable',
    variant: enabling ? 'danger' : 'info'
  });
  if (!confirmed) return;
  maintenanceToggling.value = true;
  try {
    await apiClient.toggleSiteMaintenance(id, enabling);
    siteUp.value = !siteUp.value;
    refreshStatuses();
    addToast(`Site ${enabling ? 'put into maintenance mode' : 'brought back online'}`, 'success');
  } catch (e: any) {
    addToast(e.message || 'Failed', 'error');
  } finally {
    maintenanceToggling.value = false;
  }
};

const statusBadge = (status: string) => {
  const base = 'inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold';
  if (status === 'success') return `${base} bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-900/50`;
  if (status === 'failed') return `${base} bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-900/50`;
  if (status === 'running') return `${base} bg-yellow-50 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 border border-yellow-200 dark:border-yellow-900/50`;
  if (status === 'pending') return `${base} bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 border border-indigo-200 dark:border-indigo-900/50`;
  return `${base} bg-gray-50 dark:bg-gray-800 text-gray-500 dark:text-gray-400 border border-gray-200 dark:border-gray-700`;
};

const timeAgo = (dateStr: string) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  const now = new Date();
  const diff = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (diff < 60) return 'just now';
  if (diff < 3600) return `${Math.floor(diff / 60)} minute${Math.floor(diff / 60) > 1 ? 's' : ''} ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} hour${Math.floor(diff / 3600) > 1 ? 's' : ''} ago`;
  if (diff < 604800) return `${Math.floor(diff / 86400)} day${Math.floor(diff / 86400) > 1 ? 's' : ''} ago`;
  return `${Math.floor(diff / 604800)} week${Math.floor(diff / 604800) > 1 ? 's' : ''} ago`;
};

const formatDate = (dateStr: string) => {
  if (!dateStr) return '';
  return new Date(dateStr).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
};

const padId = (id: number) => String(id).padStart(5, '0');

const frequencyLabel = (expr: string) => {
  const map: Record<string, string> = {
    '* * * * *': 'Every minute', '*/5 * * * *': 'Every 5 min',
    '0 * * * *': 'Hourly', '0 0 * * *': 'Daily',
    '0 0 * * 0': 'Weekly', '0 0 1 * *': 'Monthly',
  };
  return map[expr] || '';
};

const fetchAllData = async () => {
  loading.value = true;
  try {
    await Promise.allSettled([
      fetchSite(),
      fetchFeatures(),
      fetchDeployments(),
      fetchDaemons(),
      fetchCrons(),
      fetchActivity(),
      fetchMetrics()
    ]);
  } finally {
    loading.value = false;
  }
};

const silentRefresh = async () => {
  await Promise.allSettled([
    fetchSite(),
    fetchFeatures(),
    fetchDeployments(),
    fetchDaemons(),
    fetchCrons(),
    fetchActivity(),
    fetchMetrics()
  ]);
};

onMounted(() => {
  fetchAllData();
  wsConnect(id);
});

onActivated(() => {
  silentRefresh();
  wsConnect(id);
});

onDeactivated(() => {
  wsDisconnect();
});

onUnmounted(() => {
  wsDisconnect();
});

watch(() => route.params.id, (newId) => {
  wsDisconnect();
  if (typeof newId !== 'string' || !/^[1-9]\d*$/.test(newId)) return;
  id = newId;
  fetchAllData();
  wsConnect(id);
});
</script>
