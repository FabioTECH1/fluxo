# Bridge runtime correction: bounded source qualification

The development branch fixes an ordering error in the historical generated
System Cleanup conversion. The previous bootstrap tried to register the new
command while the database still contained the old command, so registration's
database comparison correctly refused it.

Source `1cf96663038189a132eb5bced6971f7223e8a329` keeps this historical writer
separate from the clean base. It durably preserves an exact generated original,
publishes the canonical plain trigger, and commits the matching database row.
Interrupted publication can be retried with the exact protected backup. Missing
and custom jobs are preserved. Ordinary registration and genuine cron receipts
retain their existing contracts. The separate Sury revision correction was
backported from the base implementation; repository trust is not inferred from
a version string.

Focused normal and race tests, vet, 25 resolver tests and 12 repository tests
passed. The full Go suite passed before the final directory-fsync addition and
Python-only Sury backport; the affected suites passed again afterward. The
guarded named Ubuntu VM passed actual cron-file/SQLite normalization on the
final source, including missing/custom preservation and protected-input refusal.
The test used an isolated database and a future-only schedule. Its cron files
and copied test binaries were removed; private evidence is retained. An initial
fixture JSON-escaping assertion failure is recorded separately from the pass.

This is not a public-artifact, Node migration, Imagick or Certbot acceptance
claim. The customer's modified root trigger bytes remain unqualified, so the
fix does not promise to overwrite them. Shared runtime work and eventual
bridge/base handoff qualification remain pending. No release, tag, main merge,
latest promotion or customer operation occurred. See the adjacent JSON for
exact identities, boundaries and remaining gates.
