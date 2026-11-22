# Base System and Site Network: native acceptance

**Passed on the explicitly selected `fluxo-dev`: 76 main checks and 5 targeted proxy checks.** This qualifies the recorded source candidates on Ubuntu 24.04 AMD64 within the scope below. It is not a release or public-artifact acceptance. [Machine-readable record](base-system-network-native-acceptance.json) contains every check, exact image identities, boot IDs, retained-file hashes, preparation failures and restoration evidence.

## Results

- Actual PHP/static HTTP and HTTPS Basic authentication, credential rotation, protected bcrypt files, public ACME challenge routes, exact 301/302 redirects and concurrent revision refusal passed.
- Imported/clone certificate activation, separate root/wildcard readiness, rejection of uncovered or deeper TLS names, explicit-host priority, deletion fallback, WWW behavior and IPv4/IPv6 requests passed. An alias kept its previously valid combined certificate across root rotation; an incompatible primary wildcard rotation was refused.
- Real Nginx syntax rejection preserved the previous configuration. Injected real reload failure retained pending evidence and old serving workers; explicit recovery restored confirmed settings. Managed/custom conflicts and disable/default-restore preservation passed.
- Actual Ubuntu automatic-security policy reads and both toggle directions passed, preserving unrelated effective APT values. External administrator automatic-reboot policy stayed explicit and unchanged. Missing tools were reported without installation. A real native dpkg lock blocked both mutation and reboot.
- The production API acknowledged a reboot before shutdown. After the real reboot, the authenticated return confirmed a different boot ID. The request identity survived; a competing request did not replace it. The actual reboot flag cleared only when server state confirmed it.
- Expected services were checked after startup grace. Nginx and other expected services were running; an intentionally disabled/stopped fixture remained excluded; an actual failed fixture was named without automatic repair. Explicit operator recovery changed the reported service result. Imported HTTPS and the PHP application survived the reboot, and maintenance exclusion was usable afterward.
- The separate production Node proxy renderer/transactional Nginx installer test denied absent and wrong credentials, admitted valid credentials to a real loopback backend, preserved HTTPS redirect semantics and checked the exact referenced bcrypt file's ownership/mode. Its backend was a generic Python server, not a Node deployment.

## Exact source and image scope

| Component | Source | AMD64 binary SHA256 |
| --- | --- | --- |
| Detailed Network checks | `beacaf23a6d0cf6ef3f4824fe59e116683e4c20a` (Network production `d5d2e68c`) | `5794f8375b823a29beb9359c081ee506ee101578a83ecdb12bff722f4718f1c7` |
| System policy and actual reboot | `64a2e3df91d164f39d12f455cf9e986eac869b22` | `97d23022187b63b7af81b718a3c99925eae955a51d7a18b5f31006da842793a1` |

Both private candidates used pinned Go 1.26.3, CGO disabled and all 19 official pin assignments, with clean-source manifests. The only changes between these candidates are the System APT parser and its tests. Network source and embedded UI did not change. Detailed Network checks were not all repeated on the second image; final-image imported HTTPS/PHP application smoke did pass after reboot. Installed and running image hashes matched the fixed candidate before and after that reboot.

The main environment was Ubuntu 24.04, kernel `6.8.0-136-generic`, Nginx `1.24.0-2ubuntu7.21`, PHP-FPM `8.4.28-3+debsuryorg24.04.1+1`, and unattended-upgrades `2.9.1+nmu4ubuntu1`. The separate proxy test ran after restoring the original VM, using its Nginx `1.24.0-2ubuntu7.15` and a guarded test driver built from `64a2e3df`. This driver is not a release image.

The real reboot changed boot ID from `ff0cd6fc-0704-461f-bf7d-b3e9477db514` to `4c4ac0f7-92fe-4190-b6eb-0fb2ed4da392`. The main report SHA256 is `053f6e7403642e5ea001e7c3b1169a4ced2d466adfe8fe2535fa12272db57f03`.

