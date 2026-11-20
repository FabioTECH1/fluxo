import type { GitHubSiteAccess } from '../../../ui/src/utils/githubConnection';

// Illustrative demo failures stay observable after Retry check. Replacing a
// token can repair registration, but cannot restore a missing private key.
export function demoGitHubSiteAccess(sites: Array<{ id: number; domain: string; repository: string }>, repaired = false): GitHubSiteAccess[] {
  return sites.map((site, index) => {
    const base = { site_id: site.id, domain: site.domain, repository: site.repository };
    if (index === 0 && !repaired) return { ...base, status: 'error', reason: 'ssh_access_failed', message: 'SSH repository access failed; check the deploy key, repository access and connectivity.' };
    if (index === 1) return { ...base, status: 'error', reason: 'private_key_unavailable', message: 'Site private deploy key is missing or unreadable.' };
    if (index === 2) return { ...base, status: 'error', reason: 'check_timeout', message: 'Check timed out; retry Check access.' };
    return { ...base, status: 'verified', message: 'Demo SSH repository access verified.' };
  });
}
