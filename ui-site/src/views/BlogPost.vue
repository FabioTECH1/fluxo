<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowLeft, ArrowRight, Check, Copy, Home } from '@lucide/vue'
import PublicFooter from '../components/PublicFooter.vue'
import PublicHeader from '../components/PublicHeader.vue'
import { blogPosts, getBlogPost } from '../data/blog'

const route = useRoute()
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const post = computed(() => getBlogPost(String(route.params.slug)))
const relatedPosts = computed(() => {
  if (!post.value) return []
  const sameCategory = blogPosts.filter((candidate) => candidate.slug !== post.value?.slug && candidate.category === post.value?.category)
  const others = blogPosts.filter((candidate) => candidate.slug !== post.value?.slug && candidate.category !== post.value?.category)
  return [...sameCategory, ...others].slice(0, 3)
})

async function copyLink() {
  try {
    await navigator.clipboard.writeText(window.location.href)
    copied.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copied.value = false }, 1800)
  } catch {
    copied.value = false
  }
}

onBeforeUnmount(() => clearTimeout(copiedTimer))
</script>

<template>
  <div class="min-h-screen bg-white font-sans text-gray-900 antialiased transition-colors dark:bg-gray-950 dark:text-gray-100">
    <PublicHeader active="blog" />

    <main v-if="post">
      <article>
        <header class="px-4 pb-10 pt-28 sm:px-6 sm:pb-14 sm:pt-32 lg:px-8">
          <div class="mx-auto max-w-3xl">
            <nav class="mb-8 flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400" aria-label="Breadcrumb">
              <a href="/" class="transition hover:text-gray-900 dark:hover:text-gray-100" aria-label="Home"><Home class="h-4 w-4" aria-hidden="true" /></a>
              <span aria-hidden="true">/</span>
              <router-link to="/blog" class="transition hover:text-gray-900 dark:hover:text-gray-100">Blog</router-link>
              <span aria-hidden="true">/</span>
              <span class="truncate text-gray-700 dark:text-gray-300">{{ post.category }}</span>
            </nav>

            <div class="flex flex-wrap items-center gap-3 text-xs font-semibold uppercase tracking-[0.12em]">
              <span class="text-blue-600 dark:text-blue-400">{{ post.category }}</span>
              <span class="text-gray-300 dark:text-gray-700" aria-hidden="true">·</span>
              <time :datetime="post.publishedAt" class="text-gray-500 dark:text-gray-400">{{ post.displayDate }}</time>
              <span class="text-gray-300 dark:text-gray-700" aria-hidden="true">·</span>
              <span class="text-gray-500 dark:text-gray-400">{{ post.readTime }}</span>
            </div>

            <h1 class="mt-5 text-4xl font-extrabold leading-tight tracking-tight sm:text-5xl lg:text-[3.5rem]">{{ post.title }}</h1>
            <p class="mt-6 text-xl leading-relaxed text-gray-600 dark:text-gray-400">{{ post.excerpt }}</p>

            <div class="mt-7 flex items-center justify-between gap-4 border-t border-gray-200 pt-5 dark:border-gray-800">
              <div class="flex items-center gap-3">
                <img src="/logo.png" alt="" class="h-9 w-9 rounded-lg" />
                <div>
                  <p class="text-sm font-semibold">Fluxo team</p>
                  <p class="text-xs text-gray-500">Product and operations</p>
                </div>
              </div>
              <button type="button" @click="copyLink" class="inline-flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm font-semibold text-gray-600 transition hover:border-gray-300 hover:text-gray-900 dark:border-gray-700 dark:text-gray-400 dark:hover:text-gray-100" :aria-label="copied ? 'Article link copied' : 'Copy article link'">
                <Check v-if="copied" class="h-4 w-4 text-emerald-500" aria-hidden="true" />
                <Copy v-else class="h-4 w-4" aria-hidden="true" />
                {{ copied ? 'Copied' : 'Copy link' }}
              </button>
            </div>
          </div>
        </header>

        <div class="px-4 sm:px-6 lg:px-8">
          <div class="mx-auto max-w-5xl overflow-hidden rounded-2xl border border-gray-200 bg-gray-50 dark:border-gray-800 dark:bg-gray-900">
            <img :src="post.image" :alt="post.imageAlt" class="aspect-[16/9] w-full object-cover" />
          </div>
        </div>

        <div class="px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
          <div class="mx-auto max-w-3xl">
            <div class="blog-prose" v-html="post.html"></div>

            <div class="mt-14 border-t border-gray-200 pt-8 dark:border-gray-800">
              <router-link to="/blog" class="inline-flex items-center gap-2 text-sm font-semibold text-blue-600 transition hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300">
                <ArrowLeft class="h-4 w-4" aria-hidden="true" /> Back to all articles
              </router-link>
            </div>
          </div>
        </div>
      </article>

      <section class="border-t border-gray-200 bg-gray-50 px-4 py-14 dark:border-gray-800 dark:bg-gray-900/40 sm:px-6 lg:px-8" aria-labelledby="related-heading">
        <div class="mx-auto max-w-6xl">
          <div class="mb-7 flex items-end justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-blue-600 dark:text-blue-400">Keep reading</p>
              <h2 id="related-heading" class="mt-1 text-2xl font-bold">Related articles</h2>
            </div>
            <router-link to="/blog" class="hidden items-center gap-1.5 text-sm font-semibold text-gray-600 transition hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400 sm:inline-flex">View all <ArrowRight class="h-4 w-4" aria-hidden="true" /></router-link>
          </div>
          <div class="grid gap-6 sm:grid-cols-3">
            <router-link v-for="related in relatedPosts" :key="related.slug" :to="`/blog/${related.slug}`" class="group overflow-hidden rounded-xl border border-gray-200 bg-white transition hover:-translate-y-0.5 hover:shadow-lg dark:border-gray-800 dark:bg-gray-950">
              <img :src="related.image" :alt="related.imageAlt" class="aspect-[16/9] w-full object-cover" loading="lazy" />
              <div class="p-5">
                <p class="text-xs font-semibold uppercase tracking-[0.1em] text-blue-600 dark:text-blue-400">{{ related.category }}</p>
                <h3 class="mt-2 font-bold leading-snug transition-colors group-hover:text-blue-600 dark:group-hover:text-blue-400">{{ related.title }}</h3>
              </div>
            </router-link>
          </div>
        </div>
      </section>
    </main>

    <main v-else class="flex min-h-[75vh] items-center justify-center px-4 pt-16 text-center">
      <div>
        <p class="text-sm font-semibold text-blue-600 dark:text-blue-400">404</p>
        <h1 class="mt-2 text-3xl font-bold">Article not found</h1>
        <p class="mt-3 text-gray-500 dark:text-gray-400">The article may have moved or is not available yet.</p>
        <router-link to="/blog" class="mt-6 inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-blue-700">
          <ArrowLeft class="h-4 w-4" aria-hidden="true" /> Back to the blog
        </router-link>
      </div>
    </main>

    <PublicFooter />
  </div>
