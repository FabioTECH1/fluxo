import { router } from '../router';
import { useToast } from '../composables/useToast';

const cache = new Map<string, { data: any; ts: number }>();
const CACHE_TTL = 30_000;

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
        const data = await res.json();
        if (data && data.token) {
            localStorage.setItem('fluxo_jwt', data.token);
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
    async getSites() { return cachedFetch('/api/v1/sites'); },
    async getPhpVersions() { return cachedFetch('/api/v1/server/php'); },
    async getSettings() { return cachedFetch('/api/v1/settings'); },
    async getGithubRepos() { return cachedFetch('/api/v1/github/repos'); },
    async getGithubBranches(repo: string) { return cachedFetch(`/api/v1/github/branches?repo=${encodeURIComponent(repo)}`); },
    async updateSettings(data: any) {
        const result = await cachedFetch('/api/v1/settings', { method: 'POST', body: JSON.stringify(data) });
        cache.clear(); return result;
    },
    async createSite(data: any) {
        const result = await cachedFetch('/api/v1/sites', { method: 'POST', body: JSON.stringify(data) });
        cache.clear(); return result;
    },
    async deleteSite(id: number) {
        const result = await cachedFetch(`/api/v1/sites/${id}`, { method: 'DELETE' });
        cache.clear(); return result;
    },
    async getMetrics() { return cachedFetch('/api/v1/system/metrics'); },
    async getDatabaseEngines() { return cachedFetch('/api/v1/server/engines'); },
    async installMySQL() {
        const result = await cachedFetch('/api/v1/server/engines/mysql/install', { method: 'POST' });
        cache.clear(); return result;
    },
    async installPostgres() {
        const result = await cachedFetch('/api/v1/server/engines/postgres/install', { method: 'POST' });
        cache.clear(); return result;
    },
    async installRedis() {
        const result = await cachedFetch('/api/v1/server/engines/redis/install', { method: 'POST' });
        cache.clear(); return result;
    },
    async getDatabases() { return cachedFetch('/api/v1/databases'); },
    async getDaemons() { return cachedFetch('/api/v1/daemons'); },
    async updatePassword(currentPassword: string, newPassword: string) {
        return cachedFetch('/api/v1/settings/password', {
            method: 'POST',
            body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
        });
    },
    async getSSHKeys() { return cachedFetch('/api/v1/ssh-keys'); },
    async addSSHKey(name: string, publicKey: string) {
        const result = await cachedFetch('/api/v1/ssh-keys', { method: 'POST', body: JSON.stringify({ name, public_key: publicKey }) });
        cache.clear(); return result;
    },
    async deleteSSHKey(id: number) {
        const result = await cachedFetch(`/api/v1/ssh-keys/${id}`, { method: 'DELETE' });
        cache.clear(); return result;
    },
    async getFirewallRules() { return cachedFetch('/api/v1/firewall'); },
    async addFirewallRule(name: string, port: string, fromIp: string, type: string = 'allow') {
        const result = await cachedFetch('/api/v1/firewall', { method: 'POST', body: JSON.stringify({ name, port, from_ip: fromIp, type }) });
        cache.clear(); return result;
    },
    async deleteFirewallRule(id: number) {
        const result = await cachedFetch(`/api/v1/firewall/${id}`, { method: 'DELETE' });
        cache.clear(); return result;
    },
};
