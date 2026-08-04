---
title: Development
description: Build Fluxo, run its frontends, and contribute documentation or code.
---

# Development

Fluxo is a Go daemon with an embedded Vue dashboard. The public landing page, documentation, and interactive demo live in `ui-site`.

## Prerequisites

- Go 1.26.3 or newer
- Node.js 20 or newer for frontend development
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

This command synchronizes the installer, builds the landing/demo SPA, builds VitePress into `dist/docs`, and writes the landing SPA fallback. Cloudflare Pages publishes `ui-site/dist`.

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

