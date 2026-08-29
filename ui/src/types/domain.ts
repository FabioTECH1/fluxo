export type WWWRedirectBehavior = 'none' | 'from_www' | 'to_www';

export const defaultWWWRedirect = (domain: string): WWWRedirectBehavior =>
  domain.trim().toLowerCase().startsWith('www.') ? 'none' : 'from_www';

export const wwwRedirectSummary = (behavior: WWWRedirectBehavior): string => {
  if (behavior === 'from_www') return 'Redirect from www.';
  if (behavior === 'to_www') return 'Redirect to www.';
  return 'No www redirect.';
};
