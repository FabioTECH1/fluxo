# Clean base v0.4.55-base-test.1: lease startup and check results

Ready for an opt-in **Ubuntu 24.04 AMD64 completed-bridge055 → base trial**, off main/latest. [Full JSON qualification](base-0.4.55-base-test.1-release.json) retains the exact public subjects, source-bound signatures, native report, control failures, limits and cleanup.

| Identity | Value |
|---|---|
| Immutable source | `a309e1a10124b15d9c075f4baa50c7828b322fbd` |
| Release | [v0.4.55-base-test.1](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.55-base-test.1), `454450096` |
| AMD64 SHA256 | `d2a79c93e4f5d7a1f650a00cd2412ece4512281efcb9f09460bff770eb486438` |
| ARM64 SHA256 | `637dfa262f0b6532117ef30b7cdfdc7bdc2031a88de958f1a1ad437d76f05bab` |
| Installer SHA256 | `6e20f6dfc42b6cb7c10de5cd969c6ff7a3c0338a3a735a99d2c0874fac410eba` |
| Official build | [45024866489](https://github.com/FabioTECH1/fluxo/actions/runs/45024866489) PASS |
| Exact-public native installer component | [45025292503](https://github.com/FabioTECH1/fluxo/actions/runs/45025292503) component PASS, fixture `38e5bd34acbd657be0594ec81d57820fdda50d4f`; later terminal probe failed |
| Public-only terminal recheck | [45026896208](https://github.com/FabioTECH1/fluxo/actions/runs/45026896208) PASS, fixture `a1df639c5f7e4af293feac8540082803bba23b0e` |

The installer creates its private output/error files synchronously, before asynchronous lease-helper redirections. A deterministic delayed-child test reproduces the original missing-output error against unchanged signed base4 installer bytes and passes with the correction, including `0600` files under a permissive caller umask. Helper readiness timeout or retained-running-helper failures keep original diagnostics, preserve exit status and recommend the same exact tagged installer with shell-escaped original flags, after any required recovery/review. Custom sources retain original-source instructions without exposing credential URLs. This changes no lease authority, readiness budget, reader/schema format or recovery operation.

The base-only Node UI now says “No Node.js updates available” or “A Node.js update is available” with its version, using the saved successful discovery already returned. Failed, stale, blocked, unavailable and older snapshots do not become up-to-date; migration remains distinct. Real composable/component tests cover outcomes, full diagnostics and one existing status request. The durable worker message, backend, selection and review gates remain unchanged. This copy was not added to the bridge.

Legacy admission is bound to exact signed stable v0.4.55 source/images; older v0.4.54 joins the refusal-only lineage even beside a surviving base receipt. Existing admitted bases, including base4, still update directly. Fresh-base state, protected data/key/identity, archive/floor checks, genuine receipts and all current/historical live recovery readers remain. The update banner excludes stable v0.4.55 and earlier bridge releases. No manual host certification or optional GitHub report is required.

Local validation passed:8 lease tests with1 intentionally guarded native-VM skip,9 driver checks,118 Python methods with2 built-CLI methods separately exercised,84 frontend+8 console tests, affected Go/race/vet, UI/site/docs builds, copies and syntax. Official release validation passed its full backend/frontend pipeline. Public verification covers all8 assets,4 source/tag/official-hosted signatures and both pinned binaries. Native acceptance passed completed bridge v0.4.55→base→ordinary update with data/key/history/retention/deployment preservation, real root-task/Scheduler execution and current Node preservation. The first job later failed because an unprivileged Node CLI argument probe encountered protected installed state and returned preflight exit1 before usage2. The fixture was corrected to run privileged probes in the documented root scope; product preflight and exit codes were unchanged. A public-only recheck repeated no installer or host preparation and passed all five published-CLI terminal methods, including colour/plain/NO_COLOR/dumb/unset/unknown-terminal, metadata and exit-status checks.

The existing-server fixture starts from authentic public bootstrap6 with stock PHP8.4 and no optional Redis extension, then completes055 normally; it is not a cold public055/fresh-base claim. No new native ARM64/Cronie/Ubuntu22/26, PHP/Node Apply/migration, real Reverb, ACME/DNS, GitHub credentials or customer VPS operation is claimed. Prior evidence keeps its actual source, and the user-skipped interruption remains excluded.

```sh
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.55-base-test.1/install.sh | sudo env FLUXO_VERSION=v0.4.55-base-test.1 bash
```

Already-admitted bases use this command directly. Legacy installations first complete stable v0.4.55. Any unfinished operation retains its original exact recovery route.

The transient acceptance branch was removed only after its commits were contained in baseline and all runs completed. `fluxo-dev` remains the only VM, stopped and untouched; no VM was started/created/deleted for this task. Evidence, old immutable tags/assets, caches, backup and unrelated work remain. The originating chat separately promoted stable bridge055; this base was never merged to main or promoted latest, and no VPS was deployed.
