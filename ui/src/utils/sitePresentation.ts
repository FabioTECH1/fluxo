type SitePresentation = {
  app_type?: string | null;
  php_version?: string | null;
  node_preset?: string | null;
  python_preset?: string | null;
};

const normalizedAppType = (site: SitePresentation) => String(site.app_type || 'php').trim().toLowerCase();

export const siteTypeLabel = (site: SitePresentation) => {
  const appType = normalizedAppType(site);

  if (appType === 'laravel') return 'Laravel';
  if (appType === 'php') return 'PHP';
  if (appType === 'wordpress') return 'WordPress';
  if (appType === 'html') return 'Static HTML';

  if (appType === 'node') {
    const preset = String(site.node_preset || '').trim().toLowerCase();
    if (preset === 'next') return 'Next.js';
    if (preset === 'nuxt') return 'Nuxt';
    return 'Node.js';
  }

  if (appType === 'python') {
    const preset = String(site.python_preset || '').trim().toLowerCase();
    if (preset === 'django') return 'Django';
    if (preset === 'flask') return 'Flask';
    if (preset === 'fastapi') return 'FastAPI';
    return 'Python';
  }

  return appType.charAt(0).toUpperCase() + appType.slice(1);
};

export const siteRuntimeLabel = (site: SitePresentation) => {
  const appType = normalizedAppType(site);
  const typeLabel = siteTypeLabel(site);
  const phpVersion = String(site.php_version || '').trim();

  if (!phpVersion || !['laravel', 'php', 'wordpress'].includes(appType)) return typeLabel;
  if (appType === 'php') return `PHP ${phpVersion}`;

  return `${typeLabel} (PHP ${phpVersion})`;
};
