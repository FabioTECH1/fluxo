# v0.4.41 Node recovery bootstrap

Use `stage-v042-node-worker.sh --stage-v042-node-worker` only when an official
v0.4.41 panel has a stopped Node `recovery_required` attempt and the normal
installer refuses unfinished maintenance. Other versions, active operations,
PHP recovery, foreign holds and unrelated installations are refused.

The helper downloads the official v0.4.42 binary, checks its exact checksum and
GitHub-signed release provenance using the pinned published installer verifier,
then acquires the existing lifecycle lock. It backs up the old executable and
atomically replaces `/usr/local/bin/fluxo`. It does **not** restart the dashboard,
run an update or repair, change packages, or edit recovery records or holds.
The running v0.4.41 panel can then launch the fixed v0.4.42 worker through its
existing authenticated, reviewed repair workflow.

1. Run the explicitly selected support script as root with
   `--stage-v042-node-worker`. Preserve its printed backup/receipt directory.
2. In the existing dashboard, open **Runtime > Node.js > Review repair** and
   approve the repair. Wait for a successful result and released maintenance.
3. Complete the normal v0.4.42 panel installation with `--reinstall`. This is
   necessary because the on-disk executable now reports v0.4.42 while the running
   dashboard still uses v0.4.41. Normal installer admission remains enforced.

If any guard refuses, or staging reports an uncertain replacement, preserve the
output and receipts. Do not delete holds, requeue operations in SQLite, restore
an executable while a worker is active, or repeat an uncertain action blindly.

This support tool is deliberately separate from normal installation. It is
never automatically applied to other servers, and does not change installer
policy or published release artifacts. The Python helper also pins both official
architecture-specific binary hashes before executing the candidate.
