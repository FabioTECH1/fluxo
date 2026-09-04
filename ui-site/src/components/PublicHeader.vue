<script setup lang="ts">
import { ref } from 'vue'
import { ArrowRight, Menu, Monitor, Moon, Sun, X } from '@lucide/vue'
import { useTheme } from '@fluxo/composables/useTheme'

defineProps<{ active?: 'blog' }>()

const { theme } = useTheme()
const mobileMenuOpen = ref(false)

function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : theme.value === 'light' ? 'system' : 'dark'
}

function currentTheme() {
  return theme.value
}
</script>

<template>
  <header class="fixed inset-x-0 top-0 z-50 border-b border-gray-200 bg-white/90 backdrop-blur-md dark:border-gray-800 dark:bg-gray-950/90">
    <div class="mx-auto flex h-16 max-w-6xl items-center justify-between px-4 sm:px-6 lg:px-8">
      <a href="/" class="flex items-center gap-2 text-xl font-bold" aria-label="Fluxo home">
        <img src="/logo.png" alt="" class="h-8 w-8 object-cover" />
        <span>fluxo</span>
      </a>

      <nav class="hidden items-center gap-8 text-sm font-medium text-gray-600 dark:text-gray-400 md:flex" aria-label="Main navigation">
        <a href="/#features" class="transition-colors hover:text-gray-900 dark:hover:text-gray-100">Features</a>
        <a href="/docs/" target="_blank" rel="noopener noreferrer" class="transition-colors hover:text-gray-900 dark:hover:text-gray-100">Documentation</a>
        <a href="/blog" :aria-current="active === 'blog' ? 'page' : undefined" :class="active === 'blog' ? 'text-blue-600 dark:text-blue-400' : 'hover:text-gray-900 dark:hover:text-gray-100'" class="transition-colors">Blog</a>
        <a href="https://github.com/FabioTECH1/fluxo" target="_blank" rel="noopener noreferrer" class="transition-colors hover:text-gray-900 dark:hover:text-gray-100">GitHub</a>
        <button @click="toggleTheme" class="flex h-9 w-9 items-center justify-center rounded-lg transition-colors hover:bg-gray-100 dark:hover:bg-gray-800" :aria-label="`Cycle color theme. Current theme: ${currentTheme()}`" :title="`Theme: ${currentTheme()}`">
          <Sun v-if="currentTheme() === 'light'" class="h-4 w-4" aria-hidden="true" />
          <Moon v-else-if="currentTheme() === 'dark'" class="h-4 w-4" aria-hidden="true" />
          <Monitor v-else class="h-4 w-4" aria-hidden="true" />
        </button>
        <a href="/demo/sites" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-blue-700">
          Live Demo <ArrowRight class="h-4 w-4" aria-hidden="true" />
        </a>
      </nav>

      <div class="flex items-center gap-1 md:hidden">
        <button @click="toggleTheme" class="flex h-10 w-10 items-center justify-center rounded-lg transition-colors hover:bg-gray-100 dark:hover:bg-gray-800" :aria-label="`Cycle color theme. Current theme: ${currentTheme()}`">
          <Sun v-if="currentTheme() === 'light'" class="h-5 w-5" aria-hidden="true" />
          <Moon v-else-if="currentTheme() === 'dark'" class="h-5 w-5" aria-hidden="true" />
          <Monitor v-else class="h-5 w-5" aria-hidden="true" />
        </button>
        <button @click="mobileMenuOpen = !mobileMenuOpen" class="flex h-10 w-10 items-center justify-center rounded-lg transition-colors hover:bg-gray-100 dark:hover:bg-gray-800" :aria-expanded="mobileMenuOpen" aria-controls="public-mobile-nav" aria-label="Toggle navigation menu">
          <X v-if="mobileMenuOpen" class="h-6 w-6" aria-hidden="true" />
          <Menu v-else class="h-6 w-6" aria-hidden="true" />
        </button>
      </div>
    </div>

    <nav v-if="mobileMenuOpen" id="public-mobile-nav" class="space-y-2 border-t border-gray-200 bg-white px-4 py-4 text-sm dark:border-gray-800 dark:bg-gray-950 md:hidden" aria-label="Mobile navigation">
      <a href="/#features" class="block rounded-lg px-3 py-2.5 text-gray-600 hover:bg-gray-50 dark:text-gray-400 dark:hover:bg-gray-900">Features</a>
      <a href="/docs/" target="_blank" rel="noopener noreferrer" class="block rounded-lg px-3 py-2.5 text-gray-600 hover:bg-gray-50 dark:text-gray-400 dark:hover:bg-gray-900">Documentation</a>
      <a href="/blog" aria-current="page" class="block rounded-lg bg-blue-50 px-3 py-2.5 font-semibold text-blue-700 dark:bg-blue-950/50 dark:text-blue-300">Blog</a>
      <a href="https://github.com/FabioTECH1/fluxo" target="_blank" rel="noopener noreferrer" class="block rounded-lg px-3 py-2.5 text-gray-600 hover:bg-gray-50 dark:text-gray-400 dark:hover:bg-gray-900">GitHub</a>
      <a href="/demo/sites" target="_blank" rel="noopener noreferrer" class="mt-3 flex w-full items-center justify-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 font-semibold text-white">
        Live Demo <ArrowRight class="h-4 w-4" aria-hidden="true" />
      </a>
    </nav>
  </header>
</template>
