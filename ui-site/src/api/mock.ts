import { version as appVersion } from '../../package.json'

function delay(ms = 200) {
  return new Promise(r => setTimeout(r, ms))
}

export const mockSites = [
  { id: 1, domain: 'myapp.com', path: '/home/fluxo/myapp', php_version: '8.4', repository: 'user/myapp', branch: 'main', last_deployed_at: '2026-06-28T14:22:00Z', app_type: 'laravel', app_port: 0, deployment_strategy: 'zero-downtime', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/public', push_to_deploy: true, deploy_script: '', expose_env: true, db_engine: 'mysql', github_account_id: 1, created_at: '2026-03-15T10:00:00Z', updated_at: '2026-06-28T14:22:00Z' },
  { id: 2, domain: 'blog.com', path: '/home/fluxo/blog', php_version: '8.3', repository: 'user/blog', branch: 'main', last_deployed_at: '2026-06-27T16:10:00Z', app_type: 'php', app_port: 0, deployment_strategy: 'standard', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/', push_to_deploy: false, deploy_script: '', expose_env: false, db_engine: 'postgres', github_account_id: 1, created_at: '2026-04-02T08:30:00Z', updated_at: '2026-06-27T16:10:00Z' },
  { id: 3, domain: 'landing.page', path: '/home/fluxo/landing', php_version: '8.4', repository: '', branch: 'main', last_deployed_at: null, app_type: 'html', app_port: 0, deployment_strategy: 'standard', ssl_provider: '', ssl_active: false, web_root: '/', push_to_deploy: false, deploy_script: '', expose_env: false, db_engine: '', github_account_id: 0, created_at: '2026-05-10T12:00:00Z', updated_at: '2026-06-20T09:45:00Z' },
  { id: 4, domain: 'next-shop.com', path: '/home/fluxo/next-shop', php_version: '8.4', repository: 'user/next-shop', branch: 'main', last_deployed_at: '2026-06-29T11:32:00Z', app_type: 'node', app_port: 3000, deployment_strategy: 'zero-downtime', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/', push_to_deploy: true, deploy_script: '', expose_env: true, db_engine: '', github_account_id: 1, node_preset: 'next', node_mode: 'server', package_manager: 'npm', build_command: 'npm run build', start_command: 'npm run start -- -p $FLUXO_APP_PORT -H 127.0.0.1', static_output_dir: 'out', created_at: '2026-06-12T09:00:00Z', updated_at: '2026-06-29T11:32:00Z' },
  { id: 5, domain: 'pressroom.test', path: '/home/fluxo/pressroom.test', php_version: '8.4', repository: '', branch: '', last_deployed_at: null, app_type: 'wordpress', app_port: 0, deployment_strategy: 'standard', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/public', push_to_deploy: false, deploy_script: '', expose_env: false, db_engine: 'mysql', github_account_id: 0, created_at: '2026-06-30T09:00:00Z', updated_at: '2026-06-30T09:12:00Z' },
]

export const mockDeployments: Record<number, any[]> = {
  1: [
    { id: 12, site_id: 1, status: 'success', commit_hash: 'a1b2c3d', commit_message: 'Fix navbar responsiveness', commit_author: 'Alex Morgan', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-28T14:20:00Z', updated_at: '2026-06-28T14:22:00Z' },
    { id: 11, site_id: 1, status: 'success', commit_hash: 'e4f5g6h', commit_message: 'Add dark mode toggle', commit_author: 'Priya Shah', branch: 'main', trigger_source: 'github_webhook', output: 'Deployment complete.\n', created_at: '2026-06-27T10:00:00Z', updated_at: '2026-06-27T10:03:00Z' },
    { id: 10, site_id: 1, status: 'failed', commit_hash: 'i7j8k9l', commit_message: 'Update application dependencies', commit_author: 'Alex Morgan', branch: 'main', trigger_source: 'rollback', output: 'Error: composer install failed\n', created_at: '2026-06-25T16:00:00Z', updated_at: '2026-06-25T16:02:00Z' },
  ],
  2: [
    { id: 8, site_id: 2, status: 'success', commit_hash: 'm0n1o2p', commit_message: 'Update post layout', commit_author: 'Sam Rivera', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-27T16:08:00Z', updated_at: '2026-06-27T16:10:00Z' },
  ],
  4: [
    { id: 15, site_id: 4, status: 'success', commit_hash: 'n3xt9aa', commit_message: 'Ship checkout loading state', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'github_webhook', output: 'Creating release...\nInstalling dependencies...\nBuilding Next.js application...\nActivating release...\nRestarting Node.js daemon...\nDeployment complete.\n', created_at: '2026-06-29T11:30:00Z', updated_at: '2026-06-29T11:32:00Z' },
    { id: 14, site_id: 4, status: 'success', commit_hash: 'n3xt8zz', commit_message: 'Add product detail metadata', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-25T13:10:00Z', updated_at: '2026-06-25T13:12:00Z' },
  ],
  5: [
    { id: 6, site_id: 5, domain: 'pressroom.test', created_at: '2026-06-30T09:00:00Z' },
  ],
}

export const mockDomains: Record<number, any[]> = {
  1: [
    { id: 1, site_id: 1, domain: 'myapp.com', created_at: '2026-03-15T10:00:00Z' },
    { id: 2, site_id: 1, domain: 'www.myapp.com', created_at: '2026-03-15T10:05:00Z' },
  ],
  2: [
    { id: 3, site_id: 2, domain: 'blog.com', created_at: '2026-04-02T08:30:00Z' },
  ],
  4: [
    { id: 4, site_id: 4, domain: 'next-shop.com', created_at: '2026-06-12T09:00:00Z' },
    { id: 5, site_id: 4, domain: 'www.next-shop.com', created_at: '2026-06-12T09:05:00Z' },
  ],
}

export const mockDatabases = [
  { id: 1, site_id: 1, engine: 'mysql', name: 'myapp', username: 'fluxo', created_at: '2026-03-15T10:00:00Z' },
  { id: 2, site_id: 2, engine: 'postgres', name: 'blog_db', username: 'fluxo', created_at: '2026-04-02T08:30:00Z' },
  { id: 3, site_id: 0, engine: 'mysql', name: 'analytics', username: 'fluxo', created_at: '2026-05-01T00:00:00Z' },
  { id: 4, site_id: 0, engine: 'postgres', name: 'metrics', username: 'fluxo', created_at: '2026-05-01T00:00:00Z' },
  { id: 5, site_id: 5, engine: 'mysql', name: 'pressroom_wp', username: 'fluxo', created_at: '2026-06-30T09:00:00Z' },
]

export const mockDbSizes = [
  { name: 'myapp', size_mb: '12.45', engine: 'mysql' },
  { name: 'blog_db', size_mb: '3.20', engine: 'postgres' },
  { name: 'analytics', size_mb: '48.10', engine: 'mysql' },
  { name: 'metrics', size_mb: '7.80', engine: 'postgres' },
]

export const mockDbUsers = [
  { id: 1, user: 'fluxo', engine: 'mysql' },
  { id: 2, user: 'deploy', engine: 'mysql' },
  { id: 3, user: 'fluxo', engine: 'postgres' },
  { id: 4, user: 'readonly', engine: 'postgres' },
]

export const mockPhpMyAdminStatus = {
  installed: true,
  enabled: true,
  version: '5.2.3',
  php_version: '8.4',
  mysql_available: true,
  access_path: '/phpmyadmin/',
}

export const mockBackupDestinations = [
  { id: 1, name: 'Production R2', provider: 'r2', bucket: 'fluxo-production-backups', region: '', account_id: '0123456789abcdef0123456789abcdef', jurisdiction: 'default', prefix: 'fluxo', server_id: 'demo-server', use_instance_role: false, is_default: true, created_at: '2026-06-20T09:00:00Z', updated_at: '2026-06-20T09:00:00Z' },
  { id: 2, name: 'Archive S3', provider: 's3', bucket: 'company-fluxo-archive', region: 'eu-west-1', account_id: '', jurisdiction: 'default', prefix: 'servers/production', server_id: 'demo-server', use_instance_role: true, is_default: false, created_at: '2026-06-22T11:00:00Z', updated_at: '2026-06-22T11:00:00Z' },
]

export const mockBackupPlans = [
  { id: 1, name: 'myapp.com daily backup', site_id: 1, site_domain: 'myapp.com', destination_id: 1, destination_name: 'Production R2', include_files: true, database_ids: [1], schedule: 'daily', backup_hour: 2, retention_profile: 'recommended', enabled: true, next_run_at: '2026-07-19T02:00:00Z', last_run_at: '2026-07-18T02:04:12Z', created_at: '2026-06-20T09:10:00Z', updated_at: '2026-06-20T09:10:00Z' },
  { id: 2, name: 'blog.com database backup', site_id: 2, site_domain: 'blog.com', destination_id: 2, destination_name: 'Archive S3', include_files: false, database_ids: [2], schedule: 'every_6_hours', backup_hour: 0, retention_profile: 'extended', enabled: true, next_run_at: '2026-07-18T18:00:00Z', last_run_at: '2026-07-18T12:02:31Z', created_at: '2026-06-22T11:15:00Z', updated_at: '2026-06-22T11:15:00Z' },
]

export const mockBackupRuns = [
  { id: 'demo-run-3', plan_id: 1, plan_name: 'myapp.com daily backup', destination_id: 1, destination_name: 'Production R2', site_id: 1, site_domain: 'myapp.com', trigger: 'scheduled', status: 'completed', total_size_bytes: 31771852, error: '', started_at: '2026-07-18T02:00:00Z', completed_at: '2026-07-18T02:04:12Z', created_at: '2026-07-18T02:00:00Z', artifacts: [
    { id: 5, run_id: 'demo-run-3', kind: 'files', database_id: 0, database_name: '', engine: '', filename: 'site-files.tar.gz', size_bytes: 26411490, sha256: 'f25c44d95b3cde99f146eeb2a55b3bb808f9873bd4eb2d6ea891f0bb34be1c92', created_at: '2026-07-18T02:04:12Z' },
    { id: 6, run_id: 'demo-run-3', kind: 'database', database_id: 1, database_name: 'myapp', engine: 'mysql', filename: 'mysql-myapp.sql.gz', size_bytes: 5360362, sha256: '9fd0e022f4c2742d23a3b61f917fd2e2f62abc8099f8e7f734efe330640eaf42', created_at: '2026-07-18T02:04:12Z' },
  ] },
  { id: 'demo-run-2', plan_id: 2, plan_name: 'blog.com database backup', destination_id: 2, destination_name: 'Archive S3', site_id: 2, site_domain: 'blog.com', trigger: 'scheduled', status: 'completed', total_size_bytes: 1839411, error: '', started_at: '2026-07-18T12:00:00Z', completed_at: '2026-07-18T12:02:31Z', created_at: '2026-07-18T12:00:00Z', artifacts: [
    { id: 4, run_id: 'demo-run-2', kind: 'database', database_id: 2, database_name: 'blog_db', engine: 'postgres', filename: 'postgres-blog_db.dump', size_bytes: 1839411, sha256: '64fb80b3ee7b0498c5c9ed63a9b761893af3fbedae4189d757c7ff5c5ee50861', created_at: '2026-07-18T12:02:31Z' },
  ] },
]

export const mockDaemons = [
  { id: 1, site_id: 1, command: 'php8.4 artisan queue:work', user: 'fluxo', directory: '/home/fluxo/myapp', process: 12543, status: 'running', created_at: '2026-03-15T10:10:00Z' },
  { id: 2, site_id: 2, command: 'php8.3 scripts/worker.php', user: 'fluxo', directory: '/home/fluxo/blog', process: 20391, status: 'running', created_at: '2026-04-02T08:35:00Z' },
  { id: 3, site_id: 4, command: 'npm run start -- -p $FLUXO_APP_PORT -H 127.0.0.1', user: 'fluxo', directory: '/home/fluxo/next-shop/current', process: 18842, status: 'running', created_at: '2026-06-12T09:08:00Z' },
]

export const mockCrons = [
  { id: 1, site_id: 1, command: 'php8.4 /home/fluxo/myapp/artisan schedule:run', user: 'fluxo', frequency: '* * * * *', created_at: '2026-03-15T10:15:00Z' },
  { id: 2, site_id: 0, command: 'certbot renew --quiet', user: 'root', frequency: '0 3 * * *', created_at: '2026-01-01T00:00:00Z' },
]

export const mockSshKeys = [
  { id: 1, name: 'My Laptop', public_key: 'ssh-ed25519 AAAAC3...', created_at: '2026-01-10T00:00:00Z' },
  { id: 2, name: 'CI Runner', public_key: 'ssh-ed25519 AAAAC3...', created_at: '2026-02-15T00:00:00Z' },
]

export const mockFirewallRules = [
  { id: 1, name: 'SSH', port: 22, type: 'tcp', ip: '0.0.0.0/0', action: 'allow', status: 'active', created_at: '2026-01-01T00:00:00Z' },
  { id: 2, name: 'HTTP', port: 80, type: 'tcp', ip: '0.0.0.0/0', action: 'allow', status: 'active', created_at: '2026-01-01T00:00:00Z' },
  { id: 3, name: 'HTTPS', port: 443, type: 'tcp', ip: '0.0.0.0/0', action: 'allow', status: 'active', created_at: '2026-01-01T00:00:00Z' },
]

export const mockMetrics = {
  cpu_load: '0.85 0.90 0.65',
  mem_total: 8192,
  mem_used: 3276,
  disk_total: '80G',
  disk_used: '22G',
  disk_usage: '27%',
  daemon_pid: 12543,
  platform: 'linux',
  port: '9595',
  host_address: '192.168.1.100',
  os_version: 'Ubuntu 24.04 LTS',
  os_created: 'Jan 10, 2026',
  hostname: 'fluxo-demo',
}

export const mockEnvVars: Record<number, string> = {
  1: 'APP_NAME=MyApp\nAPP_ENV=production\nAPP_DEBUG=false\nAPP_URL=https://myapp.com\nDB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_DATABASE=myapp\nDB_USERNAME=fluxo\nDB_PASSWORD=********\n',
  2: 'APP_NAME=Blog\nAPP_ENV=production\nDB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=5432\nDB_DATABASE=blog_db\nDB_USERNAME=fluxo\nDB_PASSWORD=********\n',
  4: 'NODE_ENV=production\nNEXT_TELEMETRY_DISABLED=1\nNEXT_PUBLIC_SITE_URL=https://next-shop.com\nSTRIPE_PUBLIC_KEY=pk_live_********\n',
}

export const mockWordPressConfig = `<?php
define( 'DB_NAME', 'pressroom_wp' );
define( 'DB_USER', 'fluxo' );
define( 'DB_PASSWORD', '********' );
define( 'DB_HOST', '127.0.0.1' );
define( 'DB_CHARSET', 'utf8mb4' );
define( 'DB_COLLATE', '' );
define( 'WP_CACHE_KEY_SALT', '********' );

$table_prefix = 'wp_';

if ( isset( $_SERVER['HTTP_X_FORWARDED_PROTO'] ) && strpos( $_SERVER['HTTP_X_FORWARDED_PROTO'], 'https' ) !== false ) {
  $_SERVER['HTTPS'] = 'on';
}

define( 'WP_DEBUG', false );

if ( ! defined( 'ABSPATH' ) ) {
  define( 'ABSPATH', __DIR__ . '/' );
}
require_once ABSPATH . 'wp-settings.php';
`

export const mockActivity = [
  { id: 15, site_id: 4, type: 'deployment', summary: 'Deployment #15 success', created_at: '2026-06-29T11:32:00Z' },
  { id: 12, site_id: 1, type: 'deployment', summary: 'Deployment #12 success', created_at: '2026-06-28T14:22:00Z' },
  { id: 11, site_id: 1, type: 'deployment', summary: 'Deployment #11 success', created_at: '2026-06-27T10:03:00Z' },
  { id: 10, site_id: 0, type: 'system', summary: 'SSL certificate renewed for myapp.com', created_at: '2026-06-26T03:00:00Z' },
  { id: 9, site_id: 1, type: 'deployment', summary: 'Deployment #10 failed', created_at: '2026-06-25T16:02:00Z' },
  { id: 8, site_id: 2, type: 'deployment', summary: 'Deployment #8 success', created_at: '2026-06-27T16:10:00Z' },
  { id: 7, site_id: 0, type: 'system', summary: 'Database backup completed for analytics', created_at: '2026-06-25T00:00:00Z' },
]

export const mockPhpVersions = ['8.3', '8.4']

export const mockSettings = {
  default_php: '8.4',
  admin_email: 'admin@example.com',
  github_pat_set: true,
}

export const mockGithubRepos = [
  { full_name: 'user/myapp' },
  { full_name: 'user/blog' },
  { full_name: 'user/next-shop' },
  { full_name: 'user/api-service' },
]

export const mockGithubBranches = [
  { name: 'main' },
  { name: 'develop' },
  { name: 'staging' },
]

export const mockGithubAccounts = [
  { id: 1, name: 'myuser', username: 'myuser' },
]

export const mockServerLogs: Record<string, string> = {
  'nginx': '2026/06/28 14:20:01 [info] 1234#1234: *567 client closed connection\n2026/06/28 14:19:55 [info] 1234#1234: *568 GET /index.html HTTP/1.1 200\n2026/06/28 14:19:50 [error] 1234#1234: *569 connect() failed (111: Connection refused)',
  'php': '[28-Jun-2026 14:20:01] NOTICE: fpm is running, pid 1234\n[28-Jun-2026 14:19:55] WARNING: pool www seems busy\n[28-Jun-2026 14:18:00] NOTICE: reload completed',
  'mysql': '2026-06-28T14:20:01.123456Z 0 [Note] InnoDB: Log buffer is up to date\n2026-06-28T14:15:00.000000Z 0 [Warning] Aborted connection 123',
  'postgres': '2026-06-28 14:20:01 UTC [1234] LOG: checkpoint starting\n2026-06-28 14:19:00 UTC [1235] LOG: connection received',
  'redis': '1234:M 28 Jun 2026 14:20:01.123 * DB loaded from disk\n1234:M 28 Jun 2026 14:19:00.456 * Running mode=standalone',
}

export const mockGrants: Record<string, string[]> = {
  'mysql:fluxo': ['myapp', 'analytics'],
  'mysql:deploy': [],
  'postgres:fluxo': ['blog_db'],
  'postgres:readonly': [],
}

export class MockApiClient {
  private toastCallback: ((msg: string, type: string) => void) | null = null

  setToastCallback(cb: (msg: string, type: string) => void) {
    this.toastCallback = cb
  }

  private toast(msg: string, type = 'info') {
    this.toastCallback?.(msg, type)
  }

  async get(url: string) { await delay(100); return this._handle(url, 'GET') }
  async post(url: string, body?: any) { await delay(150); return this._handle(url, 'POST', body) }
  async put(url: string, body?: any) { await delay(150); return this._handle(url, 'PUT', body) }
  async delete(url: string) { await delay(150); return this._handle(url, 'DELETE') }

  private _handle(url: string, method: string, body?: any): any {
    const isDemo = (msg: string) => this.toast(`[Demo] ${msg} — not persisted`, 'info')

    // Parse URL path and query parameters
    let pathname = url;
    let searchParams = new URLSearchParams();
    try {
      const urlObj = new URL(url, 'http://localhost');
      pathname = urlObj.pathname;
      searchParams = urlObj.searchParams;
    } catch (e) {}

    if (pathname.startsWith('/api/v1/sites')) {
      const idMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)$/)
      if (idMatch) {
        const id = parseInt(idMatch[1]);
        if (method === 'GET') {
          return mockSites.find(s => s.id === id) || null;
        } else if (method === 'PUT') {
          isDemo('Update site settings');
          return { ...body, id };
        } else if (method === 'DELETE') {
          isDemo('Delete site');
          return null;
        }
      }

      if (pathname.endsWith('/deployments')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        const deployments = mockDeployments[id] || []
        return { data: deployments, current_page: 1, total: deployments.length, per_page: 12 }
      }
      if (pathname.endsWith('/features')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        const site = mockSites.find(s => s.id === id)
        const laravelDetected = site?.app_type === 'laravel'
        return {
          composer_lock_found: laravelDetected,
          laravel_detected: laravelDetected,
          laravel_version: laravelDetected ? 'v12.0.0' : '',
          scheduler_enabled: id === 1,
          scheduler_available: laravelDetected,
          nightwatch_enabled: false,
          nightwatch_installed: laravelDetected,
          nightwatch_available: laravelDetected,
          octane_enabled: false,
          octane_installed: laravelDetected,
          octane_available: laravelDetected && site?.deployment_strategy !== 'zero-downtime',
          maintenance_available: laravelDetected,
          in_maintenance: false,
          deployment_strategy: site?.deployment_strategy || 'standard',
        }
      }
      if (pathname.endsWith('/daemons')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        if (method === 'GET') {
          return mockDaemons.filter(d => d.site_id === id)
        } else if (method === 'POST') {
          isDemo('Create site daemon');
          return { id: 99, site_id: id, ...body, status: 'running' };
        }
      }
      if (pathname.endsWith('/crons')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        if (method === 'GET') {
          return mockCrons.filter(c => c.site_id === id)
        } else if (method === 'POST') {
          isDemo('Create site cron');
          return { id: 99, site_id: id, ...body };
        }
      }
      if (pathname.endsWith('/certificates')) {
        return []
      }
      if (pathname.endsWith('/logs/list')) {
        return [
          { id: 'nginx_access', label: 'Nginx Access Log', path: '/var/log/nginx/access.log', exists: true },
          { id: 'nginx_error', label: 'Nginx Error Log', path: '/var/log/nginx/error.log', exists: true },
          { id: 'app_log', label: 'Application Log', path: '/home/fluxo/app.log', exists: true }
        ]
      }
      if (pathname.endsWith('/deploy')) {
        isDemo('Trigger deployment')
        return null
      }
      if (pathname.endsWith('/env')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        if (method === 'GET') {
          return mockEnvVars[id] || ''
        } else if (method === 'POST') {
          isDemo('Update .env')
          return null
        }
      }
      if (pathname.endsWith('/wordpress-config')) {
        if (method === 'GET') {
          return { content: mockWordPressConfig }
        } else if (method === 'POST') {
          isDemo('Update WordPress configuration')
          return { success: true }
        }
      }
      if (pathname.endsWith('/commands')) {
        if (method === 'GET') {
          return []
        } else if (method === 'POST') {
          isDemo('Run site command');
          return { id: 99, output: 'Demo command execution completed.\n' };
        }
      }
      if (pathname.endsWith('/domains')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        if (method === 'GET') {
          return mockDomains[id] || []
        } else if (method === 'POST') {
          isDemo('Add domain alias');
          return { id: 99, site_id: id, ...body };
        }
      }

      if (pathname.includes('/daemons/') && method === 'POST') {
        isDemo('Daemon action triggered');
        return null;
      }
      if (pathname.includes('/daemons/') && method === 'DELETE') {
        isDemo('Delete daemon');
        return null;
      }
      if (pathname.includes('/crons/') && method === 'POST') {
        isDemo('Cron run triggered');
        return null;
      }
      if (pathname.includes('/crons/') && method === 'DELETE') {
        isDemo('Delete cron');
        return null;
      }
      if (pathname.includes('/domains/') && method === 'DELETE') {
        isDemo('Delete domain alias');
        return null;
      }
      if (pathname.includes('/ssl/certificates/') && method === 'POST') {
        isDemo('SSL certificate action');
        return null;
      }
      if (pathname.includes('/ssl/certificates/') && method === 'DELETE') {
        isDemo('Delete SSL certificate');
        return null;
      }

      if (pathname === '/api/v1/sites') {
        if (method === 'GET') {
          return mockSites
        } else if (method === 'POST') {
          isDemo('Create site')
          return { id: 99, domain: body?.domain || 'demo.app' }
        }
      }
    }

    if (pathname.startsWith('/api/v1/databases')) {
      if (method === 'GET') {
        if (pathname.endsWith('/sizes')) return mockDbSizes
        if (pathname.endsWith('/users/grants')) {
          const engine = searchParams.get('engine') || 'mysql'
          const user = searchParams.get('user') || ''
          return mockGrants[`${engine}:${user}`] || []
        }
        if (pathname.endsWith('/users')) return mockDbUsers
        return mockDatabases
      } else if (method === 'POST') {
        if (pathname.endsWith('/users')) {
          isDemo('Create database user')
          return { user: body?.user || 'demo', password: '********', databases: body?.databases || [], engine: body?.engine || 'mysql' }
        }
        if (pathname.endsWith('/grants')) {
          isDemo('Update database user grants')
          return null
        }
        isDemo('Create database')
        return { name: body?.name || 'demo', engine: body?.engine || 'mysql' }
      } else if (method === 'DELETE') {
        isDemo('Delete database/user')
        return null
      }
    }

    if (pathname.startsWith('/api/v1/tools/phpmyadmin')) {
      if (method === 'GET') {
        return mockPhpMyAdminStatus
      }
      isDemo(pathname.endsWith('/access') ? 'Open phpMyAdmin' : 'Manage phpMyAdmin')
      if (pathname.endsWith('/access')) return { url: '' }
      return mockPhpMyAdminStatus
    }

    if (pathname.startsWith('/api/v1/backups')) {
      if (method === 'GET') {
        if (pathname.endsWith('/destinations')) return mockBackupDestinations
        if (pathname.endsWith('/plans')) return { plans: mockBackupPlans, timezone: 'Africa/Lagos' }
        if (pathname.endsWith('/runs')) return mockBackupRuns
      }
      isDemo('Manage backups')
      return null
    }

    if (pathname.startsWith('/api/v1/daemons')) {
      if (method === 'GET') return mockDaemons
      isDemo('Manage daemon')
      return null
    }

    if (pathname.startsWith('/api/v1/crons')) {
      if (method === 'GET') return mockCrons
      isDemo('Manage cron')
      return null
    }

    if (pathname.startsWith('/api/v1/ssh-keys')) {
      if (method === 'GET') return mockSshKeys
      if (method === 'POST') {
        isDemo('Add SSH key')
        return { id: 99, ...body }
      }
      if (method === 'DELETE') {
        isDemo('Delete SSH key')
        return null
      }
    }

    if (pathname.startsWith('/api/v1/firewall')) {
      if (method === 'GET') return mockFirewallRules
      if (method === 'POST') {
        isDemo('Add firewall rule')
        return { id: 99, ...body }
      }
      if (method === 'DELETE') {
        isDemo('Delete firewall rule')
        return null
      }
    }

    if (pathname.startsWith('/api/v1/system')) {
      if (pathname.endsWith('/metrics')) return mockMetrics
      if (pathname.endsWith('/activity')) {
        const siteIdStr = searchParams.get('site_id')
        if (siteIdStr) {
          const siteId = parseInt(siteIdStr)
          const filtered = mockActivity.filter(a => a.site_id === siteId)
          return { items: filtered, total: filtered.length }
        }
        return { items: mockActivity, total: mockActivity.length }
      }
      if (pathname.endsWith('/logs/list')) {
        return [
          { id: 'nginx_access', label: 'Nginx Access Log', path: '/var/log/nginx/access.log', exists: true },
          { id: 'nginx_error', label: 'Nginx Error Log', path: '/var/log/nginx/error.log', exists: true },
          { id: 'php_fpm', label: 'PHP-FPM Log', path: '/var/log/php8.4-fpm.log', exists: true },
          { id: 'mysql', label: 'MySQL/MariaDB Log', path: '/var/log/mysql/error.log', exists: true }
        ]
      }
      if (pathname.endsWith('/logs/clear')) {
        isDemo('Clear logs')
        return null
      }
      if (pathname.endsWith('/logs/download')) {
        isDemo('Download logs')
        return 'Demo logs content'
      }
      if (pathname.endsWith('/logs')) {
        const pathParam = searchParams.get('path') || ''
        if (pathParam.includes('nginx')) return mockServerLogs['nginx']
        if (pathParam.includes('php')) return mockServerLogs['php']
        if (pathParam.includes('mysql')) return mockServerLogs['mysql']
        return 'No logs found in this location.'
      }
      return {}
    }

    if (pathname.startsWith('/api/v1/server')) {
      if (pathname.endsWith('/php')) return mockPhpVersions
      if (pathname.endsWith('/php/cli-default')) return { version: '8.4' }
      if (pathname.endsWith('/php/versions/available')) {
        return [
          { version: '8.4', installed: true, status: 'running' },
          { version: '8.3', installed: false, status: 'not_installed' },
          { version: '8.2', installed: false, status: 'not_installed' },
          { version: '8.1', installed: false, status: 'not_installed' },
          { version: '8.0', installed: false, status: 'not_installed' },
          { version: '7.4', installed: false, status: 'not_installed' }
        ]
      }
      if (pathname.includes('/php/settings')) {
        return {
          upload_max_filesize: '50M',
          max_execution_time: '30',
          memory_limit: '128M',
          post_max_size: '50M',
          max_input_time: '60',
          opcache_enable: '1'
        }
      }
      if (pathname.endsWith('/engines')) return ['mysql', 'postgres', 'redis']
      if (pathname.endsWith('/mysql/info')) return { version: '10.11.6-MariaDB', status: 'running' }
      if (pathname.endsWith('/postgres/info')) return { version: '16.1', status: 'stopped' }
      if (pathname.endsWith('/redis/info')) return { version: '7.2.4', status: 'running' }
      if (pathname.endsWith('/nginx/info')) {
        return {
          binary: '/usr/sbin/nginx',
          version: 'nginx/1.24.0',
          config_dir: '/etc/nginx',
          sites_available: '/etc/nginx/sites-available',
          sites_enabled: '/etc/nginx/sites-enabled'
        }
      }
      if (pathname.endsWith('/node/info')) {
        return {
          binary: '/usr/bin/node',
          version: 'v20.11.1',
          npm: '10.2.4'
        }
      }
      if (pathname.endsWith('/logs')) {
        const type = searchParams.get('type') || 'nginx'
        return mockServerLogs[type] || ''
      }
      return {}
    }

    if (pathname.startsWith('/api/v1/github')) {
      if (pathname.endsWith('/repos')) return mockGithubRepos
      if (pathname.endsWith('/branches')) return mockGithubBranches
      if (pathname.endsWith('/accounts')) return mockGithubAccounts
      return {}
    }

    if (pathname.startsWith('/api/v1/settings')) {
      if (pathname.endsWith('/bootstrap-credentials')) return null
      if (pathname.endsWith('/bootstrap-credentials/copied')) return null
      if (method === 'GET') return mockSettings
      isDemo('Update settings')
      return null
    }

    if (pathname.startsWith('/api/v1/auth')) {
      if (pathname.endsWith('/login')) return { token: 'demo_token_123' }
      if (pathname.endsWith('/bootstrap')) return { bootstrapped: true }
      isDemo('Auth action')
      return null
    }

    if (pathname.endsWith('/api/v1/version')) {
      return { version: `${appVersion}-demo` }
    }

    if (method !== 'GET') isDemo(`${method} ${pathname}`)
    return null
  }
}

export const mockApi = new MockApiClient()
