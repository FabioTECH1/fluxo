export interface SiteRedirect {
  id: string;
  source: string;
  destination: string;
  status: 301 | 302;
}

export interface SiteNetworkState {
  managed: boolean;
  revision: string;
  pending: boolean;
  protection: { enabled: boolean; username: string };
  redirects: SiteRedirect[];
  wildcards: string[];
}

export const protectionUsernameError = (username: string): string =>
  /^[A-Za-z0-9_.@-]{1,64}$/.test(username) ? '' : 'Use 1–64 letters, numbers, or _ . @ -.';

export const protectionPasswordError = (password: string): string => {
  const bytes = new TextEncoder().encode(password).length;
  return bytes >= 8 && bytes <= 72 && !/[\u0000-\u001f\u007f]/.test(password) ? '' : 'Use 8–72 UTF-8 bytes without control characters.';
};

export const redirectSourceError = (source: string): string => {
  if (source.length > 1024 || source === '/.well-known/acme-challenge' || source.startsWith('/.well-known/acme-challenge/') || !/^\/[A-Za-z0-9/._~+-]*$/.test(source) || source.includes('//') || source.split('/').some(part => part === '.' || part === '..')) {
    return 'Use an exact path beginning with /. Queries, escapes, regular expressions and dot segments are not supported.';
  }
  return '';
};

export const redirectDestinationError = (destination: string): string => {
  const invalid = 'Use a local path or an http:// or https:// URL without credentials, control characters, $, quotes or backslashes.';
  if (!destination || destination.length > 2048 || /[\s\u0000-\u001f\u007f$'"\\;{}]/.test(destination)) return invalid;
  if (destination.startsWith('/') && !destination.startsWith('//')) return '';
  try {
    if (!/^https?:\/\//.test(destination)) return invalid;
    const url = new URL(destination);
    return /^https?:$/.test(url.protocol) && Boolean(url.hostname) && !url.username && !url.password ? '' : invalid;
  } catch { return invalid; }
};
