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

const cachedFetch = async (url: string, init?: RequestInit): Promise<any> => {
    const cacheKey = `${init?.method || 'GET'}:${url}`;
    if (!init?.method || init.method === 'GET') {
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
    if (res.status === 204) return null;

    const data = await res.json();
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
    get(url: string) { return cachedFetch(url); },
    post(url: string, data?: any) {
        return cachedFetch(url, { method: 'POST', body: data ? JSON.stringify(data) : undefined });
    },
    put(url: string, data?: any) {
        return cachedFetch(url, { method: 'PUT', body: data ? JSON.stringify(data) : undefined });
    },
    delete(url: string) { return cachedFetch(url, { method: 'DELETE' }); },
    invalidate(pattern: string) { invalidateCachePattern(pattern); },
    async getSites() { return cachedFetch('/api/v1/sites'); },
    async getPhpVersions() { return cachedFetch('/api/v1/server/php'); },
    async getSettings() { return cachedFetch('/api/v1/settings'); },
    async getGithubRepos() { return cachedFetch('/api/v1/github/repos'); },
    async getGithubBranches(repo: string) { return cachedFetch(`/api/v1/github/branches?repo=${encodeURIComponent(repo)}`); },
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
    async getMetrics() { return cachedFetch('/api/v1/system/metrics'); },
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
    async getDatabases() { return cachedFetch('/api/v1/databases'); },
    async getDaemons() { return cachedFetch('/api/v1/daemons'); },
    async getCrons() { return cachedFetch('/api/v1/crons'); },
    async getSystemActivity(limit = 5) { return cachedFetch(`/api/v1/system/activity?limit=${limit}`); },
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
    async getSite(id: string | number) {
        return cachedFetch(`/api/v1/sites/${id}`);
    },
    async getSiteFeatures(id: string | number) {
        return cachedFetch(`/api/v1/sites/${id}/features`);
    },
    async getSiteDeployments(id: string | number, page = 1) {
        return cachedFetch(`/api/v1/sites/${id}/deployments?page=${page}`);
    },
    async getSiteDaemons(id: string | number) {
        return cachedFetch(`/api/v1/sites/${id}/daemons`);
    },
    async getSiteCrons(id: string | number) {
        return cachedFetch(`/api/v1/sites/${id}/crons`);
    },
    async getSiteActivity(siteId: string | number, limit = 5) {
        return cachedFetch(`/api/v1/system/activity?site_id=${siteId}&limit=${limit}`);
    },
    async createSiteDaemon(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        return result;
    },
    async createSiteCron(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/crons`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/crons`);
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
    async getSiteEnv(siteId: string | number) {
        return cachedFetch(`/api/v1/sites/${siteId}/env`);
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
        return result;
    },
};
