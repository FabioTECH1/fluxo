import MarkdownIt from 'markdown-it'

export type BlogCategory = 'Releases' | 'Deployments' | 'Operations' | 'Security'

export interface BlogPost {
  slug: string
  title: string
  excerpt: string
  category: BlogCategory
  publishedAt: string
  displayDate: string
  readTime: string
  image: string
  imageAlt: string
  featured: boolean
  content: string
  html: string
}

const categories: BlogCategory[] = ['Releases', 'Deployments', 'Operations', 'Security']
const markdown = new MarkdownIt({ html: false, linkify: true, typographer: true })
const articleFiles = import.meta.glob('../../content/blog/*.md', {
  eager: true,
  query: '?raw',
  import: 'default',
}) as Record<string, string>

function parseFrontmatter(source: string, filePath: string) {
  const normalized = source.replace(/\r\n/g, '\n')
  if (!normalized.startsWith('---\n')) {
    throw new Error(`Blog article ${filePath} must start with frontmatter`)
  }

  const closingMarker = normalized.indexOf('\n---\n', 4)
  if (closingMarker === -1) {
    throw new Error(`Blog article ${filePath} has invalid frontmatter`)
  }

  const values: Record<string, string> = {}
  const frontmatter = normalized.slice(4, closingMarker)
  for (const line of frontmatter.split('\n')) {
    const separator = line.indexOf(':')
    if (separator === -1) continue
    const key = line.slice(0, separator).trim()
    const value = line.slice(separator + 1).trim().replace(/^(['"])(.*)\1$/, '$2')
    values[key] = value
  }

  return {
    values,
    content: normalized.slice(closingMarker + 5).trim(),
  }
}

function required(values: Record<string, string>, key: string, filePath: string) {
  const value = values[key]
  if (!value) throw new Error(`Blog article ${filePath} is missing ${key}`)
  return value
}

function toBlogPost(filePath: string, source: string): BlogPost {
  const { values, content } = parseFrontmatter(source, filePath)
  const category = required(values, 'category', filePath) as BlogCategory
  if (!categories.includes(category)) {
    throw new Error(`Blog article ${filePath} has unsupported category ${category}`)
  }

  const publishedAt = required(values, 'date', filePath)
  const slug = values.slug || filePath.split('/').pop()?.replace(/\.md$/, '') || ''
  const words = content.split(/\s+/).filter(Boolean).length

  return {
    slug,
    title: required(values, 'title', filePath),
    excerpt: required(values, 'excerpt', filePath),
    category,
    publishedAt,
    displayDate: new Intl.DateTimeFormat('en-US', {
      month: 'long',
      day: 'numeric',
      year: 'numeric',
      timeZone: 'UTC',
    }).format(new Date(`${publishedAt}T00:00:00Z`)),
    readTime: values.readTime || `${Math.max(1, Math.ceil(words / 200))} min read`,
    image: required(values, 'image', filePath),
    imageAlt: required(values, 'imageAlt', filePath),
    featured: values.featured === 'true',
    content,
    html: markdown.render(content),
  }
}

export const blogPosts = Object.entries(articleFiles)
  .map(([filePath, source]) => toBlogPost(filePath, source))
  .sort((left, right) => right.publishedAt.localeCompare(left.publishedAt))

export const blogCategories: Array<'All' | BlogCategory> = ['All', ...categories]

export function getBlogPost(slug: string) {
  return blogPosts.find((post) => post.slug === slug)
}
