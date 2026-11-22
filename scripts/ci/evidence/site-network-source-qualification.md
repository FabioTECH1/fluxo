# Site Network and wildcard source qualification

Source: `d5d2e68cc7a05c6f5514c49d42e7242656514e8e` on `baseline/clean-base` (implementation `08bf4c08`, review fixes `d5d2e68c`). This is an unreleased base source candidate, not a newly installable release.

Implemented all three approved features: managed shared password protection, exact 301/302 redirects, and compact wildcard domain configuration with separate root/wildcard certificate status. The real PasswordInput provides draft visibility and generation; saved credentials remain unreadable. Current domain/SSL/runtime settings drive regeneration; explicit custom-mode conflicts and interrupted activation remain visible.

## Validation

- UI: 8 console + 108 TAP tests, including 14 Network cases and actual compiled PasswordInput behavior. UI and demo typechecks, UI/site/SSR/prerender/docs builds pass.
- Backend: complete Go suite passed before the review follow-up; final full affected database, Nginx, SSL, server, deployment, bridge and CLI suites pass. 27 new top-level tests cover SQL storage/admission, crypto/filesystem protection, managed/custom concurrent mutations, revisions, rollback/recovery, certificate scope and ownership.
- Affected race and vet checks pass. Both Linux architectures compile with Go 1.26.3 and CGO disabled. These are private development builds, not official signed artifacts or native architecture acceptance.
- One bounded Astra 6.2 Low review of the immutable initial patch found custom wildcard case/trailing-form overlap and alias wildcard loss during root rotation. Both are fixed with regressions. Final fixes also protect primary wildcard rotation and coordinate certificate preparation/domain/delete with Network. No independent second review is claimed.

## Remaining native gate

No actual Nginx/HTTP/TLS requests or service reloads were run. The required named fixtures were deleted; only `fluxo-dev` remains Stopped and was not selected. Do not recreate them or use a real VPS implicitly. An authorized fixture must still prove Basic authentication/ACME, redirects, wildcard/explicit-host/deletion fallback, IPv4/IPv6/SNI/depth/WWW behavior and real reload rollback. Unit-rendered config and demo responses do not establish those results. Prior System native reboot qualification remains separate and pending.

## Boundaries and preservation

System/security/reboot, analytics, runtime behavior and exact bridge055 admission remain intact. No bridge source, installer/workflow, execution floor/reader contract, main, latest, existing tag or release asset changed. No release/tag was created. Only the isolated source branch is pushed. The new application tables are additive; entirely absent storage initializes, partial or malformed storage refuses without repair. Current upgrades/deploys use saved settings; downgrading to an old immutable image that lacks this feature is not feature-aware preservation.

Uncovered wildcard TLS is rejected, root SSL retained and exact hosts retain priority. Origin CA is not represented as browser-trusted; no wildcard issuance/DNS automation/automatic imported renewal exists. Redirect responses may precede auth and ACME remains public. Protected old hash files and operation history remain for genuine recovery.

Full machine-readable evidence, reference hashes, development build hashes, source-scoped commands/logs and resource/ref inventory: [site-network-source-qualification.json](./site-network-source-qualification.json). Raw test/build outputs are retained in `.base-validation/site-network/` on the host; no credentials or private keys are copied into this report.