## Defect found and corrected

Stock `apt-config dump` includes filename-ignore regex values containing literal `\.`. Treating APT output as Go string literals rejected these valid Ubuntu settings and made security-update controls unavailable. Base-only commit `64a2e3df` preserves the exact serialized APT values for before/after comparison. Management scalars remain strict: unsupported escapes do not acquire Go escape meanings, and embedded NUL is refused.

Two regressions cover stock regex preservation/toggling and unsupported escape semantics. The full hostsystem suite and race suite, corrected `TestServerSystem` selection, affected vet and pinned build passed. An earlier `TestSystem` selector matched no tests and is not counted. Native verified policy mutation then passed on the fixed image. No bridge code changed.

## Preparation and fixture corrections

These are incremental results from one preserved VM, not an assertion that one uninterrupted harness run passed.

An initial transfer accidentally overwrote a running temporary installer; its snapshot was restored. A slow WP-CLI download caused another setup abort/restore. Later provisioning used a fixed cache of exact public tool bytes, while the original Composer SHA and WP-CLI GPG fingerprint/signature/version checks stayed active. The unmodified normal installer used explicit local candidate hashes, so these runs do not establish public delivery or signed release acceptance.

The original development panel's manual Scheduler state lacked historical loader qualification. Stable055 installed, but base handoff correctly refused that state. No history, nonce, receipt or admission authority was fabricated. Feature testing instead used an isolated fresh panel inside the same snapshotted VM, preserving original files/account data. Missed old domain-named PHP pools required fixture cleanup and ordinary `dpkg --configure -a`; fixture Nginx/FPM also needed starting after deliberate isolation. These preparation failures are retained and are not feature passes or old-dev handoff acceptance.

Client fixture assumptions were corrected without product changes: Domains and Network have distinct revisions; valid alias certificate pinning can permit root rotation; clone returns 201; site deletion/reload completes asynchronously; syntax rejection returns 422. The proxy fixture also needed bounded activation readiness, stable certificate inputs during continuation, and validation of the active credential reference rather than a false assumption that old immutable credential files disappear. Every retained earlier observation remains separate in the JSON record.

## Preservation and cleanup

The user explicitly selected `fluxo-dev`. Before touching it, the original stopped VM was snapshotted as `before-base-system-network`; that snapshot was restored after main acceptance. A second stopped snapshot, `before-base-proxy-native`, protected the final proxy test and was restored afterward. Both task snapshots were deleted by exact name only after successful restoration.

Final inventory contains only `fluxo-dev`, **Stopped**, with **zero snapshots**. Its original image identity remains unchanged. The restored guest was intentionally not booted again for a file-by-file comparison. No cloud VPS, new VM or global purge was used. Safe reports, test drivers, manifests and redacted logs remain under `.base-validation/native-system-network/`; no passwords, tokens, private keys or original databases were exported into this report.

## Limits and publication boundary

- No new ARM64, other Ubuntu release, Cronie or kernel matrix. No PHP/Node runtime Apply or full Node application deployment.
- No external DNS, live Cloudflare Origin delivery, public ACME issuance/renewal or imported-certificate automatic renewal proof. Local certificate validation and ACME webroot reachability were exercised.
- The restart-required file was a controlled Ubuntu-format fixture, not evidence that a tested security package/kernel upgrade requested reboot. No automatic security-package transaction was initiated by the feature; provisioning did modify temporary guest packages before restoration.
- Native API/systemd/TLS results do not prove live browser geometry or the full reconnect UI. The delayed service had settled by the first authenticated observation; a live `starting` state was not observed. Existing component tests retain their separate scope.
- Service status is not proof that every hosted application or database is healthy. No power-loss/crash matrix or whole-host certification is claimed.
- No new tag/release, main/latest/stable promotion, bridge source, installer or workflow changes. Existing immutable `v0.4.55-base-test.1` does not contain these features or the parser correction. A separately authorized feature build still needs exact-public artifact acceptance.
