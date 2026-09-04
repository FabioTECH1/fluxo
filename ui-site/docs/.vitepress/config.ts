import { defineConfig } from 'vitepress'

const docsUrl = 'https://fluxo.fottify.com/docs/'
const defaultDescription = 'Install, configure, deploy, and operate sites with Fluxo.'

function canonicalUrl(relativePath: string) {
  const cleanPath = relativePath
    .replace(/(^|\/)index\.md$/, '$1')
    .replace(/\.md$/, '')

  return new URL(cleanPath, docsUrl).toString()
}

export default defineConfig({
  title: 'Fluxo Documentation',
  description: defaultDescription,
  base: '/docs/',
  outDir: '../dist/docs',
  cleanUrls: true,
  lastUpdated: true,
  sitemap: {
    hostname: 'https://fluxo.fottify.com/docs/',
  },
  head: [
    ['link', { rel: 'icon', href: '/favicon.png' }],
    ['meta', { name: 'theme-color', content: '#2563eb' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Fluxo Documentation' }],
    ['meta', { property: 'og:image', content: 'https://fluxo.fottify.com/og-image.png' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
  ],
  transformHead({ pageData }) {
    const canonical = canonicalUrl(pageData.relativePath)
    const title = pageData.title || 'Fluxo Documentation'
    const description = pageData.description || defaultDescription
    const isHome = pageData.relativePath === 'index.md'
    const structuredData = isHome
      ? {
          '@context': 'https://schema.org',
          '@type': 'WebSite',
          name: 'Fluxo Documentation',
          url: docsUrl,
          description,
          publisher: {
            '@type': 'Organization',
            name: 'Fluxo',
            url: 'https://fluxo.fottify.com/',
          },
        }
      : {
          '@context': 'https://schema.org',
          '@type': 'TechArticle',
          headline: title,
          description,
          url: canonical,
          isPartOf: {
            '@type': 'WebSite',
            name: 'Fluxo Documentation',
            url: docsUrl,
          },
          publisher: {
            '@type': 'Organization',
            name: 'Fluxo',
            url: 'https://fluxo.fottify.com/',
          },
        }

    return [
      ['link', { rel: 'canonical', href: canonical }],
      ['meta', { property: 'og:url', content: canonical }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['script', { type: 'application/ld+json' }, JSON.stringify(structuredData)],
    ]
  },
  markdown: {
    lineNumbers: true,
  },
  themeConfig: {
    logo: '/logo.png',
    siteTitle: 'Fluxo Docs',
    search: {
      provider: 'local',
      options: {
        detailedView: true,
      },
    },
    nav: [
      { text: 'Guide', link: '/' },
      { text: 'Install', link: '/getting-started/installation' },
      { text: 'Site Types', link: '/sites/' },
      { text: 'Deployments', link: '/deployments/' },
      { text: 'Operations', link: '/operations/runtimes' },
      { text: 'Website', link: 'https://fluxo.fottify.com/' },
      { text: 'Live Demo', link: 'https://fluxo.fottify.com/demo/sites' },
    ],
    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'Welcome to Fluxo', link: '/' },
          { text: 'How Fluxo works', link: '/concepts/how-fluxo-works' },
        ],
      },
      {
        text: 'Getting Started',
        items: [
          { text: 'Requirements', link: '/getting-started/requirements' },
          { text: 'Installation', link: '/getting-started/installation' },
          { text: 'First login', link: '/getting-started/first-login' },
          { text: 'Upgrade Fluxo', link: '/getting-started/upgrade' },
        ],
      },
      {
        text: 'Sites',
        items: [
          { text: 'Create a site', link: '/sites/' },
          { text: 'Laravel', link: '/sites/laravel' },
          { text: 'PHP', link: '/sites/php' },
          { text: 'WordPress', link: '/sites/wordpress' },
          { text: 'Node.js', link: '/sites/nodejs' },
          { text: 'Python', link: '/sites/python' },
          { text: 'Static HTML', link: '/sites/static-html' },
          { text: 'Delete a site', link: '/sites/deletion' },
        ],
      },
      {
        text: 'Deployments',
        items: [
          { text: 'Deployment workflow', link: '/deployments/' },
          { text: 'Deployment scripts', link: '/deployments/scripts' },
          { text: 'Zero downtime', link: '/deployments/zero-downtime' },
          { text: 'Failures and rollbacks', link: '/deployments/failures-rollbacks' },
          { text: 'GitHub integration', link: '/deployments/github' },
        ],
      },
      {
        text: 'Manage a Site',
        items: [
          { text: 'Domains and SSL', link: '/site-management/domains-ssl' },
          { text: 'Environment and WordPress config', link: '/site-management/environment' },
          { text: 'Nginx vhost editor', link: '/site-management/vhost' },
          { text: 'File manager', link: '/site-management/files' },
          { text: 'Commands', link: '/site-management/commands' },
          { text: 'Daemons and scheduler', link: '/site-management/processes' },
          { text: 'Laravel features', link: '/site-management/laravel-features' },
        ],
      },
      {
        text: 'Server Operations',
        items: [
          { text: 'Runtimes', link: '/operations/runtimes' },
          { text: 'Databases', link: '/operations/databases' },
          { text: 'Backups', link: '/operations/backups' },
          { text: 'Monitoring and logs', link: '/operations/monitoring' },
          { text: 'Settings and access', link: '/operations/settings' },
          { text: 'Security and firewall', link: '/operations/security' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'CLI commands', link: '/reference/cli' },
          { text: 'REST API', link: '/reference/api' },
          { text: 'Paths and services', link: '/reference/paths-services' },
          { text: 'Troubleshooting', link: '/reference/troubleshooting' },
          { text: 'Development', link: '/reference/development' },
        ],
      },
    ],
    outline: {
      level: [2, 3],
      label: 'On this page',
    },
    editLink: {
      pattern: 'https://github.com/FabioTECH1/fluxo/edit/main/ui-site/docs/:path',
      text: 'Edit this page on GitHub',
    },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/FabioTECH1/fluxo' },
    ],
    footer: {
      message: '<a href="https://fluxo.fottify.com/">Fluxo website</a> &middot; Source-available under the BSL 1.1 License.',
      copyright: 'Fluxo documentation',
    },
  },
})
