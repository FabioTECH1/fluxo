import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '../stores/auth';
import Overview from '../views/Overview.vue';
import Sites from '../views/Sites.vue';
import Observe from '../views/Observe.vue';
import ObserveMonitoring from '../views/ObserveMonitoring.vue';
import ObserveLogs from '../views/ObserveLogs.vue';
import ObserveActivity from '../views/ObserveActivity.vue';
import Processes from '../views/Processes.vue';
import ProcessesDaemons from '../views/ProcessesDaemons.vue';
import ProcessesScheduler from '../views/ProcessesScheduler.vue';
import Runtime from '../views/Runtime.vue';
import RuntimePHP from '../views/RuntimePHP.vue';
import RuntimeNode from '../views/RuntimeNode.vue';
import RuntimeNginx from '../views/RuntimeNginx.vue';
import RuntimeDatabases from '../views/RuntimeDatabases.vue';
import Storage from '../views/Storage.vue';
import StorageFiles from '../views/StorageFiles.vue';
import StorageDatabases from '../views/StorageDatabases.vue';
import StorageBackups from '../views/StorageBackups.vue';
import Settings from '../views/Settings.vue';
import SettingsGeneral from '../views/SettingsGeneral.vue';
import SettingsSourceControl from '../views/SettingsSourceControl.vue';
import SettingsSSHKeys from '../views/SettingsSSHKeys.vue';
import SettingsNetwork from '../views/SettingsNetwork.vue';
import SiteDashboard from '../views/SiteDashboard.vue';
import SiteOverview from '../views/site/SiteOverview.vue';
import SiteDeployments from '../views/site/SiteDeployments.vue';
import SiteProcesses from '../views/site/SiteProcesses.vue';
import SiteProcessesDaemons from '../views/site/SiteProcessesDaemons.vue';
import SiteProcessesScheduler from '../views/site/SiteProcessesScheduler.vue';
import SiteCommands from '../views/site/SiteCommands.vue';
import SiteObserve from '../views/site/SiteObserve.vue';
import SiteObserveLogs from '../views/site/SiteObserveLogs.vue';
import SiteObserveActivity from '../views/site/SiteObserveActivity.vue';
import SiteDomains from '../views/site/SiteDomains.vue';
import SiteDomainsList from '../views/site/SiteDomainsList.vue';
import SiteDomainsSSL from '../views/site/SiteDomainsSSL.vue';
import SiteSettings from '../views/site/SiteSettings.vue';
import SiteSettingsGeneral from '../views/site/SiteSettingsGeneral.vue';
import SiteSettingsDeployments from '../views/site/SiteSettingsDeployments.vue';
import SiteSettingsEnvironment from '../views/site/SiteSettingsEnvironment.vue';
import SiteSettingsWordPress from '../views/site/SiteSettingsWordPress.vue';
import Login from '../views/Login.vue';

const routes = [
  { path: '/', redirect: '/overview' },
  { path: '/login', component: Login, meta: { title: 'Login' } },
  { path: '/overview', component: Overview, meta: { title: 'Overview' } },
  { path: '/sites', component: Sites, meta: { title: 'Sites' } },
  {
    path: '/sites/:id',
    component: SiteDashboard,
    meta: { title: 'Manage Site' },
    children: [
      { path: '', redirect: (to: any) => `/sites/${to.params.id}/overview` },
      { path: 'overview', component: SiteOverview, meta: { title: 'Site Overview' } },
      { path: 'deployments', component: SiteDeployments, meta: { title: 'Deployments' } },
      { path: 'processes', component: SiteProcesses, children: [
        { path: '', redirect: (to: any) => `/sites/${to.params.id}/processes/daemons` },
        { path: 'daemons', component: SiteProcessesDaemons, meta: { title: 'Site Daemons' } },
        { path: 'scheduler', component: SiteProcessesScheduler, meta: { title: 'Site Scheduler' } },
      ] },
      { path: 'commands', component: SiteCommands, meta: { title: 'Commands' } },
      { path: 'observe', component: SiteObserve, children: [
        { path: '', redirect: (to: any) => `/sites/${to.params.id}/observe/logs` },
        { path: 'logs', component: SiteObserveLogs, meta: { title: 'Site Logs' } },
        { path: 'activity', component: SiteObserveActivity, meta: { title: 'Site Activity' } },
      ] },
      { path: 'domains', component: SiteDomains, children: [
        { path: '', redirect: (to: any) => `/sites/${to.params.id}/domains/list` },
        { path: 'list', component: SiteDomainsList, meta: { title: 'Domains' } },
        { path: 'ssl', component: SiteDomainsSSL, meta: { title: 'SSL' } },
      ] },
      { path: 'settings', component: SiteSettings, children: [
        { path: '', redirect: (to: any) => `/sites/${to.params.id}/settings/general` },
        { path: 'general', name: 'site-settings-general', component: SiteSettingsGeneral, meta: { title: 'Site Settings' } },
        { path: 'deployments', name: 'site-settings-deployments', component: SiteSettingsDeployments, meta: { title: 'Deploy Settings', cacheSiteEditor: true } },
        { path: 'environment', name: 'site-settings-environment', component: SiteSettingsEnvironment, meta: { title: 'Environment', cacheSiteEditor: true } },
        { path: 'wordpress', name: 'site-settings-wordpress', component: SiteSettingsWordPress, meta: { title: 'WordPress Configuration', cacheSiteEditor: true } },
      ] },
    ]
  },
  {
    path: '/processes',
    component: Processes,
    children: [
      { path: '', redirect: '/processes/daemons' },
      { path: 'daemons', component: ProcessesDaemons, meta: { title: 'Daemons' } },
      { path: 'scheduler', component: ProcessesScheduler, meta: { title: 'Scheduler' } },
    ]
  },
  {
    path: '/runtime',
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
    path: '/observe',
    component: Observe,
    children: [
      { path: '', redirect: '/observe/monitoring' },
      { path: 'monitoring', component: ObserveMonitoring, meta: { title: 'Monitoring' } },
      { path: 'logs', component: ObserveLogs, meta: { title: 'Server Logs' } },
      { path: 'activity', component: ObserveActivity, meta: { title: 'Activity' } },
    ]
  },
  {
    path: '/storage',
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
    path: '/settings',
    component: Settings,
    children: [
      { path: '', redirect: '/settings/general' },
      { path: 'general', component: SettingsGeneral, meta: { title: 'Settings' } },
      { path: 'source-control', component: SettingsSourceControl, meta: { title: 'Source Control' } },
      { path: 'ssh-keys', component: SettingsSSHKeys, meta: { title: 'SSH Keys' } },
      { path: 'network', component: SettingsNetwork, meta: { title: 'Firewall' } },
    ]
  }
];

export const router = createRouter({
  history: createWebHistory(),
  routes
});

router.beforeEach((to) => {
  const auth = useAuthStore();
  auth.syncFromStorage();
  if (to.path !== '/login' && !auth.isAuthenticated) {
    return '/login';
  } else if (to.path === '/login' && auth.isAuthenticated) {
    return '/overview';
  }
});

router.afterEach((to) => {
  const title = to.meta.title as string;
  document.title = title ? `${title} — fluxo` : 'fluxo';
});
