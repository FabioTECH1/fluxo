---
title: CLI Commands
description: Fluxo service, version, and root-only account-recovery commands.
---

# CLI commands

## Baseline bridge

`fluxo --bridge-support` and `fluxo --bridge-contract` report static capabilities without initializing the daemon. `fluxo baseline-bridge inspect` checks existing state without changing the installation; `certify` requires a stopped panel and records completion only after qualification; `verify` requires that receipt and current readiness. These commands require root and the installed HTTP/data-directory context. Configuration testing in `certify` and `verify` can open Nginx logs. See [Baseline bridge](../operations/baseline-bridge) for recovery and service restart instructions.

The production binary is installed at `/usr/local/bin/fluxo`. Most server management belongs in the dashboard; the public CLI focuses on service startup, diagnostics, and account recovery.

## Execution reader formats

`fluxo --execution-capabilities` reports compiled journal, adapter and profile contracts without initializing state. `sudo fluxo execution-compatibility inspect` checks protected compatibility requirements without migration or lock creation.

The normal installer performs first reader conversion automatically after the installed panel passes its health checks. It briefly stops and restarts the panel, preserves application daemons and cron, and retains the exact operation and original restart intent for interruption recovery. Rerun the same release's normal installer to resume; a different image cannot replay that operation. Existing compatible committed reader6/reader7 floors are preserved. Genuine job acknowledgements and bridge certification remain separate from format conversion.

The v0.4.49 correction guards historical task writers before recording the operation pointer or replacing helpers and cores. It covers supported first conversions of existing schema1/2 task state with unchanged, prior-boot or independently retired providers. If interrupted between guard and pointer publication, the same image recovers the exact staged plan using the guard's operation and plan binding. It keeps the guard through partial core conversion and restores the original initialization marker only after the exact converted task core and prepared reader floor are durable. Ordinary task3 dispatch remains blocked until commit. Do not edit or remove either marker to force a retry. See the [qualification record](../operations/baseline-bridge) for the native interruption case that was skipped and the separate regression evidence.

For an older **unfenced, entirely unapplied** historical operation, the corrected normal installer can authenticate a newer release and withdraw the old plan before replanning. This is a separate withdrawal, not cross-image replay. It requires the exact installation, original image and service, no reader floor or applied conversion target, matching archived sources and independently validated terminal-history advancement. It preserves the original operation under `execution-installer-withdrawals` and retains the live journal. Ambiguous advancement, incomplete evidence or any applied conversion remains refused. Ordinary updates do not require a separate withdrawal command.

**If Cronie restarts again during an interrupted historical task conversion**, the installer may report that the archived generation has been replaced on the same kernel boot. Preserve the pending operation. For this diagnostic, schedule an operator-approved host reboot, then rerun the **same release’s normal installer**. Recovery checks the kernel boot ID, qualifies Cronie under the archived provider, group and configuration, and checks the live execution domains. If the incomplete reader floor caused the panel to fail at boot, the installer can clear its failed status only after verifying the exact pending operation, archived plan/image, installation, service binding and empty panel domain. Clearing the failed status does not authorize execution; the normal activation checks and genuine cron acknowledgements still apply. It publishes the original planned task registration with its acknowledgement pending. Only a genuine cron invocation can acknowledge the nonce and bind the new boot’s provider. A combined Scheduler conversion keeps its separate retirement and acknowledgement requirements. Ordinary installations follow the normal procedure above.

For a separately managed binary installation, the explicit operator commands remain available. Finish active panel commands and runtime maintenance, then stop the panel and activate:

```bash
sudo systemctl stop fluxo.service
sudo /usr/local/bin/fluxo execution-compatibility activate
sudo systemctl start fluxo.service
```

Activation requires root and the default installed data directory. It preserves existing identity and original evidence, stages the exact executing helper image, converts Scheduler/system-task formats and refreshes enabled job triggers. New triggers require genuine cron acknowledgements after commit. `format_active: true` does not mean bridge certification or successful job execution.

A certified pre-schema5 Scheduler proof can belong to an earlier server boot. Ordinary runtime checks and bridge certification continue to reject that stale launcher identity. Stopped-panel `activate` can convert this state only when the exact historical per-entry acknowledgement remains intact, all retirement and invocation obligations are resolved, and the current engine independently qualifies under the unchanged saved backend/configuration contract with its execution domain naturally empty. Activation preserves the old proof; the first current loader epoch needs fresh genuine acknowledgements before jobs or certification are admitted. A same-boot engine restart, changed provider/configuration, missing receipt or unresolved work remains refused.

