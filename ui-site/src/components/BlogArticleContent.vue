<script setup lang="ts">
defineProps<{
  html: string
}>()
</script>

<template>
  <div class="blog-prose" v-html="html"></div>
</template>

<style>
.blog-prose {
  --prose-body: rgb(55 65 81);
  --prose-heading: rgb(17 24 39);
  --prose-link: rgb(37 99 235);
  --prose-inline-code: rgb(31 41 55);
  --prose-inline-code-bg: rgb(243 244 246);
  --prose-block-code: rgb(226 232 240);
  --prose-block-code-bg: rgb(15 23 42);
  --prose-table-border: rgb(229 231 235);
  --prose-table-heading: rgb(17 24 39);
  --prose-table-heading-bg: rgb(249 250 251);
}

html.dark .blog-prose {
  --prose-body: rgb(209 213 219);
  --prose-heading: rgb(243 244 246);
  --prose-link: rgb(96 165 250);
  --prose-inline-code: rgb(243 244 246);
  --prose-inline-code-bg: rgb(31 41 55);
  --prose-table-border: rgb(55 65 81);
  --prose-table-heading: rgb(243 244 246);
  --prose-table-heading-bg: rgb(31 41 55);
}

.blog-prose h2 {
  margin-top: 2.75rem;
  color: var(--prose-heading);
  font-size: 1.875rem;
  font-weight: 700;
  line-height: 1.2;
  letter-spacing: -0.025em;
}

.blog-prose h2:first-child {
  margin-top: 0;
}

.blog-prose p {
  margin-top: 1.25rem;
  color: var(--prose-body);
  font-size: 1.0625rem;
  line-height: 2rem;
}

.blog-prose ul,
.blog-prose ol {
  margin-top: 1.25rem;
  display: grid;
  gap: 0.75rem;
  padding-left: 1.5rem;
  color: var(--prose-body);
  font-size: 1.0625rem;
  line-height: 1.75rem;
}

.blog-prose ul {
  list-style: disc;
}

.blog-prose ol {
  list-style: decimal;
}

.blog-prose a {
  color: var(--prose-link);
  font-weight: 600;
  text-decoration: underline;
  text-underline-offset: 0.2em;
}

.blog-prose code {
  border-radius: 0.375rem;
  background: var(--prose-inline-code-bg);
  color: var(--prose-inline-code);
  padding: 0.125rem 0.375rem;
  font-size: 0.9375em;
}

.blog-prose pre {
  margin-top: 1.25rem;
  overflow-x: auto;
  border-radius: 0.75rem;
  background: var(--prose-block-code-bg);
  padding: 1rem;
}

.blog-prose pre code {
  display: block;
  background: transparent;
  color: var(--prose-block-code);
  padding: 0;
  line-height: 1.625;
}

.blog-prose table {
  margin-top: 1.5rem;
  display: block;
  width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  color: var(--prose-body);
  font-size: 0.9375rem;
  line-height: 1.5rem;
}

.blog-prose th,
.blog-prose td {
  min-width: 10rem;
  border: 1px solid var(--prose-table-border);
  padding: 0.75rem 1rem;
  text-align: left;
  vertical-align: top;
}

.blog-prose th {
  background: var(--prose-table-heading-bg);
  color: var(--prose-table-heading);
  font-weight: 700;
}
</style>
