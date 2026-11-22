export type WWWRedirectBehavior = 'none' | 'from_www' | 'to_www';

export interface WWWDomainClassification {
  isSubdomain: boolean;
  defaultRedirect: WWWRedirectBehavior;
}

export const classifyWWWDomain = async (domain: string): Promise<WWWDomainClassification> => {
  const normalized = domain.trim().toLowerCase();
  if (!normalized || normalized.startsWith('www.')) {
    return { isSubdomain: normalized.startsWith('www.'), defaultRedirect: 'none' };
  }
  if (!normalized.includes('.')) {
    return { isSubdomain: false, defaultRedirect: 'none' };
  }

  const { parse } = await import('tldts');
  const parsed = parse(normalized, { allowPrivateDomains: true });
  const isSubdomain = Boolean(parsed.subdomain) || Boolean(parsed.isPrivate);
  return {
    isSubdomain,
    defaultRedirect: parsed.isIcann === true && parsed.domain === normalized && !isSubdomain ? 'from_www' : 'none',
  };
};

export const wwwRedirectSummary = (behavior: WWWRedirectBehavior): string => {
  if (behavior === 'from_www') return 'Redirect from www.';
  if (behavior === 'to_www') return 'Redirect to www.';
  return 'No www redirect.';
};

export interface DomainItem {
  revision?: string;
  id: number;
  site_id?: number;
  domain: string;
  primary: boolean;
  ssl_active: boolean;
  ssl_inherited?: boolean;
  certificate_id?: number;
  www_redirect: WWWRedirectBehavior;
  wildcard: boolean;
  wildcard_ssl_active: boolean;
  wildcard_ssl_state: 'disabled' | 'pending' | 'active' | 'inactive_custom';
}

export const normalizeDomainInput = (value: string) => {
  const domain = value.trim().toLowerCase();
  return { domain: domain.replace(/^\*\./, ''), wildcard: domain.startsWith('*.') };
};

export const certificateHostnames = (target: Pick<DomainItem, 'domain' | 'www_redirect' | 'wildcard'>): string[] => {
  const hostname = target.domain.trim().toLowerCase();
  if (!hostname) return [];
  const hosts = [hostname];
  if (!hostname.startsWith('www.') && target.www_redirect !== 'none') hosts.push(`www.${hostname}`);
  if (target.wildcard) hosts.push(`*.${hostname}`);
  return hosts;
};
