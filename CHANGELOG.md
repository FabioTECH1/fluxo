# Release notes

## v0.4.20 — 2026-08-24

### Fixed

- Quote database passwords whenever Laravel, PHP, or Node.js environment files are generated or merged from `.env.example`, preserving characters such as `#`, `$`, spaces, and backslashes. Generated database settings now consistently use the managed `127.0.0.1` TCP account.
- Keep environment and deployment-script editor text, selections, line numbers, carets, and syntax highlighting aligned while scrolling. Native scrollbar space is reserved so the final line no longer renders beneath a scrollbar.
- Prevent selected syntax-highlighted text from being painted twice by the browser.
- Keep deployment-output modals full width with responsive commit and branch labels. Long labels are visually truncated with their complete values available on hover, while long log lines stay inside the modal.

### Upgrade notes

- No database migration or configuration change is required.
- Existing site `.env` files are not rewritten during upgrade. Passwords already affected by dotenv comment parsing should remain single-quoted.

Earlier releases are documented on the [GitHub releases page](https://github.com/FabioTECH1/fluxo/releases).
