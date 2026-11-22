# v0.4.55-base-test.2 public release qualification

Published immutable opt-in release **454689292**, source **82c22057c81d71fe081b5f5f9c3f2c22880c3c90**, off main/latest/stable. [Official build](https://github.com/FabioTECH1/fluxo/actions/runs/45060073766) passed. The release includes the approved System, managed Network and Nginx discovery/compact instructions work.

Eight public files, four exact-source/tag official attestations, both ELF architectures, Go 1.26.3/CGO0/all 19 pins, exact stable v0.4.55 contract, source installer and approved UI were independently verified. Official binaries report vcs.modified=true from the UI build, recorded rather than treated as a clean-VCS claim. The JSON binds every asset ID/size/SHA and independent proof SHA.

**The affected native update is blocked, not passed.** [First attempt 45067056591](https://github.com/FabioTECH1/fluxo/actions/runs/45067056591) stopped while installing the authentic public .1 predecessor. One [diagnostic attempt 45067740588](https://github.com/FabioTECH1/fluxo/actions/runs/45067740588), with private external tracing of that unchanged installer, confirmed:

> packages.sury.org: File has unexpected size (217028 !=218472). Mirror sync in progress?

The separately signature-verified InRelease also requires 218472 bytes and SHA 3246b8f1…; the received index is 217028 bytes and SHA d268670e…. No .2 installer ran. Both cleanup reports passed. No hashes, keys, package/provider selection or installation safeguards were bypassed; no further unchanged native retries were made. Raw traces/credentials were never exported. The diagnostic observer adds overhead, so the two refusal durations are not a speed comparison.

The .1→.2→ordinary update, new authenticated Nginx check/owning package/saved 24-hour due time/no mutation and System/Network setting preservation therefore remain unqualified on these public assets. Existing System/Network 81 native cases retain their original source scope; daily Nginx source/virtual-clock evidence is not reassigned to the new binary.

[Public-only 45068079682](https://github.com/FabioTECH1/fluxo/actions/runs/45068079682) passed exact-public CLI and installer PTY/plain/NO_COLOR/dumb/unset/unknown-terminal, ANSI-free metadata and exit-status checks. It created no installation/host fixture and does not close the native gate. 49 focused local acceptance/feature tests passed after correcting a new test's import typo; whitespace passed.

Existing admitted bases remain direct; legacy installs still require completed exact stable v0.4.55. The following explicit opt-in command is not a claim of native readiness; repository incoherence can still block the normal installer. An interrupted operation must use its original exact installer.

```sh
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.55-base-test.2/install.sh -o /tmp/fluxo-base-test2-install.sh &&
sudo env FLUXO_VERSION=v0.4.55-base-test.2 bash /tmp/fluxo-base-test2-install.sh
```

Only baseline/clean-base is pushed. Main c0249c19 and latest stable v0.4.55, older immutable assets, bridge code and fluxo-dev remain unchanged. The fully-contained inactive temporary acceptance branch was removed after all runs settled. Only fluxo-dev exists and is stopped; no local VM or VPS action occurred. Stable base promotion and the blocked native gate remain separate future work.
