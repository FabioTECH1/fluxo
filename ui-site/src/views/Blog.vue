<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowRight, BookOpen, Search, X } from '@lucide/vue'
import PublicFooter from '../components/PublicFooter.vue'
import PublicHeader from '../components/PublicHeader.vue'
import { blogCategories, blogPosts, type BlogCategory } from '../data/blog'

const query = ref('')
const activeCategory = ref<'All' | BlogCategory>('All')

const normalizedQuery = computed(() => query.value.trim().toLowerCase())
const showFeatured = computed(() => !normalizedQuery.value && activeCategory.value === 'All')
const featuredPost = blogPosts.find((post) => post.featured) ?? blogPosts[0]

const filteredPosts = computed(() => blogPosts.filter((post) => {
  const matchesCategory = activeCategory.value === 'All' || post.category === activeCategory.value
  const searchableText = `${post.title} ${post.excerpt} ${post.category}`.toLowerCase()
  return matchesCategory && (!normalizedQuery.value || searchableText.includes(normalizedQuery.value))
}))

const gridPosts = computed(() => showFeatured.value
  ? filteredPosts.value.filter((post) => post.slug !== featuredPost.slug)
  : filteredPosts.value)

function resetFilters() {
  query.value = ''
  activeCategory.value = 'All'
}
</script>

