import type { RouteRecordRaw } from 'vue-router'
import Landing from './views/Landing.vue'
import Blog from './views/Blog.vue'
import BlogPost from './views/BlogPost.vue'

export const publicRoutes: RouteRecordRaw[] = [
  { path: '/', component: Landing },
  { path: '/blog', component: Blog },
  { path: '/blog/:slug', component: BlogPost },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]
