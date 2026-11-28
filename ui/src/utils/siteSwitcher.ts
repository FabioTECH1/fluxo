export interface SwitchableSite { id: number | string; domain: string; app_type: string; php_version?: string; repository?: string; branch?: string }
export function siteSummary(site: SwitchableSite): string {
  const labels: Record<string,string> = { php: 'PHP', laravel: 'Laravel', wordpress: 'WordPress', html: 'Static HTML', node: 'Node.js', python: 'Python' };
  const type = labels[site.app_type] || site.app_type || 'Application';
  const php = ['php','laravel','wordpress'].includes(site.app_type) && site.php_version ? ` (${site.app_type === 'php' ? '' : 'PHP '}${site.php_version})` : '';
  return type + php + (site.repository ? ` · ${site.repository}${site.branch ? ':' + site.branch : ''}` : '');
}
export function switchedSitePath(path: string, site: SwitchableSite): string {
  const section = path.replace(/^\/sites\/[^/]+\/?/, '').replace(/\/+$/, '');
  if (section === 'settings/wordpress' && site.app_type !== 'wordpress') return `/sites/${site.id}/settings/general`;
  if (section === 'commands/tinker' && site.app_type !== 'laravel') return `/sites/${site.id}/overview`;
  const supported = /^(overview|deployments|processes(\/(daemons|scheduler))?|commands(\/tinker)?|observe(\/(logs|activity|traffic))?|domains(\/(list|ssl))?|settings(\/(general|deployments|environment|network|vhost|wordpress))?)$/;
  const fallback = section.startsWith('settings/') ? 'settings/general' : 'overview';
  return `/sites/${site.id}/${supported.test(section) ? section : fallback}`;
}
