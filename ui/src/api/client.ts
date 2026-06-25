import { router } from '../router';
import { useToast } from '../composables/useToast';

const cache = new Map<string, { data: any; ts: number }>();
const CACHE_TTL = 300_000; // 5 minutes

const invalidateCachePattern = (pattern: string) => {
    for (const key of cache.keys()) {
        if (key.includes(pattern)) cache.delete(key);
    }
};

const getHeaders = () => {
    const token = localStorage.getItem('fluxo_jwt');
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    return headers;
};

const cachedFetch = async (url: string, init?: RequestInit & { bypassCache?: boolean }): Promise<any> => {
    const cacheKey = `${init?.method || 'GET'}:${url}`;
    if (!init?.bypassCache && (!init?.method || init.method === 'GET')) {
        const hit = cache.get(cacheKey);
        if (hit && Date.now() - hit.ts < CACHE_TTL) return hit.data;
    }

    const headers = getHeaders();
    if (init?.headers) Object.assign(headers, init.headers);

    const res = await fetch(url, { ...init, headers });

    if (res.status === 401) {
        localStorage.removeItem('fluxo_jwt');
        if (window.location.pathname !== '/login') {
            const { addToast } = useToast();
            addToast('Session expired or unauthorized. Please sign in again.', 'error');
            router.push('/login');
        }
        throw new Error('Unauthorized');
    }
    if (!res.ok) {
        const err = await res.text();
        throw new Error(err || 'Request failed');
    }
    if (res.status === 204 || res.status === 202) return null;

    const text = await res.text();
    if (!text) return null;

    let data;
    try {
        data = JSON.parse(text);
    } catch (e) {
        data = text;
    }

    if (!init?.method || init.method === 'GET') {
        cache.set(cacheKey, { data, ts: Date.now() });
    }
    return data;
};

