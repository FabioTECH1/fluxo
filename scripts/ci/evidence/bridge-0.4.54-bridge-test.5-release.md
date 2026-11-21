# Bridge test5 exact public acceptance

**PASS within the scope below.** Immutable opt-in `v0.4.54-bridge-test.5`, source `84a207934cd503aaa5a9b55910a8b25f04947ee0`, official hosted build [44941753515](https://github.com/FabioTECH1/fluxo/actions/runs/44941753515). Main/latest remain stable v0.4.53. No base publication or stable promotion.

All eight assets matched published digests; both ELF architectures, Go1.26.3, CGO0 and all19 official pins passed. All four attestations bound the installer, bridge contract and binaries to this exact source, tag and hosted workflow. Build VCS flags are retained as reported. One bounded artifact-range download timed out; completed parts were resumed, with full final hashes and all attestations still mandatory. No assets or trust checks were changed.

| Public artifact | SHA256 |
| --- | --- |
| AMD64 | `127c7cf35844910f1f098e86588c52b4c4745584bc1cf00262ab507bd144abeb` |
| ARM64 | `99c797a7de2845deb01c5a4cf6b1ee52c95c045601f0ce6d4e5310732f930073` |
| Installer | `d69c8204fffe6ac1e30c36f1423dc12a65ee99f35bb4cc7eab879ba4f15a82a6` |

## Actual installer wait result

The first normal provisioning invocation on the preserved Ubuntu24.04 AMD64 fixture ran 522.272s and refused `candidate input changed during inspection: /run/systemd/transient` during final compatibility. No Scheduler intent or holds remained. This is retained as a failed initial installation, not a successful first-attempt claim. One supported same-release normal installer recovery, with the same authenticated SHA-bound local public binary and no authority edits, then completed in 29.302s. A full public-binary recovery download was first stopped after 74.767s because transfer remained slow; this was before compatibility actions and no cron/application received a signal. The successful recovery reused the independently authenticated exact local public bytes. These are single observations, not a speed benchmark. The first WP-CLI transfer hit the stock 300s timeout; the unmodified installer retry succeeded without any override or guard change. Independently authenticated public bytes were transferred with a local SHA-bound binary flag. Installed image, running image, dashboard and dispatcher matched the public SHA. Captured records, credentials and protected key/TLS/SSH/history bytes were preserved; three genuine root-task acknowledgements, retained helper bytes/contracts/identities and reader roles passed.

A real cron launch then ran one protected, harmless Python task for 70.001s. Its actual parent ancestry included the saved cron provider, and the provider hierarchy had 4 tasks before the installer. A normal authenticated APT installation of `ncal` changed the package metadata without changing any existing package version. The same-version public installer ran with **no artifact overrides**, printed the new typed waiting message and completed successfully in 120.895s on its first attempt. The cron task finished naturally; no job was stopped or acknowledgement fabricated.

The same provider identity, original reader-floor bytes and predecessor policy history were preserved. Scheduler obtained a fresh policy epoch/nonce, a genuine cron acknowledgement, and subsequent hosted-user marker execution. Duplicate enable was refused; explicit disable retained the inactive registration. This directly exercises the bounded wait rather than relying on a compiled string or mocked observation.

## Limits and cleanup

The predecessor was the preserved original `0.0.0-base.dev` fixture, not a newly constructed public trial4 route. Both public architectures passed artifact verification; this new runtime case is AMD64 stock Noble cron only. No new ARM64/Cronie, actual PHP/Node apply, real Laravel-framework, Reverb, ACME/Composer or operator-hook execution is claimed. The original customer transient busy cause remains unconfirmed. Existing exact-image recovery and unsupported-state refusals remain intact.

Snapshot `bridge-trial5-before-20261121` was restored and read back. Original binary/version, captured records, accounts/credentials and protected files matched; timer states matched. All three local guests are stopped. Private evidence SHA256 `63faf44733d457b2c7884f171993b8aeb4944703a69725bc7c1c1d3e34e9c1ca`. JSON binds the actual scripts/results/private logs without exporting credentials or nonces.

## Explicit opt-in command

Download the installer first so a normal early exit cannot produce the earlier pipeline curl23 error:

```bash
sudo curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54-bridge-test.5/install.sh -o /root/fluxo-install-test5.sh &&
sudo env FLUXO_VERSION=v0.4.54-bridge-test.5 bash /root/fluxo-install-test5.sh
```

Saved component choices are reused. Refresh the panel after completion and test Scheduler. Main/latest and previous immutable assets remain unchanged. Future clean-base admission still requires separately verified final stable bridge identities.
