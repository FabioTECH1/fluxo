import { createRouter, createWebHistory } from 'vue-router'
import Landing from './views/Landing.vue'

// Import Fluxo Dashboard views
import FluxoApp from '@fluxo/App.vue'
import Overview from '@fluxo/views/Overview.vue'
import Sites from '@fluxo/views/Sites.vue'
import Observe from '@fluxo/views/Observe.vue'
import ObserveMonitoring from '@fluxo/views/ObserveMonitoring.vue'
import ObserveLogs from '@fluxo/views/ObserveLogs.vue'
import ObserveActivity from '@fluxo/views/ObserveActivity.vue'
import Processes from '@fluxo/views/Processes.vue'
import ProcessesDaemons from '@fluxo/views/ProcessesDaemons.vue'
import ProcessesScheduler from '@fluxo/views/ProcessesScheduler.vue'
import Runtime from '@fluxo/views/Runtime.vue'
import RuntimePHP from '@fluxo/views/RuntimePHP.vue'
import RuntimeNode from '@fluxo/views/RuntimeNode.vue'
import RuntimeNginx from '@fluxo/views/RuntimeNginx.vue'
import RuntimeDatabases from '@fluxo/views/RuntimeDatabases.vue'
import Storage from '@fluxo/views/Storage.vue'
import StorageFiles from '@fluxo/views/StorageFiles.vue'
import StorageDatabases from '@fluxo/views/StorageDatabases.vue'
import StorageBackups from '@fluxo/views/StorageBackups.vue'
import FluxoSettings from '@fluxo/views/Settings.vue'
import SettingsGeneral from '@fluxo/views/SettingsGeneral.vue'
import SettingsSourceControl from '@fluxo/views/SettingsSourceControl.vue'
import SettingsSSHKeys from '@fluxo/views/SettingsSSHKeys.vue'
import SettingsNetwork from '@fluxo/views/SettingsNetwork.vue'
import SiteDashboard from '@fluxo/views/SiteDashboard.vue'
import SiteOverview from '@fluxo/views/site/SiteOverview.vue'
import SiteDeployments from '@fluxo/views/site/SiteDeployments.vue'
import SiteProcesses from '@fluxo/views/site/SiteProcesses.vue'
import SiteProcessesDaemons from '@fluxo/views/site/SiteProcessesDaemons.vue'
import SiteProcessesScheduler from '@fluxo/views/site/SiteProcessesScheduler.vue'
import SiteCommands from '@fluxo/views/site/SiteCommands.vue'
import SiteObserve from '@fluxo/views/site/SiteObserve.vue'
import SiteObserveLogs from '@fluxo/views/site/SiteObserveLogs.vue'
import SiteObserveActivity from '@fluxo/views/site/SiteObserveActivity.vue'
import SiteDomains from '@fluxo/views/site/SiteDomains.vue'
import SiteDomainsList from '@fluxo/views/site/SiteDomainsList.vue'
import SiteDomainsSSL from '@fluxo/views/site/SiteDomainsSSL.vue'
import SiteSettings from '@fluxo/views/site/SiteSettings.vue'
import SiteSettingsGeneral from '@fluxo/views/site/SiteSettingsGeneral.vue'
import SiteSettingsDeployments from '@fluxo/views/site/SiteSettingsDeployments.vue'
import SiteSettingsEnvironment from '@fluxo/views/site/SiteSettingsEnvironment.vue'
import SiteSettingsWordPress from '@fluxo/views/site/SiteSettingsWordPress.vue'

