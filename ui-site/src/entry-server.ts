import { createSSRApp } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { renderToString } from 'vue/server-renderer'
import App from './App.vue'
import { blogPosts } from './data/blog'
import { publicRoutes } from './public-routes'
import { getPublicPageMeta } from './seo'

export const routesToPrerender = [
  '/',
  '/blog',
  ...blogPosts.map((post) => `/blog/${post.slug}`),
]

export async function render(routePath: string) {
  const meta = getPublicPageMeta(routePath)
  if (!meta) throw new Error(`No SEO metadata configured for ${routePath}`)

  const app = createSSRApp(App)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: publicRoutes,
  })

  app.use(router)
  await router.push(routePath)
  await router.isReady()

  return {
    appHtml: await renderToString(app),
    meta,
  }
}
