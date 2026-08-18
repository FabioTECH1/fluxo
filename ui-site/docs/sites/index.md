---
title: Create a Site
description: Choose an application type and create a correctly configured Fluxo site.
---

# Create a site

Open **Sites** and select **Add Site**. The application type controls the provisioning steps, Nginx template, available settings, default deployment script, logs, and optional features.

## Application types

| Type | Use it for | Runtime model |
|---|---|---|
| Laravel | Laravel framework applications | PHP-FPM, Laravel-aware defaults and features |
| PHP | Frameworks and custom PHP applications | PHP-FPM without automatic Artisan commands |
| WordPress | A fresh or managed WordPress installation | PHP-FPM, MySQL/MariaDB, WP-CLI, in-place files |
| Node.js | Next.js, Nuxt, and generic Node applications | Managed server process or static build |
| HTML | Plain files or repository-backed static sites | Files served directly by Nginx |

Choose the actual operating model, not the language used to write the source. A TypeScript Next.js app is still a **Node.js** site. A static export built by Next.js is a **Node.js** site in **Static build** mode.

::: warning Choose the application type carefully
The application type and deployment strategy are fixed when the site is created. Fluxo shows the selected type in **Settings > General**, but does not convert an existing site between Laravel, PHP, WordPress, Node.js, or HTML because those types use different provisioning, runtime, and deployment behavior. Create a new site and migrate the application when its operating model must change.
:::

## Domain

Enter the primary hostname without a scheme or path, for example `app.example.com`. Point its DNS records at the server before requesting a certificate.

The domain becomes part of the site path and generated configuration, so choose the production hostname carefully. Additional domains can be added after creation.

## Source control

WordPress does not require Git and hides source-control fields by default. Other site types can use a connected GitHub account, repository, and branch.

Connect GitHub under **Settings > Source Control** first when the repository is private. Fluxo can then list repositories and branches and configure access for the site.

## Database

Laravel and PHP sites can optionally attach an available database. WordPress requires an available MySQL or MariaDB database. You can select an unassigned database or create one from the site form.

Every attached application database requires a dedicated username and password. Fluxo does not place the `fluxo`, `root`, or `postgres` control-plane account in application configuration. When you create the database from the site form, Fluxo creates and grants the dedicated account; when you select an existing database, enter a dedicated account that already has access. Fluxo verifies the credentials before provisioning the site.

Attaching a database records the relationship and writes the dedicated credentials to supported application defaults. It does not imply that deleting the site will delete the database; deletion asks separately and preserves database users.

## Advanced settings

Zero-downtime deployment is enabled by default for Git-backed application types. It requires a repository because each deployment creates a release from source. Disable it when an application must be updated in place or when using Laravel Octane.

The web directory is relative to the site root. Laravel and WordPress normally use `/public`; a custom PHP application may use another directory.

## Provisioning progress

After confirmation, Fluxo replaces the generic form with application-aware progress. Keep the page open until the API reports success or an actionable error. Successful creation opens the site's Overview.

Site creation configures hosting but does not always deploy application code. Review the application-specific page below for the next step.
