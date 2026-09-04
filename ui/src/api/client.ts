import { router } from '../router';
import { useToast } from '../composables/useToast';
import { useAuthStore } from '../stores/auth';
import { clearLogViewSnapshots } from '../utils/logViewCache';

const cache = new Map<string, { data: any; ts: number }>();
const pending = new Map<string, Promise<any>>();
const cacheVersions = new Map<string, number>();
const CACHE_TTL = 300_000; // 5 minutes

const assertValidSiteRequest = (url: string) => {
    const prefix = '/api/v1/sites/';
    if (!url.startsWith(prefix)) return;

    const siteId = url.slice(prefix.length).split(/[/?]/, 1)[0];
    if (!/^[1-9]\d*$/.test(siteId)) {
        throw new Error('A valid site ID is required');
    }
};

const bumpCacheVersion = (key: string) => {
    cacheVersions.set(key, (cacheVersions.get(key) || 0) + 1);
};

const invalidateCachePattern = (pattern: string) => {
    for (const key of cache.keys()) {
        if (key.includes(pattern)) {
            cache.delete(key);
            bumpCacheVersion(key);
        }
    }
    for (const key of pending.keys()) {
        if (key.includes(pattern)) {
            pending.delete(key);
            bumpCacheVersion(key);
        }
    }
};

const getHeaders = () => {
    const token = getToken();
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    return headers;
};

const getToken = () => {
    try {
        return useAuthStore().token;
    } catch {
        return localStorage.getItem('fluxo_jwt') || '';
    }
};

const clearToken = () => {
    clearLogViewSnapshots();
    try {
        useAuthStore().clearToken();
    } catch {
        localStorage.removeItem('fluxo_jwt');
    }
};

const setToken = (token: string) => {
    clearLogViewSnapshots();
    try {
        useAuthStore().setToken(token);
    } catch {
        localStorage.setItem('fluxo_jwt', token);
    }
};

