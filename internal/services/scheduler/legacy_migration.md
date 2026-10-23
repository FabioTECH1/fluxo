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

The root-only `scheduler-verify-panel` recovery command can establish this same
boundary while the compatible installed panel keeps running. It owns the
exclusive lifecycle lease, refuses pending PHP generations and any existing
installer marker, verifies the known nondelegated panel domain drains naturally,
and binds the proof to the still-running exact executable. It saves proof under
the journal gate without creating an installer marker; an interrupted verification
does not strand upgrade admission. It does not retire backend attempts, certify
cron receipts, remove legacy history, or release PHP holds. A process/binary change fails closed. Normal
same-version installer short-circuiting is not evidence of this verification.

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

## Cronie adapter and companion evidence

The additional adapter qualifies Ubuntu noble Cronie `1.7.1-1` on amd64 with
its exact packaged binary, unit, defaults and recursively included standard PAM
files recorded in `cronie.go`. Arm64 package hashes were inspected, but automatic
legacy certification remains disabled there until runtime acceptance is available.
An installed provider's name alone is insufficient. Existing custom or unsupported
providers keep their ordinary cron-file behavior; Fluxo does not convert them.

The independently inspected Ubuntu source artifacts are:

- https://archive.ubuntu.com/ubuntu/pool/universe/c/cronie/cronie_1.7.1-1.dsc
  SHA256 `d00a1e7512637368a803f3ce9e9a014fd92f247ebcd420c158dc77a888b7ddf0`
- https://archive.ubuntu.com/ubuntu/pool/universe/c/cronie/cronie_1.7.1.orig.tar.gz
  SHA256 `78033100c24413f0c40f93e6138774d6a4f55bc31050567b90db45a2f9f1b954`
- https://archive.ubuntu.com/ubuntu/pool/universe/c/cronie/cronie_1.7.1-1.debian.tar.xz
  SHA256 `7279ac17d898839c8a1aa896c13d689bfec7c21783f562ff5475d2c0e5b44e0f`
- amd64 binary package SHA256
  `3f8f6a61fd5a4e8fe8f1c8938e8442685f251a9cceaea235d83349db156d008a`

The complete Ubuntu quilt patch series was applied before inspection. Cronie's
job queue forks each job supervisor synchronously before advancing to its next
reload. Its supervisors retain `comm=crond`; operational inventory excludes only
same-executable descendants proven to remain under the stable daemon identity
and cgroup. Drainage does not subtract supervisors or any other descendants.
Non-root jobs use PAM, so the chain's containment is qualified independently of
the root receipt. `KillMode=process` means restarting a daemon is not proof that
its earlier children finished.

Cronie's polling loader and inotify full-rescan both clamp file timestamps to
`now-1`. Same-second changes can therefore retain cached content even after an
event. Publication retries improve progress, but never authorize readiness. Exact
replacement receipts, deletion receipts A then B, stable pinned identity and
hierarchical `pids.current == 1` remain mandatory. The tests use genuine packaged
Cronie receipts separately from deterministic loader instrumentation.

`cronie_loader_race_test.py` retains source hashes and its instrumentation. It
uses the real loader/parser, real inotify reads and exact extracted watch
functions; only scratch paths, time, enumeration order and an in-scan publication
hook are controlled. It reproduces A coexisting with a deleted legacy command,
same-second B remaining cached, a later B reload removing legacy/A while retaining
custom/new work, and watch reinstatement after loss. No parsed command executes.

### Recovering an initial failed qualification

The trust assumption remains a standard trusted installation without unaccounted
historical relocation. Missing historical logs alone do not disqualify it. Positive
evidence of a different unresolved domain cannot be ignored. A schema-2
`Host:null` record with no accepted/certified proof may start its first qualified
attempt. This pins its backend before publishing fresh nonces and does not itself
certify any work. Changed recorded identities remain blocked.

Schema 2 remains readable by v0.4.36 invocation helpers. Authoritative backend
attempts, exact predecessor/intended migrations, registration UUIDs and retained
attempt history live in the separate root-private `backend-proofs.json`. Cronie
is never represented as the Ubuntu-only host in core state. Both journals use
the existing gate. Complete current obligations are compared before projection;
mutations rebase only after reconciling exact authorized predecessor/intended
file contents. Deleted application triggers remain absent.

First initialization writes a durable `backend-proofs.initializing` intent before
the proof file and anchor. Completion removes and fsyncs that intent. Recovery
finishes storage of that exact recorded evidence without granting publication or
readiness authority. Ordinary reconciliation then rebases supported registration
changes and checks the pinned host or natural boot boundary. After completion,
loss of either final file stays fail-closed. Missing
required JSON fields, including nested receipt/transition evidence, are errors.
A natural later boot can retire earlier-boot domains only after current
registrations pass verification; its distinct boot boundary is retained.

An immutable older mutation helper can reject an interrupted marker transition
after recording a deletion but before removing its application file. Maintenance
then reports the incomplete deletion. Retrying the failed schedule action with
the current panel finishes only the recorded operation; no application file is
silently removed by reconciliation. Current actions recognize exact authorized
predecessor markers and do not reject them as unrelated edits.

The background coordinator performs bounded retries, separately from read-only
status/discovery. New receipts and maintenance boundaries always use fresh
qualification. Operational observations alone share a 30-second cache.
Support level 3 prevents rollback to a panel incapable of preserving companion
history. Installer transaction guards preserve existing providers, including
intentional disabled or masked states. Only a genuinely fresh host with no
provider and no pre-existing work may receive the standard cron default.

### Acceptance arrangement

The reusable amd64 VM keeps its installed standard cron and demo. Packaged Cronie
is extracted, not installed, and runs under a test-only isolated systemd unit
with private cron/spool/PID/default/PAM paths. Test binaries alone recognize those
root-protected adapter mappings. Systemd launch helpers share the fixture state.
No production binary accepts adapter overrides from environment variables/files.
Real minute ticks cover both default inotify and `-i` polling, non-root execution,
detached non-PHP descendants, natural drain, new 8.3/8.4/custom jobs, Run Now,
branch holds/resumption and absent-file preservation with unchanged PID/boot.
Immutable v0.4.36 helper fixtures exercise mutations and cross-journal crash
boundaries. Loader-only evidence complements, and does not replace, these tests.
