---
title: Databases
description: Manage MariaDB/MySQL and PostgreSQL databases, users, grants, passwords, and phpMyAdmin.
---

# Databases

Open **Storage > Databases** for server-wide database administration. Fluxo supports MariaDB/MySQL and PostgreSQL engines installed on the same server.

## Create a database

Choose the engine and enter a valid database name. You can optionally create a dedicated user and password or use the existing Fluxo-managed database identity where the form permits it.

Databases can be created globally or from a site form. A database selected during site creation becomes attached to that site and appears in site-specific database choices and backup plans.

## Existing databases

The WordPress and PHP/Laravel site forms load unassigned databases from Fluxo's records and filter them by compatible engine. WordPress shows only MySQL/MariaDB because its provisioning workflow does not support PostgreSQL.

If an engine was added outside Fluxo, its databases do not automatically become Fluxo-managed records. Add or import management metadata deliberately rather than assuming system discovery.

## Users and grants

You can create database users, inspect grants, update database access, rotate a user's password, and delete a user. Use a separate least-privilege application user instead of sharing a broad administrative account.

Password rotation changes the database engine credential; update every application environment that uses it before or immediately after rotation.

## Delete a database

Deleting a database is irreversible. Fluxo coordinates the deletion with active backup work and control records, then drops the selected engine database.

Deleting a site does not drop attached databases unless the site-deletion checkbox explicitly selects them. Even when selected databases are dropped, database users and PostgreSQL roles remain so they are not unexpectedly removed from other access arrangements.

## phpMyAdmin

Fluxo can install and enable phpMyAdmin as an optional server tool for MariaDB/MySQL. Access uses a short-lived one-time link created from the dashboard instead of leaving a permanently discoverable public login route.

The tool can be enabled, disabled without removing its files, or removed entirely. Disabling phpMyAdmin does not stop MariaDB/MySQL or affect applications.

Prefer command-line dump tools or Fluxo backups for reliable export and recovery of large databases. phpMyAdmin is best suited to inspecting tables and making small deliberate changes.

## PostgreSQL administration

phpMyAdmin does not support PostgreSQL. Use application migrations, `psql`, or another separately secured PostgreSQL client.

::: danger Destructive actions
Database deletion and user-password rotation can take production applications offline immediately. Verify backups and application configuration before confirming them.
:::

