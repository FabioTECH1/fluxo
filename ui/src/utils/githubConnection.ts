import { apiErrorMessage } from './apiError.ts';
export interface GitHubSiteAccess {
  site_id: number;
  domain: string;
  repository: string;
  status: 'unchecked' | 'verified' | 'error';
  message: string;
  reason?: 'private_key_unavailable' | 'ssh_access_failed' | 'check_timeout' | 'check_canceled' | 'site_changed' | 'registration_failed' | 'check_failed';
}
export interface GitHubAccount {
  github_user_id: number;
  id: number;
  name: string;
  username: string;
  created_at: string;
  updated_at: string;
  token_expires_at: string | null;
  token_status: 'unchecked' | 'valid' | 'invalid' | 'error';
  token_checked_at: string | null;
  linked_sites: number;
  deployment_access: { checked_at: string | null; sites: GitHubSiteAccess[] };
}
export interface GitHubLegacyRecovery {
  password: string;
  username: string;
  confirm: boolean;
  sites: Array<{ site_id: number; repository: string }>;
  github_user_id?: number;
}
export interface GitHubRecoveryReview {
  stored: { name?: string; username: string; github_user_id: number };
  proposed: { username: string; github_user_id: number };
  sites: GitHubSiteAccess[];
  needs_recovery: boolean;
}
export function requiresLegacyGitHubRecovery(account: GitHubAccount | null, message: string): boolean {
  return !!account && !account.github_user_id && message.includes('The saved credential could not verify this legacy GitHub account.');
}
export function tokenPresentation(account: GitHubAccount, now = Date.now()): {
  label: string; variant: 'green' | 'red' | 'yellow' | 'gray'; reconnect: boolean; remaining: string;
} {
  const expiry = account.token_expires_at ? Date.parse(account.token_expires_at) : NaN;
  if (Number.isFinite(expiry) && expiry <= now) return { label: 'Expired', variant: 'red', reconnect: true, remaining: 'Expired' };
  if (account.token_status === 'invalid') return { label: 'Reconnect required', variant: 'red', reconnect: true, remaining: '' };
  if (account.token_status === 'error') return { label: 'Check unavailable', variant: 'yellow', reconnect: false, remaining: '' };
  if (account.token_status !== 'valid') return { label: 'Not checked', variant: 'gray', reconnect: false, remaining: '' };
  const days = Math.ceil((expiry - now) / 86400000);
  if (Number.isFinite(days) && days <= 14) return { label: 'Expires soon', variant: 'yellow', reconnect: false, remaining: `${days} ${days === 1 ? 'day' : 'days'} left` };
  return { label: 'Connected', variant: 'green', reconnect: false, remaining: Number.isFinite(days) ? `${days} days left` : '' };
}
export function deploymentAccessLabel(account: GitHubAccount): string {
  if (!account.linked_sites) return 'No linked sites';
  if (!account.deployment_access?.checked_at) return 'Not checked';
  const verified = account.deployment_access.sites.filter(site => site.status === 'verified').length;
  return verified === account.linked_sites ? `${verified} ${verified === 1 ? 'site' : 'sites'} verified` : `${verified} of ${account.linked_sites} sites verified`;
}
export function formatGitHubDate(value: string | null): string {
  if (!value || !Number.isFinite(Date.parse(value))) return 'Unavailable';
  return new Date(value).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function savedGitOptions(options: Array<{ label: string; value: string }>, value: string): Array<{ label: string; value: string }> {
  if (!value || options.some(option => option.value === value)) return options;
  // Listing availability is independent of the site's persisted selection.
  // Keep that selection visible without asserting GitHub access or changing it.
  return [...options, { label: value, value }];
}

export async function siteGitOptions<R, B>(client: {
  getGithubRepos(accountId: number, refresh: boolean): Promise<R[]>;
  getGithubBranches(repo: string, accountId: number, refresh: boolean): Promise<B[]>;
}, accountId: number, repo: string, refresh = false): Promise<{ repos: R[]; branches: B[]; errors: string[] }> {
  if (!accountId) return { repos: [], branches: [], errors: [] };
  const results = await Promise.allSettled([
    client.getGithubRepos(accountId, refresh),
    repo ? client.getGithubBranches(repo, accountId, refresh) : Promise.resolve([]),
  ]);
  const errors = results.flatMap((result, index) => result.status === 'rejected'
    ? [`${index === 0 ? 'Repositories' : 'Branches'}: ${apiErrorMessage(result.reason, 'GitHub options could not be loaded.')}`]
    : []);
  return {
    repos: results[0]!.status === 'fulfilled' && Array.isArray(results[0]!.value) ? results[0]!.value as R[] : [],
    branches: results[1]!.status === 'fulfilled' && Array.isArray(results[1]!.value) ? results[1]!.value as B[] : [],
    errors,
  };
}

export function githubTokenAction(account: GitHubAccount, now = Date.now()): string {
  const token = tokenPresentation(account, now);
  if (token.reconnect) return 'Reconnect';
  const registrationFailed = account.token_status === 'valid' && !!account.deployment_access?.checked_at
    && account.deployment_access.sites.some(site => site.status === 'error' && site.reason === 'registration_failed');
  return registrationFailed ? 'Update token & repair' : 'Update token';
}
export function githubAccessNeedsTokenUpdate(account: GitHubAccount, now = Date.now()): boolean {
  if (tokenPresentation(account, now).reconnect) return true;
  // A timeout or unavailable private key is not evidence that replacing the
  // token will help. SSH failure permits review without diagnosing its cause.
  return account.token_status === 'valid' && !!account.deployment_access?.checked_at
    && account.deployment_access.sites.some(site => site.status === 'error'
      && (site.reason === 'registration_failed' || site.reason === 'ssh_access_failed'));
}
export function githubAccessNotice(account: GitHubAccount, now = Date.now()): string {
  const token = tokenPresentation(account, now);
  if (token.label === 'Expired') return 'Token expired. Reconnect with a new token for the same GitHub account.';
  if (token.reconnect) return 'Token rejected. Reconnect with a new token for the same GitHub account.';
  if (account.token_status === 'error') return 'GitHub validation is unavailable. Retry check; expiry is unconfirmed.';
  if (account.token_status !== 'valid') return 'Token not checked. Retry check to verify account and SSH access.';
  if (!account.linked_sites) return 'Token valid. No repositories linked.';
  if (!account.deployment_access?.checked_at) return 'Token valid. Deployment access has not been checked.';
  return account.deployment_access.sites.length === account.linked_sites && account.deployment_access.sites.every(site => site.status === 'verified')
    ? 'Token valid. Deployment access verified.' : 'Token valid. Deployment access needs attention.';
}
export function githubSiteAccessPresentation(site: GitHubSiteAccess): { label: string; variant: 'green' | 'yellow' | 'gray'; message: string } {
  if (site.status === 'verified') return { label: 'Verified', variant: 'green', message: 'SSH repository access verified.' };
  if (site.status === 'unchecked') return { label: 'Not checked', variant: 'gray', message: 'Retry check to verify SSH repository access.' };
  switch (site.reason) {
    case 'private_key_unavailable': return { label: 'Private key unavailable', variant: 'yellow', message: 'Key missing, unreadable or invalid. Reconnect cannot restore it; restore it on the server.' };
    case 'check_timeout': return { label: 'Check timed out', variant: 'yellow', message: 'Access unconfirmed. Retry check.' };
    case 'check_canceled': return { label: 'Check canceled', variant: 'yellow', message: 'Access unconfirmed. Retry check.' };
    case 'ssh_access_failed': return { label: 'Needs attention', variant: 'yellow', message: 'SSH access failed. Check the deploy key, repository access or connectivity.' };
    case 'site_changed': return { label: 'Check again', variant: 'yellow', message: 'Site settings changed. Retry check.' };
    case 'registration_failed': return { label: 'Registration failed', variant: 'yellow', message: site.message || 'Deploy key registration failed. Review token permissions and retry token update.' };
    default: return { label: 'Needs attention', variant: 'yellow', message: site.message || 'Access could not be verified. Retry check.' };
  }
}