First conversion of a schema1/2 maintenance-task core likewise retains the archived original registration and can renew an earlier-boot identity only under its unchanged saved backend/configuration contract after the current domain drains. The replacement helper gets a new nonce and requires a genuine acknowledgement. This does not extend current-schema helper refresh or ordinary readiness.

The normal installer also supports first conversion of an acknowledged schema1/2 task registration after a same-boot Noble Cronie replacement under the unchanged saved provider/configuration contract. It requires the saved daemon to be gone, the saved cgroup inode to be absent from the complete visible unified hierarchy, and the qualified current provider plus all recorded task domains to be naturally drained. Active or uncertain authority, hidden hierarchy mounts and unsupported provider/configuration changes remain refused. Conversion archives the exact original task state and both provider identities, retains old helpers/history, and publishes a fresh helper and nonce requiring a genuine new acknowledgement. The installer rechecks retirement during exact recovery and before commit; it never restarts cron or treats the replacement domain's process count alone as proof. Ordinary execution, receipt checks and explicit helper refresh retain their identity equality rules.

If activation is interrupted, preserve its reported operation UUID and use the same installed image with the panel stopped:

```bash
sudo /usr/local/bin/fluxo execution-compatibility recover OPERATION_UUID
```

Recovery accepts only the recorded original/intended file states and matching installation/image. It does not replay jobs, clear holds or provide a force flag. Unknown or unrelated changes remain blocked with their evidence retained. Continue with bridge inspection and certification only after current readiness passes.

### Historical retirement and reader generations

New activations use archive format 4 and the reader7 contract. Existing format2 (schema4/reader5) and format3 (schema5/reader6) archives retain their original meanings. `execution-compatibility upgrade`, with the panel stopped, explicitly upgrades a reviewed committed reader5 or reader6 installation. It preserves exact source-floor bytes and refuses an unrelated or incomplete source.

An existing historical Scheduler can need real same-boot retirement acknowledgements before it can become ready, while those acknowledgements themselves require current cores. For this case use the separate operation:

```bash
sudo systemctl stop fluxo.service
sudo /usr/local/bin/fluxo execution-compatibility activate-historical
sudo systemctl start fluxo.service
```

This requires matching managed database rows/triggers, preserved historical evidence, no installer/runtime holds and naturally drained recorded work. It archives the original cores and trigger/marker observations, retains original helpers and commits a reader7 floor with retirement still **pending**. Scheduler dispatch and registration changes remain blocked. Existing backend-attempt validation must obtain genuine cron-origin receipts and prove drainage, including deletion obligations. An immutable completion record precedes the permanent completed marker; loader publication and its own acknowledgements follow. Conversion alone grants neither execution nor certification. Recovery uses the exact operation UUID and installed image as above.

### Maintenance registration backend configuration

A package transaction can change the configuration identity of the same cron process. Ordinary V1 identity equality remains strict. Inspect the authentic candidate under each registration's saved backend contract:

```bash
sudo /usr/local/bin/fluxo maintenance-registration inspect
```

The JSON includes `task_core_sha256`, `current_backend_sha256`, `affected_ids` and saved/current identities. Review actual package/configuration changes separately; a qualified current identity is not automatic approval. The correction supports the reviewed Jammy/Resolute cron contracts and requires unchanged backend kind, boot, PID, process start and cgroup path/inode. Reboots, restarts, inconsistent saved cohorts and provider changes are refused.

After reviewing both exact lowercase SHA-256 values:

```bash
sudo systemctl stop fluxo.service
sudo /usr/local/bin/fluxo maintenance-registration requalify TASK_CORE_SHA256 CURRENT_BACKEND_SHA256
sudo systemctl start fluxo.service
```

The operation retains the exact existing reader6 or reader7 contract and compatible helpers. Exclusive lifecycle/package/native-dpkg admission, both journal gates, a stopped panel, no outstanding publication/recovery and authentic empty execution domains are required. Only affected registrations' backend identity, nonce and acknowledgement flag change. Registration boot/identity, contracts, database bindings, helper paths, predecessors, invocation history and Scheduler state remain preserved. The prepared floor durably blocks ordinary helpers before any core/trigger replacement. Publication requires fresh genuine cron acknowledgements; it does not report readiness or modify the bridge receipt.