</template>

<style scoped>
:global(:root) {
  --prose-body: rgb(55 65 81);
  --prose-heading: rgb(17 24 39);
  --prose-link: rgb(37 99 235);
  --prose-code: rgb(31 41 55);
  --prose-code-bg: rgb(243 244 246);
  --prose-table-border: rgb(229 231 235);
  --prose-table-heading: rgb(17 24 39);
  --prose-table-heading-bg: rgb(249 250 251);
}

:global(html.dark) .blog-prose {
  --prose-body: rgb(209 213 219);
  --prose-heading: rgb(243 244 246);
  --prose-link: rgb(96 165 250);
  --prose-code: rgb(243 244 246);
  --prose-code-bg: rgb(31 41 55);
  --prose-table-border: rgb(55 65 81);
  --prose-table-heading: rgb(243 244 246);
  --prose-table-heading-bg: rgb(31 41 55);
}

.blog-prose :deep(h2) {
  margin-top: 2.75rem;
  color: var(--prose-heading);
  font-size: 1.875rem;
  font-weight: 700;
  line-height: 1.2;
  letter-spacing: -0.025em;
}

.blog-prose :deep(h2:first-child) {
  margin-top: 0;
}

.blog-prose :deep(p) {
  margin-top: 1.25rem;
  color: var(--prose-body);
  font-size: 1.0625rem;
  line-height: 2rem;
}

.blog-prose :deep(ul),
.blog-prose :deep(ol) {
  margin-top: 1.25rem;
  display: grid;
  gap: 0.75rem;
  padding-left: 1.5rem;
  color: var(--prose-body);
  font-size: 1.0625rem;
  line-height: 1.75rem;
}

.blog-prose :deep(ul) {
  list-style: disc;
}

.blog-prose :deep(ol) {
  list-style: decimal;
}

.blog-prose :deep(a) {
  color: var(--prose-link);
  font-weight: 600;
  text-decoration: underline;
  text-underline-offset: 0.2em;
}

.blog-prose :deep(code) {
  border-radius: 0.375rem;
  background: var(--prose-code-bg);
  color: var(--prose-code);
  padding: 0.125rem 0.375rem;
  font-size: 0.9375em;
}

.blog-prose :deep(pre) {
  margin-top: 1.25rem;
  overflow-x: auto;
  border-radius: 0.75rem;
  background: rgb(17 24 39);
  padding: 1rem;
  color: rgb(229 231 235);
}

.blog-prose :deep(pre code) {
  background: transparent;
  padding: 0;
}

.blog-prose :deep(table) {
  margin-top: 1.5rem;
  display: block;
  width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  color: var(--prose-body);
  font-size: 0.9375rem;
  line-height: 1.5rem;
}

.blog-prose :deep(th),
.blog-prose :deep(td) {
  min-width: 10rem;
  border: 1px solid var(--prose-table-border);
  padding: 0.75rem 1rem;
  text-align: left;
  vertical-align: top;
}

.blog-prose :deep(th) {
  background: var(--prose-table-heading-bg);
  color: var(--prose-table-heading);
  font-weight: 700;
}
</style>
