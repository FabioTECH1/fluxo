---
title: Development
description: Build Fluxo, run its frontends, and contribute documentation or code.
---

# Development

Fluxo is a Go daemon with an embedded Vue dashboard. The public landing page, documentation, and interactive demo live in `ui-site`.

## Prerequisites

- Go 1.26.3 or newer
- Node.js 20.19 or newer, or Node.js 22.12 or newer, for frontend development
- npm

## Build the dashboard and daemon

Build the embedded dashboard before compiling Go:

```bash
cd ui
npm install
npm run build
cd ..
go build -o fluxo ./cmd/fluxo
```

## Run the dashboard frontend

```bash
cd ui
npm install
npm run dev
```

The standalone Vite server needs a reachable Fluxo API for real operations.

## Run the landing page and demo

```bash
cd ui-site
npm install
npm run dev
```

The demo uses a mock API and does not need a Fluxo server.

## Run documentation locally

```bash
cd ui-site
npm run docs:dev
```

The documentation is served under `/docs/`. Source Markdown lives in `ui-site/docs`.

## Production website build

```bash
cd ui-site
npm run build
```

This command synchronizes the installer, builds the Vue client, prerenders the landing page and Markdown-backed blog routes as static HTML, writes a separate `noindex` shell for the demo SPA, and builds VitePress into `dist/docs`. Cloudflare Pages publishes `ui-site/dist`.

Blog source Markdown lives in `ui-site/content/blog`. Each article is discovered during the build, rendered through the shared Vue article template, added to `sitemap-pages.xml`, and emitted as an extensionless-compatible HTML route under `dist/blog`.

Create a post by adding one Markdown file whose filename becomes its URL slug:

```md
---
title: Deploy a Laravel application with Fluxo
excerpt: A short description used on the blog listing and by search engines.
category: Deployments
date: 2026-09-04
image: /blog/deploy-laravel.webp
imageAlt: A concise description of the cover image
featured: true
---

## Start with a production-ready server

Write the article in Markdown here.
```

Supported categories are `Releases`, `Deployments`, `Operations`, and `Security`. `readTime` is optional and is calculated from the article when omitted. `slug` is also optional and overrides the filename-derived URL when present. Put cover images in `ui-site/public/blog`.

## Verification

```bash
cd ui
npx vue-tsc -b --noEmit
npm run build
cd ../ui-site
npm run build
cd ..
go test ./...
go vet ./...
```

System integration features require Nginx, PHP-FPM, systemd, and related packages and are best verified on a disposable VM.

## Contributing

Read the repository's [contribution guide](https://github.com/FabioTECH1/fluxo/blob/main/CONTRIBUTING.md). Keep documentation behavior aligned with the code and update the relevant guide whenever a user-facing workflow changes.
