---
title: File Manager
description: Browse, upload, download, create, move, edit, and delete site files safely.
---

# File manager

Open **Storage > Files** and select a site to manage files inside its root directory. The file manager is intended for targeted operational changes, not bulk source-control replacement.

## Supported actions

- Browse directories and optionally show hidden entries
- Create a file or directory
- Upload a file, with an explicit overwrite choice
- Download a file
- Rename or move an entry within the managed site root
- Edit a small UTF-8 text file
- Delete a file or directory after confirmation

Uploads are limited to 100 MiB. The built-in editor accepts UTF-8 text files up to 1 MiB. Use SSH, `rsync`, Git, or object storage for larger transfers.

## Path protection

Fluxo normalizes requested paths and constrains file operations to the selected site root under `/home/fluxo`. It rejects traversal and unsafe symlink behavior rather than trusting a browser-supplied path.

## Zero-downtime sites

The active application lives under `current`, but release directories are disposable. Do not manually upload persistent user content into the active release. Place persistent data in the shared location expected by the application or add it to the deployment's shared-state design.

For Laravel, use shared `storage`. For a Node or PHP application, define and link a persistent upload directory explicitly.

## Conflict protection

Text edits use the file state loaded by the editor to avoid silently overwriting a concurrent change. If the file changed elsewhere, refresh and reconcile the new content before saving.

::: danger Deletion is immediate
The file manager has no recycle bin. Back up important content before deleting or overwriting it.
:::
