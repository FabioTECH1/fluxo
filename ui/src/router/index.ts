import { createRouter, createWebHistory } from 'vue-router';
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
import Storage from '../views/Storage.vue';
import StorageDatabases from '../views/StorageDatabases.vue';
import StorageBackups from '../views/StorageBackups.vue';
import Settings from '../views/Settings.vue';
import SettingsGeneral from '../views/SettingsGeneral.vue';
import SettingsSourceControl from '../views/SettingsSourceControl.vue';
import SettingsSSHKeys from '../views/SettingsSSHKeys.vue';
import SettingsNetwork from '../views/SettingsNetwork.vue';
import SiteDashboard from '../views/SiteDashboard.vue';
import Login from '../views/Login.vue';

const routes = [
  { path: '/', redirect: '/overview' },
  { path: '/login', component: Login },
  { path: '/overview', component: Overview },
  { path: '/sites', component: Sites },
  { path: '/sites/:id', component: SiteDashboard },
  {
    path: '/processes',
    component: Processes,
    children: [
      { path: '', redirect: '/processes/daemons' },
      { path: 'daemons', component: ProcessesDaemons },
      { path: 'scheduler', component: ProcessesScheduler },
    ]
  },
  {
    path: '/runtime',
    component: Runtime,
    children: [
      { path: '', redirect: '/runtime/php' },
      { path: 'php', component: RuntimePHP },
      { path: 'node', component: RuntimeNode },
      { path: 'nginx', component: RuntimeNginx },
    ]
  },
  {
    path: '/observe',
    component: Observe,
    children: [
      { path: '', redirect: '/observe/monitoring' },
      { path: 'monitoring', component: ObserveMonitoring },
      { path: 'logs', component: ObserveLogs },
      { path: 'activity', component: ObserveActivity },
    ]
  },
  {
    path: '/storage',
    component: Storage,
    children: [
      { path: '', redirect: '/storage/databases' },
      { path: 'databases', component: StorageDatabases },
      { path: 'users', redirect: '/storage/databases' },
      { path: 'backups', component: StorageBackups },
    ]
  },
  {
    path: '/settings',
    component: Settings,
    children: [
      { path: '', redirect: '/settings/general' },
      { path: 'general', component: SettingsGeneral },
      { path: 'source-control', component: SettingsSourceControl },
      { path: 'ssh-keys', component: SettingsSSHKeys },
      { path: 'network', component: SettingsNetwork },
    ]
  }
];

export const router = createRouter({
  history: createWebHistory(),
  routes
});

router.beforeEach((to) => {
  const token = localStorage.getItem('fluxo_jwt');
  if (to.path !== '/login' && !token) {
    return '/login';
  } else if (to.path === '/login' && token) {
    return '/overview';
  }
});
