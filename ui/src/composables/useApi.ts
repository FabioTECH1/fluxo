const cache = new Map<string, { data: any; ts: number }>();
const TTL = 30_000; // 30 seconds

export function useApi() {
  const token = () => localStorage.getItem('fluxo_jwt');

  const cachedFetch = async (url: string, init?: RequestInit, ttl = TTL) => {
    const cacheKey = `${init?.method || 'GET'}:${url}`;

    // Only cache GET requests
    if (!init?.method || init.method === 'GET') {
      const cached = cache.get(cacheKey);
      if (cached && Date.now() - cached.ts < ttl) {
        return cached.data;
      }
    }

    const headers: Record<string, string> = {};
    if (init?.headers) Object.assign(headers, init.headers as Record<string, string>);
    if (token()) headers['Authorization'] = `Bearer ${token()}`;
    if (!headers['Content-Type'] && !(init?.body instanceof FormData)) {
      headers['Content-Type'] = 'application/json';
    }

    const res = await window.fetch(url, { ...init, headers });

    if (res.status === 401) {
      localStorage.removeItem('fluxo_jwt');
      window.location.href = '/login';
      throw new Error('Unauthorized');
    }

    const data = await res.json().catch(() => null);
    if (data && (!init?.method || init.method === 'GET')) {
      cache.set(cacheKey, { data, ts: Date.now() });
    }

    return data;
  };

  const invalidate = (urlPattern?: string) => {
    if (!urlPattern) {
      cache.clear();
      return;
    }
    for (const key of cache.keys()) {
      if (key.includes(urlPattern)) cache.delete(key);
    }
  };

  return { cachedFetch, invalidate };
}
