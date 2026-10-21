# Legacy Scheduler qualification

New invocations use the journaled launcher and a separate nondelegated systemd
cgroup. This note concerns earlier Fluxo minute entries that executed Artisan
directly. No normal migration step restarts cron or the VPS, stops scheduled
tasks, or treats an elapsed delay as proof of completion.

The initial qualification is Ubuntu noble cron `3.0pl1-184ubuntu2`, amd64 and
arm64, with the standard configuration fingerprints in `legacy_host.go`.
Unknown PAM session relocation, alternate cron implementations and custom
effective units do not receive an automatic certificate. This assumes a trusted
standard installation without an unaccounted mechanism for applications to
relocate execution. It is not a retrospective proof against root modification.

Sources inspected after applying the complete Ubuntu quilt patch series:

- https://archive.ubuntu.com/ubuntu/pool/main/c/cron/cron_3.0pl1-184ubuntu2.dsc
  SHA256 `d7f600d21c4015c80d61645c870f9af27826b0403b0518ae3657e982bbf850c1`
- https://archive.ubuntu.com/ubuntu/pool/main/c/cron/cron_3.0pl1.orig.tar.gz
  SHA256 `d931e0688005dfa85cfdb60e19bf0a3848ebfa3ee3415bf2a6ea3ea9e5bcfd21`
- https://archive.ubuntu.com/ubuntu/pool/main/c/cron/cron_3.0pl1-184ubuntu2.debian.tar.xz
  SHA256 `22ec5b009e7aebd61c156895366c504d53f1201c763861ce948bae988ccbc329`

`cron.c` reloads the database before selecting jobs. Every clock-adjustment
branch drains `job_runqueue()` before the next outer reload. `job.c` calls
`do_command()` synchronously for each queued entry; `do_command.c` attempts the
first fork before returning. Therefore a genuine nonce embedded in the exact
replacement file proves older queued commands from that file have already
forked or failed. It does not prove their descendants finished.

`database.c` reuses entries whose whole-second file mtime matches. Publication
must change that value and still wait for the exact nonce. An inode change or a
generic heartbeat does not establish replacement reload.

Deletion uses two marker receipts. One marker can be loaded by a directory scan
that already read the old application file. After receipt A, publishing nonce B
forces another database pass entirely after deletion. The target must remain
absent; no application trigger is recreated to obtain a receipt.

After all receipts, the same single-threaded cron daemon and pinned hierarchy
must have `pids.current == 1`. This includes supervisors and detached non-PHP
children. No unknown processes are subtracted. Linux charges hierarchical PID
counters before making a fork runnable; reparenting and `setsid` do not move a
cgroup. See the documented interface and upstream implementation:

- https://www.kernel.org/doc/html/v6.8/admin-guide/cgroup-v2.html
- https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux/+/refs/tags/v6.8/kernel/cgroup/pids.c
- https://github.com/torvalds/linux/blob/v6.8/kernel/fork.c

Ubuntu's exact kernel patch delta was not independently audited. The feature
relies on the documented Linux cgroup PID-controller semantics.

Old synchronous Scheduler **Run now** has a separate panel-origin boundary.
Fluxo 0.4.34 and 0.4.35 retain the shared lifecycle lease through those handlers.
The installer acquires exclusivity while the old panel remains alive, preserving
its output readers and PrivateTmp until admitted work finishes. Remaining
children in that known nondelegated panel domain must finish naturally before
the normal panel stop. The candidate hash and boot bind the resulting proof;
restoring a different panel invalidates it. The database is checked again after
stop because old versions can enqueue checks while execution is locked.

This is a managed-Scheduler guarantee. Historical arbitrary terminal/SSH
commands, external schedulers, backups, alternate process managers and older or
unaccounted panel execution domains are not reconstructed from a PID scan.
They retain their existing operational requirements. No claim is made that all
preexisting installer behavior for those unrelated operations is changed.

An uncertain new systemd submission can be retired without reboot because the
fixed root execution helper must reacquire its journaled, at-most-once launch
authorization. A late helper with missing authority exits before running PHP.
After authorization is saved, a crash before application fork can skip that
tick; the same authorization is never replayed. The helper and any descendants
remain accounted for until their execution domain is empty.

## Regression evidence

`legacy_loader_race_test.py` builds an isolated loader-only executable from the
qualified, fully patched Ubuntu source above. It retains source hashes and the
instrumentation diff. The test redirects paths, orders enumeration, and inserts
a hook after the old application's `process_crontab` call. The hook deletes that
file and publishes A during the same scan. The completed real database must
contain both the deleted command and A. Publishing B with a new whole-second
mtime then removes the old command, while custom and new schedules remain.
The second pass preserves the directory mtime to exercise individual-file
change detection. Ownership checks remain intact; run the executable as root
in a new scratch directory on the reusable test VM. It never executes commands.
This loader test complements the real-daemon A/B receipt and descendant-drain
tests; it does not itself authenticate receipts or certify a drain.

`TestVMLegacySchedulerHTTPFallbackAfterAdoptionFailureAndRetry` runs the actual
HTTP handler after a gate-lock timeout prevents startup adoption. It verifies
that legacy Run now still works, removes the earlier panel proof, and that a
successful adoption retry cannot manufacture readiness. Run the guarded server
test executable inside a private mount namespace with empty scratch directories
bind-mounted on `/var/lib` and `/etc/cron.d`, setting
`FLUXO_SCHEDULER_ISOLATED_MOUNTS=1`, `FLUXO_PHP_UPDATE_VM_TEST=1` and
`FLUXO_SCHEDULER_HTTP_EVIDENCE` to its protected evidence directory. The VM's
real journal and cron entries remain outside those mounts.