<template>
  <div class="min-h-screen bg-white font-sans text-gray-900 antialiased transition-colors dark:bg-gray-950 dark:text-gray-100">
    <PublicHeader active="blog" />

    <main>
      <section class="border-b border-gray-200 bg-gray-50 px-4 pb-16 pt-32 dark:border-gray-800 dark:bg-gray-900/40 sm:px-6 sm:pb-20 sm:pt-36 lg:px-8">
        <div class="mx-auto max-w-6xl">
          <div class="max-w-3xl">
            <div class="mb-5 inline-flex items-center gap-2 rounded-full border border-blue-200 bg-blue-50 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-blue-700 dark:border-blue-800 dark:bg-blue-950/50 dark:text-blue-300">
              <BookOpen class="h-3.5 w-3.5" aria-hidden="true" />
              Field notes from Fluxo
            </div>
            <h1 class="text-4xl font-extrabold tracking-tight sm:text-5xl lg:text-6xl">Build, deploy, and operate with confidence.</h1>
            <p class="mt-5 max-w-2xl text-lg leading-relaxed text-gray-600 dark:text-gray-400 sm:text-xl">
              Product updates and practical guides for running modern web applications on infrastructure you control.
            </p>
          </div>

          <div class="mt-10 max-w-3xl">
            <label for="blog-search" class="sr-only">Search Fluxo articles</label>
            <div class="relative">
              <Search class="pointer-events-none absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-gray-400" aria-hidden="true" />
              <input id="blog-search" v-model="query" type="search" placeholder="Search deployments, backups, security..." class="h-14 w-full rounded-xl border border-gray-300 bg-white pl-12 pr-12 text-base shadow-sm outline-none transition focus:border-blue-500 focus:ring-4 focus:ring-blue-500/10 dark:border-gray-700 dark:bg-gray-950 dark:placeholder:text-gray-500" />
              <button v-if="query" type="button" @click="query = ''" class="absolute right-3 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-lg text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800 dark:hover:text-gray-200" aria-label="Clear search">
                <X class="h-4 w-4" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <div class="mx-auto max-w-6xl">
          <article v-if="showFeatured" class="group mb-14 overflow-hidden rounded-2xl border border-gray-200 bg-gray-50 shadow-sm transition hover:-translate-y-0.5 hover:shadow-lg dark:border-gray-800 dark:bg-gray-900/50">
            <router-link :to="`/blog/${featuredPost.slug}`" class="grid lg:grid-cols-[1.2fr_1fr]" :aria-label="`Read ${featuredPost.title}`">
              <div class="aspect-[16/9] overflow-hidden bg-white lg:aspect-auto lg:min-h-[390px] dark:bg-gray-900">
                <img :src="featuredPost.image" :alt="featuredPost.imageAlt" class="h-full w-full object-cover transition duration-500 group-hover:scale-[1.02]" fetchpriority="high" />
              </div>
              <div class="flex flex-col justify-center p-7 sm:p-10 lg:p-12">
                <div class="flex flex-wrap items-center gap-3 text-xs font-semibold uppercase tracking-[0.12em] text-gray-500 dark:text-gray-400">
                  <span class="text-blue-600 dark:text-blue-400">Featured</span>
                  <span aria-hidden="true">·</span>
                  <span>{{ featuredPost.category }}</span>
                </div>
                <h2 class="mt-4 text-3xl font-bold leading-tight tracking-tight sm:text-4xl">{{ featuredPost.title }}</h2>
                <p class="mt-4 text-base leading-relaxed text-gray-600 dark:text-gray-400">{{ featuredPost.excerpt }}</p>
                <div class="mt-7 flex items-center justify-between gap-4 border-t border-gray-200 pt-5 text-sm dark:border-gray-800">
                  <span class="text-gray-500 dark:text-gray-400">{{ featuredPost.displayDate }} · {{ featuredPost.readTime }}</span>
                  <span class="inline-flex items-center gap-1.5 font-semibold text-blue-600 dark:text-blue-400">
                    Read article <ArrowRight class="h-4 w-4 transition-transform group-hover:translate-x-1" aria-hidden="true" />
                  </span>
                </div>
              </div>
            </router-link>
          </article>

          <div class="mb-8 flex flex-col gap-5 border-b border-gray-200 pb-6 dark:border-gray-800 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <p class="text-sm font-semibold text-blue-600 dark:text-blue-400">Explore the journal</p>
              <h2 class="mt-1 text-2xl font-bold">Latest articles</h2>
            </div>
            <div class="flex gap-2 overflow-x-auto pb-1" aria-label="Filter articles by category">
              <button v-for="category in blogCategories" :key="category" type="button" @click="activeCategory = category" :aria-pressed="activeCategory === category" :class="activeCategory === category ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 bg-white text-gray-600 hover:border-gray-300 hover:text-gray-900 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400 dark:hover:text-gray-200'" class="whitespace-nowrap rounded-full border px-4 py-2 text-sm font-semibold transition">
                {{ category }}
              </button>
            </div>
          </div>

          <p class="sr-only" aria-live="polite">{{ filteredPosts.length }} articles found</p>

          <div v-if="gridPosts.length" class="grid gap-x-7 gap-y-10 sm:grid-cols-2 lg:grid-cols-3">
            <article v-for="post in gridPosts" :key="post.slug" class="group flex min-w-0 flex-col">
              <router-link :to="`/blog/${post.slug}`" class="block overflow-hidden rounded-xl border border-gray-200 bg-gray-50 dark:border-gray-800 dark:bg-gray-900" tabindex="-1" aria-hidden="true">
                <img :src="post.image" :alt="post.imageAlt" class="aspect-[16/9] w-full object-cover transition duration-500 group-hover:scale-[1.03]" loading="lazy" />
              </router-link>
              <div class="mt-5 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.1em]">
                <span class="text-blue-600 dark:text-blue-400">{{ post.category }}</span>
                <span class="text-gray-300 dark:text-gray-700" aria-hidden="true">·</span>
                <span class="text-gray-500 dark:text-gray-400">{{ post.readTime }}</span>
              </div>
              <h3 class="mt-2 text-xl font-bold leading-snug tracking-tight">
                <router-link :to="`/blog/${post.slug}`" class="transition-colors hover:text-blue-600 dark:hover:text-blue-400">{{ post.title }}</router-link>
              </h3>
              <p class="mt-3 line-clamp-3 text-sm leading-relaxed text-gray-600 dark:text-gray-400">{{ post.excerpt }}</p>
              <p class="mt-4 text-xs text-gray-500 dark:text-gray-500">{{ post.displayDate }}</p>
            </article>
          </div>

          <div v-else class="rounded-2xl border border-dashed border-gray-300 px-6 py-16 text-center dark:border-gray-700">
            <Search class="mx-auto h-8 w-8 text-gray-400" aria-hidden="true" />
            <h3 class="mt-4 text-lg font-bold">No articles found</h3>
            <p class="mx-auto mt-2 max-w-md text-sm text-gray-500 dark:text-gray-400">Try another phrase or clear the filters to see every Fluxo article.</p>
            <button type="button" @click="resetFilters" class="mt-5 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-blue-700">Clear filters</button>
          </div>
        </div>
      </section>

      <section class="border-y border-gray-200 bg-gray-50 px-4 py-14 dark:border-gray-800 dark:bg-gray-900/40 sm:px-6 lg:px-8">
        <div class="mx-auto flex max-w-6xl flex-col gap-6 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p class="text-sm font-semibold text-blue-600 dark:text-blue-400">Ready to put it into practice?</p>
            <h2 class="mt-1 text-2xl font-bold">Start with the Fluxo documentation.</h2>
          </div>
          <a href="/docs/" class="inline-flex items-center justify-center gap-2 rounded-lg bg-gray-900 px-5 py-3 text-sm font-semibold text-white transition hover:bg-gray-700 dark:bg-white dark:text-gray-950 dark:hover:bg-gray-200">
            Browse the docs <ArrowRight class="h-4 w-4" aria-hidden="true" />
          </a>
        </div>
      </section>
    </main>

    <PublicFooter />
  </div>
</template>