If interrupted, keep the panel stopped and use the exact original image:

```bash
sudo /usr/local/bin/fluxo maintenance-registration recover OPERATION_UUID
```

Recovery accepts only archived predecessor/intended bytes and unchanged current qualification. After commit, an exact retry verifies sealed publication without resetting a genuine acknowledgement or replaying the archived core. A separate Scheduler loader-policy review may also be required after a package transaction.

#### Repair before the normal installer lease

When the installed image lacks this command or stale task identities block the ordinary upgrade lease, a selected compatible installer provides a single-purpose entry point. Use an immutable reviewed release tag and its matching installer:

```bash
sudo env FLUXO_VERSION=vX.Y.Z bash install.sh --maintenance-registration=inspect
sudo env FLUXO_VERSION=vX.Y.Z bash install.sh --maintenance-registration=requalify:TASK_CORE_SHA256:CURRENT_BACKEND_SHA256
sudo env FLUXO_VERSION=vX.Y.Z bash install.sh --maintenance-registration=recover:OPERATION_UUID
```

The installer authenticates a protected staged artifact using release provenance before executing it. A local acceptance build requires explicit `--local-binary=PATH` and `FLUXO_LOCAL_BINARY_SHA256`; that is local-build authorization, not public-release verification. A custom URL or skipped attestation cannot authorize this entry point. The private single-use handoff binds image, version, architecture, installation, source floor, operation, arguments, parent process generation and expiry. Expiry is enforced through publication and commit; interrupted work requires a fresh authenticated handoff for exact recovery.

Mutation records the original panel restart intent durably, stops the panel, and restarts the original compatible panel after successful publication so retained helpers can acknowledge. A failure retains the stopped panel, candidate, handoff, installer intent and operation evidence. Installer evidence is under `/var/lib/fluxo/maintenance-registration-installer`; restart intents are under `/var/lib/fluxo/maintenance-registration-installer-intents`. If no activation plan was created before interruption, preserve the intent, verify compatibility of the unchanged installation and restore its prior service state explicitly before creating a fresh reviewed operation. This command exits after repair; run the ordinary installer separately after real acknowledgements arrive.

On failure, the installer prints its evidence-directory path. Read that directory's root-only `stdout.json` for the structured operation report and recovery UUID, and `stderr.txt` for the refusal reason. A missing operation UUID means a new activation plan was not established; do not invent one or use an unrelated recovery operation.

### Refresh retained task helpers

A compatible panel update does not overwrite immutable helper images. If a retained task helper can no longer qualify the updated host, explicitly refresh acknowledged task registrations using the installed versioned candidate:

```bash
sudo systemctl stop fluxo.service
sudo sha256sum /var/lib/fluxo-system-tasks/state.json
sudo /usr/local/bin/fluxo execution-compatibility refresh-task-helpers TASK_CORE_SHA256
sudo systemctl start fluxo.service
```

`TASK_CORE_SHA256` is the exact lowercase SHA-256 printed for the current task journal. A changed source or missing/currently unsupported reader contract is refused before a new operation is archived. This requires an existing committed schema5/task3 reader7 installation, naturally drained execution and the existing lifecycle/package/native and journal gates. It does not initialize absent state or convert historical formats. Registrations must retain their genuine prior acknowledgement and exact published trigger; this command cannot invent that history.

The operation archives the original floor and task state, publishes a prepared floor before replacing helper references, stages the exact executing image and refreshes enabled task nonces. While prepared, compatible retained binaries refuse ordinary execution. The committed floor keeps the same reader contracts and records the new operation; previous archives and helper bytes remain available. Disabled registrations, application intent and invocation history are preserved. Scheduler state and loader epochs are not rewritten. After restart, real cron receipts must acknowledge the new nonces before readiness returns.

Use `execution-compatibility recover OPERATION_UUID` with the same installed image if interrupted, including a failure before the prepared floor was written. Recovery reuses the exact archived intent; it does not regenerate nonces. Preserve the stopped panel and all evidence if recovery fails. `format_active: true` still does not certify the bridge or assert successful task execution.

## Interrupted historical maintenance

After an interrupted job has naturally drained, use explicit root recovery:

