import { router } from '../router';

const getHeaders = () => {
    const token = localStorage.getItem('fluxo_jwt');
    const headers: Record<string, string> = {
        'Content-Type': 'application/json'
    };
    if (token) {
        headers['Authorization'] = `Bearer ${token}`;
    }
    return headers;
};

const handleResponse = async (res: Response) => {
    if (res.status === 401) {
        localStorage.removeItem('fluxo_jwt');
        // Do not redirect if we are already on the login page to prevent loops
        if (window.location.pathname !== '/login') {
            router.push('/login');
        }
        throw new Error('Unauthorized');
    }
    if (!res.ok) {
        const err = await res.text();
        throw new Error(err || 'Request failed');
    }
    if (res.status === 204) {
        return null;
    }
    return res.json();
};

export const apiClient = {
    async login(username: string, token: string) {
        const res = await fetch('/api/v1/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password: token })
        });
        const data = await handleResponse(res);
        if (data && data.token) {
            localStorage.setItem('fluxo_jwt', data.token);
            return data.token;
        }
        throw new Error('No token returned');
    },
    logout() {
        localStorage.removeItem('fluxo_jwt');
        router.push('/login');
    },
    isAuthenticated() {
        return !!localStorage.getItem('fluxo_jwt');
    },
    async getSites() {
        const res = await fetch('/api/v1/sites', { headers: getHeaders() });
        return handleResponse(res);
    },
    async getPhpVersions() {
        const res = await fetch('/api/v1/server/php', { headers: getHeaders() });
        return handleResponse(res);
    },
    async getSettings() {
        const res = await fetch('/api/v1/settings', { headers: getHeaders() });
        return handleResponse(res);
    },
    async getGithubRepos() {
        const res = await fetch('/api/v1/github/repos', { headers: getHeaders() });
        return handleResponse(res);
    },
    async updateSettings(data: any) {
        const res = await fetch('/api/v1/settings', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify(data)
        });
        return handleResponse(res);
    },
    async createSite(data: any) {
        const res = await fetch('/api/v1/sites', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify(data)
        });
        return handleResponse(res);
    },
    async deleteSite(id: number) {
        const res = await fetch(`/api/v1/sites/${id}`, { 
            method: 'DELETE',
            headers: getHeaders()
        });
        return handleResponse(res);
    },
    async getMetrics() {
        const res = await fetch('/api/v1/system/metrics', { headers: getHeaders() });
        return handleResponse(res);
    },
    async getDatabaseEngines() {
        const res = await fetch('/api/v1/server/engines', { headers: getHeaders() });
        return handleResponse(res);
    },
    async getDatabases() {
        const res = await fetch('/api/v1/databases', { headers: getHeaders() });
        return handleResponse(res);
    },
    async getDaemons() {
        const res = await fetch('/api/v1/daemons', { headers: getHeaders() });
        return handleResponse(res);
    },
    async updatePassword(currentPassword: string, newPassword: string) {
        const res = await fetch('/api/v1/settings/password', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
        });
        return handleResponse(res);
    },
    async getSSHKeys() {
        const res = await fetch('/api/v1/ssh-keys', { headers: getHeaders() });
        return handleResponse(res);
    },
    async addSSHKey(name: string, publicKey: string) {
        const res = await fetch('/api/v1/ssh-keys', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify({ name, public_key: publicKey })
        });
        return handleResponse(res);
    },
    async deleteSSHKey(id: number) {
        const res = await fetch(`/api/v1/ssh-keys/${id}`, {
            method: 'DELETE',
            headers: getHeaders()
        });
        return handleResponse(res);
    },
    async getFirewallRules() {
        const res = await fetch('/api/v1/firewall', { headers: getHeaders() });
        return handleResponse(res);
    },
    async addFirewallRule(name: string, port: string, fromIp: string, type: string = 'allow') {
        const res = await fetch('/api/v1/firewall', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify({ name, port, from_ip: fromIp, type })
        });
        return handleResponse(res);
    },
    async deleteFirewallRule(id: number) {
        const res = await fetch(`/api/v1/firewall/${id}`, {
            method: 'DELETE',
            headers: getHeaders()
        });
        return handleResponse(res);
    }
};
