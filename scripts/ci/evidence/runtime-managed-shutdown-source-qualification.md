# Managed runtime shutdown: source and bounded native qualification

Runtime source: `81db2fcce68e81c9f4f5400939db069e20ca719f` on
`fix/bridge-runtime-compat`. **Source and the scoped native lifecycle checks pass.
This is not a published release or signed public-artifact acceptance.**

New PHP reviews and Node's PHP coordination use policy 2. Supported custom direct
PHP services use their saved graceful signal and stop timeout, without an Artisan
action allowlist or changes to feature ownership. The protected maintenance
barrier signals the full service group, disables automatic restarts and prevents
systemd's SIGKILL escalation. All descendants must finish before mutation.
Ignored shutdown produces a named timeout and retains recovery state. Previously
stopped services remain stopped. Activation identity/completion evidence prevents
replay of completed custom commands.

Old PHP approvals retain policy 0 and old Node PHP coordination retains
policy/schema 1. Their natural-only custom commands receive no newly granted
signal authority. New Node state uses schema 2; read-only installer replacement
admission and preparation both bind policy, schema, approved shutdown metadata
and retained snapshots. Global execution-reader/task schema contracts did not
change. Configured waits remain subject to the operation's remaining budget;
Node workers retain their 30-minute overall limit.

The PHP planner now includes an installed `phpX.Y-phpdbg` companion while keeping
source/coherence, trusted archives, dependency, hold and no-removal checks.
Unmanaged running branch debugger processes are included in the mutation guard.
CGI and development packages remain outside this transaction's supported family.

Shared UI commits:

- `bcc2d2ee6754d50aa6fc19ca914e1f8648a269ee`: keep notification banners visible;
  hide only navigation whose resolved destination is already current. Includes
  failures, modal actions and both responsive toast layouts. Different views,
  query/hash values and external/callback actions retain their controls.
- `f9efc82b299572c762e777acf5f3534abab9a416`: track accepted maintenance before
  follow-up status reads. Repair/rollback confirmations close on acknowledgement;
  submissions and accepted operations lock conflicting PHP/Node actions through
  refresh failures. Rejected/uncertain requests retain the existing request ID.

## Validation

- Full Go suite, affected PHP/Node race suites and all-package vet pass.
- 48 frontend tests pass (40 frontend/mounted plus 8 Node maintenance console).
  Typecheck, UI build, site/docs build, diff checks and installer-copy comparison
  pass. Existing Vite chunk-size advisory remains.
- 33 planner fixtures pass, including debugger coherence/trust/hold/dependency
  negatives and continued CGI/dev refusal. Installer policy/schema read-only
  compatibility and tamper regressions pass.
- One bounded correctness review identified group-signal delivery, installer
  policy/schema binding and stale copy issues; all were fixed and a narrow
  follow-up found no further concrete issue. The remaining time-budget limit is
  explicit in review copy/docs.

Three actual Ubuntu 24.04 AMD64 native tests passed at the exact runtime source:

| Lifecycle fixture | Result |
| --- | --- |
| PHP declared INT/TERM, parent and PHP child, ignored shutdown past the original native timeout, retained holds, cooperative completion and once-only recovery | Pass, 13.50s |
| Retained legacy custom recovery sends no signal, waits for real completion and resumes once | Pass, 4.80s |
| Node-owned policy/schema 2 PHP shutdown, QUIT delivered to parent and child, stopped instance preserved, fresh recovery and once-only completion | Pass, 8.93s |

The transferred CGO-free test binaries matched their local SHA-256s. All six owned
service units ended inactive with zero main/control PIDs; owned unit/drop-in and
evidence files were absent. The original snapshot
`fluxo-php-patch-test.before-managed-stop-20261121` was restored. All three existing
Multipass guests are stopped. No customer VPS was accessed.

Two earlier native attempts failed on fixture assumptions: an intentionally empty
Scheduler directory was incorrectly sent through global Enqueue/pending checks.
The lifecycle-only fixture now creates private test operation state and asserts
its exact retained barrier rows. Production admission and Scheduler receipt
checks were not changed or bypassed by an application update. A subsequent
three-case pass preceded the final interpreter-flag rejection; the final pass
above rebuilt and executed the frozen source. Earlier logs remain retained.

## Scope limits and publication

These new fixtures test actual systemd service lifecycle, not a full PHP package
transaction, Node migration or real Reverb application. They execute no real
ACME, Composer or custom renewal hook. The VM has no debugger companion package,
so debugger installation/apply is covered by planner/process tests, not a new
native debugger transaction. There is no new native ARM64 or signed public-asset
claim. Shell wrappers, PHP interpreter flags, unsupported descendants and custom
lifecycle overrides still require review. Existing specialised feature shutdown
protocols remain in place.

The maintenance-lock implementation from the qualified immutable
`v0.4.54-bridge-test.2` is unchanged. These fixtures do not claim another complete
installer-lease acceptance. Locks are never deleted or overridden to make an
update succeed.

Main, latest, tags and existing immutable assets were not changed. A new opt-in
build and its exact public-artifact acceptance remain separate from this source
qualification. Do not relabel this development image as an existing trial.
Exact identities, log digests, review dispositions, cleanup and limitations are
in `runtime-managed-shutdown-source-qualification.json`.