// Landing page router (base: '/')
export const landingRouter = createRouter({
  history: createWebHistory('/'),
  routes: [
    { path: '/', component: Landing },
    // Catch-all: redirect unknown paths to landing
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// Demo dashboard router (base: '/demo') — route.path is always unprefixed
// e.g. route.path === '/sites' when URL is /demo/sites
export const demoRouter = createRouter({
  history: createWebHistory('/demo'),
  routes: [
    {
      path: '/',
      component: FluxoApp,
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', component: Overview, meta: { title: 'Overview' } },
        { path: 'sites', component: Sites, meta: { title: 'Sites' } },
        {
          path: 'sites/:id',
          component: SiteDashboard,
          meta: { title: 'Manage Site' },
          children: [
            { path: '', redirect: (to) => `/sites/${to.params.id}/overview` },
            { path: 'overview', component: SiteOverview, meta: { title: 'Site Overview' } },
            { path: 'deployments', component: SiteDeployments, meta: { title: 'Deployments' } },
            {
              path: 'processes',
              component: SiteProcesses,
              children: [
                { path: '', redirect: (to) => `/sites/${to.params.id}/processes/daemons` },
                { path: 'daemons', component: SiteProcessesDaemons, meta: { title: 'Site Daemons' } },
                { path: 'scheduler', component: SiteProcessesScheduler, meta: { title: 'Site Scheduler' } },
              ]
            },
            { path: 'commands', component: SiteCommands, meta: { title: 'Commands' } },
            {
              path: 'observe',
              component: SiteObserve,
              children: [
                { path: '', redirect: (to) => `/sites/${to.params.id}/observe/logs` },
                { path: 'logs', component: SiteObserveLogs, meta: { title: 'Site Logs' } },
                { path: 'activity', component: SiteObserveActivity, meta: { title: 'Site Activity' } },
              ]
            },
            {
              path: 'domains',
              component: SiteDomains,
              children: [
                { path: '', redirect: (to) => `/sites/${to.params.id}/domains/list` },
                { path: 'list', component: SiteDomainsList, meta: { title: 'Domains' } },
                { path: 'ssl', component: SiteDomainsSSL, meta: { title: 'SSL' } },
              ]
            },
            {
              path: 'settings',
              component: SiteSettings,
              children: [
                { path: '', redirect: (to) => `/sites/${to.params.id}/settings/general` },
                { path: 'general', name: 'site-settings-general', component: SiteSettingsGeneral, meta: { title: 'Site Settings' } },
                { path: 'deployments', name: 'site-settings-deployments', component: SiteSettingsDeployments, meta: { title: 'Deploy Settings', cacheSiteEditor: true } },
                { path: 'environment', name: 'site-settings-environment', component: SiteSettingsEnvironment, meta: { title: 'Environment', cacheSiteEditor: true } },
                { path: 'wordpress', name: 'site-settings-wordpress', component: SiteSettingsWordPress, meta: { title: 'WordPress Configuration', cacheSiteEditor: true } },
              ]
            },
          ]
        },
        {
          path: 'processes',
          component: Processes,
          children: [
            { path: '', redirect: '/processes/daemons' },
            { path: 'daemons', component: ProcessesDaemons, meta: { title: 'Daemons' } },
            { path: 'scheduler', component: ProcessesScheduler, meta: { title: 'Scheduler' } },
          ]
        },
        {
          path: 'runtime',
          component: Runtime,
          children: [
            { path: '', redirect: '/runtime/php' },
            { path: 'php', component: RuntimePHP, meta: { title: 'PHP' } },
            { path: 'node', component: RuntimeNode, meta: { title: 'Node.js' } },
            { path: 'nginx', component: RuntimeNginx, meta: { title: 'Nginx' } },
            { path: 'databases', component: RuntimeDatabases, meta: { title: 'Database Engines' } },
          ]
        },
        {
          path: 'observe',
          component: Observe,
          children: [
            { path: '', redirect: '/observe/monitoring' },
            { path: 'monitoring', component: ObserveMonitoring, meta: { title: 'Monitoring' } },
            { path: 'logs', component: ObserveLogs, meta: { title: 'Server Logs' } },
            { path: 'activity', component: ObserveActivity, meta: { title: 'Activity' } },
          ]
        },
        {
          path: 'storage',
          component: Storage,
          children: [
            { path: '', redirect: '/storage/files' },
            { path: 'files', component: StorageFiles, meta: { title: 'Files' } },
            { path: 'databases', component: StorageDatabases, meta: { title: 'Databases' } },
            { path: 'users', redirect: '/storage/databases' },
            { path: 'backups', component: StorageBackups, meta: { title: 'Backups' } },
          ]
        },
        {
          path: 'settings',
          component: FluxoSettings,
          children: [
            { path: '', redirect: '/settings/general' },
            { path: 'general', component: SettingsGeneral, meta: { title: 'Settings' } },
            { path: 'source-control', component: SettingsSourceControl, meta: { title: 'Source Control' } },
            { path: 'ssh-keys', component: SettingsSSHKeys, meta: { title: 'SSH Keys' } },
            { path: 'network', component: SettingsNetwork, meta: { title: 'Firewall' } },
          ]
        }
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/overview' },
  ]
})

// Guards for the demo router
demoRouter.beforeEach((to) => {
  // Intercept logout
  if (to.path.endsWith('/login')) {
    localStorage.removeItem('fluxo_jwt')
    // Navigate back to the landing page (outside /demo base)
    window.location.href = '/'
    return false
  }

  // Ensure mock token exists
  if (!localStorage.getItem('fluxo_jwt')) {
    localStorage.setItem('fluxo_jwt', 'demo_token_123')
  }
})

demoRouter.afterEach((to) => {
  const title = to.meta.title as string
  document.title = title ? `${title} — Fluxo` : 'Fluxo'
})

// Default export for backwards compat (used by landing)
export default landingRouter
