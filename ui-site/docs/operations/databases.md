---
title: Databases
description: Manage MariaDB/MySQL and PostgreSQL databases, users, grants, passwords, downloads, and phpMyAdmin.
---

# Databases

Open **Storage > Databases** for server-wide database administration. Fluxo supports MariaDB/MySQL and PostgreSQL engines installed on the same server.

## Create a database

Choose the engine and enter a valid database name. For a database that will be attached during site creation, create a dedicated application user and password. Fluxo's database control-plane identities are reserved for server administration and cannot be connected to applications.

Databases can be created globally or from a site form. A database selected during site creation becomes attached to that site and appears in site-specific database choices and backup plans.

## Existing databases

The WordPress, Python, and PHP/Laravel site forms load unassigned databases from Fluxo's records and filter them by compatible engine. WordPress shows only MySQL/MariaDB because its provisioning workflow does not support PostgreSQL; Python can use either MySQL/MariaDB or PostgreSQL.

If an engine was added outside Fluxo, its databases do not automatically become Fluxo-managed records. Add or import management metadata deliberately rather than assuming system discovery.

## Users and grants

You can create database users, inspect grants, update database access, rotate a user's password, and delete a user. Use a separate least-privilege application user for each application instead of sharing a broad administrative account.

Fluxo only changes MySQL accounts it owns. Accounts created outside Fluxo are shown as external and cannot be edited, rotated, or deleted from the dashboard. Fluxo-managed application accounts connect through `127.0.0.1`; their grants are limited to the databases selected for that account.

Password rotation changes the database engine credential; update every application environment that uses it before or immediately after rotation.

## Download a database

Open **Storage > Databases**, open the database row's **⋯** menu, and choose **Download database**. The row shows **Preparing download…** while Fluxo creates the export. When preparation finishes, the browser starts downloading the completed file.

| Engine | Download format |
|---|---|
| MariaDB / MySQL | Compressed SQL (`.sql.gz`) |
| PostgreSQL | Custom-format archive (`.dump`) for `pg_restore` |

Downloads include the selected database, not application files or server users and passwords. PostgreSQL exports omit ownership and privileges. No S3 or R2 destination is required.

Exports have a **256 MB file limit** and a **30-minute preparation timeout**. Fluxo prepares one export at a time and retains up to four exports. If another export is preparing or the retained-export limit is reached, wait before trying again. For larger databases or scheduled off-server copies, use [Backups](./backups).

Completed exports expire after **10 minutes** and temporary files are cleaned up automatically. Download requests require your signed-in session. Reloading the page does not cancel preparation, but a new download must be requested if the result is lost or Fluxo restarts. Failed preparation does not offer a partial file for download.

Live exports use server CPU, disk space, and I/O. Fluxo preserves at least 512 MB of free disk space during export. Avoid schema changes during MySQL/MariaDB exports; transaction-based consistency applies to transactional tables. Keep downloaded database files secure and continue using scheduled backups for recovery.

## Delete a database

Deleting a database is irreversible. Fluxo coordinates the deletion with active backup work and control records, then drops the selected engine database.

Deleting a site does not drop attached databases unless the site-deletion checkbox explicitly selects them. Even when selected databases are dropped, database users and PostgreSQL roles remain so they are not unexpectedly removed from other access arrangements.

## phpMyAdmin

Fluxo can install and enable phpMyAdmin as an optional server tool for MariaDB/MySQL. Access uses a short-lived one-time link created from the dashboard instead of leaving a permanently discoverable public login route. The link opens the protected phpMyAdmin gateway; sign in to phpMyAdmin with a dedicated MySQL application account to see the databases granted to that account. Root login is disabled.

The tool can be enabled, disabled without removing its files, or removed entirely. Disabling phpMyAdmin does not stop MariaDB/MySQL or affect applications.

Prefer command-line dump tools or Fluxo backups for reliable export and recovery of large databases. phpMyAdmin is best suited to inspecting tables and making small deliberate changes.

## PostgreSQL administration

phpMyAdmin does not support PostgreSQL. Use application migrations, `psql`, or another separately secured PostgreSQL client.

::: danger Destructive actions
Database deletion and user-password rotation can take production applications offline immediately. Verify backups and application configuration before confirming them.
:::
