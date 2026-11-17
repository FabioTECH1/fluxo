# Current Scheduler loader epochs

This implementation is staged behind Scheduler schema 5 and reader support 6.
The schema 4 floor does not authorize epoch writes. Compatible helpers and the
schema 5 core fence precede the committed format floor; ordinary epoch
publication follows that commit. Format activation does not establish readiness.

Historical A/B migration state remains retirement evidence. A loader epoch
proves that the current provider has loaded an exact current launcher. Its
`LoaderNonce` is separate from the historical `MigrationNonce`. Publishing an
epoch preserves historical nonces, readiness, legacy boot fields, migrations,
certificates, deletion tombstones, outstanding invocations and maintenance
holds. Publication refuses unresolved historical retirement; format activation
retains that same prerequisite. No current-epoch receipt falls through to A/B
receipt interpretation.

The epoch store retains every epoch and publication, including original and
intended core and trigger bytes. Superseded pairs become immutable checksummed
files under `loader-history`; the active ledger retains their lineage and
protected device/inode/size/mtime/ctime identities. Ordinary admission validates
that protected metadata, and bridge/backup auditing verifies full content hashes.
Historical core snapshots are not rewritten for every current receipt.
A publication records its complete prepared
epoch and intent before replacing any core nonce or cron file. Recovery accepts
only exact original or intended bytes at every boundary. Unknown core changes,
missing non-absent triggers and operator edits require reconciliation instead
of replacement. All targets are read back before the epoch becomes published.
Acknowledgement remains false until a genuine origin-checked cron receipt is
persisted. A crash after publication and before acknowledgement grants no
execution authority.

The new intent first retains both its own pair and the exact predecessor pair.
While that intent fences ordinary work, the predecessor is archived, read back,
and replaced by its reference before live core/trigger publication starts. A
crash at the archive boundary resumes the same recorded bytes; no unrecorded
orphan file is adopted as a replacement history.

Archive publication uses the kernel's no-replacement rename operation. Existing
names must already be protected regular files containing the exact intended
bytes; links and special files never become replacement targets. Reads reject
unsafe type and size before opening, use no-follow/nonblocking descriptors,
and verify descriptor/path identity before and after bounded reads. The ledger,
restore snapshot and each archive are limited to 64 MiB. Full auditing verifies
the decoded epoch, publication identity and predecessor as well as the hash;
publication identities cannot be reused across retained archives.

`LoaderEpochEstablished` in the core and a separate initialization anchor keep
lost epoch files from being treated as a fresh installation. The initialization
intent is independently retained until the first store and anchor are durable.
Ordinary core readers/writers reject a pending publication; explicit recovery
uses the exact recorded snapshots. History is never pruned automatically. The
bounded store refuses additional publication before exceeding its preservation
budget.

A compatible physical restore can change archive filesystem identities. The
explicit `RebindRestoredLoaderArchivesLocked` step requires the installer's
authenticated snapshot bytes, exact logical equality, every archive content
hash, exclusive lifecycle admission and the core gate. It updates only physical
reference identities. Ordinary admission never performs this repair or accepts
a mismatching file as an EACCES/metadata fallback. Compatible restore still
requires the installer's whole-state and execution-authority checks.
An outstanding initialization marker or publication blocks compatible restore.
Installer and maintenance holds block new generation publication; exact recovery
of an already durable publication may finish without starting another one.

An initialized zero-job Scheduler with an active qualified provider records a
real published empty-target epoch. Missing epoch storage never proves readiness.
This does not initialize an absent subsystem or qualify a disabled provider.

The exact provider identity includes boot, PID/start, hierarchy identity and
the complete qualified runtime configuration. A separate stable policy digest
allows fresh receipt generation after a real boot or daemon restart. It retains
all configuration, code/account resolution and unknown daemon environment
variables. The explicitly excluded runtime fields are validated systemd
`INVOCATION_ID`, `SYSTEMD_EXEC_PID`, `JOURNAL_STREAM`, the authenticated PID1
DynamicUser listener inode set and Noble's manager-matched mount-namespace
generation. RuntimeHash retains these full live observations. Contract, profile or stable policy changes
require explicit migration. Generation renewal creates new targets and retains
every previous epoch; it never relaunches an old invocation or kills children.

The privileged fixed bootstrap calls `RequireEntryLoaderBinding` to validate
the exact receipt, trigger and full current provider. It seals that binding
before irreversible capability reduction and pinned same-image execution. The
three-capability worker calls `ValidateEntryLoaderBinding` under the core gate
to compare the exact current durable authority. It does not retry privileged
provider inspection, accept an EACCES fallback, or infer provider continuity
from peer credentials. These are point-in-time admission checks, not a claim
that an administrator cannot change configuration later.

Jammy and Resolute use their reviewed distribution/PAM closure for the staged
provider policy. Noble has a separate reviewed and natively inspected strict
NSS/SELinux loader profile; its original empty backend identity is preserved.
RuntimeHash separately binds its full live closure. Active Cronie still needs a
strict loader profile; its existing identity/history remain readable and its
new loader qualification currently refuses. Production selectors stay disabled pending
the complete platform and bootstrap acceptance matrix.
