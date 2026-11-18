# Unchanged ARM64 release investigation

This branch tests the published release before changing application code. It does
not enable ARM64 production support or change the public release channel.

## Result

The final [native ARM64 run](https://github.com/FabioTECH1/fluxo/actions/runs/44316884590)
of the **unchanged** public release passed base installation, automatic format
setup, administrator login, site listing, metrics, PHP site creation, PHP serving
through Nginx and a genuine non-root PHP cron tick. The HTTP probe waited through
one connection closure after Nginx reload; its next request returned the expected
executed PHP output. No application patch was needed for these checks.

Full ARM support is **not qualified**. The provider remained unqualified and the
Scheduler core had zero entries with no loader epoch; the working tick used the
existing ordinary-cron path. No guarded task registration was present. Automatic
runtime maintenance, protected Scheduler execution and historical ARM upgrades
remain unapproved. The final job deliberately fails its overall gate while
recording `base_installation_passed: true` and `basic_application_passed: true`.

The hosted image needed fixture preparation: restricting writable root-owned
system directories, temporarily isolating an unrelated rate-limited Microsoft
repository, and starting its preinstalled Nginx service. These are documented
test-environment changes, not changes to Fluxo or customer installation steps.
Original repository/timer states were restored and the started service stopped.

The source assessment supports a targeted architecture/profile port, rather than
a UI, database or application rewrite. There is no ARM-enabling production patch
ready for merge in this branch. See [the compact result](evidence/arm64-v0.4.47.json).

## Pinned subject

- Release: `v0.4.47`, source `71c35f4e7810005f3e06cc25339e86631ab9bf2b`.
- ARM64 binary SHA256:
  `621f162b43e481f777929b8f280800cd4e9837facf6414545be317563bedc621`.
- Installer SHA256:
  `d5830d050b14b6209f28a74a5da06a0deb040c8025e0fe27b654d3423656c16d`.
- Native `ubuntu-24.04-arm` GitHub-hosted runner, Ubuntu 24.04.5, aarch64 kernel.
- The installer downloads its own public binary and verifies release provenance;
  there is no local binary override, rebuilt application or skipped attestation.
- Optional Node/Python/Redis/database provisioning and SSH hardening are declined
  with normal installer flags. Their fresh-install behavior is not covered.

## Fixture observations

The stock hosted image is not a pristine VPS. In
[run 44312531243](https://github.com/FabioTECH1/fluxo/actions/runs/44312531243),
the binary's version/capability/bridge metadata commands and installer provenance
verification passed. PHP repository preparation correctly refused an unsafe key
path before installing Fluxo. The next run recorded root-owned `/usr/share` and
`/usr/share/keyrings` with mode `0777`; restricting them to `0755` let provisioning
continue without changing Fluxo.

[Run 44313214232](https://github.com/FabioTECH1/fluxo/actions/runs/44313214232)
installed the exact ARM64 image and started the production panel, which returned
version `0.4.47`. Compatibility setup then refused unsafe `/usr/local/bin`;
no reader floor or completed installer intent was produced. A diagnostic metrics
request also used the obsolete `/server/metrics` route; that observer error was
corrected to the source-defined `/system/metrics` route, not treated as an ARM bug.
Both failed runs remain failed evidence.

[Run 44313961507](https://github.com/FabioTECH1/fluxo/actions/runs/44313961507)
recorded and restricted the three writable root-owned directories. APT repository
refresh then failed with exit 100 before Fluxo installation; an earlier run had
passed this stage. Independently, the exact public binary booted in a private
development-mode data directory, initialized SQLite and served version, successful
first login, site listing, metrics and Scheduler status on native ARM. Scheduler
status reported the active ARM64 cron provider as unqualified. This core result
is not an installed upgrade, PHP-site or scheduled-execution pass.

[Run 44314564627](https://github.com/FabioTECH1/fluxo/actions/runs/44314564627)
identified the fetch failure as HTTP 429 from the runner's unrelated Microsoft
package repository. The Ubuntu and Sury repositories responded successfully.
The independent core/API checks passed again. Cron's executable matched Fluxo's
existing ARM64 hash, and five of the six pinned legacy configuration files
matched; `common-session-noninteractive` differed. Consequently this runner's
observed Scheduler refusal is configuration-related. The source-confirmed amd64
guards below are additional independent restrictions; this evidence does not
claim that the runtime reached them through this mismatched PAM stack.

[Run 44315239958](https://github.com/FabioTECH1/fluxo/actions/runs/44315239958)
temporarily archived the CI-only Microsoft source without changing Fluxo. The
unchanged public installer then returned **0**, installed the exact published
ARM64 image, started the production panel and automatically committed Scheduler
schema 5 / task schema 3 with a completed installer intent. Login, site listing
and metrics passed on that installed image. No loader epoch or guarded task
registration was present, so this is not a scheduled-execution pass. PHP site
creation failed because the image's preinstalled Nginx service was inactive.
The Microsoft source and original APT timer states were restored.

[Run 44315899229](https://github.com/FabioTECH1/fluxo/actions/runs/44315899229)
also completed the unchanged installer. Starting the preinstalled Nginx fixture
allowed site creation through the real API. A genuine minute tick executed the
PHP CLI fixture as the non-root `fluxo` user. This was ordinary cron execution:
the Scheduler core still had zero registered entries and no established loader
epoch. It does not prove guarded execution, a loader acknowledgement or safe
automatic PHP maintenance. The first HTTP request after reload closed without a
response; the final bounded readiness observer passed on retry. Original
timer/source states were restored and the started Nginx fixture was stopped.

The probe records directory ownership/modes and restricts writable permissions
only on real, root-owned system directories in the disposable runner. Symlinks,
non-root owners, unknown inputs and application qualification guards remain
untouched. This fixture preparation is not advice to rewrite customer metadata.

## Source-confirmed ARM integration gaps

These are existing production restrictions, not failures introduced by this branch:

- `scheduler/pam_candidate.go`: requires amd64 and selects x86 NSS/PAM paths.
- `scheduler/pam_candidate_loader.go`: x86 search directories, glibc cache ABI
  selection, ELF machine and interpreter checks.
- `scheduler/pam_candidate_vendor.go`: amd64 installed-package filtering and
  `:amd64` manifest selection.
- `scheduler/distribution_candidate.go`: amd64 provider admission and executable
  hashes in the current distribution/loader profiles.
- `scheduler/cronie.go`: explicit amd64 execution qualification despite retaining
  a historical ARM64 executable hash.
- `maintenancetask/apt_platform.go`: Jammy/Resolute execution admission stays
  amd64-only pending native qualification.

The likely change is a bounded platform port: explicit reviewed architecture
parameters and authenticated ARM package/library profiles, using shared inspection
logic. Merely deleting architecture refusals would leave incorrect code and loader
assumptions. Historical backend meanings, exact policy/receipt checks, retained
helpers and active-child drainage must remain intact.

ARM support needs native installed execution, interruption/upgrade coverage and
amd64 regression checks before an enabling patch is suitable for merge. Ubuntu
22.04/26.04 ARM, Cronie ARM, optional runtime downloads, live ACME and full Laravel
framework behavior are not established by this Noble hosted-runner probe.

Sanitized reports/logs are retained as workflow artifacts; raw logs, credentials,
database contents, keys and process environments are not uploaded.
