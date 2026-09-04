import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const siteRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = resolve(siteRoot, 'dist')
const serverDir = resolve(siteRoot, '.ssr')
const serverEntry = resolve(serverDir, 'entry-server.js')
const template = await readFile(resolve(distDir, 'index.html'), 'utf8')
const { render, routesToPrerender } = await import(serverEntry)

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}

function renderSeo(meta, robots = 'index,follow') {
  const articleMeta = meta.type === 'article'
    ? `\n  <meta id="seo-article-published" property="article:published_time" content="${escapeHtml(meta.publishedAt)}">\n  <meta id="seo-article-section" property="article:section" content="${escapeHtml(meta.category)}">`
    : ''
  const structuredData = JSON.stringify(meta.structuredData).replaceAll('<', '\\u003c')

  return `<!--app-seo-start-->
  <title>${escapeHtml(meta.title)}</title>
  <meta id="seo-description" name="description" content="${escapeHtml(meta.description)}">
  <meta name="robots" content="${robots}">
  <link id="seo-canonical" rel="canonical" href="${escapeHtml(meta.canonical)}">
  <meta id="seo-og-type" property="og:type" content="${meta.type}">
  <meta property="og:site_name" content="Fluxo">
  <meta id="seo-og-url" property="og:url" content="${escapeHtml(meta.canonical)}">
  <meta id="seo-og-title" property="og:title" content="${escapeHtml(meta.title)}">
  <meta id="seo-og-description" property="og:description" content="${escapeHtml(meta.description)}">
  <meta id="seo-og-image" property="og:image" content="${escapeHtml(meta.image)}">${articleMeta}
  <meta name="twitter:card" content="summary_large_image">
  <meta id="seo-twitter-url" name="twitter:url" content="${escapeHtml(meta.canonical)}">
  <meta id="seo-twitter-title" name="twitter:title" content="${escapeHtml(meta.title)}">
  <meta id="seo-twitter-description" name="twitter:description" content="${escapeHtml(meta.description)}">
  <meta id="seo-twitter-image" name="twitter:image" content="${escapeHtml(meta.image)}">
  <script id="seo-structured-data" type="application/ld+json">${structuredData}</script>
  <!--app-seo-end-->`
}

function routeFile(routePath) {
  if (routePath === '/') return resolve(distDir, 'index.html')
  return resolve(distDir, `${routePath.slice(1)}.html`)
}

function renderPage(appHtml, meta) {
  return template
    .replace(/<!--app-seo-start-->[\s\S]*?<!--app-seo-end-->/, renderSeo(meta))
    .replace('<div id="app"></div>', `<div id="app" data-server-rendered="true">${appHtml}</div>`)
}

const renderedPages = []
for (const routePath of routesToPrerender) {
  const { appHtml, meta } = await render(routePath)
  const filePath = routeFile(routePath)
  await mkdir(dirname(filePath), { recursive: true })
  await writeFile(filePath, renderPage(appHtml, meta))
  renderedPages.push({ routePath, meta })
}

const demoMeta = {
  title: 'Fluxo Live Demo',
  description: 'Explore a read-only demonstration of the Fluxo server control panel.',
  canonical: 'https://fluxo.fottify.com/demo/sites',
  image: 'https://fluxo.fottify.com/og-image.png',
  type: 'website',
  structuredData: {
    '@context': 'https://schema.org',
    '@type': 'WebApplication',
    name: 'Fluxo Live Demo',
    url: 'https://fluxo.fottify.com/demo/sites',
    isPartOf: { '@type': 'WebSite', name: 'Fluxo', url: 'https://fluxo.fottify.com/' },
  },
}
const demoDir = resolve(distDir, 'demo')
await mkdir(demoDir, { recursive: true })
await writeFile(
  resolve(demoDir, 'index.html'),
  template.replace(/<!--app-seo-start-->[\s\S]*?<!--app-seo-end-->/, renderSeo(demoMeta, 'noindex,nofollow')),
)

const sitemapEntries = renderedPages.map(({ routePath, meta }) => {
  const priority = routePath === '/' ? '1.0' : routePath === '/blog' ? '0.8' : '0.7'
  const lastModified = meta.publishedAt ? `\n    <lastmod>${meta.publishedAt}</lastmod>` : ''
  return `  <url>\n    <loc>${escapeHtml(meta.canonical)}</loc>${lastModified}\n    <changefreq>${routePath.startsWith('/blog/') ? 'monthly' : 'weekly'}</changefreq>\n    <priority>${priority}</priority>\n  </url>`
})
await writeFile(
  resolve(distDir, 'sitemap-pages.xml'),
  `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${sitemapEntries.join('\n')}\n</urlset>\n`,
)

await rm(serverDir, { recursive: true, force: true })
console.log(`Prerendered ${renderedPages.length} public routes and the demo shell.`)
