import { blogPosts, getBlogPost } from './data/blog'

export const siteUrl = 'https://fluxo.fottify.com'

export interface PublicPageMeta {
  title: string
  description: string
  path: string
  canonical: string
  image: string
  type: 'website' | 'article'
  publishedAt?: string
  category?: string
  structuredData: Record<string, unknown>
}

function absoluteUrl(path: string) {
  return new URL(path, `${siteUrl}/`).toString()
}

function softwareStructuredData() {
  return {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: 'Fluxo',
    applicationCategory: 'DeveloperApplication',
    operatingSystem: 'Ubuntu',
    softwareVersion: '0.4.27',
    url: `${siteUrl}/`,
    downloadUrl: `${siteUrl}/install.sh`,
    image: `${siteUrl}/og-image.png`,
    description: 'A self-hosted server control panel for deploying and managing PHP, Laravel, WordPress, Python, Node.js, and static applications.',
    sameAs: 'https://github.com/FabioTECH1/fluxo',
  }
}

export function getPublicPageMeta(path: string): PublicPageMeta | undefined {
  if (path === '/') {
    return {
      title: 'Fluxo — Modern Server Control Panel',
      description: 'Deploy and manage Laravel, WordPress, PHP, Python, Node.js, and static sites on your own VPS with zero-downtime deployments, SSL, and database management.',
      path,
      canonical: `${siteUrl}/`,
      image: `${siteUrl}/og-image.png`,
      type: 'website',
      structuredData: softwareStructuredData(),
    }
  }

  if (path === '/blog') {
    const title = 'Fluxo Blog — Deployment and server operations guides'
    const description = 'Product updates and practical guides for deploying, securing, and operating modern web applications with Fluxo.'
    return {
      title,
      description,
      path,
      canonical: `${siteUrl}/blog`,
      image: `${siteUrl}/blog/zero-downtime-deployments.webp`,
      type: 'website',
      structuredData: {
        '@context': 'https://schema.org',
        '@type': 'Blog',
        name: 'Fluxo Blog',
        url: `${siteUrl}/blog`,
        description,
        publisher: {
          '@type': 'Organization',
          name: 'Fluxo',
          url: `${siteUrl}/`,
          logo: { '@type': 'ImageObject', url: `${siteUrl}/logo.png` },
        },
        blogPost: blogPosts.map((post) => ({
          '@type': 'BlogPosting',
          headline: post.title,
          url: `${siteUrl}/blog/${post.slug}`,
          datePublished: post.publishedAt,
        })),
      },
    }
  }

  const match = path.match(/^\/blog\/([^/]+)$/)
  const post = match ? getBlogPost(match[1]) : undefined
  if (!post) return undefined

  const canonical = `${siteUrl}/blog/${post.slug}`
  const image = absoluteUrl(post.image)
  return {
    title: `${post.title} — Fluxo Blog`,
    description: post.excerpt,
    path,
    canonical,
    image,
    type: 'article',
    publishedAt: post.publishedAt,
    category: post.category,
    structuredData: {
      '@context': 'https://schema.org',
      '@type': 'BlogPosting',
      headline: post.title,
      description: post.excerpt,
      image,
      datePublished: post.publishedAt,
      dateModified: post.publishedAt,
      articleSection: post.category,
      mainEntityOfPage: { '@type': 'WebPage', '@id': canonical },
      author: { '@type': 'Organization', name: 'Fluxo team', url: `${siteUrl}/` },
      publisher: {
        '@type': 'Organization',
        name: 'Fluxo',
        url: `${siteUrl}/`,
        logo: { '@type': 'ImageObject', url: `${siteUrl}/logo.png` },
      },
    },
  }
}

function setMeta(selector: string, attribute: string, value: string) {
  document.querySelector(selector)?.setAttribute(attribute, value)
}

function setOptionalPropertyMeta(id: string, property: string, value?: string) {
  const existing = document.querySelector(`#${id}`)
  if (!value) {
    existing?.remove()
    return
  }

  const element = existing instanceof HTMLMetaElement ? existing : document.createElement('meta')
  element.id = id
  element.setAttribute('property', property)
  element.content = value
  if (!existing) document.head.append(element)
}

export function applyPublicPageMeta(path: string) {
  if (typeof document === 'undefined') return
  const meta = getPublicPageMeta(path)
  if (!meta) return

  document.title = meta.title
  setMeta('#seo-description', 'content', meta.description)
  setMeta('#seo-canonical', 'href', meta.canonical)
  setMeta('#seo-og-type', 'content', meta.type)
  setMeta('#seo-og-url', 'content', meta.canonical)
  setMeta('#seo-og-title', 'content', meta.title)
  setMeta('#seo-og-description', 'content', meta.description)
  setMeta('#seo-og-image', 'content', meta.image)
  setMeta('#seo-twitter-url', 'content', meta.canonical)
  setMeta('#seo-twitter-title', 'content', meta.title)
  setMeta('#seo-twitter-description', 'content', meta.description)
  setMeta('#seo-twitter-image', 'content', meta.image)

  setOptionalPropertyMeta('seo-article-published', 'article:published_time', meta.publishedAt)
  setOptionalPropertyMeta('seo-article-section', 'article:section', meta.category)

  const structuredData = document.querySelector('#seo-structured-data')
  if (structuredData) structuredData.textContent = JSON.stringify(meta.structuredData)
}
