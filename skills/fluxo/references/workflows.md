# Fluxo workflow references

Read the row that matches the user's task. The website documentation is the
maintained source of truth; fetch that page and the API contract as needed.
Check installed version and endpoint availability before using a feature.

| Task | Guidance |
| --- | --- |
| Understand the panel and privileges | [How Fluxo works](https://fluxo.fabiotech.dev/docs/concepts/how-fluxo-works) |
| Install or upgrade Fluxo | [Installation](https://fluxo.fabiotech.dev/docs/getting-started/installation), [Upgrade](https://fluxo.fabiotech.dev/docs/getting-started/upgrade) |
| Provision a site | [Site types](https://fluxo.fabiotech.dev/docs/sites/) and the relevant Laravel, PHP, WordPress, Node.js, Python or static-site guide linked there |
| Deploy, inspect a failure or roll back | [Deployment workflow](https://fluxo.fabiotech.dev/docs/deployments/), [Failures and rollbacks](https://fluxo.fabiotech.dev/docs/deployments/failures-rollbacks) |
| Domains, wildcard routing and SSL | [Domains and SSL](https://fluxo.fabiotech.dev/docs/site-management/domains-ssl) |
| Environment or WordPress configuration | [Environment and WordPress config](https://fluxo.fabiotech.dev/docs/site-management/environment) |
| Daemons, queue workers or cron | [Processes](https://fluxo.fabiotech.dev/docs/site-management/processes), [Laravel features](https://fluxo.fabiotech.dev/docs/site-management/laravel-features) |
| PHP/Node maintenance or Nginx awareness | [Runtimes](https://fluxo.fabiotech.dev/docs/operations/runtimes) |
| Security updates or server restart | [Settings and access](https://fluxo.fabiotech.dev/docs/operations/settings) |
| Databases or backups | [Databases](https://fluxo.fabiotech.dev/docs/operations/databases), [Backups](https://fluxo.fabiotech.dev/docs/operations/backups) |
| Traffic history or missing analytics | [Traffic analytics](https://fluxo.fabiotech.dev/docs/operations/traffic-analytics) |
| Logs, metrics and diagnostics | [Monitoring](https://fluxo.fabiotech.dev/docs/operations/monitoring), [Troubleshooting](https://fluxo.fabiotech.dev/docs/reference/troubleshooting) |
| Exact HTTP requests | [REST API](https://fluxo.fabiotech.dev/docs/reference/api) |

## Interpret common states correctly

- Fluxo's public live demo simulates privileged actions. It is useful for
  explaining navigation, not verifying a real server operation.
- An available runtime update and a reviewed/applicable plan are separate
  states. A failed/stale discovery result does not establish that software is
  current. Preserve prior results with their freshness and error context.
- Nginx's panel action reloads configuration. An on-disk package version does
  not establish that running master/workers adopted a newly installed binary.
- Wildcard routing needs DNS at the provider and appropriate certificate
  coverage. Root-domain SSL does not establish wildcard coverage. Imported
  certificates have their own renewal responsibility.
- Retention controls storage/deletion; a displayed range selects the view.
  Increasing retention cannot restore deleted data or invent prior collection.
- Server restart acceptance, a changed boot identity and expected service
  readiness are distinct. Preserve unknown/offline/authentication states rather
  than reporting success merely because an HTTP connection returned.

## Access prerequisites

There is no credential in this package. Use a connection the user has configured
in the agent's secure environment. The current panel's documented Bearer
authentication is an administrator session; the skill does not narrow its
permissions. Do not invent an integration, credential setting or login tool.
Without an authorized live connection, provide documentation-based guidance
and state that current server state has not been inspected.

For an API task, read its current request/response documentation first. Do not
guess resource IDs, use a token on another origin, bypass TLS verification, or
silently switch to root SSH when an API operation is refused.
