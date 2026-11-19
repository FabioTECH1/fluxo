export interface GitHubSiteAccess {
  site_id: number;
  domain: string;
  repository: string;
  status: 'unchecked' | 'verified' | 'error';
  message: string;
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

export async function siteGitOptions<R, B>(client: {
  getGithubRepos(accountId: number, refresh: boolean): Promise<R[]>;
  getGithubBranches(repo: string, accountId: number, refresh: boolean): Promise<B[]>;
}, accountId: number, repo: string, refresh = false): Promise<[R[], B[]]> {
  if (!accountId) return [[], []];
  const repos = client.getGithubRepos(accountId, refresh);
  const branches = repo ? client.getGithubBranches(repo, accountId, refresh) : Promise.resolve([]);
  if (refresh) return Promise.all([repos, branches]);
  return Promise.all([repos.catch(() => []), branches.catch(() => [])]);
}