```bash
sudo env FLUXO_ENV=prod FLUXO_DATA_DIR=/var/lib/fluxo \
  /usr/local/bin/fluxo system-task-reconcile
```

This does not stop a running job, replay its payload or declare success. Existing lifecycle/package/native locks and actual execution-domain drainage remain required. Recovery retains the original profile and unknown outcome, records fully qualified current evidence separately and archives only terminal, empty authority.

Baseline V1 Composer, Certbot and package-cleanup observations have a retained versioned producer. The host must first pass the full current inspection closure; every original nonmutable observation must then match the independently derived V1 view, including key presence. Added current evidence is not a reason to discard old observations. Existing permitted data effects and the narrowly scoped Nginx reboot rule remain; changed executable/configuration or missing anchors are refused.

Exact Ubuntu 24.04 APT `2.8.3` is available only for historical recovery inspection. It is not selected for fresh cleanup execution. No unreviewed V2 producer mapping is guessed; unsupported or mixed profiles retain their evidence and require investigation. Do not downgrade packages, edit the journal or clear holds to make recovery pass.

## Scheduler loader policy

The normal bridge installer qualifies and renews supported Scheduler policy changes automatically. Rerunning the selected installed release also checks compatibility without replaying package provisioning. The installer keeps the saved backend/profile contract, validates the full current closure, archives exact source/target publication evidence, and waits for genuine cron acknowledgements. Unsupported provider or pinned configuration changes remain refused.

On Ubuntu 22.04/26.04 the backend configuration digest also includes the independently qualified PAM/package closure. The installer can renew maintenance registrations for a fully qualified replacement provider after a cron restart or reboot, using a distinct archived transition before Scheduler publication. It requires empty provider and known execution hierarchies, preserves original reader roles and histories, and publishes fresh unacknowledged nonces. The explicit configuration-requalification command retains its same-generation restriction. The ordinary execution/receipt equality checks are unchanged.

The policy still includes protected package metadata. An unrelated transaction can invalidate current evidence, but a separate policy-renewal command is no longer part of the normal installation procedure. The commands below are advanced inspection and explicit administration tools. Interrupted installer setup is resumed by rerunning the exact same release installer; it authenticates the recorded image before ordinary provisioning.

```bash
sudo /usr/local/bin/fluxo scheduler-loader inspect
```

Inspection requires root and the default installed data directory. It acquires existing coordination locks without creating them or changing registrations. JSON contains `epoch`, `saved` and freshly qualified `candidate` providers. Each provider includes its backend identity, profile, `policy_hash` and `runtime_hash`. A pending interrupted publication is returned as `pending_publication` with a nonzero exit status. Historical aggregate digests cannot reconstruct a field-level configuration diff; review actual package/configuration changes separately. A qualified candidate does not authorize migration automatically.

After review, stop the panel and supply the exact epoch and both policy hashes from inspection:

```bash
sudo systemctl stop fluxo.service
sudo /usr/local/bin/fluxo scheduler-loader requalify EPOCH OLD_POLICY NEW_POLICY
sudo systemctl start fluxo.service
```

`EPOCH` is the saved epoch UUID. `OLD_POLICY` and `NEW_POLICY` are distinct lowercase 64-character SHA-256 digests. Publication requires a versioned installed binary, exclusive lifecycle/package/native-dpkg admission, naturally empty execution domains, no pending panel work and unchanged expectations. It cannot select a different backend/profile, edit database rows, clear holds or replace activation/bridge receipts. Existing entry identities and history are preserved; enabled trigger nonces rotate. The JSON result reports `review`, `publication`, `new_epoch`, `published` and `requires_fresh_receipts`. Publication is not readiness: allow real cron acknowledgements before bridge certification. An empty target set needs no acknowledgements.

SQLite write exclusion can checkpoint existing WAL data and update sidecar files. Database-row preservation does not imply byte-for-byte preservation of the physical database and WAL files.

On failure before intent, inspect again. After an interrupted durable publication, preserve its UUID and recover that exact intent with the panel stopped:

```bash
sudo /usr/local/bin/fluxo scheduler-loader recover PUBLICATION_UUID
sudo systemctl start fluxo.service
```

Recovery accepts only the exact pending UUID and recorded file states. It reports `recovered` and `readiness_verified: false`; it does not replay application work. Do not delete history or restore old nonces. If recovery fails, preserve the stopped panel and reported evidence for investigation. Otherwise restart the panel, including after a refused pre-intent migration, so normal work can resume.

