<template>
  <footer class="border-t border-gray-200 px-4 py-10 dark:border-gray-800 sm:px-6 lg:px-8">
    <div class="mx-auto flex max-w-6xl flex-col items-center justify-between gap-5 sm:flex-row">
      <router-link to="/" class="flex items-center gap-2 text-lg font-bold">
        <img src="/logo.png" alt="" class="h-8 w-8 object-cover" />
        <span>fluxo</span>
      </router-link>
      <nav class="flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-sm text-gray-500" aria-label="Footer navigation">
        <a href="/docs/" class="transition-colors hover:text-gray-800 dark:hover:text-gray-200">Documentation</a>
        <router-link to="/blog" class="transition-colors hover:text-gray-800 dark:hover:text-gray-200">Blog</router-link>
        <a href="https://github.com/FabioTECH1/fluxo" target="_blank" rel="noopener noreferrer" class="transition-colors hover:text-gray-800 dark:hover:text-gray-200">GitHub</a>
        <a href="https://github.com/FabioTECH1/fluxo/releases" target="_blank" rel="noopener noreferrer" class="transition-colors hover:text-gray-800 dark:hover:text-gray-200">Releases</a>
      </nav>
      <p class="text-sm text-gray-400 dark:text-gray-600">Source-Available · BSL 1.1</p>
    </div>
  </footer>
</template>
