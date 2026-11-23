import { protectionPasswordError, protectionUsernameError, redirectDestinationError, redirectSourceError } from '@fluxo/types/siteNetwork';
import { certificateHostnames } from '@fluxo/types/domain';
import { demoGitHubSiteAccess } from './githubAccessDemo';
import { version as appVersion } from '../../package.json'
import { createRequestID } from '@fluxo/utils/requestID'
import type { GitHubAccount } from '@fluxo/utils/githubConnection'
import { trafficAPISettings } from '@fluxo/utils/trafficAnalytics'
import type { TrafficAPIBucket, TrafficAPIMetrics, TrafficAPIReport, TrafficAPISettings, TrafficBucket, TrafficGap, TrafficMetrics, TrafficRange, TrafficReport, TrafficRetentionDays, TrafficSiteStatus, TrafficState } from '@fluxo/types/trafficAnalytics'

function delay(ms = 200) {
  return new Promise(r => setTimeout(r, ms))
}

export const mockSites = [
  { id: 1, domain: 'myapp.com', path: '/home/fluxo/myapp', php_version: '8.4', repository: 'user/myapp', branch: 'main', last_deployed_at: '2026-06-28T14:22:00Z', app_type: 'laravel', app_port: 0, deployment_strategy: 'zero-downtime', deploy_script_mode: 'managed', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/public', push_to_deploy: true, deploy_script: '$FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader\n$FLUXO_PHP artisan migrate --force\nnpm ci\nnpm run build', expose_env: true, db_engine: 'mysql', github_account_id: 1, created_at: '2026-03-15T10:00:00Z', updated_at: '2026-06-28T14:22:00Z' },
  { id: 2, domain: 'blog.com', path: '/home/fluxo/blog', php_version: '8.3', repository: 'user/blog', branch: 'main', last_deployed_at: '2026-06-27T16:10:00Z', app_type: 'php', app_port: 0, deployment_strategy: 'standard', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/', push_to_deploy: false, deploy_script: '', expose_env: false, db_engine: 'postgres', github_account_id: 1, created_at: '2026-04-02T08:30:00Z', updated_at: '2026-06-27T16:10:00Z' },
  { id: 3, domain: 'landing.page', path: '/home/fluxo/landing', php_version: '', repository: '', branch: 'main', last_deployed_at: null, app_type: 'html', app_port: 0, deployment_strategy: 'standard', ssl_provider: '', ssl_active: false, web_root: '/', push_to_deploy: false, deploy_script: '', expose_env: false, db_engine: '', github_account_id: 0, created_at: '2026-05-10T12:00:00Z', updated_at: '2026-06-20T09:45:00Z' },
  { id: 4, domain: 'next-shop.com', path: '/home/fluxo/next-shop', php_version: '', repository: 'user/next-shop', branch: 'main', last_deployed_at: '2026-06-29T11:32:00Z', app_type: 'node', app_port: 3000, deployment_strategy: 'zero-downtime', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/', push_to_deploy: true, deploy_script: '', expose_env: true, db_engine: '', github_account_id: 1, node_preset: 'next', node_mode: 'server', package_manager: 'npm', build_command: 'npm run build', start_command: 'npm run start -- -p $FLUXO_APP_PORT -H 127.0.0.1', static_output_dir: 'out', created_at: '2026-06-12T09:00:00Z', updated_at: '2026-06-29T11:32:00Z' },
  { id: 5, domain: 'pressroom.test', path: '/home/fluxo/pressroom.test', php_version: '8.4', repository: '', branch: '', last_deployed_at: null, app_type: 'wordpress', app_port: 0, deployment_strategy: 'standard', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/public', push_to_deploy: false, deploy_script: '', expose_env: false, db_engine: 'mysql', github_account_id: 0, created_at: '2026-06-30T09:00:00Z', updated_at: '2026-06-30T09:12:00Z' },
  { id: 6, domain: 'api.studio.test', path: '/home/fluxo/api.studio.test', php_version: '', repository: 'user/api-service', branch: 'main', last_deployed_at: '2026-06-30T13:44:00Z', app_type: 'python', app_port: 8000, deployment_strategy: 'zero-downtime', ssl_provider: 'letsencrypt', ssl_active: true, web_root: '/', push_to_deploy: true, deploy_script_mode: 'managed', deploy_script: '', expose_env: true, db_engine: 'postgres', github_account_id: 1, python_preset: 'fastapi', python_entrypoint: 'main:app', app_directory: '.', package_manager: 'uv', build_command: '', start_command: '.venv/bin/uvicorn main:app --host 127.0.0.1 --port $FLUXO_APP_PORT', created_at: '2026-06-21T11:00:00Z', updated_at: '2026-06-30T13:44:00Z' },
]

const renderMockVhost = (site: any) => {
  const root = `${site.path}${site.deployment_strategy === 'zero-downtime' ? '/current' : ''}${site.web_root === '/' ? '' : site.web_root}`
  const application = (site.app_type === 'node' && site.node_mode === 'server') || site.app_type === 'python'
    ? `    location / {
        proxy_pass http://127.0.0.1:${site.app_port};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }`
    : site.app_type === 'html' || (site.app_type === 'node' && site.node_mode === 'static')
      ? `    location / {
        try_files $uri $uri/ =404;
    }`
      : `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \\.php$ {
        fastcgi_pass unix:/var/run/php/php${site.php_version || '8.4'}-fpm-${site.domain}.sock;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
    }`
  const tls = site.ssl_active
    ? `
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    ssl_certificate /etc/letsencrypt/live/${site.domain}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${site.domain}/privkey.pem;`
    : ''
  return `# Managed by Fluxo. Saving an edit creates a durable custom vhost.
server {
    listen 80;
    listen [::]:80;${tls}
    server_name ${site.domain};
    root ${root};
    server_tokens off;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root ${root};
    }

${application}
}
`
}

const mockVhostDefaults: Record<number, string> = Object.fromEntries(mockSites.map(site => [site.id, renderMockVhost(site)]))
const mockVhostStates: Record<number, any> = Object.fromEntries(mockSites.map(site => [site.id, {
  config: mockVhostDefaults[site.id],
  customized: false,
  revision: `demo-managed-${site.id}`,
  path: `/etc/nginx/sites-available/${site.path.split('/').filter(Boolean).pop()}`,
}]))
let mockVhostRevision = 1

export const mockDeployments: Record<number, any[]> = {
  1: [
    { id: 12, site_id: 1, status: 'success', commit_hash: 'a1b2c3d', commit_message: 'Fix navbar responsiveness', commit_author: 'Alex Morgan', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-28T14:20:00Z', started_at: '2026-06-28T14:20:18Z', finished_at: '2026-06-28T14:21:42Z', updated_at: '2026-06-28T14:22:00Z' },
    { id: 11, site_id: 1, status: 'success', commit_hash: 'e4f5g6h', commit_message: 'Add dark mode toggle', commit_author: 'Priya Shah', branch: 'main', trigger_source: 'github_webhook', output: 'Deployment complete.\n', created_at: '2026-06-27T10:00:00Z', updated_at: '2026-06-27T10:03:00Z' },
    { id: 10, site_id: 1, status: 'failed', commit_hash: 'i7j8k9l', commit_message: 'Update application dependencies', commit_author: 'Alex Morgan', branch: 'main', trigger_source: 'rollback', output: 'Error: composer install failed\n', created_at: '2026-06-25T16:00:00Z', updated_at: '2026-06-25T16:02:00Z' },
  ],
  2: [
    { id: 8, site_id: 2, status: 'success', commit_hash: 'm0n1o2p', commit_message: 'Update post layout', commit_author: 'Sam Rivera', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-27T16:08:00Z', updated_at: '2026-06-27T16:10:00Z' },
  ],
  4: [
    { id: 16, site_id: 4, status: 'failed', commit_hash: 'n3xtfail', commit_message: 'Upgrade checkout dependencies', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'manual', failure_reason: 'deployment failed: exit status 1', failure_dismissed_at: null, output: 'Creating release...\nInstalling dependencies...\nnpm error ERESOLVE unable to resolve dependency tree\n\nDeployment failed: exit status 1\n', created_at: '2026-06-30T10:05:00Z', started_at: '2026-06-30T10:05:12Z', finished_at: '2026-06-30T10:05:44Z', updated_at: '2026-06-30T10:06:00Z' },
    { id: 15, site_id: 4, status: 'success', commit_hash: 'n3xt9aa', commit_message: 'Ship checkout loading state', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'github_webhook', output: 'Creating release...\nInstalling dependencies...\nBuilding Next.js application...\nActivating release...\nRestarting Node.js daemon...\nDeployment complete.\n', created_at: '2026-06-29T11:30:00Z', started_at: '2026-06-29T11:30:16Z', finished_at: '2026-06-29T11:31:12Z', updated_at: '2026-06-29T11:32:00Z' },
    { id: 14, site_id: 4, status: 'success', commit_hash: 'n3xt8zz', commit_message: 'Add product detail metadata', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-25T13:10:00Z', updated_at: '2026-06-25T13:12:00Z' },
  ],
  5: [
    { id: 6, site_id: 5, domain: 'pressroom.test', created_at: '2026-06-30T09:00:00Z' },
  ],
  6: [
    { id: 18, site_id: 6, status: 'success', commit_hash: 'py9a2bc', commit_message: 'Add account search endpoint', commit_author: 'Maya Chen', branch: 'main', trigger_source: 'github_webhook', output: 'Creating release...\nCreating Python virtual environment...\nInstalling dependencies with uv...\nActivating release...\nRestarting Python service...\nDeployment complete.\n', created_at: '2026-06-30T13:42:00Z', updated_at: '2026-06-30T13:44:00Z' },
    { id: 17, site_id: 6, status: 'success', commit_hash: 'py71d0a', commit_message: 'Configure health checks', commit_author: 'Maya Chen', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-28T09:15:00Z', updated_at: '2026-06-28T09:17:00Z' },
  ],
}

export const mockDomains: Record<number, any[]> = {
  1: [
    { id: 0, site_id: 1, domain: 'myapp.com', primary: true, ssl_active: true, www_redirect: 'from_www', created_at: '2026-03-15T10:00:00Z' },
  ],
  2: [
    { id: 0, site_id: 2, domain: 'blog.com', primary: true, ssl_active: true, www_redirect: 'none', created_at: '2026-04-02T08:30:00Z' },
  ],
  4: [
    { id: 0, site_id: 4, domain: 'next-shop.com', primary: true, ssl_active: true, www_redirect: 'from_www', created_at: '2026-06-12T09:00:00Z' },
  ],
  6: [
    { id: 0, site_id: 6, domain: 'api.studio.test', primary: true, ssl_active: true, www_redirect: 'none', created_at: '2026-06-21T11:00:00Z' },
  ],
}

for (const site of mockSites) if (!mockDomains[site.id]) mockDomains[site.id] = [{ id: 0, site_id: site.id, domain: site.domain, primary: true, ssl_active: site.ssl_active, www_redirect: 'none' }]

export const mockCertificates: Record<number, any[]> = {
  1: [
    { id: 101, site_id: 1, domain: 'myapp.com', provider: 'letsencrypt', cert_path: '', key_path: '', expires_at: '2026-09-26T03:00:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-28T14:25:00Z' },
    { id: 106, site_id: 1, domain: 'myapp.com', provider: 'custom', domains: ['myapp.com', '*.myapp.com'], cert_path: '', key_path: '', expires_at: '2036-06-28T14:24:00Z', active: false, source_certificate_id: 0, created_at: '2026-06-28T14:24:00Z' },
  ],
  2: [
    { id: 102, site_id: 2, domain: 'blog.com', provider: 'custom', domains: ['blog.com', '*.blog.com'], cert_path: '', key_path: '', expires_at: '2036-06-27T16:15:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-27T16:15:00Z' },
  ],
  4: [
    { id: 104, site_id: 4, domain: 'next-shop.com', provider: 'letsencrypt', cert_path: '', key_path: '', expires_at: '2026-09-27T11:35:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-29T11:35:00Z' },
  ],
  5: [
    { id: 105, site_id: 5, domain: 'pressroom.test', provider: 'letsencrypt', cert_path: '', key_path: '', expires_at: '2026-09-28T09:12:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-30T09:12:00Z' },
  ],
  6: [
    { id: 107, site_id: 6, domain: 'api.studio.test', provider: 'letsencrypt', cert_path: '', key_path: '', expires_at: '2026-09-29T13:45:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-30T13:45:00Z' },
  ],
}

export const mockDatabases = [
  { id: 1, site_id: 1, engine: 'mysql', name: 'myapp', username: 'fluxo', created_at: '2026-03-15T10:00:00Z' },
  { id: 2, site_id: 2, engine: 'postgres', name: 'blog_db', username: 'fluxo', created_at: '2026-04-02T08:30:00Z' },
  { id: 3, site_id: 0, engine: 'mysql', name: 'analytics', username: 'fluxo', created_at: '2026-05-01T00:00:00Z' },
  { id: 4, site_id: 0, engine: 'postgres', name: 'metrics', username: 'fluxo', created_at: '2026-05-01T00:00:00Z' },
  { id: 5, site_id: 5, engine: 'mysql', name: 'pressroom_wp', username: 'fluxo', created_at: '2026-06-30T09:00:00Z' },
  { id: 6, site_id: 6, engine: 'postgres', name: 'studio_api', username: 'studio_api', created_at: '2026-06-21T11:00:00Z' },
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
  { id: 1, name: 'Production R2', provider: 'r2', bucket: 'fluxo-production-backups', region: '', account_id: '0123456789abcdef0123456789abcdef', jurisdiction: 'default', prefix: 'fluxo-backups', server_id: 'demo-server', use_instance_role: false, is_default: true, created_at: '2026-06-20T09:00:00Z', updated_at: '2026-06-20T09:00:00Z' },
  { id: 2, name: 'Archive S3', provider: 's3', bucket: 'company-fluxo-archive', region: 'eu-west-1', account_id: '', jurisdiction: 'default', prefix: 'servers/production', server_id: 'demo-server', use_instance_role: true, is_default: false, created_at: '2026-06-22T11:00:00Z', updated_at: '2026-06-22T11:00:00Z' },
]

export const mockBackupPlans = [
  { id: 1, name: 'myapp.com daily backup', site_id: 1, site_domain: 'myapp.com', destination_id: 1, destination_name: 'Production R2', include_files: true, database_ids: [1], schedule: 'daily', backup_hour: 2, retention_profile: 'recommended', enabled: true, encryption_enabled: true, next_run_at: '2026-07-19T02:00:00Z', last_run_at: '2026-07-18T02:04:12Z', created_at: '2026-06-20T09:10:00Z', updated_at: '2026-06-20T09:10:00Z' },
  { id: 2, name: 'blog.com database backup', site_id: 2, site_domain: 'blog.com', destination_id: 2, destination_name: 'Archive S3', include_files: false, database_ids: [2], schedule: 'every_6_hours', backup_hour: 0, retention_profile: 'extended', enabled: true, encryption_enabled: false, next_run_at: '2026-07-18T18:00:00Z', last_run_at: '2026-07-18T12:02:31Z', created_at: '2026-06-22T11:15:00Z', updated_at: '2026-06-22T11:15:00Z' },
]

export const mockBackupRuns = [
  { id: 'demo-run-3', plan_id: 1, plan_name: 'myapp.com daily backup', destination_id: 1, destination_name: 'Production R2', site_id: 1, site_domain: 'myapp.com', trigger: 'scheduled', status: 'completed', encrypted: true, total_size_bytes: 31771852, error: '', started_at: '2026-07-18T02:00:00Z', completed_at: '2026-07-18T02:04:12Z', created_at: '2026-07-18T02:00:00Z', artifacts: [
    { id: 5, run_id: 'demo-run-3', kind: 'files', database_id: 0, database_name: '', engine: '', filename: 'site-files.tar.gz.gpg', size_bytes: 26411490, sha256: 'f25c44d95b3cde99f146eeb2a55b3bb808f9873bd4eb2d6ea891f0bb34be1c92', created_at: '2026-07-18T02:04:12Z' },
    { id: 6, run_id: 'demo-run-3', kind: 'database', database_id: 1, database_name: 'myapp', engine: 'mysql', filename: 'mysql-myapp.sql.gz.gpg', size_bytes: 5360362, sha256: '9fd0e022f4c2742d23a3b61f917fd2e2f62abc8099f8e7f734efe330640eaf42', created_at: '2026-07-18T02:04:12Z' },
  ] },
  { id: 'demo-run-2', plan_id: 2, plan_name: 'blog.com database backup', destination_id: 2, destination_name: 'Archive S3', site_id: 2, site_domain: 'blog.com', trigger: 'scheduled', status: 'completed', encrypted: false, total_size_bytes: 1839411, error: '', started_at: '2026-07-18T12:00:00Z', completed_at: '2026-07-18T12:02:31Z', created_at: '2026-07-18T12:00:00Z', artifacts: [
    { id: 4, run_id: 'demo-run-2', kind: 'database', database_id: 2, database_name: 'blog_db', engine: 'postgres', filename: 'postgres-blog_db.dump', size_bytes: 1839411, sha256: '64fb80b3ee7b0498c5c9ed63a9b761893af3fbedae4189d757c7ff5c5ee50861', created_at: '2026-07-18T12:02:31Z' },
  ] },
]

export const mockDaemons = [
  { id: 1, site_id: 1, command: 'php8.4 artisan queue:work', user: 'fluxo', directory: '/home/fluxo/myapp', process: 12543, status: 'running', created_at: '2026-03-15T10:10:00Z' },
  { id: 2, site_id: 2, command: 'php8.3 scripts/worker.php', user: 'fluxo', directory: '/home/fluxo/blog', process: 20391, status: 'running', created_at: '2026-04-02T08:35:00Z' },
  { id: 3, site_id: 4, name: 'Node.js', managed_kind: 'node_app', command: 'npm run start -- -p 3000 -H 127.0.0.1', user: 'fluxo', directory: '/home/fluxo/next-shop/current', instances: 1, process: 18842, status: 'running', created_at: '2026-06-12T09:08:00Z' },
  { id: 4, site_id: 6, name: 'Python', managed_kind: 'python_app', command: '/usr/bin/env PYTHONUNBUFFERED=1 PORT=8000 HOST=127.0.0.1 .venv/bin/uvicorn main:app --host 127.0.0.1 --port 8000', user: 'fluxo', directory: '/home/fluxo/api.studio.test/current', instances: 1, process: 21407, status: 'running', created_at: '2026-06-21T11:08:00Z' },
]

export const mockCrons = [
  { id: 1, site_id: 1, command: 'php8.4 /home/fluxo/myapp/artisan schedule:run', user: 'fluxo', frequency: '* * * * *', created_at: '2026-03-15T10:15:00Z' },
  { id: 2, site_id: 0, command: 'certbot renew --quiet', user: 'root', frequency: '0 */12 * * *', created_at: '2026-01-01T00:00:00Z' },
]

export const mockSshKeys = [
  { id: 1, name: 'My Laptop', public_key: 'ssh-ed25519 AAAAC3...', created_at: '2026-01-10T00:00:00Z' },
  { id: 2, name: 'CI Runner', public_key: 'ssh-ed25519 AAAAC3...', created_at: '2026-02-15T00:00:00Z' },
]

export const mockSshSecurity = {
  available: true,
  password_authentication: 'yes',
  keyboard_interactive_authentication: 'no',
  public_key_authentication: 'yes',
  permit_root_login: 'prohibit-password',
  password_login_enabled: true,
  hardened: false,
  managed: false,
  authorized_key_count: 2,
  authorized_keys_valid: true,
  can_harden: true,
}

export const mockFirewallRules = [
  { id: 1, name: 'SSH', port: '22/tcp', type: 'allow', from_ip: 'Any', managed_by: 'installer', active: true, created_at: '2026-01-01T00:00:00Z' },
  { id: 2, name: 'HTTP', port: '80/tcp', type: 'allow', from_ip: 'Any', managed_by: 'installer', active: true, created_at: '2026-01-01T00:00:00Z' },
  { id: 3, name: 'HTTPS', port: '443/tcp', type: 'allow', from_ip: 'Any', managed_by: 'installer', active: true, created_at: '2026-01-01T00:00:00Z' },
  { id: 4, name: 'Fluxo Dashboard', port: '9595/tcp', type: 'allow', from_ip: 'Any', managed_by: 'installer', active: true, created_at: '2026-01-01T00:00:00Z' },
  { id: -5, name: 'External UFW rule', port: '9100/tcp', type: 'allow', from_ip: '10.0.0.0/8', managed_by: 'external', active: true, raw_command: 'ufw allow from 10.0.0.0/8 to any port 9100 proto tcp', created_at: '0001-01-01T00:00:00Z' },
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
  1: "APP_NAME=MyApp\nAPP_ENV=production\nAPP_DEBUG=false\nAPP_URL=https://myapp.com\nDB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_DATABASE=myapp\nDB_USERNAME=fluxo\nDB_PASSWORD='********'\n",
  2: "APP_NAME=Blog\nAPP_ENV=production\nDB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=5432\nDB_DATABASE=blog_db\nDB_USERNAME=fluxo\nDB_PASSWORD='********'\n",
  4: 'NODE_ENV=production\nNEXT_TELEMETRY_DISABLED=1\nNEXT_PUBLIC_SITE_URL=https://next-shop.com\nSTRIPE_PUBLIC_KEY=pk_live_********\n',
  6: "APP_ENV=production\nALLOWED_HOSTS=api.studio.test\nDATABASE_URL='postgresql://studio_api:********@127.0.0.1:5432/studio_api'\n",
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

export const mockPhpVersions = ['8.3', '8.4', '8.5']

export const mockSettings = {
  default_php: '8.4',
  admin_email: 'admin@example.com',
  github_pat_set: true,
}

export const mockPanelDomain = {
  domain: 'admin.myapp.com',
  url: 'https://admin.myapp.com',
  ssl_provider: 'letsencrypt',
  ssl_active: true,
  expires_at: '2026-09-26T03:00:00Z',
  status: 'active',
  direct_access_preserved: true,
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

const demoDisconnectedAccounts = new Map<number, number>()
// Demo credentials are simulated locally and are never persisted.
function demoGitHubIdentity(token: string) {
  if (token === 'demo-secondary') return { github_user_id: 67890, username: 'demo-secondary' }
  return { github_user_id: 12345, username: token === 'demo-renamed' ? 'renamed-user' : 'myuser' }
}
let nextGithubAccountID = 2
export const mockGithubAccounts: GitHubAccount[] = [{
  id: 1, name: 'Work GitHub', username: 'myuser', github_user_id: 12345, created_at: '2026-06-01T10:00:00Z', updated_at: new Date().toISOString(),
  token_status: 'valid', token_checked_at: new Date().toISOString(), token_expires_at: new Date(Date.now() + 12 * 86400000).toISOString(),
  linked_sites: 4, deployment_access: { checked_at: new Date().toISOString(), sites: demoGitHubSiteAccess(mockSites.filter(site => site.github_account_id === 1 && site.repository)) },
}]

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
  private domainRevision(siteId:number,domain:any):string { return `demo-domain-${siteId}-${domain.id}-${domain.www_redirect}-${Boolean(domain.wildcard)}-${Boolean(mockVhostStates[siteId]?.customized)}` }

  private systemEnabled = true;
  nginxDismissed = '';
  nginxDiscovery: any = { snapshot: null, last_successful_check: 0, attempted_at: 0, error: '', stale: true, checking: false, notice: false, running_adoption: 'not_verified' };
  async getServerSystem() { return {boot_id:'00000000-0000-4000-8000-000000000001',running_kernel:'6.8.0-demo',updates:{state:this.systemEnabled?'enabled':'disabled',editable:true,automatic_reboot:'disabled',last_result:'unknown',detail:'Demo only. No server settings are changed.'},restart_required:{state:'not_required',packages:[],detail:'Simulated status; no actual server is inspected.'},reboot:null}; }
  async setSystemSecurityUpdates(enabled:boolean) { this.systemEnabled = enabled; return (await this.getServerSystem()).updates; }
  async restartServer(_boot:string):Promise<never> { throw new Error('Server restarts are unavailable in the demo. No server was restarted.'); }
  async getServerRebootServices(_id:string):Promise<never> { throw new Error('Actual service status is unavailable in the demo.'); }

  private networkRevision = 1
  private networkStates = new Map<number, any>()

  private networkState(id: number) {
    if (!mockVhostStates[id]) throw new Error('Site not found.')
    let saved = this.networkStates.get(id)
    if (!saved) {
      saved = { revision: `demo-network-${id}-${this.networkRevision++}`, pending: false, protection: { enabled: false, username: '' }, redirects: [] }
      this.networkStates.set(id, saved)
    }
    return { ...structuredClone(saved), managed: !mockVhostStates[id].customized, wildcards: mockVhostStates[id].customized ? [] : (mockDomains[id] || []).filter(domain => domain.wildcard).map(domain => domain.domain) }
  }

  private networkChanged(id: number) {
    this.networkState(id)
    this.networkStates.get(id).revision = `demo-network-${id}-${this.networkRevision++}`
    return this.networkState(id)
  }

  private networkWritable(id: number, body: any, recovery = false) {
    const state = this.networkState(id)
    if (body?.expected_revision !== state.revision) throw new Error('Network settings changed. Reload and review the current settings.')
    if (!recovery && (!state.managed || state.pending)) throw new Error(state.pending ? 'Restore the last confirmed network configuration first.' : 'Restore the Fluxo default vhost to manage network features.')
    return this.networkStates.get(id)
  }

  private guardDemoPanel(next: string) {
    if (!mockPanelDomain.domain || next === mockPanelDomain.domain) return
    for (const [id, domains] of Object.entries(mockDomains)) {
      if (mockVhostStates[Number(id)]?.customized) continue
      if (domains.some(d => d.wildcard && mockPanelDomain.domain.endsWith(`.${d.domain}`))) throw new Error('Disable wildcard routing before moving or removing the panel hostname. Demo only; no server was changed.')
    }
  }

  private validateDemoDomain(siteId:number,domain:string,wildcard:boolean,excludeId?:number) {
    if (!/^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\.[a-z0-9-]+$/.test(domain)) throw new Error('Invalid domain format.')
    if (mockPanelDomain.domain === domain) throw new Error('The panel hostname is reserved.')
    if (wildcard && mockPanelDomain.domain.endsWith(`.${domain}`) && mockPanelDomain.status !== 'active') throw new Error('Restore the panel exact route first. Demo only; no server was changed.')
    for (const [owner,domains] of Object.entries(mockDomains)) for (const existing of domains) {
      if (Number(owner) === siteId && existing.id === excludeId) continue
      if (existing.domain === domain) throw new Error('Domain is already attached to a site.')
      if (wildcard && existing.wildcard && (domain.endsWith(`.${existing.domain}`) || existing.domain.endsWith(`.${domain}`))) throw new Error('Wildcard scope overlaps an existing owner.')
    }
  }

  private demoDomain(id: number, domainId: number) {
    return (mockDomains[id] || []).find(domain => domain.id === domainId)
  }

  private demoCertificateCovers(certificate: any, target: any) {
    if (!certificate || !target) return false
    const names = certificate.domains || [certificate.domain]
    return certificateHostnames(target).every(host => names.some((name: string) => name === host || (!host.startsWith('*.') && name.startsWith('*.') && host.endsWith(name.slice(1)) && host.split('.').length === name.split('.').length)))
  }

  private trafficRetentionDays: TrafficRetentionDays = 30
  private trafficMaster = false
  private trafficPending: { readyAt: number; siteId?: number } | null = null
  private trafficSites = new Map(mockSites.map(site => [site.id, { enabled: false, effective: false, sessions: [] as { from: number; to: number | null }[], apiRules: [{ from: 0, settings: { prefixes: ['/api'], routes: [] } as TrafficAPISettings }] }]))

  private advanceTraffic() {
    // Discard expired sample history before policy changes; lengthening cannot
    // reconstruct intervals that have already expired in this browser session.
    if (this.trafficRetentionDays > 0) {
      const cutoff = Date.now() - this.trafficRetentionDays * 86_400_000
      for (const site of this.trafficSites.values()) {
        site.sessions = site.sessions.filter(session => (session.to ?? Date.now()) > cutoff).map(session => ({ ...session, from: Math.max(session.from, cutoff) }))
      }
    }
    if (!this.trafficPending || Date.now() < this.trafficPending.readyAt) return
    const now = Date.now()
    for (const site of this.trafficSites.values()) {
      const effective = this.trafficMaster && site.enabled
      if (effective && !site.effective) site.sessions.push({ from: now, to: null })
      if (!effective && site.effective) {
        const active = site.sessions.at(-1)
        if (active) active.to = now
      }
      site.effective = effective
      site.sessions = site.sessions.slice(-128)
    }
    this.trafficPending = null
  }

  private trafficSiteStatus(id: number): TrafficSiteStatus {
    const site = this.trafficSites.get(id)
    const definition = mockSites.find(candidate => candidate.id === id)
    if (!site || !definition) throw new Error('Site not found.')
    const pending = !!this.trafficPending && (!this.trafficPending.siteId || this.trafficPending.siteId === id)
    const last = site.sessions.at(-1)
    return {
      site_id: id, domain: definition.domain, enabled: site.enabled, effective_enabled: site.effective,
      status: pending ? site.effective ? 'draining' : 'pending' : site.effective ? 'collecting' : 'disabled', pending,
      last_processed_at: last ? new Date(last.to ?? Date.now()).toISOString() : null,
      last_checked_at: site.effective ? new Date().toISOString() : last?.to ? new Date(last.to).toISOString() : null,
      backlog_bytes: 0, incomplete: false, dropped_bytes: 0,
    }
  }

  private trafficState(): TrafficState {
    return { master_enabled: this.trafficMaster, pending: !!this.trafficPending, status: this.trafficPending ? this.trafficMaster ? 'pending' : 'draining' : this.trafficMaster ? 'enabled' : 'disabled', retention_days: this.trafficRetentionDays, freshness_seconds: 30, sites: mockSites.map(site => this.trafficSiteStatus(site.id)) }
  }

  private trafficAPIReport(id: number, to: string, overviewBuckets: TrafficBucket[], gaps: TrafficGap[], bucketSeconds: number): TrafficAPIReport {
    const site = this.trafficSites.get(id)!
    const current = site.apiRules.at(-1)!
    const emptyMetrics = (): TrafficAPIMetrics => ({ requests: 0, status_1xx: 0, status_2xx: 0, status_3xx: 0, status_4xx: 0, status_5xx: 0, response_bytes: 0, avg_request_time_ms: 0 })
    const add = (target: TrafficAPIMetrics, metrics: TrafficAPIMetrics) => {
      const time = target.avg_request_time_ms * target.requests + metrics.avg_request_time_ms * metrics.requests
      for (const key of ['requests', 'status_1xx', 'status_2xx', 'status_3xx', 'status_4xx', 'status_5xx', 'response_bytes'] as const) target[key] += metrics[key]
      target.avg_request_time_ms = target.requests ? time / target.requests : 0
    }
    const examples = [{ method: 'GET', path: '/api/users/42', share: 0.42, time: 34.8 }, { method: 'POST', path: '/api/orders', share: 0.28, time: 86.2 }, { method: 'GET', path: '/api/health', share: 0.18, time: 5.7 }, { method: 'DELETE', path: '/api/users/42', share: 0.08, time: 48.3 }, { method: 'POST', path: '/webhooks/payments', share: 0.04, time: 62.1 }]
    const matches = (template: string, path: string) => {
      const parts = template.split('/'), segments = path.split('/')
      return parts.length === segments.length && parts.every((part, index) => part.startsWith(':') || part === segments[index])
    }
    const totals = emptyMetrics()
    const endpoints = new Map<string, { method: string; path: string; metrics: TrafficAPIMetrics }>()
    const buckets: TrafficAPIBucket[] = overviewBuckets.map(bucket => {
      if (!bucket.metrics || bucket.collection === 'disabled' || bucket.collection === 'unknown') return { ...bucket, metrics: null }
      const metrics = emptyMetrics()
      const hour = Date.parse(bucket.hour), end = Math.min(Date.parse(to), hour + bucketSeconds * 1000)
      const coverage = site.sessions.reduce((sum, session) => sum + Math.max(0, Math.min(end, session.to ?? end) - Math.max(hour, session.from)), 0)
      for (const [index, rules] of site.apiRules.entries()) {
        const ruleEnd = site.apiRules[index + 1]?.from ?? end
        const duration = site.sessions.reduce((sum, session) => sum + Math.max(0, Math.min(end, ruleEnd, session.to ?? end) - Math.max(hour, rules.from, session.from)), 0)
        if (!duration) continue
        const candidates = Math.max(1, Math.round(bucket.metrics.requests * 0.24 * duration / coverage))
        for (const [exampleIndex, example] of examples.entries()) {
          const normalized = rules.settings.routes.filter(route => matches(route, example.path)).sort((a, b) => b.split('/').filter(part => !part.startsWith(':')).length - a.split('/').filter(part => !part.startsWith(':')).length)[0]
          if (!normalized && !rules.settings.prefixes.some(prefix => prefix === '/' || example.path === prefix || example.path.startsWith(prefix + '/'))) continue
          const requests = Math.floor(candidates * example.share)
          if (!requests) continue
          const values = emptyMetrics()
          values.requests = requests
          values.status_1xx = Math.floor(requests * 0.002)
          values.status_3xx = Math.floor(requests * 0.018)
          values.status_4xx = Math.floor(requests * (exampleIndex === 1 ? 0.07 : 0.025))
          values.status_5xx = Math.floor(requests * 0.009)
          values.status_2xx = requests - values.status_1xx - values.status_3xx - values.status_4xx - values.status_5xx
          values.response_bytes = requests * (540 + exampleIndex * 260)
          values.avg_request_time_ms = example.time
          add(metrics, values)
          const path = normalized || example.path, key = example.method + ' ' + path
          if (!endpoints.has(key)) endpoints.set(key, { method: example.method, path, metrics: emptyMetrics() })
          add(endpoints.get(key)!.metrics, values)
        }
      }
      add(totals, metrics)
      return { ...bucket, metrics }
    })
    return {
      settings: structuredClone(current.settings), effective_from: current.from ? new Date(current.from).toISOString() : null,
      available_from: site.sessions.length ? new Date(site.sessions[0]!.from).toISOString() : null,
      totals, buckets, endpoints: [...endpoints.values()].sort((a, b) => b.metrics.requests - a.metrics.requests),
      gaps: gaps.map(gap => ({ ...gap, reason: 'API data unavailable: collection disabled or not yet enabled' })), incomplete: false,
      notes: ['Simulated API requests for this demo only. Settings apply forward within this browser session; no real traffic is measured.'],
    }
  }

  private trafficReport(id: number, range: TrafficRange): TrafficReport {
    const site = this.trafficSites.get(id)
    if (!site) throw new Error('Site not found.')
    const now = Date.now()
    const from = new Date(now)
    from.setUTCHours(0, 0, 0, 0)
    const days = ({ today: 1, '7d': 7, '30d': 30, '60d': 60, '90d': 90, '180d': 180, '365d': 365, all: 1 } as const)[range]
    from.setUTCDate(from.getUTCDate() - days + 1)
    const available = site.sessions[0]?.from ?? null
    if (range === 'all' && available !== null) from.setTime(Math.floor(available / 3_600_000) * 3_600_000)
    const start = from.getTime()
    const span = Math.max(0, (now - start) / 1000)
    const bucketSeconds = span <= 30 * 86400 ? 3600 : span <= 180 * 86400 ? 86400 : Math.max(1, Math.ceil(span / (720 * 604800))) * 604800
    const sessions = site.sessions.map(session => ({ from: Math.max(start, session.from), to: Math.min(now, session.to ?? now) })).filter(session => session.to > session.from)
    const gaps: TrafficGap[] = []
    let cursor = start
    for (const session of sessions) {
      if (session.from > cursor) gaps.push({ from: new Date(cursor).toISOString(), to: new Date(session.from).toISOString(), reason: 'Collection disabled or not yet enabled' })
      cursor = Math.max(cursor, session.to)
    }
    if (cursor < now) gaps.push({ from: new Date(cursor).toISOString(), to: new Date(now).toISOString(), reason: 'Collection disabled or not yet enabled' })
    const totals: TrafficMetrics = { requests: 0, page_views: 0, not_found: 0, other_4xx: 0, server_errors: 0, response_bytes: 0, avg_request_time_ms: 0 }
    const buckets: TrafficBucket[] = []
    let totalTime = 0
    for (let hour = start; hour < now; hour += bucketSeconds * 1000) {
      const end = Math.min(now, hour + bucketSeconds * 1000)
      const duration = sessions.reduce((sum, session) => sum + Math.max(0, Math.min(end, session.to) - Math.max(hour, session.from)), 0)
      let metrics: TrafficMetrics | null = null
      if (duration > 0) {
        const requests = Math.max(1, Math.round((980 + id * 173 + new Date(hour).getUTCHours() * 41) * Math.max(0.08, duration / 3_600_000)))
        metrics = { requests, page_views: Math.floor(requests * 0.68), not_found: Math.floor(requests * 0.035), other_4xx: Math.floor(requests * 0.009), server_errors: Math.floor(requests * 0.004), response_bytes: requests * (12_380 + id * 230), avg_request_time_ms: 24.6 + id * 7.4 }
        totalTime += metrics.avg_request_time_ms * requests
        for (const key of ['requests', 'page_views', 'not_found', 'other_4xx', 'server_errors', 'response_bytes'] as const) totals[key] += metrics[key]
      }
      buckets.push({ hour: new Date(hour).toISOString(), metrics, collection: !duration ? site.sessions.length ? 'disabled' : 'unknown' : duration < end - hour ? 'partial' : 'enabled', incomplete: false })
    }
    totals.avg_request_time_ms = totals.requests ? totalTime / totals.requests : 0
    const rank = (values: string[], count: number) => {
      const shares = [0.47, 0.26, 0.17]
      let assigned = 0
      return values.map((value, index) => {
        const requests = index === values.length - 1 ? count - assigned : Math.floor(count * (shares[index] ?? 0))
        assigned += requests
        return { value, requests }
      }).filter(row => row.requests > 0)
    }
    return { range, retention_days: this.trafficRetentionDays, bucket_seconds: bucketSeconds, available_from: available === null ? null : new Date(available).toISOString(), from: from.toISOString(), to: new Date(now).toISOString(), master_enabled: this.trafficMaster, site: this.trafficSiteStatus(id), totals, buckets, top_paths: rank(['/', '/pricing', '/docs', '/contact'], totals.page_views), referrers: rank(['google.com', 'github.com', 'fluxo.fabiotech.dev'], Math.floor(totals.requests * 0.62)), gaps, api: this.trafficAPIReport(id, new Date(now).toISOString(), buckets, gaps, bucketSeconds), notes: ['Demo sample data. Retention choices, collection changes and history deletion are simulated for this browser session; no server configuration is changed.'] }
  }

  private nodeLegacy = typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('node_source') === 'legacy'
  private nodeVersion = this.nodeLegacy ? '22.22.3' : '24.19.0'
  private nodeNPM = this.nodeLegacy ? '10.9.8' : '11.17.0'
  private nodeCandidate = this.nodeLegacy ? '22.23.4' : '24.20.0'
  private nodeDiscoveryID = createRequestID()
  private nodeDiscoveryState = this.nodeLegacy ? 'migration_available' : 'available'
  private nodeCheckedAt = Math.floor(Date.now() / 1000)
  private nodeDismissed = false
  private nodeOperations = new Map<string, any>()
  private nodePlans = new Map<string, any>()
  private nodeGeneration: any = null
  private nodeRequests = new Map<string, any>()
  private nodeRelease(version: string, npm: string) { return { version, npm, architecture: 'x64', filename: `node-v${version}-linux-x64.tar.gz`, sha256: 'a'.repeat(64), manifest_sha256: 'b'.repeat(64), signer: 'C'.repeat(40), trust_revision: 'd'.repeat(64), tree_sha256: 'f'.repeat(64) } }
  private nodeLegacyReceipt() { return { schema: 1, kind: 'nodesource-apt-local-baseline', version: '22.22.3', npm: '10.9.8', package_version: '22.22.3-1nodesource1', architecture: 'amd64', node: { path: '/usr/bin/node', sha256: '9'.repeat(64) }, managers: { pnpm: '11.20.0', yarn: '4.18.0', corepack: '0.35.0', bun: '1.3.14' } } }
  private nodeImpact() { return { php_policy: 1, php_resumes: this.nodeGeneration?.coordination?.php_resumes || {}, php_consumers: [{ branch: '8.4', domain: 'myapp.com', consumer: { id: 1, site_id: 1, name: 'Queue worker', kind: '', protocol: 'laravel_queue', command: mockDaemons[0]!.command, directory: '/home/fluxo/myapp', instances: 1, stop_seconds: 15 }, php: { path: '/usr/bin/php8.4', version: '8.4.22', sha256: 'e'.repeat(64) }, units: [{ enabled: 'enabled', runtime: { name: 'fluxo-daemon-1.service', active: true, state: 'active', sub_state: 'running' } }] }], sites: mockSites.map(s => ({ id: s.id, domain: s.domain, app_type: s.app_type, mode: (s as any).node_mode || '' })), units: mockSites.filter(s => s.app_type === 'node' && (s as any).node_mode !== 'static').map(s => ({ id: s.id, site_id: s.id, domain: s.domain, name: `fluxo-daemon-${s.id}.service`, active: true, bun_only: false })), blocked_reasons: [] } }
  private advanceNodeOperations() {
    for (const op of this.nodeOperations.values()) {
      if (op.status !== 'running' || Date.now() < op.readyAt) continue;
      op.status = op.kind === 'rollback' ? 'rolled_back' : 'succeeded'; op.phase = 'complete'; op.updated_at = Math.floor(Date.now() / 1000);
      if (op.kind === 'check') { this.nodeDiscoveryState = this.nodeLegacy ? 'migration_available' : this.nodeVersion === this.nodeCandidate ? 'current' : 'available'; this.nodeDiscoveryID = createRequestID(); this.nodeCheckedAt = op.updated_at; op.message = 'Node.js update check completed'; continue; }
      const plan = this.nodePlans.get(op.plan_id); this.nodeLegacy = !!plan.migration && op.kind === 'rollback'; this.nodeVersion = this.nodeLegacy ? plan.migration.version : plan.after.version; this.nodeNPM = this.nodeLegacy ? plan.migration.npm : plan.after.npm; this.nodeDiscoveryState = 'stale';
      if (op.kind === 'apply' || op.kind === 'migrate') this.nodeGeneration = { id: op.id, head: op.id, state: 'succeeded', plan, coordination: plan.impact };
      else this.nodeGeneration = { ...this.nodeGeneration, head: op.id, state: op.status, coordination: plan.impact };
      op.message = op.kind === 'rollback' ? 'Previous Node.js runtime restored and applications verified' : 'Node.js updated and affected applications verified';
    }
  }
  private phpUpdateAvailable = true
  private phpUpdateDismissed = false
  private phpOperations = new Map<string, any>()
  private phpRecovery: any = null
  private phpRecoveryPlans = new Map<string, any>()
  private phpRepositoryConfigured = true;
  private phpRepositoryNeedsCheck = false;
  private phpRepositoryReviews = new Map<string, any>();
  private phpReviewedPlans = new Map<string, any>()

  private phpPlan() {
    return {
      id: '00000000-0000-4000-8000-000000000084', version: '8.4', candidate_id: 'd'.repeat(64),
      inventory: { changes: ['fpm', 'cli', 'common'].map(name => ({ name: `php8.4-${name}`, installed: '8.4.26-1+demo', candidate: '8.4.26-2+demo' })) },
      reviewed: false,
      impact: { sites: [], workers: [], phpmyadmin: false, blocked_reasons: [] },
      rollback: { available: true },
    }
  }
  private phpInstallationImpact() {
    return { sites: mockSites.filter(s => s.php_version === '8.4').map(s => ({ id: s.id, domain: s.domain })), workers: [{ id: 7, site_id: 1, name: 'Nightwatch', kind: 'laravel_nightwatch', command: 'php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:2407', directory: '/home/fluxo/myapp.com', instances: 1, stop_seconds: 15, port: 2407 }], schedulers: [{ id: 4, site_id: 1, version: '8.4' }], phpmyadmin: false, blocked_reasons: [], notes: ['Nightwatch monitoring pauses during maintenance. Buffered telemetry may be lost when its agent stops.'] }
  }
  private toastCallback: ((msg: string, type: string) => void) | null = null

  setToastCallback(cb: (msg: string, type: string) => void) {
    this.toastCallback = cb
  }

  private toast(msg: string, type = 'info') {
    this.toastCallback?.(msg, type)
  }

  async get(url: string) { await delay(100); return this._handle(url, 'GET') }
  async post(url: string, body?: any) { await delay(url.includes('/settings/panel-domain/') ? 1200 : 150); return this._handle(url, 'POST', body) }
  async put(url: string, body?: any) { await delay(150); return this._handle(url, 'PUT', body) }
  async delete(url: string, body?: any) { await delay(url.includes('/settings/panel-domain') ? 800 : 150); return this._handle(url, 'DELETE', body) }

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

    if (pathname === '/api/v1/server/nginx/updates/check' && method === 'POST') {
      const snapshot = { state: 'update_available', reason: 'Simulated configured Ubuntu candidate; no real packages are inspected.', package: 'nginx', architecture: 'amd64', binary: '/usr/sbin/nginx', installed_version: '1.24.0-2ubuntu7.15', candidate_version: '1.24.0-2ubuntu7.21', held: false, sources: [{ origin: 'Ubuntu', archive: 'noble-updates', site: 'archive.ubuntu.com', component: 'main', trusted: true }], source_identity: 'demo-policy', identity: 'demo-nginx-7.21' };
      this.nginxDiscovery = { snapshot, last_successful_check: Math.floor(Date.now()/1000), attempted_at: Math.floor(Date.now()/1000), error: '', stale: false, checking: false, notice: this.nginxDismissed !== snapshot.identity, running_adoption: 'not_verified' };
      return structuredClone(this.nginxDiscovery);
    }
    if (pathname === '/api/v1/server/nginx/updates/notices/dismiss' && method === 'POST') {
      if (!body?.identity || body.identity !== this.nginxDiscovery.snapshot?.identity) throw new Error('The available Nginx update changed; refresh before dismissing it');
      this.nginxDismissed = body.identity; this.nginxDiscovery.notice = false; return structuredClone(this.nginxDiscovery);
    }
    if (pathname === '/api/v1/server/nginx/restart' && method === 'POST') return null;
    if (pathname === '/api/v1/server/system' && method === 'GET') return this.getServerSystem()
    if (pathname === '/api/v1/server/system/security-updates' && method === 'PUT') {
      if (!body || Object.keys(body).length !== 1 || typeof body.enabled !== 'boolean') throw new Error('enabled must be a boolean.')
      return this.setSystemSecurityUpdates(body.enabled)
    }
    if (pathname === '/api/v1/server/system/reboots' && method === 'POST') return this.restartServer(body?.boot_id)
    if (/^\/api\/v1\/server\/system\/reboots\/[^/]+\/services$/.test(pathname) && method === 'GET') return this.getServerRebootServices(pathname)

    if (pathname === '/api/v1/traffic-analytics') {
      this.advanceTraffic()
      if (method === 'PUT') {
        if (this.trafficPending) throw new Error('A collection change is still being applied.')
        if (!body || typeof body !== 'object' || Array.isArray(body) || Object.keys(body).length !== 1) throw new Error('Provide enabled or retention_days only.')
        if ('retention_days' in body) {
          if (!Number.isInteger(body.retention_days) || ![0, 30, 60, 90, 180, 365].includes(body.retention_days)) throw new Error('Choose a supported retention_days value: 0, 30, 60, 90, 180 or 365.')
          this.trafficRetentionDays = body.retention_days
          this.advanceTraffic()
        } else {
          if (typeof body.enabled !== 'boolean') throw new Error('Choose whether to enable collection.')
          this.trafficMaster = body.enabled
          this.trafficPending = { readyAt: Date.now() + 1800 }
        }
      }
      return this.trafficState()
    }
    const trafficSettingsMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)\/traffic-analytics\/api-settings$/)
    if (trafficSettingsMatch) {
      this.advanceTraffic()
      const site = this.trafficSites.get(Number(trafficSettingsMatch[1]))
      if (!site) throw new Error('Site not found.')
      if (method === 'PUT') {
        if (this.trafficPending) throw new Error('Wait for the collection change to finish before changing API paths.')
        if (!Array.isArray(body?.prefixes) || !Array.isArray(body?.routes) || [...body.prefixes, ...body.routes].some(value => typeof value !== 'string')) throw new Error('Provide prefix and route lists.')
        const parsed = trafficAPISettings(body.prefixes.join('\n'), body.routes.join('\n'))
        if (parsed.errors.prefixes || parsed.errors.routes) throw new Error(parsed.errors.prefixes || parsed.errors.routes)
        const now = Date.now(), current = site.apiRules.at(-1)!
        if (JSON.stringify(current.settings) !== JSON.stringify(parsed.settings)) {
          if (site.apiRules.filter(rule => rule.from > now - 30 * 86_400_000).length >= 128) throw new Error('The retained API settings limit has been reached. Try again after older settings expire.')
          site.apiRules.push({ from: now, settings: parsed.settings })
        }
      }
      const rules = site.apiRules.at(-1)!
      return { settings: structuredClone(rules.settings), effective_from: rules.from ? new Date(rules.from).toISOString() : null }
    }
    const trafficMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)\/traffic-analytics$/)
    if (trafficMatch) {
      this.advanceTraffic()
      const id = Number(trafficMatch[1])
      const site = this.trafficSites.get(id)
      if (!site) throw new Error('Site not found.')
      if (method === 'PUT') {
        if (this.trafficPending) throw new Error('A collection change is still being applied.')
        if (typeof body?.enabled !== 'boolean') throw new Error('Choose whether to enable collection.')
        site.enabled = body.enabled
        this.trafficPending = { readyAt: Date.now() + 1800, siteId: id }
        return this.trafficSiteStatus(id)
      }
      if (method === 'DELETE') {
        if (this.trafficPending) throw new Error('Wait for the collection change to finish before deleting history.')
        site.sessions = site.effective ? [{ from: Date.now(), to: null }] : []
        return { deleted: true, site: this.trafficSiteStatus(id) }
      }
      const range = searchParams.get('range') || 'today'
      if (!['today', '7d', '30d', '60d', '90d', '180d', '365d', 'all'].includes(range)) throw new Error('Choose a supported traffic history range.')
      return this.trafficReport(id, range as TrafficRange)
    }

    if (pathname.startsWith('/api/v1/server/node/updates')) {
      this.advanceNodeOperations();
      const operations = [...this.nodeOperations.values()].reverse(); const active = operations.find(o => o.status === 'running') || null;
      if (method === 'GET' && /\/operations\/[^/]+$/.test(pathname)) { const op = this.nodeOperations.get(pathname.split('/').pop()!); if (!op) throw new Error(JSON.stringify({ code: 'not_found', message: 'Operation not found' })); return op; }
      if (method === 'GET' && pathname.endsWith('/operations')) return operations;
      if (method === 'GET') return { snapshot: { migration_available: this.nodeDiscoveryState === 'migration_available', legacy_source: this.nodeLegacy ? this.nodeLegacyReceipt() : undefined, state: this.nodeDiscoveryState, checked_at: this.nodeCheckedAt, attempted_at: this.nodeCheckedAt, discovery_id: this.nodeDiscoveryID, candidate_id: 'e'.repeat(64), installed: this.nodeLegacy ? undefined : { release: this.nodeRelease(this.nodeVersion, this.nodeNPM) }, candidate: this.nodeVersion === this.nodeCandidate ? null : this.nodeRelease(this.nodeCandidate, '11.18.0'), update_available: this.nodeDiscoveryState === 'available', notice: this.nodeDiscoveryState === 'available' && !this.nodeDismissed }, active_operation: active, operations, recoveries: this.nodeGeneration ? [this.nodeGeneration] : [], locked: !!active && active.kind !== 'check' };
      if (pathname.endsWith('/notices/dismiss')) { this.nodeDismissed = true; return null; }
      if (pathname.includes('/plans/') && pathname.endsWith('/review')) {
        if (active || !['available', 'migration_available'].includes(this.nodeDiscoveryState) || pathname.split('/').at(-2) !== this.nodeDiscoveryID) throw new Error(JSON.stringify({ code: 'review_changed', message: 'Check and review the Node.js update again.' }));
        const plan = { consumer_policy: 3, id: createRequestID(), kind: this.nodeLegacy ? 'migrate' : 'apply', migration: this.nodeLegacy ? this.nodeLegacyReceipt() : undefined, discovery_id: this.nodeDiscoveryID, created_at: Math.floor(Date.now() / 1000), before: { release: this.nodeRelease(this.nodeVersion, this.nodeNPM) }, after: this.nodeRelease(this.nodeCandidate, '11.18.0'), impact: this.nodeImpact() }; this.nodePlans.set(plan.id, plan); return plan;
      }
      if (pathname.endsWith('/recovery-plan')) {
        if (!this.nodeGeneration || active || this.nodeGeneration.head !== pathname.split('/').at(-2) || !['repair', 'rollback'].includes(body?.kind)) throw new Error(JSON.stringify({ code: 'review_changed', message: 'Review the latest Node.js maintenance report.' }));
        const migration = this.nodeGeneration.plan.migration; const retained = { before: this.nodeGeneration.plan.before.release, after: this.nodeGeneration.plan.after };
        const plan = { consumer_policy: 3, id: createRequestID(), kind: body.kind, migration, generation_id: this.nodeGeneration.id, head: this.nodeGeneration.head, created_at: Math.floor(Date.now() / 1000), before: { release: this.nodeRelease(this.nodeVersion, this.nodeNPM) }, after: body.kind === 'rollback' && !migration ? retained.before : retained.after, retained, impact: this.nodeImpact() }; this.nodePlans.set(plan.id, plan); return plan;
      }
      if (pathname.endsWith('/check') || pathname.endsWith('/apply') || pathname.endsWith('/migrate')) {
        const plan = this.nodePlans.get(body?.plan_id); const kind = pathname.endsWith('/check') ? 'check' : plan?.kind;
        const previous = this.nodeRequests.get(body?.request_id); if (previous) { if (previous.kind !== kind || previous.plan_id !== (body?.plan_id || '')) throw new Error(JSON.stringify({ code: 'review_changed', message: 'Request ID belongs to another operation.' })); return previous; }
        if (active) throw new Error(JSON.stringify({ code: 'maintenance_busy', message: 'Node.js maintenance is already running.' }));
        if (!body?.request_id || !kind || (['apply', 'migrate'].includes(kind) && plan.discovery_id !== this.nodeDiscoveryID)) throw new Error(JSON.stringify({ code: 'review_changed', message: 'Review this Node.js update again.' }));
        const op = { id: createRequestID(), kind, plan_id: body?.plan_id || '', status: 'running', phase: kind === 'check' ? 'checking' : 'verifying', message: kind === 'check' ? 'Checking signed Node.js releases…' : 'Verifying the selected runtime and affected applications…', created_at: Math.floor(Date.now() / 1000), updated_at: Math.floor(Date.now() / 1000), readyAt: Date.now() + 2500 };
        this.nodeOperations.set(op.id, op); this.nodeRequests.set(body.request_id, op); if (kind !== 'check') isDemo(kind === 'rollback' ? 'Restore previous Node.js runtime' : 'Update Node.js runtime'); return op;
      }
      throw new Error('Unsupported Node.js update demo request');
    }

    if (pathname.startsWith('/api/v1/server/php/updates')) {
      if (method === 'GET' && pathname.endsWith('/repository')) return this.phpRepositoryConfigured
        ? { state: 'current', message: 'Using Ondřej Surý’s new PHP repository.', can_review: false }
        : { state: 'available', message: 'PHP repository setup is incomplete. Review the repair to continue receiving PHP updates.', can_review: true };
      if (method === 'POST' && pathname.endsWith('/repository/review')) {
        const review = { id: createRequestID(), created_at: Math.floor(Date.now() / 1000), codename: 'noble', architecture: 'amd64', files: ['/etc/apt/sources.list.d/ondrej-php.sources', '/etc/apt/sources.list.d/fluxo-sury-php.sources'], disabled_ppa_entries: 1, key_fingerprint: '15058500A0235D97F5D10063B188E2B695BD4743', rollback_unavailable: [], package_blockers: {} };
        this.phpRepositoryReviews.set(review.id, review);
        const operation = { id: createRequestID(), kind: 'repository_review', version: '', status: 'succeeded', phase: 'succeeded', message: 'Demo repository review ready. No server was changed.', output: '', result: review };
        this.phpOperations.set(operation.id, operation); return operation;
      }
      if (method === 'POST' && pathname.endsWith('/repository/apply')) {
        if (!this.phpRepositoryReviews.has(body.plan_id)) throw new Error('Review the demo repository switch again.');
        this.phpRepositoryConfigured = true; this.phpRepositoryNeedsCheck = true;
        const operation = { id: createRequestID(), kind: 'repository_apply', version: '', status: 'succeeded', phase: 'succeeded', message: 'Demo repository switched. Check for updates next; no packages were installed.', output: '', result: {} };
        this.phpOperations.set(operation.id, operation); return operation;
      }

      const operationMatch = pathname.match(/\/operations\/([^/]+)$/)
      if (method === 'GET' && operationMatch) return this.phpOperations.get(operationMatch[1])
      if (method === 'GET' && pathname.includes('/plans/')) {
        const id = pathname.split('/').pop()!;
        if (id === this.phpPlan().id) return this.phpPlan();
        const reviewed = this.phpReviewedPlans.get(id);
        if (!reviewed) throw new Error('This demo installation review was not found.');
        return reviewed;
      }
      if (method === 'GET') return {
        branches: [{ version: '8.4', state: this.phpRepositoryNeedsCheck ? 'unchecked' : this.phpUpdateAvailable ? 'available' : 'current', update_available: !this.phpRepositoryNeedsCheck && this.phpUpdateAvailable,
          notice: !this.phpRepositoryNeedsCheck && this.phpUpdateAvailable && !this.phpUpdateDismissed, blocked_reasons: [], plan: !this.phpRepositoryNeedsCheck && this.phpUpdateAvailable ? this.phpPlan() : null }],
        active_operation: null, failures: [], recoveries: this.phpRecovery ? [this.phpRecovery] : [],
      }
      if (method === 'POST' && /^\/api\/v1\/server\/php\/updates\/plans\/[a-zA-Z0-9-]+\/review$/.test(pathname)) {
        const plan = { ...this.phpPlan(), id: createRequestID(), parent_plan_id: this.phpPlan().id, reviewed: true, impact: this.phpInstallationImpact() };
        this.phpReviewedPlans.set(plan.id, plan);
        return plan;
      }
      if (pathname.endsWith('/recovery-plan')) {
        const plan = { id: createRequestID(), version: '8.4', kind: body.kind, goal: body.kind === 'rollback' ? 'before' : 'after',
          packages: this.phpPlan().inventory.changes.map(p => ({ name: p.name, architecture: 'amd64', version: body.kind === 'rollback' ? p.installed : p.candidate })), impact: this.phpInstallationImpact() };
        this.phpRecoveryPlans.set(plan.id, plan); return plan;
      }
      if (pathname.endsWith('/recovery/apply')) {
        const plan = this.phpRecoveryPlans.get(body.plan_id);
        if (!plan) throw new Error('The demo recovery review expired. Review it again.');
        isDemo(plan.kind === 'rollback' ? 'Restore retained PHP packages' : 'Repair recorded PHP maintenance');
        const operation = { id: createRequestID(), kind: plan.kind, version: '8.4', status: 'succeeded', phase: 'succeeded', message: `Demo PHP ${plan.kind} completed. No server was changed.`, output: '', result: {} };
        this.phpOperations.set(operation.id, operation);
        this.phpUpdateAvailable = plan.kind === 'rollback';
        this.phpRecovery = { generation_id: this.phpRecovery?.generation_id || operation.id, operation_id: operation.id, version: '8.4', state: plan.kind === 'rollback' ? 'rolled_back' : 'succeeded', can_repair: false, can_rollback: plan.kind !== 'rollback' };
        return operation;
      }
      if (pathname.endsWith('/notices/dismiss')) { this.phpUpdateDismissed = true; return null }
      if (pathname.endsWith('/check')) this.phpRepositoryNeedsCheck = false;
      const kind = pathname.endsWith('/check') ? 'check' : 'apply'
      isDemo(kind === 'check' ? 'Check PHP package updates' : 'Update PHP packages')
      if (kind === 'apply') this.phpUpdateAvailable = false
      const operation = { id: createRequestID(), kind, version: body?.version || '8.4', status: 'succeeded', phase: 'succeeded',
        message: kind === 'check' ? 'Demo PHP check completed.' : 'Demo PHP update completed. No server was changed.', output: '', result: {} }
      this.phpOperations.set(operation.id, operation)
      if (kind === 'apply') this.phpRecovery = { generation_id: operation.id, operation_id: operation.id, version: '8.4', state: 'succeeded', can_repair: false, can_rollback: true };
      return operation
    }

    if (pathname.startsWith('/api/v1/sites')) {
      const networkMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)\/network(?:\/(protection|redirects|recover)(?:\/([^/]+))?)?$/)
      if (networkMatch) {
        const id = Number(networkMatch[1]), action = networkMatch[2], redirectId = networkMatch[3]
        if (method === 'GET' && !action) return this.networkState(id)
        const state = this.networkWritable(id, body, action === 'recover')
        if (method === 'POST' && action === 'recover') {
          if (!state.pending) throw new Error('No interrupted network change needs recovery.')
          state.pending = false
        } else if (method === 'PUT' && action === 'protection') {
          if (typeof body.enabled !== 'boolean') throw new Error('enabled must be a boolean.')
          if (body.enabled) {
            const error = protectionUsernameError(body.username || '') || protectionPasswordError(body.password || '')
            if (error) throw new Error(error)
            state.protection = { enabled: true, username: body.username }
          } else state.protection.enabled = false
        } else if (action === 'redirects') {
          const at = state.redirects.findIndex((rule: any) => rule.id === redirectId)
          if (method === 'DELETE' && at >= 0) state.redirects.splice(at, 1)
          else if (method === 'POST' && !redirectId || method === 'PUT' && at >= 0) {
            const error = redirectSourceError(body.source || '') || redirectDestinationError(body.destination || '')
            if (error || ![301, 302].includes(body.status)) throw new Error(error || 'Choose status 301 or 302.')
            if (state.redirects.some((rule: any) => rule.source === body.source && rule.id !== redirectId)) throw new Error('A redirect already uses this source path.')
            const rule = { id: redirectId || `demo-redirect-${this.networkRevision++}`, source: body.source, destination: body.destination, status: body.status }
            if (at < 0) state.redirects.push(rule)
            else state.redirects[at] = rule
          } else throw new Error('Redirect not found.')
        } else throw new Error('Unsupported network action.')
        isDemo('Update managed network settings')
        return this.networkChanged(id)
      }
      const vhostMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)\/vhost(?:\/(restore))?$/)
      if (vhostMatch) {
        const id = parseInt(vhostMatch[1])
        const state = mockVhostStates[id]
        if (!state) return null
        if (method === 'GET' && !vhostMatch[2]) return { ...state }
        if (method === 'PUT' && !vhostMatch[2]) {
          const network = this.networkState(id)
          if (network.protection.enabled || network.redirects.length || network.wildcards.length) throw new Error('Disable password protection, remove redirects and disable wildcard subdomains before saving a custom vhost (409).')
          isDemo('Validate and save custom Nginx vhost')
          state.config = String(body?.config || '')
          state.customized = true
          state.revision = `demo-custom-${id}-${mockVhostRevision++}`
          state.updated_at = new Date().toISOString()
          this.networkChanged(id)
          return { ...state }
        }
        if (method === 'POST' && vhostMatch[2] === 'restore') {
          isDemo('Restore Fluxo Nginx vhost')
          state.config = mockVhostDefaults[id]
          state.customized = false
          state.revision = `demo-managed-${id}-${mockVhostRevision++}`
          delete state.updated_at
          this.networkChanged(id)
          return { ...state }
        }
      }
      const idMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)$/)
      if (idMatch) {
        const id = parseInt(idMatch[1]);
        if (method === 'GET') {
          return mockSites.find(s => s.id === id) || null;
        } else if (method === 'PUT') {
          isDemo('Update site settings');
          return { ...body, id };
        } else if (method === 'DELETE') {
          isDemo(searchParams.get('delete_databases') === 'true' ? 'Delete site and attached databases' : 'Delete site');
          return null;
        }
      }

      const deploymentDetail = pathname.match(/^\/api\/v1\/sites\/(\d+)\/deployments\/(\d+)$/)
      if (deploymentDetail && method === 'GET') {
        const deployment = (mockDeployments[Number(deploymentDetail[1])] || []).find(item => item.id === Number(deploymentDetail[2]))
        if (!deployment) throw new Error('Deployment not found')
        return deployment
      }

      if (pathname.endsWith('/deployments')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        const deployments = mockDeployments[id] || []
        const latestTerminal = deployments.find(deployment =>
          deployment.trigger_source !== 'repo_sync' && (deployment.status === 'success' || deployment.status === 'failed'))
        const unresolvedFailure = latestTerminal?.status === 'failed' && !latestTerminal.failure_dismissed_at
          ? latestTerminal
          : null
        return { data: deployments, current_page: 1, total: deployments.length, per_page: 12, unresolved_failure: unresolvedFailure }
      }

      const dismissFailureMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)\/deployments\/(\d+)\/dismiss$/)
      if (dismissFailureMatch && method === 'POST') {
        const siteId = parseInt(dismissFailureMatch[1])
        const deploymentId = parseInt(dismissFailureMatch[2])
        const deployment = (mockDeployments[siteId] || []).find(item => item.id === deploymentId)
        if (deployment) deployment.failure_dismissed_at = new Date().toISOString()
        isDemo('Dismiss deployment error')
        return null
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
          horizon_enabled: false,
          horizon_installed: laravelDetected,
          horizon_available: laravelDetected,
          queue_worker_enabled: false,
          queue_worker_available: laravelDetected,
          queue_worker_config: {
            connection: 'database',
            queues: 'default',
            processes: 1,
            sleep_seconds: 3,
            tries: 3,
            timeout_seconds: 60,
            backoff_seconds: 0,
            memory_mb: 128,
            max_time_seconds: 3600,
            force: false,
          },
          queue_connection: 'database',
          custom_queue_workers: 0,
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
      if (pathname.endsWith('/ssl/cloneable')) {
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1])
        const target = this.demoDomain(id, Number(searchParams.get('domain_id') || 0))
        return Object.values(mockCertificates).flat().filter(cert => cert.site_id !== id && ['custom', 'cloned'].includes(cert.provider) && this.demoCertificateCovers(cert, target)).map(cert => ({
          ...cert, site_domain: mockSites.find(site => site.id === cert.site_id)?.domain, domains: cert.domains || [cert.domain], issuer: 'Demo Certificate Authority', fingerprint: `demo-${cert.id}`,
        }))
      }
      if (pathname.endsWith('/ssl/clone') && method === 'POST') {
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1])
        const target = this.demoDomain(id, Number(body?.domain_id || 0))
        const source = Object.values(mockCertificates).flat().find(cert => cert.id === body?.certificate_id)
        if (!source || source.site_id === id || !['custom', 'cloned'].includes(source.provider) || !this.demoCertificateCovers(source, target)) throw new Error('Choose a compatible custom certificate covering every required hostname.')
        const cert = { ...source, id: Date.now(), site_id: id, domain: target.domain, provider: 'cloned', active: false, source_certificate_id: source.id, created_at: new Date().toISOString() }
        ;(mockCertificates[id] ||= []).push(cert)
        isDemo('Clone SSL certificate')
        return cert
      }
      if (pathname.endsWith('/ssl/letsencrypt') && method === 'POST') {
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1])
        const target = this.demoDomain(id, 0)
        if (!target) throw new Error('Domain not found.')
        const cert = { id: Date.now(), site_id: id, domain: target.domain, provider: 'letsencrypt', domains: certificateHostnames({ ...target, wildcard: false }), active: true, source_certificate_id: 0, expires_at: new Date(Date.now() + 90 * 86400000).toISOString(), created_at: new Date().toISOString() }
        for (const existing of mockCertificates[id] || []) existing.active = false
        ;(mockCertificates[id] ||= []).push(cert)
        target.ssl_active = true
        target.wildcard_ssl_active = false
        target.certificate_id = cert.id
        isDemo('Issue ordinary-hostname certificate; wildcard SSL stays pending')
        return cert
      }
      if (pathname.endsWith('/ssl/custom') && method === 'POST') {
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1])
        const target = this.demoDomain(id, Number(body?.domain_id || 0))
        if (!target || !body?.certificate || !body?.private_key) throw new Error('Provide a certificate and private key.')
        // No real key material is retained by this simulated demo.
        const cert = { id: Date.now(), site_id: id, domain: target.domain, provider: 'custom', domains: certificateHostnames(target), active: false, source_certificate_id: 0, expires_at: '2036-06-28T14:24:00Z', created_at: new Date().toISOString() }
        ;(mockCertificates[id] ||= []).push(cert)
        isDemo('Import certificate (simulated validation)')
        return cert
      }
      if (pathname.endsWith('/certificates')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        return mockCertificates[id] || []
      }
      if (pathname.endsWith('/logs/list')) {
        return [
          { id: 'nginx_access', label: 'Nginx Access Log', path: '/var/log/nginx/access.log', exists: true },
          { id: 'nginx_error', label: 'Nginx Error Log', path: '/var/log/nginx/error.log', exists: true },
          { id: 'app_log', label: 'Application Log', path: '/home/fluxo/app.log', exists: true }
        ]
      }
      if (pathname.endsWith('/deploy')) {
        const siteId = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        const deployments = mockDeployments[siteId] || (mockDeployments[siteId] = [])
        const deploymentId = Math.max(0, ...Object.values(mockDeployments).flat().map(deployment => Number(deployment.id) || 0)) + 1
        const now = new Date().toISOString()
        const deployment = {
          id: deploymentId,
          site_id: siteId,
          status: 'pending',
          commit_hash: '',
          commit_message: 'Manual deployment',
          commit_author: 'Demo User',
          branch: mockSites.find(site => site.id === siteId)?.branch || 'main',
          trigger_source: 'manual',
          failure_reason: '',
          failure_dismissed_at: null,
          output: 'Deployment queued...\n',
          started_at: null as string | null,
          finished_at: null as string | null,
          created_at: now,
          updated_at: now,
        }
        deployments.unshift(deployment)
        window.setTimeout(() => {
          deployment.status = 'running'
          deployment.started_at = new Date().toISOString()
          deployment.updated_at = deployment.started_at
          deployment.output += 'Starting deployment...\n'
        }, 500)
        window.setTimeout(() => {
          deployment.status = 'success'
          deployment.finished_at = new Date().toISOString()
          deployment.commit_hash = 'demo123'
          deployment.output += 'Building application...\nActivating release...\nDeployment completed successfully.\n'
          deployment.updated_at = deployment.finished_at
        }, 2500)
        isDemo('Trigger deployment')
        return { deployment_id: deploymentId, status: 'pending' }
      }
      if (pathname.endsWith('/env')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        if (method === 'GET') {
          return { content: mockEnvVars[id] || '', cache_config_after_save: false }
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
      if (pathname.includes('/tinker/sessions')) {
        throw new Error('Interactive Tinker requires a real Laravel installation. This demo does not execute PHP.');
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
          return (mockDomains[id] || []).map(domain => ({ ...domain, revision: this.domainRevision(id,domain), wildcard: Boolean(domain.wildcard), wildcard_ssl_active: Boolean(domain.wildcard && domain.wildcard_ssl_active && !mockVhostStates[id]?.customized), wildcard_ssl_state: !domain.wildcard ? 'disabled' : mockVhostStates[id]?.customized ? 'inactive_custom' : domain.wildcard_ssl_active ? 'active' : 'pending' }))
        } else if (method === 'POST') {
          if (mockVhostStates[id]?.customized) throw new Error('Restore the Fluxo default vhost to manage wildcard subdomains.')
          if (this.networkState(id).pending) throw new Error('Restore the last confirmed network configuration first.')
          const input = String(body?.domain || '').trim().toLowerCase();const parent = input.replace(/^\*\./,'');const wildcard = body?.wildcard === true || input.startsWith('*.')
          this.validateDemoDomain(id,parent,wildcard)
          const domain = { id: Date.now(), site_id: id, primary: false, ssl_active: false, www_redirect: 'none', ...body, domain:parent, wildcard, wildcard_ssl_active: false }
          ;(mockDomains[id] ||= []).push(domain)
          this.networkChanged(id)
          isDemo('Add domain alias');
          return domain;
        }
      }

      if (pathname.includes('/features/') && method === 'POST') {
        isDemo('Toggle Laravel feature');
        return null;
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
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1]), domainId = Number(pathname.split('/').at(-1))
        mockDomains[id] = (mockDomains[id] || []).filter(domain => domain.id !== domainId)
        this.networkChanged(id)
        isDemo('Delete domain alias');
        return null;
      }
      if (pathname.includes('/domains/') && method === 'PUT') {
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1]), domainId = Number(pathname.split('/').at(-1))
        if (this.networkState(id).pending) throw new Error('Restore the last confirmed network configuration first.')
        if (mockVhostStates[id]?.customized) throw new Error('Restore the Fluxo default vhost to manage wildcard subdomains.')
        const domain = this.demoDomain(id, domainId)
        if (!domain) throw new Error('Domain not found.')
        if (body?.wildcard !== undefined && body.expected_revision !== this.domainRevision(id,domain)) throw new Error('Domain settings changed; refresh before saving.')
        this.validateDemoDomain(id,domain.domain,body?.wildcard ?? domain.wildcard,domain.id)
        if (body?.www_redirect !== undefined) domain.www_redirect = body.www_redirect
        if (body?.wildcard !== undefined) { domain.wildcard = body.wildcard; domain.wildcard_ssl_active = false }
        this.networkChanged(id)
        isDemo('Update domain configuration');
        return domain;
      }
      if (pathname.includes('/ssl/certificates/') && method === 'POST') {
        const id = Number(pathname.match(/\/sites\/(\d+)/)?.[1]), certId = Number(pathname.match(/\/certificates\/(\d+)/)?.[1])
        const cert = (mockCertificates[id] || []).find(item => item.id === certId)
        if (!cert) throw new Error('Certificate not found.')
        if (pathname.endsWith('/activate')) {
          const domainMatch = pathname.match(/\/domains\/(\d+)\/ssl/)
          const target = this.demoDomain(id, Number(domainMatch?.[1] || 0))
          if (!target) throw new Error('Domain not found.')
          // Existing ordinary-hostname certificates may stay active with wildcard SSL pending.
          if (!this.demoCertificateCovers(cert, { ...target, wildcard: false })) throw new Error('Certificate does not cover the target hostnames.')
          if (!domainMatch) for (const existing of mockCertificates[id]) existing.active = false
          cert.active = true
          target.ssl_active = true
          target.wildcard_ssl_active = target.wildcard && this.demoCertificateCovers(cert, target)
          target.certificate_id = cert.id
        } else if (pathname.endsWith('/deactivate')) {
          cert.active = false
          for (const domain of mockDomains[id] || []) if (domain.certificate_id === cert.id || domain.primary) { domain.ssl_active = false; domain.wildcard_ssl_active = false }
        }
        isDemo('SSL certificate action');
        return cert;
      }
      if (pathname.includes('/ssl/certificates/') && method === 'DELETE') {
        isDemo('Delete SSL certificate');
        return null;
      }

      if (pathname.includes('/files/entries')) {
        isDemo('File operations')
        return null;
      }
      if (pathname.includes('/files/move')) {
        isDemo('Move file')
        return null;
      }
      if (pathname.includes('/files/upload')) {
        isDemo('Upload file')
        return null;
      }
      if (pathname.includes('/files/download')) {
        isDemo('Download file')
        return null;
      }
      if (pathname.includes('/files/content')) {
        if (method === 'GET') {
          const filePath = searchParams.get('path') || 'index.php'
          const content = '<?php\n\necho "Hello from Fluxo!";\n'
          return {
            path: filePath,
            content,
            sha256: '8e50e4eaf46caac9d66ba28250c5b4f52f58f2357b6f529d34b7115df186b73b',
            size: content.length,
            modified: '2026-07-21T10:10:00Z',
          };
        } else if (method === 'PUT') {
          isDemo('Save file content');
          return null;
        }
      }
      if (pathname.match(/\/api\/v1\/sites\/\d+\/files$/)) {
        if (method === 'GET') {
          return {
            path: '.',
            parent: '.',
            total: 3,
            entries: [
              { name: 'public', path: 'public', is_directory: true, is_file: false, size: 4096, modified: '2026-07-21T10:00:00Z', permissions: 'drwxr-xr-x', editable: false },
              { name: '.env', path: '.env', is_directory: false, is_file: true, size: 512, modified: '2026-07-21T10:05:00Z', permissions: '-rw-r--r--', editable: true },
              { name: 'index.php', path: 'index.php', is_directory: false, is_file: true, size: 1024, modified: '2026-07-21T10:10:00Z', permissions: '-rw-r--r--', editable: true },
            ]
          };
        } else if (method === 'POST') {
          isDemo('Create file or directory');
          return null;
        } else if (method === 'DELETE') {
          isDemo('Delete file or directory');
          return null;
        }
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
      if (pathname.includes('/exports')) throw new Error('Database downloads are unavailable in the demo.');
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

    if (pathname === '/api/v1/scheduler/status' && method === 'GET') {
      return { backend: { engine: 'cronie', state: 'active', package: 'cronie', version: '1.7.1-1', architecture: 'amd64', qualification: 'qualified', observed_at: Math.floor(Date.now() / 1000) }, branches: [] }
    }
    if (pathname.startsWith('/api/v1/crons')) {
      if (method === 'GET') return mockCrons.map(c => ({ ...c, configuration_state: 'configured' }))
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

    if (pathname.startsWith('/api/v1/ssh/security')) {
      if (method === 'GET') return mockSshSecurity
      isDemo(method === 'POST' ? 'Disable SSH password login' : 'Restore server SSH policy')
      return method === 'POST'
        ? { ...mockSshSecurity, password_authentication: 'no', password_login_enabled: false, hardened: true, managed: true, can_harden: false }
        : mockSshSecurity
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
        const content = pathParam.includes('nginx')
          ? mockServerLogs.nginx
          : pathParam.includes('php')
            ? mockServerLogs.php
            : pathParam.includes('mysql')
              ? mockServerLogs.mysql
              : ''
        const lines = content ? content.split('\n') : []
        return { path: pathParam, lines, total: lines.length }
      }
      return {}
    }

    if (pathname.startsWith('/api/v1/server')) {
      if (pathname.endsWith('/php')) return mockPhpVersions
      if (pathname.endsWith('/php/cli-default')) return { version: '8.4' }
      if (pathname.endsWith('/php/versions/available')) {
        return [
          { version: '8.5', installed: false, status: 'not_installed' },
          { version: '8.4', full_version: '8.4.26', installed: true, status: 'running' },
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
      if (pathname.endsWith('/nginx/updates')) return structuredClone(this.nginxDiscovery);
      if (pathname.endsWith('/nginx/info')) {
        return {
          binary: '/usr/sbin/nginx',
          version: 'nginx/1.24.0',
          version_scope: 'on_disk_binary', running_adoption: 'not_verified',
          config_dir: '/etc/nginx',
          sites_available: '/etc/nginx/sites-available',
          sites_enabled: '/etc/nginx/sites-enabled'
        }
      }
      if (pathname.endsWith('/node/info')) {
        return {
          binary: this.nodeLegacy ? '/usr/bin/node' : '/usr/local/bin/node',
          installed: true,
          managed: true,
          toolchain_ready: true,
          node_compatible: true,
          minimum_node_version: '22.13.0',
          version: `v${this.nodeVersion}`,
          npm: this.nodeNPM,
          pnpm: '11.20.0',
          yarn: '4.18.0',
          corepack: '0.35.0',
          bun: '1.3.14',
          missing: []
        }
      }
      if (pathname.endsWith('/python/info')) {
        return {
          binary: '/usr/bin/python3',
          installed: true,
          managed: true,
          toolchain_ready: true,
          python_compatible: true,
          minimum_python_version: '3.10.0',
          version: '3.12.3',
          venv: true,
          pip: '24.0',
          uv: '0.12.9',
          missing: [],
        }
      }
      if (pathname.startsWith('/api/v1/server/python/') && method !== 'GET') {
        isDemo(pathname.endsWith('/restart') ? 'Restart Python applications' : 'Manage Python application support')
        return null
      }
      if (pathname.endsWith('/logs')) {
        const type = searchParams.get('type') || 'nginx'
        return mockServerLogs[type] || ''
      }
      return {}
    }

    if (pathname.startsWith('/api/v1/github')) {
      if (pathname.endsWith('/repos') || pathname.endsWith('/branches')) {
        const account = searchParams.has('account_id')
          ? mockGithubAccounts.find(item => item.id === Number(searchParams.get('account_id')))
          : mockGithubAccounts[0]
        if (!account) return []
        const repos = account.github_user_id === 67890 ? [{ full_name: 'demo-secondary/app' }] : mockGithubRepos
        if (pathname.endsWith('/repos')) return repos
        return repos.some(repo => repo.full_name === searchParams.get('repo')) ? mockGithubBranches : []
      }
      if (pathname === '/api/v1/github/accounts' && method === 'GET') return mockGithubAccounts
      if (pathname === '/api/v1/github/accounts' && method === 'POST') {
        isDemo('Connect GitHub account')
        const identity = demoGitHubIdentity(body?.token)
        if (mockGithubAccounts.some(account => account.github_user_id === identity.github_user_id)) throw new Error('This GitHub account is already connected; use Update token')
        const created: GitHubAccount = { id: nextGithubAccountID++, name: body?.name || identity.username, ...identity, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), token_status: 'valid', token_checked_at: new Date().toISOString(), token_expires_at: null, linked_sites: 0, deployment_access: { checked_at: new Date().toISOString(), sites: [] } }
        for (const site of mockSites) {
          if (site.github_account_id === 0 && demoDisconnectedAccounts.get(site.id) === created.github_user_id) {
            site.github_account_id = created.id
            demoDisconnectedAccounts.delete(site.id)
          }
        }
        const sites = mockSites.filter(site => site.github_account_id === created.id && site.repository)
        created.linked_sites = sites.length
        created.deployment_access.sites = demoGitHubSiteAccess(sites, true)
        mockGithubAccounts.push(created)
        return created
      }
      const accountMatch = pathname.match(/^\/api\/v1\/github\/accounts\/(\d+)(\/check|\/recovery-review)?$/)
      if (accountMatch) {
        const account = mockGithubAccounts.find(item => item.id === Number(accountMatch[1]))
        if (!account) throw new Error('Account not found')
        if (method === 'POST' && accountMatch[2] === '/recovery-review') {
          if (account.github_user_id) throw new Error('This connection already has a verified owner; use ordinary token replacement')
          const identity = demoGitHubIdentity(body?.token)
          if (mockGithubAccounts.some(other => other.id !== account.id && other.github_user_id === identity.github_user_id)) throw new Error('This GitHub account is already connected; use Update token')
          return {
            stored: { name: account.name, username: account.username, github_user_id: account.github_user_id },
            proposed: identity,
            needs_recovery: true,
            sites: mockSites.filter(site => site.github_account_id === account.id && site.repository).map(site => ({ site_id: site.id, domain: site.domain, repository: site.repository, status: 'unchecked', message: '' })),
          }
        }
        if (method === 'DELETE') {
          isDemo('Disconnect GitHub account')
          for (const site of mockSites) {
            if (site.github_account_id === account.id) {
              site.github_account_id = 0
              demoDisconnectedAccounts.set(site.id, account.github_user_id)
            }
          }
          mockGithubAccounts.splice(mockGithubAccounts.indexOf(account), 1)
          return null
        }
        if (method === 'PUT' || (method === 'POST' && accountMatch[2])) {
          isDemo(method === 'PUT' ? 'Update GitHub token' : 'Check deployment access')
          if (method === 'PUT') {
            const identity = demoGitHubIdentity(body?.token)
            if (!account.github_user_id) {
              const recovery = body?.legacy_recovery
              if (!recovery) throw new Error('The saved credential could not verify this legacy GitHub account. Its site links have been preserved.')
              if (!recovery.confirm || recovery.password !== 'demo-password') throw new Error('Use demo-password and confirm recovery in this simulated demo')
              if (recovery.github_user_id && recovery.github_user_id !== identity.github_user_id) throw new Error("The new token's GitHub account changed; review the connection again")
              if (String(recovery.username).toLowerCase() !== identity.username.toLowerCase()) throw new Error('Confirmation username must match the GitHub account authenticated by the new token')
              const sites = mockSites.filter(site => site.github_account_id === account.id && site.repository)
              if (!Array.isArray(recovery.sites) || recovery.sites.length !== sites.length || new Set(recovery.sites.map((site: { site_id: number }) => site.site_id)).size !== sites.length || !sites.every(site => recovery.sites.some((reviewed: { site_id: number; repository: string }) => reviewed.site_id === site.id && reviewed.repository === site.repository))) throw new Error('Site links changed; close and reopen the recovery dialog to review them')
              if (mockGithubAccounts.some(other => other.id !== account.id && other.github_user_id === identity.github_user_id)) throw new Error('This GitHub account is already connected; use Update token')
              account.github_user_id = identity.github_user_id
            } else {
              if (body?.legacy_recovery) throw new Error('This connection already has a verified owner; use ordinary token replacement')
              if (account.github_user_id !== identity.github_user_id) throw new Error('Replacement token must belong to the same GitHub account')
            }
            account.username = identity.username
          }
          const sites = mockSites.filter(site => site.github_account_id === account.id && site.repository)
          account.linked_sites = sites.length
          account.deployment_access.sites = demoGitHubSiteAccess(sites, method === 'PUT')
          account.token_status = 'valid'
          account.token_checked_at = new Date().toISOString()
          if (method === 'PUT') account.token_expires_at = new Date(Date.now() + 90 * 86400000).toISOString()
          account.deployment_access.checked_at = new Date().toISOString()
          return { ...account }
        }
      }
      return {}
    }

    if (pathname.startsWith('/api/v1/settings')) {
      if (pathname.endsWith('/bootstrap-credentials')) return null
      if (pathname.endsWith('/bootstrap-credentials/copied')) return null
      if (pathname.endsWith('/panel-domain/cloneable') && method === 'GET') {
        if ((searchParams.get('domain') || '').toLowerCase() !== 'admin.myapp.com') return []
        return [{
          id: 106,
          site_id: 1,
          site_domain: 'myapp.com',
          provider: 'custom',
          domains: ['*.myapp.com', 'myapp.com'],
          expires_at: '2036-06-28T14:24:00Z',
          issuer: 'Demo Certificate Authority',
          fingerprint: 'demo-panel-clone',
          active: false,
        }]
      }
      if (pathname.endsWith('/panel-domain/letsencrypt') && method === 'POST') {
        this.guardDemoPanel(String(body?.domain || '').toLowerCase())
        Object.assign(mockPanelDomain, {
          domain: String(body?.domain || '').toLowerCase(),
          url: `https://${String(body?.domain || '').toLowerCase()}`,
          ssl_provider: 'letsencrypt',
          ssl_active: true,
          expires_at: '2026-11-07T03:00:00Z',
          status: 'active',
          direct_access_preserved: true,
        })
        return { ...mockPanelDomain }
      }
      if (pathname.endsWith('/panel-domain/custom') && method === 'POST') {
        this.guardDemoPanel(String(body?.domain || '').toLowerCase())
        Object.assign(mockPanelDomain, {
          domain: String(body?.domain || '').toLowerCase(),
          url: `https://${String(body?.domain || '').toLowerCase()}`,
          ssl_provider: 'custom',
          ssl_active: true,
          expires_at: '2036-06-28T14:24:00Z',
          status: 'active',
          direct_access_preserved: true,
        })
        return { ...mockPanelDomain }
      }
      if (pathname.endsWith('/panel-domain/clone') && method === 'POST') {
        this.guardDemoPanel(String(body?.domain || '').toLowerCase())
        Object.assign(mockPanelDomain, {
          domain: String(body?.domain || '').toLowerCase(),
          url: `https://${String(body?.domain || '').toLowerCase()}`,
          ssl_provider: 'cloned',
          ssl_active: true,
          expires_at: '2036-06-28T14:24:00Z',
          status: 'active',
          direct_access_preserved: true,
        })
        return { ...mockPanelDomain }
      }
      if (pathname.endsWith('/panel-domain') && method === 'DELETE') {
        this.guardDemoPanel('')
        Object.assign(mockPanelDomain, {
          domain: '',
          url: '',
          ssl_provider: '',
          ssl_active: false,
          expires_at: '',
          status: 'not_configured',
          direct_access_preserved: true,
        })
        return { ...mockPanelDomain }
      }
      if (pathname.endsWith('/panel-domain') && method === 'GET') return { ...mockPanelDomain }
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