## Version

```bash
fluxo --version
```

Example output:

```text
fluxo version 0.4.49
```

## Show the administrator username

```bash
sudo fluxo --show-admin-username
```

This root-only command opens the installed Fluxo SQLite database read-only and reports the claimed username. It does not change account state or invalidate sessions.

## Reset the administrator token

```bash
sudo fluxo --reset-token
```

This root-only command:

1. Reports the configured username or first-login state.
2. Generates a new secure token.
3. Displays the new token once in the command output.
4. Replaces the stored password hash.
5. Invalidates existing JWT sessions.
6. Writes the current recovery details to `/var/lib/fluxo/.fluxo_credentials` with root-only permissions.

The reset is journaled before either the recovery file or database hash becomes authoritative. If the command is interrupted, normal Fluxo startup completes the same reset token instead of leaving an invalid recovery copy.

Use the token to sign in, then change the password from Settings.

Do not combine `--reset-token` and `--show-admin-username`; Fluxo exits with status 2 rather than choosing one unexpectedly.

## Scheduler verification and diagnostics

If PHP maintenance reports that earlier manual Scheduler runs need verification,
run this from SSH using the installed binary:

```bash
sudo /usr/local/bin/fluxo scheduler-verify-panel
```

It briefly holds exclusive Fluxo maintenance admission, waits for admitted panel
work and its descendants to finish naturally, and records proof for the exact
running binary and current boot. It does not reinstall packages, restart the
panel or cron, or terminate jobs. A busy panel can time out safely; let its work
finish and retry. It refuses unsupported/custom panel execution, a different
running binary, pending PHP recovery, and an unfinished installer. Retry the
original installer for an unfinished installer marker. Verification does not
clear cron receipts, backend-history checks, custom process blockers, or holds.
Review the PHP update again afterward.

For a read-only, allowlisted support bundle without application commands,
credentials, environments or receipt nonces:

```bash
sudo /usr/local/bin/fluxo scheduler-diagnostics
```

These commands default to `/var/lib/fluxo/fluxo.db` from an ordinary SSH session.
If the panel uses a custom data directory, set `FLUXO_DATA_DIR` explicitly to
that same absolute directory. Older releases without `scheduler-verify-panel`
require a reviewed upgrade, or an installer repair pinned to the intended
published release with `--reinstall`; an ordinary same-version installer run
exits without establishing the boundary.

## Service commands

```bash
sudo systemctl status fluxo --no-pager
sudo systemctl restart fluxo
sudo systemctl stop fluxo
sudo systemctl start fluxo
sudo journalctl -u fluxo -f
```

Restarting the Fluxo daemon temporarily interrupts dashboard and API access. It does not restart hosted Nginx, PHP-FPM, database, or application services unless startup recovery explicitly reconciles their state.

## Health endpoint

```bash
curl -k https://127.0.0.1:9595/api/v1/health
```

The health endpoint is public and suitable for local service verification. Do not treat it as proof that every hosted site and dependency is healthy.

When a panel domain is active, `https://PANEL_DOMAIN/api/v1/health` checks the Nginx proxy path as well. Fluxo's installer verifies both the local endpoint and any active panel domain during upgrades.

## Runtime environment

| Variable | Default | Purpose |
|---|---|---|
| `FLUXO_ENV` | `dev` | Use `prod` for installed-server data defaults |
| `FLUXO_PORT` | `9595` | Dashboard/API listen port; port `6060` is reserved for loopback diagnostics |
| `FLUXO_DATA_DIR` | Current directory, or `/var/lib/fluxo` in prod | Persistent control data |
| `FLUXO_USE_HTTP` | unset | Set to `1` only behind a trusted local reverse proxy |

The installed systemd service sets production configuration. Override it only with a deliberate systemd drop-in and retain a recovery path.

## Installer runtime admission

The SSH installer uses the root-only `installer-runtime-state` command with
explicit `FLUXO_ENV=prod` and an absolute `FLUXO_DATA_DIR`. It reads existing
maintenance tables without initializing or migrating them and reports whether
work is active, PHP maintenance blocks upgrading, or a paused Node generation
is understood by the selected release. It does not approve repairs, clear holds
or rewrite history. Users normally run the installer rather than this internal
admission command.
