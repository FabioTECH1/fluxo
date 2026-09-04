import { version as appVersion } from '../../package.json'

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
    { id: 12, site_id: 1, status: 'success', commit_hash: 'a1b2c3d', commit_message: 'Fix navbar responsiveness', commit_author: 'Alex Morgan', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-28T14:20:00Z', updated_at: '2026-06-28T14:22:00Z' },
    { id: 11, site_id: 1, status: 'success', commit_hash: 'e4f5g6h', commit_message: 'Add dark mode toggle', commit_author: 'Priya Shah', branch: 'main', trigger_source: 'github_webhook', output: 'Deployment complete.\n', created_at: '2026-06-27T10:00:00Z', updated_at: '2026-06-27T10:03:00Z' },
    { id: 10, site_id: 1, status: 'failed', commit_hash: 'i7j8k9l', commit_message: 'Update application dependencies', commit_author: 'Alex Morgan', branch: 'main', trigger_source: 'rollback', output: 'Error: composer install failed\n', created_at: '2026-06-25T16:00:00Z', updated_at: '2026-06-25T16:02:00Z' },
  ],
  2: [
    { id: 8, site_id: 2, status: 'success', commit_hash: 'm0n1o2p', commit_message: 'Update post layout', commit_author: 'Sam Rivera', branch: 'main', trigger_source: 'manual', output: 'Deployment complete.\n', created_at: '2026-06-27T16:08:00Z', updated_at: '2026-06-27T16:10:00Z' },
  ],
  4: [
    { id: 16, site_id: 4, status: 'failed', commit_hash: 'n3xtfail', commit_message: 'Upgrade checkout dependencies', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'manual', failure_reason: 'deployment failed: exit status 1', failure_dismissed_at: null, output: 'Creating release...\nInstalling dependencies...\nnpm error ERESOLVE unable to resolve dependency tree\n\nDeployment failed: exit status 1\n', created_at: '2026-06-30T10:05:00Z', updated_at: '2026-06-30T10:06:00Z' },
    { id: 15, site_id: 4, status: 'success', commit_hash: 'n3xt9aa', commit_message: 'Ship checkout loading state', commit_author: 'Jordan Lee', branch: 'main', trigger_source: 'github_webhook', output: 'Creating release...\nInstalling dependencies...\nBuilding Next.js application...\nActivating release...\nRestarting Node.js daemon...\nDeployment complete.\n', created_at: '2026-06-29T11:30:00Z', updated_at: '2026-06-29T11:32:00Z' },
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

export const mockCertificates: Record<number, any[]> = {
  1: [
    { id: 101, site_id: 1, domain: 'myapp.com', provider: 'letsencrypt', cert_path: '', key_path: '', expires_at: '2026-09-26T03:00:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-28T14:25:00Z' },
    { id: 106, site_id: 1, domain: 'myapp.com', provider: 'custom', cert_path: '', key_path: '', expires_at: '2036-06-28T14:24:00Z', active: false, source_certificate_id: 0, created_at: '2026-06-28T14:24:00Z' },
  ],
  2: [
    { id: 102, site_id: 2, domain: 'blog.com', provider: 'custom', cert_path: '', key_path: '', expires_at: '2036-06-27T16:15:00Z', active: true, source_certificate_id: 0, created_at: '2026-06-27T16:15:00Z' },
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
  async post(url: string, body?: any) { await delay(url.includes('/settings/panel-domain/') ? 1200 : 150); return this._handle(url, 'POST', body) }
  async put(url: string, body?: any) { await delay(150); return this._handle(url, 'PUT', body) }
  async delete(url: string) { await delay(url.includes('/settings/panel-domain') ? 800 : 150); return this._handle(url, 'DELETE') }

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
      const vhostMatch = pathname.match(/^\/api\/v1\/sites\/(\d+)\/vhost(?:\/(restore))?$/)
      if (vhostMatch) {
        const id = parseInt(vhostMatch[1])
        const state = mockVhostStates[id]
        if (!state) return null
        if (method === 'GET' && !vhostMatch[2]) return { ...state }
        if (method === 'PUT' && !vhostMatch[2]) {
          isDemo('Validate and save custom Nginx vhost')
          state.config = String(body?.config || '')
          state.customized = true
          state.revision = `demo-custom-${id}-${mockVhostRevision++}`
          state.updated_at = new Date().toISOString()
          return { ...state }
        }
        if (method === 'POST' && vhostMatch[2] === 'restore') {
          isDemo('Restore Fluxo Nginx vhost')
          state.config = mockVhostDefaults[id]
          state.customized = false
          state.revision = `demo-managed-${id}-${mockVhostRevision++}`
          delete state.updated_at
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
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        const target = mockSites.find(site => site.id === id)
        const sourceCertificate = Object.values(mockCertificates).flat().find(cert => cert.site_id !== id && cert.provider === 'custom')
        const source = mockSites.find(site => site.id === sourceCertificate?.site_id)
        if (!target || !source || !sourceCertificate) return []

        const domains = Array.from(new Set([
          target.domain,
          ...((mockDomains[id] || []).map(domain => domain.domain)),
          `*.${target.domain}`,
        ]))
        return [{
          id: sourceCertificate.id,
          site_id: source.id,
          site_domain: source.domain,
          provider: sourceCertificate.provider,
          domains,
          expires_at: '2036-06-27T16:15:00Z',
          issuer: sourceCertificate.provider === 'letsencrypt' ? "Let's Encrypt R11" : 'Cloudflare Inc ECC CA-3',
          fingerprint: `demo-${id}-${source.id}`,
          active: true,
        }]
      }
      if (pathname.endsWith('/ssl/clone') && method === 'POST') {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        const target = mockSites.find(site => site.id === id)
        const sourceCertificate = Object.values(mockCertificates).flat().find(cert => cert.id === body?.certificate_id)
        const source = mockSites.find(site => site.id === sourceCertificate?.site_id)
        isDemo('Clone SSL certificate')
        return {
          id: 199,
          provider: 'cloned',
          active: !target?.ssl_active,
          source_certificate_id: body?.certificate_id,
          source_site: source?.domain || 'another site',
        }
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
          created_at: now,
          updated_at: now,
        }
        deployments.unshift(deployment)
        window.setTimeout(() => {
          deployment.status = 'success'
          deployment.commit_hash = 'demo123'
          deployment.output += 'Building application...\nActivating release...\nDeployment completed successfully.\n'
          deployment.updated_at = new Date().toISOString()
        }, 2500)
        isDemo('Trigger deployment')
        return { deployment_id: deploymentId, status: 'pending' }
      }
      if (pathname.endsWith('/env')) {
        const id = parseInt(pathname.match(/\/api\/v1\/sites\/(\d+)/)?.[1] || '0')
        if (method === 'GET') {
          return { content: mockEnvVars[id] || '' }
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
        isDemo('Delete domain alias');
        return null;
      }
      if (pathname.includes('/domains/') && method === 'PUT') {
        isDemo('Update domain configuration');
        return body;
      }
      if (pathname.includes('/ssl/certificates/') && method === 'POST') {
        isDemo('SSL certificate action');
        return null;
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
          binary: '/usr/local/bin/node',
          installed: true,
          managed: true,
          toolchain_ready: true,
          node_compatible: true,
          minimum_node_version: '22.13.0',
          version: 'v24.19.0',
          npm: '11.17.0',
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
      if (pathname.endsWith('/repos')) return mockGithubRepos
      if (pathname.endsWith('/branches')) return mockGithubBranches
      if (pathname.endsWith('/accounts')) return mockGithubAccounts
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
