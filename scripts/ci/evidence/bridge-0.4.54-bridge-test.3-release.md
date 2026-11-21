# Opt-in bridge trial 3

Ready for an explicitly selected VPS trial: immutable prerelease
[v0.4.54-bridge-test.3](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.54-bridge-test.3),
source `39583b78d99f7dfc56ce1a5c69be1b0379bbffdd`.
Main and latest remain stable v0.4.53. No clean-base release or existing immutable
asset was changed.

New PHP/Node reviews use the saved graceful shutdown settings for supported custom
direct PHP services, including full process-group drainage and once-only recovery.
There is no automatic SIGKILL. Earlier unfinished approvals keep their original
shutdown policy. The PHP debugger companion participates in coherent authenticated
branch updates. Repair tracks accepted starts through status-read failures. Error
details offer the applicable repair review or update check; repair still requires
its existing confirmation. Notification banners remain visible at their
destination while redundant navigation hides.

The official build [44921030602](https://github.com/FabioTECH1/fluxo/actions/runs/44921030602)
passed on its first attempt. Independent checks verified all eight immutable
assets, four source/tag/hosted-workflow attestations, both ELF metadata sets,
19 official tool pins and embedded new UI/runtime markers. Source qualification
includes the full Go suite, affected race/vet, 33 planner and 58 UI tests, builds,
and three actual AMD64 lifecycle/recovery cases. All Go inputs in this release
match that lifecycle subject `81db2fc`.

The exact-public Ubuntu 24.04 AMD64 installer upgrade from genuine public trial 2
passed. Installed executable, running panel and reconciled Scheduler dispatcher
matched the trial-3 image. The dashboard was healthy, three enabled root tasks had
genuine acknowledgements, and task identity/contracts/helper bytes, reader roles,
site/user/cron configuration, keys/TLS/SSH/history were preserved. The fixture had
one site, one user, three root cron records, no GitHub accounts and no daemon rows;
this is not nonempty account/daemon migration coverage. Separate native lifecycle
fixtures cover the changed process shutdown path.

The first final-read assertion compared the dispatcher before startup
reconciliation settled. The exact trial-3 dispatcher was subsequently observed;
a read-only recheck with a bounded convergence wait passed. The initial assertion
log is retained. No installer rerun or production relaxation was used for that
check. A normal same-version installer without local/custom overrides then
passed in 13.771s while preserving the panel PID, executable and reader floor.
The full predecessor/candidate runs took 341.336s/241.465s and included slow public
WP-CLI downloads; these are not controlled speed comparisons.

Completed candidate transport used independently attested public installer/binary
bytes with local SHA verification. Full public-network candidate transfer, new
native ARM execution, full PHP package update or Node migration, real Reverb
health, ACME/Composer/hook execution and automatic wildcard renewal are not
claimed. Official build metadata records `vcs.modified=true`; exact source/tag
and workflow provenance and pins passed, without a pristine-VCS or reproducibility
claim.

The original named-VM snapshot was restored and read back against original image,
version, selected data/configuration, protected files and all timer states. Trial
scratch was absent. All three local guests are stopped. Private attempt evidence
is retained under archive SHA-256
`9dbf0db7bdabf811330cfd74abd1fbab76f9758c03256f67915f7dcca87402bc`.
No customer VPS was accessed.

## Install explicitly

Retain the server's usual component choices; for a Node/Python setup:

```bash
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54-bridge-test.3/install.sh | sudo env FLUXO_VERSION=v0.4.54-bridge-test.3 bash -s -- --node --python
```

Refresh the panel afterward and request a fresh PHP/Node check/review. This is an
opt-in bridge trial, off main/latest. It is not the final stable v0.4.54 identity
required for the future clean-base handoff.
