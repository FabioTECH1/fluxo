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