const cachedFetch = async (url: string, init?: RequestInit & { bypassCache?: boolean; useCache?: boolean }): Promise<any> => {
    assertValidSiteRequest(url);
    const cacheKey = `${init?.method || 'GET'}:${url}`;
    const isGet = !init?.method || init.method === 'GET';
    const useCache = isGet && init?.useCache !== false;
    const bypassCache = !!init?.bypassCache;
    if (useCache && !bypassCache) {
        const hit = cache.get(cacheKey);
        if (hit && Date.now() - hit.ts < CACHE_TTL) return hit.data;
        const pendingHit = pending.get(cacheKey);
        if (pendingHit) return pendingHit;
    }

    if (useCache && bypassCache) {
        bumpCacheVersion(cacheKey);
    }

    const cacheVersion = cacheVersions.get(cacheKey) || 0;
    const request = (async () => {
        const headers = getHeaders();
        if (init?.headers) Object.assign(headers, init.headers);

        const res = await fetch(url, { ...init, headers });

        if (res.status === 401) {
            clearToken();
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

        const text = await res.text();
        if (!text) return null;

        let data;
        try {
            data = JSON.parse(text);
        } catch (e) {
            data = text;
        }

        if (useCache && (cacheVersions.get(cacheKey) || 0) === cacheVersion) {
            cache.set(cacheKey, { data, ts: Date.now() });
        }
        return data;
    })();

    if (useCache) {
        pending.set(cacheKey, request);
        request.finally(() => {
            if (pending.get(cacheKey) === request) pending.delete(cacheKey);
        }).catch(() => {});
    }

    return request;
};

const authenticatedFetch = async (url: string, init?: RequestInit): Promise<Response> => {
    assertValidSiteRequest(url);
    const headers = new Headers(init?.headers);
    const token = getToken();
    if (token) headers.set('Authorization', `Bearer ${token}`);

    const res = await fetch(url, { ...init, headers });
    if (res.status === 401) {
        clearToken();
        if (window.location.pathname !== '/login') {
            const { addToast } = useToast();
            addToast('Session expired or unauthorized. Please sign in again.', 'error');
            router.push('/login');
        }
        throw new Error('Unauthorized');
    }
    if (!res.ok) {
        const message = (await res.text()).trim();
        throw new Error(message || 'Request failed');
    }
    return res;
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
            setToken(data.token);
            // Warm cache with common endpoints so first page loads instantly
            Promise.allSettled([
                cachedFetch('/api/v1/sites'),
                cachedFetch('/api/v1/settings', { useCache: true }),
                cachedFetch('/api/v1/ssh-keys'),
                cachedFetch('/api/v1/firewall'),
            ]).catch(() => {});
            return data.token;
        }
        throw new Error('No token returned');
    },
    logout() {
        cache.clear();
        pending.clear();
        cacheVersions.clear();
        clearToken();
        router.push('/login');
    },
    isAuthenticated() {
        return !!getToken();
    },
    get(url: string, options?: { bypassCache?: boolean; useCache?: boolean }) { return cachedFetch(url, options); },
    post(url: string, data?: any) {
        return cachedFetch(url, { method: 'POST', body: data ? JSON.stringify(data) : undefined });
    },
    put(url: string, data?: any) {
        return cachedFetch(url, { method: 'PUT', body: data ? JSON.stringify(data) : undefined });
    },
    delete(url: string) { return cachedFetch(url, { method: 'DELETE' }); },
    invalidate(pattern: string) { invalidateCachePattern(pattern); },
    async getSites(bypassCache = false) { return cachedFetch('/api/v1/sites', { bypassCache }); },
    async getPhpVersions(bypassCache = false) { return cachedFetch('/api/v1/server/php', { bypassCache, useCache: true }); },
    async getSettings(bypassCache = false) { return cachedFetch('/api/v1/settings', { bypassCache, useCache: true }); },
    async getPanelDomain(bypassCache = false) {
        return cachedFetch('/api/v1/settings/panel-domain', { bypassCache, useCache: true });
    },
    async connectPanelDomainLetsEncrypt(domain: string) {
        const result = await cachedFetch('/api/v1/settings/panel-domain/letsencrypt', {
            method: 'POST', body: JSON.stringify({ domain })
        });
        invalidateCachePattern('/api/v1/settings/panel-domain');
        return result;
    },
    async connectPanelDomainCustom(domain: string, certificate: string, privateKey: string) {
        const result = await cachedFetch('/api/v1/settings/panel-domain/custom', {
            method: 'POST', body: JSON.stringify({ domain, certificate, private_key: privateKey })
        });
        invalidateCachePattern('/api/v1/settings/panel-domain');
        return result;
    },
    async getPanelCloneableCertificates(domain: string, bypassCache = false) {
        return cachedFetch(`/api/v1/settings/panel-domain/cloneable?domain=${encodeURIComponent(domain)}`, { bypassCache });
    },
    async connectPanelDomainClone(domain: string, certificateId: number) {
        const result = await cachedFetch('/api/v1/settings/panel-domain/clone', {
            method: 'POST', body: JSON.stringify({ domain, certificate_id: certificateId })
        });
        invalidateCachePattern('/api/v1/settings/panel-domain');
        return result;
    },
    async removePanelDomain() {
        const result = await cachedFetch('/api/v1/settings/panel-domain', { method: 'DELETE' });
        invalidateCachePattern('/api/v1/settings/panel-domain');
        return result;
    },
    async getGithubAccounts(bypassCache = false) { return cachedFetch('/api/v1/github/accounts', { bypassCache, useCache: true }); },
    async connectGithubAccount(data: { name?: string; token: string }) {
        const result = await cachedFetch('/api/v1/github/accounts', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/github/accounts');
        return result;
    },
    async disconnectGithubAccount(id: number) {
        const result = await cachedFetch(`/api/v1/github/accounts/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/github/accounts');
        return result;
    },
    async getGithubRepos(accountId?: number, bypassCache = false) {
        const url = accountId ? `/api/v1/github/repos?account_id=${accountId}` : '/api/v1/github/repos';
        return cachedFetch(url, { bypassCache, useCache: true });
    },
    async getGithubBranches(repo: string, accountId?: number, bypassCache = false) {
        const url = accountId
            ? `/api/v1/github/branches?repo=${encodeURIComponent(repo)}&account_id=${accountId}`
            : `/api/v1/github/branches?repo=${encodeURIComponent(repo)}`;
        return cachedFetch(url, { bypassCache, useCache: true });
    },
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
    async deleteSite(id: number, deleteDatabases = false, databaseIds: number[] = []) {
        const params = new URLSearchParams();
        if (deleteDatabases) {
            params.set('delete_databases', 'true');
            params.set('database_ids', databaseIds.join(','));
        }
        const query = params.size > 0 ? `?${params.toString()}` : '';
        try {
            return await cachedFetch(`/api/v1/sites/${id}${query}`, { method: 'DELETE' });
        } finally {
            invalidateCachePattern('/api/v1/sites');
            invalidateCachePattern('/api/v1/databases');
        }
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
    async getSSHSecurity() {
        return cachedFetch('/api/v1/ssh/security', { bypassCache: true, useCache: false, cache: 'no-store' });
    },
    async enableSSHHardening() {
        const result = await cachedFetch('/api/v1/ssh/security/harden', {
            method: 'POST',
            body: JSON.stringify({ key_access_confirmed: true, recovery_access_confirmed: true })
        });
        invalidateCachePattern('/api/v1/ssh/security');
        return result;
    },
    async disableSSHHardening() {
        const result = await cachedFetch('/api/v1/ssh/security/hardening', { method: 'DELETE' });
        invalidateCachePattern('/api/v1/ssh/security');
        return result;
    },
    async addSSHKey(name: string, publicKey: string) {
        const result = await cachedFetch('/api/v1/ssh-keys', { method: 'POST', body: JSON.stringify({ name, public_key: publicKey }) });
        invalidateCachePattern('/api/v1/ssh-keys');
        invalidateCachePattern('/api/v1/ssh/security');
        return result;
    },
    async deleteSSHKey(id: number) {
        const result = await cachedFetch(`/api/v1/ssh-keys/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/ssh-keys');
        invalidateCachePattern('/api/v1/ssh/security');
        return result;
    },
    async getFirewallRules() {
        return cachedFetch('/api/v1/firewall', { bypassCache: true, useCache: false, cache: 'no-store' });
    },
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
    async getBootstrapCredentials(bypassCache = false) {
        return cachedFetch('/api/v1/settings/bootstrap-credentials', { bypassCache, useCache: false, cache: 'no-store' });
    },
    async getBootstrapCredentialsStatus() {
        return cachedFetch('/api/v1/settings/bootstrap-credentials/status', { bypassCache: true, useCache: false, cache: 'no-store' });
    },
    async downloadBootstrapCredentials() {
        return cachedFetch('/api/v1/settings/bootstrap-credentials/download', { bypassCache: true, useCache: false, cache: 'no-store' });
    },
    async markCredentialsCopied() {
        const result = await cachedFetch('/api/v1/settings/bootstrap-credentials/copied', { method: 'POST' });
        invalidateCachePattern('/api/v1/settings/bootstrap-credentials');
        return result;
    },
    // Site-Specific APIs
    async getSite(id: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${id}`, { bypassCache });
    },
    async getSiteVhost(siteId: string | number) {
        return cachedFetch(`/api/v1/sites/${siteId}/vhost`, { bypassCache: true, useCache: false, cache: 'no-store' });
    },
    async updateSiteVhost(siteId: string | number, config: string, expectedRevision: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/vhost`, {
            method: 'PUT',
            body: JSON.stringify({ config, expected_revision: expectedRevision }),
            useCache: false,
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/vhost`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async restoreSiteVhost(siteId: string | number, expectedRevision: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/vhost/restore`, {
            method: 'POST',
            body: JSON.stringify({ expected_revision: expectedRevision }),
            useCache: false,
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/vhost`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
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
    async updateSiteDaemonDeploymentPolicy(siteId: string | number, daemonId: number, restartOnDeploy: boolean) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/daemons/${daemonId}`, {
            method: 'PUT',
            body: JSON.stringify({ restart_on_deploy: restartOnDeploy }),
        });
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
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    async toggleSiteHorizon(siteId: string | number, enable: boolean) {
        const action = enable ? 'enable' : 'disable';
        const result = await cachedFetch(`/api/v1/sites/${siteId}/features/horizon/${action}`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/features`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async toggleSiteQueueWorker(siteId: string | number, enable: boolean, settings?: any) {
        const action = enable ? 'enable' : 'disable';
        const result = await cachedFetch(`/api/v1/sites/${siteId}/features/queue-worker/${action}`, {
            method: 'POST',
            body: enable ? JSON.stringify(settings || {}) : undefined,
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/features`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        return result;
    },
    async toggleSiteOctane(siteId: string | number, enable: boolean) {
        const action = enable ? 'enable' : 'disable';
        const result = await cachedFetch(`/api/v1/sites/${siteId}/features/octane/${action}`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/features`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/daemons`);
        invalidateCachePattern('/api/v1/daemons');
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
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
    async getSiteFiles(siteId: string | number, path = '.', hidden = false, offset = 0, limit = 250) {
        const params = new URLSearchParams({
            path,
            hidden: String(hidden),
            offset: String(offset),
            limit: String(limit),
        });
        return cachedFetch(`/api/v1/sites/${siteId}/files?${params}`, { bypassCache: true, useCache: false, cache: 'no-store' });
    },
    async getSiteFileContent(siteId: string | number, path: string) {
        return cachedFetch(`/api/v1/sites/${siteId}/files/content?path=${encodeURIComponent(path)}`, { bypassCache: true, useCache: false, cache: 'no-store' });
    },
    async saveSiteFileContent(siteId: string | number, path: string, content: string, sha256: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/files/content`, {
            method: 'PUT',
            body: JSON.stringify({ path, content, sha256 }),
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/files`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async createSiteFileEntry(siteId: string | number, path: string, type: 'file' | 'directory') {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/files/entries`, {
            method: 'POST',
            body: JSON.stringify({ path, type }),
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/files`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async moveSiteFileEntry(siteId: string | number, source: string, destination: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/files/move`, {
            method: 'POST',
            body: JSON.stringify({ source, destination }),
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/files`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async deleteSiteFileEntry(siteId: string | number, path: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/files?path=${encodeURIComponent(path)}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/files`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async uploadSiteFile(siteId: string | number, path: string, file: File, overwrite = false) {
        const params = new URLSearchParams({ path, name: file.name, overwrite: String(overwrite) });
        const res = await authenticatedFetch(`/api/v1/sites/${siteId}/files/upload?${params}`, {
            method: 'POST',
            headers: { 'Content-Type': file.type || 'application/octet-stream' },
            body: file,
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/files`);
        invalidateCachePattern('/api/v1/system/activity');
        return res.json();
    },
    async downloadSiteFile(siteId: string | number, path: string) {
        const res = await authenticatedFetch(`/api/v1/sites/${siteId}/files/download?path=${encodeURIComponent(path)}`, { cache: 'no-store' });
        return res.blob();
    },
    async triggerSiteDeploy(siteId: string | number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/deploy`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/deployments`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async dismissDeploymentFailure(siteId: string | number, depId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/deployments/${depId}/dismiss`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/deployments`);
        return result;
    },
    async rollbackDeployment(siteId: string | number, depId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/deployments/${depId}/rollback`, { method: 'POST' });
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
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        return result;
    },
    async updateSiteDomain(siteId: string | number, domainId: number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}`, { method: 'PUT', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        return result;
    },
    async deleteSiteDomain(siteId: string | number, domainId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        return result;
    },
    async promoteSiteDomain(siteId: string | number, domainId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}/primary`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        invalidateCachePattern('/api/v1/sites');
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async installDomainLetsEncryptSSL(siteId: string | number, domainId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}/ssl/letsencrypt`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        return result;
    },
    async activateDomainCert(siteId: string | number, domainId: number, certId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}/ssl/certificates/${certId}/activate`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        return result;
    },
    async deactivateDomainSSL(siteId: string | number, domainId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/domains/${domainId}/ssl`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        return result;
    },
    // SSL Certificates
    async getSiteCertificates(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/certificates`, { bypassCache });
    },
    async getCloneableCertificates(siteId: string | number, bypassCache = false, domainId = 0) {
        const query = domainId > 0 ? `?domain_id=${domainId}` : '';
        return cachedFetch(`/api/v1/sites/${siteId}/ssl/cloneable${query}`, { bypassCache });
    },
    async cloneCertificate(siteId: string | number, certificateId: number, domainId = 0) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/clone`, {
            method: 'POST',
            body: JSON.stringify({ certificate_id: certificateId, domain_id: domainId })
        });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/ssl/cloneable`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async installLetsEncryptSSL(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/letsencrypt`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async installCustomSSL(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/custom`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async activateCert(siteId: string | number, certId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/certificates/${certId}/activate`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
        invalidateCachePattern(`/api/v1/sites/${siteId}`);
        return result;
    },
    async deactivateCert(siteId: string | number, certId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/ssl/certificates/${certId}/deactivate`, { method: 'POST' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/certificates`);
        invalidateCachePattern(`/api/v1/sites/${siteId}/domains`);
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
    async getSiteCommands(siteId: string | number, page = 1, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/commands?page=${page}`, { bypassCache });
    },
    async getSiteCommand(siteId: string | number, commandId: number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/commands/${commandId}`, { bypassCache });
    },
    async runSiteCommand(siteId: string | number, data: any) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/commands`, { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/commands`);
        invalidateCachePattern('/api/v1/system/activity');
        return result;
    },
    async deleteSiteCommand(siteId: string | number, commandId: number) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/commands/${commandId}`, { method: 'DELETE' });
        invalidateCachePattern(`/api/v1/sites/${siteId}/commands`);
        return result;
    },
    async getWordPressConfig(siteId: string | number, bypassCache = false) {
        return cachedFetch(`/api/v1/sites/${siteId}/wordpress-config`, { bypassCache });
    },
    async saveWordPressConfig(siteId: string | number, content: string) {
        const result = await cachedFetch(`/api/v1/sites/${siteId}/wordpress-config`, { method: 'POST', body: JSON.stringify({ content }) });
        invalidateCachePattern(`/api/v1/sites/${siteId}/wordpress-config`);
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
        invalidateCachePattern('/api/v1/databases/users/grants');
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
        invalidateCachePattern('/api/v1/databases/users/grants');
        return result;
    },
    async deleteDatabaseUser(id: number) {
        const result = await cachedFetch(`/api/v1/databases/users/${id}`, { method: 'DELETE' });
        invalidateCachePattern('/api/v1/databases/users');
        invalidateCachePattern('/api/v1/databases/users/grants');
        return result;
    },
    async getDatabaseUserGrants() {
        return cachedFetch('/api/v1/databases/users/grants');
    },
    async createDatabaseUserGrant(data: any) {
        const result = await cachedFetch('/api/v1/databases/users/grants', { method: 'POST', body: JSON.stringify(data) });
        invalidateCachePattern('/api/v1/databases/users/grants');
        invalidateCachePattern('/api/v1/databases/users');
        invalidateCachePattern('/api/v1/databases');
        return result;
    },
    async rotateDatabaseUserPassword(data: { user: string; password: string; engine: string }) {
        return cachedFetch('/api/v1/databases/users/password', { method: 'POST', body: JSON.stringify(data) });
    },
    // System logs
    async getSystemLogs(path: string, lines = 100, bypassCache = false) {
        return cachedFetch(`/api/v1/system/logs?path=${encodeURIComponent(path)}&lines=${lines}`, { bypassCache });
    },
    async downloadSystemLog(path: string) {
        const headers = getHeaders();
        const res = await fetch(`/api/v1/system/logs/download?path=${encodeURIComponent(path)}`, { headers });
        if (!res.ok) {
            const message = (await res.text()).trim();
            throw new Error(message || 'Download failed');
        }
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
    async getUpdateStatus() { return cachedFetch('/api/v1/update-status'); },
    async getBootstrapStatus() { return cachedFetch('/api/v1/auth/bootstrap'); },
};
