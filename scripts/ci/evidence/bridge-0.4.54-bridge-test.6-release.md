# Bridge test6 early admission qualification

**Known limitation — wait for the next opt-in correction:** the Node provisioning child still performs a one-shot Scheduler check. Cron work arriving after initial admission can therefore roll this trial back during Node setup. Existing source-component/CLI results below do not constitute full Node/Python installer acceptance. A corrective trial is being qualified; no stable/latest promotion has occurred.

**Opt-in trial qualified within the scopes below.** Immutable `v0.4.54-bridge-test.6`, tag source `8d14ea5344b6d61d5572127075f5171d518c3329`, runtime source `3d0f40540c6ff369fc448f2dbda31bc21573b49b`. Official hosted build [44947375536](https://github.com/FabioTECH1/fluxo/actions/runs/44947375536) passed. Main/latest remain stable v0.4.53; this does not publish the clean base or promote stable v0.4.54.

## Problem and change

The earlier upgrade can acquire exclusive lifecycle access after a Scheduler launch is recorded but before its bootstrap obtains shared access. The original check immediately refused the unconsumed launch. User diagnostics showed schema 5, two submitted/guarded unstarted active units and subsequently two live shared lifecycle holders. The exact instantaneous customer timing was not traced.

The initial installer now recognizes only structurally valid current bootstrap authority as eligible to wait. It releases every gate, allows helper progress and repeats reconciliation, maintenance, parent and upgrade checks. Acquisition and pauses share 90s; each root attempt keeps the existing fresh 10s check budget (readiness remains below the shell 120s bound). Parent death, real maintenance, mixed errors, malformed/historical authority and unresolved work still refuse. No launch is consumed, marked started, replayed or discarded by waiting. Published reader/helper capabilities are byte-for-byte equivalent metadata to trial 5; no floor/schema or recovery-format change.

## Actual results and limits

The guarded Ubuntu 24.04 AMD64 component used a normal API site/entry, real cron acknowledgement and hosted-user PHP marker. A real exclusive FD was retained in the FIRST acquisition callback until a genuine cron helper was active and unstarted, making the observation deterministic. The actual reconciliation/prepare path produced 1 typed pending observation, released that FD, allowed natural helper/application progress, reacquired through production lifecycle code and obtained the upgrade lease on attempt 2. Its own normal EndUpgrade cleared holds and upgrade state. Entry/nonce and floor bytes were preserved before normal disable. No journal, seal, nonce, ACK or service definition was synthesized; no job was force-stopped.

The marker proves progress **between admission attempts** after the first FD release, not a separate newly observed tick after final upgrade-lease release. Native preparation used an explicit source-candidate overlay and standard compatibility commands. The Go 1.26.3 CGO=0 source CLI carries all 19 pins; the component Go test has its own separately recorded image. Both are source qualification, not a normal installer/public-image claim. Go/UI/installer/pin inputs are unchanged between runtime source and release tag.

Separately, the exact signed public AMD64 executable obtained READY and released its real upgrade lease through the public CLI in 1.975s (one observation). Its actual process image matched the public digest; parent/lifecycle checks, final cleared holds and unchanged reader-floor bytes passed. **That public run had no pending launch.** It verifies actual public CLI wiring and does not relabel the component as a full public installer test.

All eight public assets, four source/tag/hosted attestations, both ELF architectures, Go 1.26.3, CGO=0 and all 19 pins passed independent checks. Two bounded browser/range download attempts timed out. AMD64 was fetched via GitHub’s official asset API; one slow ARM64 API transfer was stopped before acceptance. Smaller missing ranges completed the downloads. Both full immutable public digests and all four attestations remained mandatory. No source or trust check changed. No new public daemon/full install, native ARM/Cronie, PHP/Node apply, real Laravel-framework, Reverb or ACME acceptance is claimed.

| Public artifact | SHA256 |
| --- | --- |
| AMD64 | `b4224628aa2263036ea2c116c901cf8f1c598cf6610319d7b48b38727605d178` |
| ARM64 | `f80236f444b2464db311e9020cbf5574e33d21cd69f78290eb867fc73758cbca` |
| Installer | `1bab7eef4d04ff4b8a76515fb7e19da366c3dd44277bdaf851d1e5e0da5d2e8d` |

Full affected Go suites, focused race tests, vet, bash/copy checks and 3 inert installer Python cases passed; 1 separately guarded native Python case was skipped on the host. The new guarded native Go component ran for real. No new delegated audit or broad matrix is claimed.

## Cleanup and command

Original snapshot `bridge-initial-admission-before-20261121` was restored and read back. Captured binary/version, site/cron/daemon records, accounts/credentials, protected key/TLS/SSH/history files and timer states matched. All three local guests are stopped. Private evidence SHA256 `cfb81cf22dc63dcdcb6c2fc9d94b3c59c1dd523ace74f1b024c498b4cc005922`; raw credentials and authority records stay private.

```bash
sudo curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54-bridge-test.6/install.sh -o /root/fluxo-install-test6.sh &&
sudo env FLUXO_VERSION=v0.4.54-bridge-test.6 bash /root/fluxo-install-test6.sh
```

Saved choices are reused. This is an opt-in VPS trial off main/latest. Existing immutable assets stay unchanged; future base admission still requires separately qualified final stable bridge identities.
