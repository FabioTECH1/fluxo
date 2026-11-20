# Guarded execution contracts

`systemd-cgroup-exit-v1` describes the existing transient-service execution
contract used by Scheduler and maintenance tasks. systemd 250 or newer and
cgroup v2 are required. This package centralizes only the common launch and
loaded-unit properties; callers still validate their complete invocation policy.
It introduces no journal migration, new execution authority or additional
qualified platform.

The Scheduler and maintenance journals continue to own durable intent and
one-use launch authorization. They acquire the package lifecycle lease before
their service-specific journal gate at execution admission. Scheduler holds the
shared lifecycle lease through descendant drain. Maintenance retains its
existing execution guard and external-effect reconciliation. This package
takes no locks, opens no journals and never launches or retires an invocation.
No lock order changes are implied by sharing the property contract.

The versioned identity is immutable. Existing unversioned records retain their
current cgroup-exit interpretation. Before another adapter can issue execution
authority, its identity must be persisted per invocation and minimum-reader
requirements must prevent incompatible downgrade. Readers must never select an
old invocation's semantics from the current host version.

Ubuntu 22.04's systemd 249 remains unsupported by this contract. An independently
qualified adapter must track descendants after helper death, close late launch
authorization before retirement, preserve uncertainty, and prove natural drain
without killing descendants or replaying consumed work. A wrapper that merely
waits for children, or changing the minimum version to 249, is insufficient.
Native cron migration and APT/Certbot profiles require separate qualification on
every supported release and architecture.

## Disabled systemd 249 candidate

`systemd-persistent-249-candidate-v1` has no production selector or journal
integration. It uses `Type=exec`, `RemainAfterExit=yes`, `KillMode=none`,
`Restart=no`, and `CollectMode=inactive`, without `systemd-run --collect`.
Successful main exit and main failure are different service transitions; neither
is a natural-drain certificate. Population must be checked independently and
bound to the original boot, invocation ID and cgroup identity. A successful empty
unit remains active until explicitly retired after that proof.

The upstream v249 implementation grounds the candidate, but does not establish
Ubuntu package qualification:

- [`unit_may_gc` in unit.c](https://github.com/systemd/systemd/blob/v249/src/core/unit.c)
  checks cgroup population before allowing collection.
- [`unit_prune_cgroup` in cgroup.c](https://github.com/systemd/systemd/blob/v249/src/core/cgroup.c)
  does not erase the accounting path when trimming a populated group fails.
- [`service_enter_running` and main-exit handling in service.c](https://github.com/systemd/systemd/blob/v249/src/core/service.c)
  distinguish failure from successful `RemainAfterExit` behavior.

`TestVMPersistent249DomainLifetime` runs only as root with
`FLUXO_PHP_UPDATE_VM_TEST=1` on `fluxo-bridge-jammy-20261101`. It copies its fixed
executable into a unique root-owned directory and launches an unprivileged,
session-detached grandchild. Readiness and challenge/response handshakes establish
progress before and after main exit, `reset-failed`, `daemon-reload`, and
`daemon-reexec`. The crash case signals only the recorded helper through a pidfd.
Boot, process start, unit invocation ID and cgroup inode/device are recorded.
The payload exits cooperatively; cleanup never kills it to establish drain.
Evidence remains in its private acceptance directory.

Passing this experiment establishes only the tested domain lifetime behavior.
It does not establish late-launch retirement, durable one-use authorization,
crash-boundary recovery, package lifecycle admission, downgrade safety, native
cron migration, APT/Certbot profiles, or production Ubuntu 22.04 support. Those
remain prerequisites before any production integration.

## Versioned execution readers and explicit activation

`fluxo --execution-capabilities` is pure metadata. It declares Scheduler support
5 (core schemas 1/2/3/4), system-task support 3 (core schemas 1/2/3), exact adapter IDs
and separate execution/history-reader/backend contracts. The stable
`systemd-persistent-v1` lifetime implementation is compiled and routes saved
records, while automatic host selection still uses only `systemd-cgroup-exit-v1`.
The old experimental candidate ID remains unsupported in journals.
`system-task-profile` requires `apt-a0-noqueue-v3`. Legacy host-input V1 and
`apt-a0-host-inputs-v2` remain history-reader contracts; they do not authorize
current APT execution. The first bridge activation must publish the V3 floor.
An existing V2 execution floor requires a separately reviewed whole-state upgrade;
changing its role metadata alone does not migrate retained helper authority.

Pure `--execution-capabilities` metadata additionally advertises optional
`system-task-optional-profile` features for reviewed Imagick startup and
conditional Certbot deploy hooks. They are not global floor requirements.
Only a new submission using one of these profiles requires its exact retained
helper to advertise that feature, before persisting authority. Unsupported old
helpers remain retained for compatible historical recovery; an explicit archived
helper refresh is required to use the new profile.

`fluxo execution-compatibility inspect` checks protected reader constraints and
live formats without LoadConfig, SQLite initialization or lock creation. Every
ordinary startup (including unset/dev environments) and installed mutation CLI
checks compatibility before configuration initialization. Pure metadata and
read-only account recovery remain available during incompatibility.

From SSH, install the compatible versioned binary through the reviewed installer,
stop `fluxo.service`, then explicitly run:

```
sudo /usr/local/bin/fluxo execution-compatibility activate
```

Activation takes exclusive lifecycle, package, native-dpkg and coordination gates.
It requires existing guarded invocations to be quiescent, preserves the existing
installation identity and original state snapshots, stages the exact executing
installed image into dispatch/helper roles, converts both execution cores
into durable old-reader admission fences, then publishes a prepared minimum-reader
floor and refreshes enabled task triggers. Scheduler
core is fenced first because legacy task helpers consult its pending admission.
Genuine absence is an explicit creation plan: both empty current cores and their
protected directories/gates/markers are created. Operation-bound invalid anchors
are published atomically without replacing existing markers before creation;
ordinary startup refuses these partial markers even before a floor/core exists.
Current core publication precedes the prepared floor, and ready markers are
finalized only after core publication. Plain legacy cron files and database rows
are preserved and remain separate migration obligations. New readers reject
new core authority without its floor. Existing task acknowledgements must already
be settled and their original receipt encoding replay-stable. Command,
enabled intent and registration identity remain unchanged. Each refreshed trigger
gets a new nonce and `Received=false`; its genuine provider acknowledgement and
normal readiness checks happen after floor commit.

The protected immutable operation plan binds original/intended file checksums,
image, installation and database state. Interrupted work resumes only through:

```
sudo /usr/local/bin/fluxo execution-compatibility recover OPERATION_UUID
```

Recovery requires the same installed image and exact operation evidence. Each
target must contain its recorded original or intended bytes; unrelated changes
are refused and preserved. It does not replay application work or offer a force
flag. The activation archive, original helpers and historical journals remain.

Every new guarded invocation requires the committed current floor and both
current cores, and saves its selected adapter before submission. Consumed
persistent invocations also bind PID1 invocation generation and kernel cgroup
identity before payload launch. Recovery inspects the saved adapter; missing IDs
in historical records always retain the original250 lifetime. Persistent
retirement requires a natural hierarchical-empty proof before stop/reset-failed;
failed/inactive service metadata alone never grants that proof. Caller-owned
Scheduler capabilities and root maintenance namespace policies remain distinct.

New cron backend writes independently require exact compiled, committed floor and
actual retained-helper `scheduler-backend-reader` contracts. Format activation
refreshes helper identity without changing backend labels. Explicit backend
migration follows format commit; host selection remains a separate reviewed gate.

A committed `execution-readers.json` means format/helper readback succeeded. It
is not bridge certification, successful task execution, platform qualification or
permission to remove compatibility/recovery history. New core invocations require
explicit known adapter identities; absent identities in old cores retain their
original cgroup-exit meaning independently of current systemd.

Installers refuse prepared/unsupported floors before provisioning. Committed
floors require actual candidate compatibility, including profile contracts.
Rollback preserves a compatible candidate and evidence when the previous image
cannot read the current floor. Previously downloaded installers and older
executables cannot retrospectively acquire these checks.

## Reader7 historical retirement

Reader7 adds `scheduler-historical-retirement` / `fenced-historical-retirement-v1`
without changing the immutable reader6 minimum of ordinary schema5 execution
and backend identities. Compiled support and historical authority minima are
separate. Archive formats2/3 retain literal reader5/6 contracts; format4 records
new reader7 activation and narrowly scoped task requalification operations.

Historical conversion retains unresolved intent behind an operation-bound
pending marker. Current cores plus the committed reader7 floor allow existing
backend receipt/drainage bookkeeping; application dispatch, registration changes
and loader publication remain fenced. All obligations must resolve before an
immutable completion record and completed marker permit loader publication.
A completed marker is retained permanently and verified through archive lineage.

Task backend configuration requalification preserves its exact reader6 or7
source contract, never downgrades or drops a role, and leaves Scheduler state
unchanged. Both prepared-floor fencing and exact-image recovery remain required.