export const apiClient = {
    async login(username: string, token: string) {
        const res = await fetch('/api/v1/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password: token })
        });
        if (!res.ok) {
            const err = await res.text();
            throw new Error(err || 'Invalid credentials');
        }
        const data = await res.json();
        if (data && data.token) {
            localStorage.setItem('fluxo_jwt', data.token);
            // Warm cache with common endpoints so first page loads instantly
            Promise.allSettled([
                cachedFetch('/api/v1/sites'),
                cachedFetch('/api/v1/daemons'),
                cachedFetch('/api/v1/system/metrics'),
                cachedFetch('/api/v1/settings'),
                cachedFetch('/api/v1/ssh-keys'),
                cachedFetch('/api/v1/firewall'),
            ]).catch(() => {});
            return data.token;
        }
        throw new Error('No token returned');
    },
    logout() {
        cache.clear();
        localStorage.removeItem('fluxo_jwt');
        router.push('/login');
    },
    isAuthenticated() {
        return !!localStorage.getItem('fluxo_jwt');
    },
    get(url: string, options?: { bypassCache?: boolean }) { return cachedFetch(url, options); },
    post(url: string, data?: any) {
        return cachedFetch(url, { method: 'POST', body: data ? JSON.stringify(data) : undefined });
    },
    put(url: string, data?: any) {
        return cachedFetch(url, { method: 'PUT', body: data ? JSON.stringify(data) : undefined });
    },
    delete(url: string) { return cachedFetch(url, { method: 'DELETE' }); },
    invalidate(pattern: string) { invalidateCachePattern(pattern); },
    async getSites(bypassCache = false) { return cachedFetch('/api/v1/sites', { bypassCache }); },
    async getPhpVersions(bypassCache = false) { return cachedFetch('/api/v1/server/php', { bypassCache }); },
    async getSettings(bypassCache = false) { return cachedFetch('/api/v1/settings', { bypassCache }); },
    async getGithubRepos(bypassCache = false) { return cachedFetch('/api/v1/github/repos', { bypassCache }); },
    async getGithubBranches(repo: string, bypassCache = false) { return cachedFetch(`/api/v1/github/branches?repo=${encodeURIComponent(repo)}`, { bypassCache }); },
    async updateSettings(data: any) {
        const result = await cachedFetch('/api/v1/settings', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/settings');
        return result;
    },
    async createSite(data: any) {
        const result = await cachedFetch('/api/v1/sites', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/sites');
        return result;
    },
    async deleteSite(id: number) {
        const result = await cachedFetch(`/api/v1/sites/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/sites');
        return result;
    },
    async getMetrics(bypassCache = false) { return cachedFetch('/api/v1/system/metrics', { bypassCache }); },
    async getDatabaseEngines() { return cachedFetch('/api/v1/server/engines'); },
    async installMySQL() {
        const result = await cachedFetch('/api/v1/server/engines/mysql/install', { method: 'POST' });
        invalidateCachePattern('/api/v1/server/engines');
        return result;
    },
    async installPostgres() {
        const result = await cachedFetch('/api/v1/server/engines/postgres/install', { method: 'POST' });
        invalidateCachePattern('/api/v1/server/engines');
        return result;
    },
    async installRedis() {
        const result = await cachedFetch('/api/v1/server/engines/redis/install', { method: 'POST' });
        invalidateCachePattern('/api/v1/server/engines');
        return result;
    },
    async getDatabases(bypassCache = false) { return cachedFetch('/api/v1/databases', { bypassCache }); },
    async getDaemons(bypassCache = false) { return cachedFetch('/api/v1/daemons', { bypassCache }); },
    async getCrons(bypassCache = false) { return cachedFetch('/api/v1/crons', { bypassCache }); },
    async getSystemActivity(limit = 5, bypassCache = false) { return cachedFetch(`/api/v1/system/activity?limit=${limit}`, { bypassCache }); },
    async updatePassword(currentPassword: string, newPassword: string) {
        return cachedFetch('/api/v1/settings/password', {
            method: 'POST',
            body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
        });
    },
    async getSSHKeys() { return cachedFetch('/api/v1/ssh-keys'); },
    async addSSHKey(name: string, publicKey: string) {
        const result = await cachedFetch('/api/v1/ssh-keys', { method: 'POST', body: JSON.stringify({ name, public_key: publicKey }) });
        invalidateCachePattern('/api/v1/ssh-keys');
        return result;
    },
    async deleteSSHKey(id: number) {
        const result = await cachedFetch(`/api/v1/ssh-keys/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/ssh-keys');
        return result;
    },
    async getFirewallRules() { return cachedFetch('/api/v1/firewall'); },
    async addFirewallRule(name: string, port: string, fromIp: string, type: string = 'allow') {
        const result = await cachedFetch('/api/v1/firewall', { method: 'POST', body: JSON.stringify({ name, port, from_ip: fromIp, type }) });
        invalidateCachePattern('/api/v1/firewall');
        return result;
    },
    async deleteFirewallRule(id: number) {
        const result = await cachedFetch(`/api/v1/firewall/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/firewall');
        return result;
    },
    async getBootstrapCredentials() { return cachedFetch('/api/v1/settings/bootstrap-credentials'); },
    async markCredentialsCopied() {
        const result = await cachedFetch('/api/v1/settings/bootstrap-credentials/copied', { method: 'POST' });
        invalidateCachePattern('/api/v1/settings/bootstrap-credentials');
        return result;
    },
    // Site-Specific APIs
    async getSite(id: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${id}`, { bypassCache });
    },
    async getSiteFeatures(id: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${id}/features`, { bypassCache });
    },
    async getSiteDeployments(id: string | number, page = 1, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${id}/deployments?page=${page}`, { bypassCache });
    },
    async getSiteDaemons(id: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${id}/daemons`, { bypassCache });
    },
    async getSiteCrons(id: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${id}/crons`, { bypassCache });
    },
    async getSiteActivity(siteId: string | number, limit = 5, bypassCache = false) {
        return cachedFetch(`/api/v1/system/activity?site_id=${siteId}&limit=${limit}`, { bypassCache });
    },
    async createSiteDaemon(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    async createSiteCron(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/crons`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/crons`);
        invalidateCachePattern('/api/v1/crons');
        return result;
    },
    async toggleSiteScheduler(siteId: string | number, enable: boolean) {
        const action = enable ? 'enable' : 'disable';
        const result = await cachedFetch(`/api/v1/sites/${siteId}/features/scheduler/${action}`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/features`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/crons`);
        return result;
    },
    async toggleSiteNightwatch(siteId: string | number, enable: boolean, token?: string) {
        const action = enable ? 'enable' : 'disable';
        const body = token ? JSON.stringify({ token }) : undefined;
        const result = await cachedFetch(`/api/v1/sites/${siteId}/features/nightwatch/${action}`, { method: 'POST', body });
        invalidateCachePattern(`/api/v1/sites/${siteId}/features`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        return result;
    },
    async toggleSiteMaintenance(siteId: string | number, enable: boolean) {
        const action = enable ? 'enable' : 'disable';
        const result = await cachedFetch(`/api/v1/sites/${siteId}/features/maintenance/${action}`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/features`);
        return result;
    },
    async getSiteEnv(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/env`, { bypassCache });
    },
    async saveSiteEnv(siteId: string | number, content: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/env`, {
            method: 'POST',
            body: JSON.stringify({ content })
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/env`);
        return result;
    },
    async triggerSiteDeploy(siteId: string | number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/deploy`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/deployments`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    // Site Domains
    async getSiteDomains(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/domains`, { bypassCache });
    },
    async addSiteDomain(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        return result;
    },
    async deleteSiteDomain(siteId: string | number, domainId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        return result;
    },
    // SSL Certificates
    async getSiteCertificates(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/certificates`, { bypassCache });
    },
    async installLetsEncryptSSL(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/letsencrypt`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async installCustomSSL(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/custom`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async activateCert(siteId: string | number, certId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/certificates/${certId}/activate`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async deactivateCert(siteId: string | number, certId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/certificates/${certId}/deactivate`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async deleteSiteCertificate(siteId: string | number, certId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/certificates/${certId}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    // Site Commands
    async getSiteCommands(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/commands`, { bypassCache });
    },
    async runSiteCommand(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/commands`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/commands`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    // Site Daemon actions
    async startSiteDaemon(siteId: string | number, daemonId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons/${daemonId}/start`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    async stopSiteDaemon(siteId: string | number, daemonId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons/${daemonId}/stop`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    async restartSiteDaemon(siteId: string | number, daemonId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons/${daemonId}/restart`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    async getSiteDaemonLogs(siteId: string | number, daemonId: number) {
        return cachedFetch(`/api/v1/sites/${siteId}/daemons/${daemonId}/logs`);
    },
    async deleteSiteDaemon(siteId: string | number, daemonId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons/${daemonId}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    // Site Cron actions
    async runSiteCron(siteId: string | number, cronId: number) {
        return cachedFetch(`/api/v1/sites/${siteId}/crons/${cronId}/run`, { method: 'POST' });
    },
    async getSiteCronLogs(siteId: string | number, cronId: number) {
        return cachedFetch(`/api/v1/sites/${siteId}/crons/${cronId}/logs`);
    },
    async deleteSiteCron(siteId: string | number, cronId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/crons/${cronId}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/crons`);
        invalidateCachePattern('/api/v1/crons');
        return result;
    },
    // Site update
    async updateSite(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}`, { method: 'PUT', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        invalidateCachePattern('/api/v1/sites');
        return result;
    },
    // Site logs
    async getSiteLogsList(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/logs/list`, { bypassCache });
    },
    // Global Daemon actions
    async startDaemon(daemonId: number) {
        return cachedFetch(`/api/v1/daemons/${daemonId}/start`, { method: 'POST' });
    },
    async stopDaemon(daemonId: number) {
        return cachedFetch(`/api/v1/daemons/${daemonId}/stop`, { method: 'POST' });
    },
    async restartDaemon(daemonId: number) {
        return cachedFetch(`/api/v1/daemons/${daemonId}/restart`, { method: 'POST' });
    },
    async getDaemonLogs(daemonId: number) {
        return cachedFetch(`/api/v1/daemons/${daemonId}/logs`);
    },
    async deleteDaemon(daemonId: number) {
        const result = await cachedFetch(`/api/v1/daemons/${daemonId}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    // Global Cron actions
    async runCron(cronId: number) {
        return cachedFetch(`/api/v1/crons/${cronId}/run`, { method: 'POST' });
    },
    async getCronLogs(cronId: number) {
        return cachedFetch(`/api/v1/crons/${cronId}/logs`);
    },
    async deleteCron(cronId: number) {
        const result = await cachedFetch(`/api/v1/crons/${cronId}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/crons');
        return result;
    },
    // Databases
    async createDatabase(data: any) {
        const result = await cachedFetch('/api/v1/databases', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/databases');
        return result;
    },
    async deleteDatabase(id: number) {
        const result = await cachedFetch(`/api/v1/databases/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/databases');
        return result;
    },
    async getDatabaseUsers() {
        return cachedFetch('/api/v1/databases/users');
    },
    async createDatabaseUser(data: any) {
        const result = await cachedFetch('/api/v1/databases/users', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/databases/users');
        return result;
    },
    async deleteDatabaseUser(id: number) {
        const result = await cachedFetch(`/api/v1/databases/users/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/databases/users');
        return result;
    },
    async getDatabaseUserGrants() {
        return cachedFetch('/api/v1/databases/users/grants');
    },
    async createDatabaseUserGrant(data: any) {
        const result = await cachedFetch('/api/v1/databases/users/grants', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/databases/users/grants');
        return result;
    },
    // System logs
    async getSystemLogs(path: string, lines = 100, bypassCache = false) {
        return cachedFetch(`/api/v1/system/logs?path=${encodeURIComponent(path)}&lines=${lines}`, { bypassCache });
    },
    async downloadSystemLog(path: string) {
        const headers = getHeaders();
        const res = await fetch(`/api/v1/system/logs/download?path=${encodeURIComponent(path)}`, { headers });
        if (!res.ok) throw new Error('Download failed');
        return res.blob();
    },
    async clearSystemLog(path: string) {
        const result = await cachedFetch(`/api/v1/system/logs/clear?path=${encodeURIComponent(path)}`, { method: 'POST' });
        invalidateCachePattern('/api/v1/system/logs');
        return result;
    },
    // System activity with pagination
    async getSystemActivityPaginated(limit = 50, offset = 0) {
        return cachedFetch(`/api/v1/system/activity?limit=${limit}&offset=${offset}`);
    },
    async getSiteActivityPaginated(siteId: string | number, limit = 50, offset = 0, bypassCache = false) {
        return cachedFetch(`/api/v1/system/activity?site_id=${siteId}&limit=${limit}&offset=${offset}`, { bypassCache });
    },
    // Version & bootstrap
    async getVersion() { return cachedFetch('/api/v1/version'); },
    async getBootstrapStatus() { return cachedFetch('/api/v1/auth/bootstrap'); },
};
