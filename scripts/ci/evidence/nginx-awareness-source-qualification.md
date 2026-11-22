# Nginx awareness source qualification

Original source checkpoint `d82e152ed2979ddd536ff988af136b357a1c73ca` on `baseline/clean-base`, after implementation `27cfd552`. This is a base-only unreleased source candidate. The subsequent user-approved [daily-discovery follow-up](nginx-daily-discovery-source-qualification.md) adds production scheduling at `1bea2f33`, with separately recorded source tests/build. The original checkpoint below remains historical.

## Implemented behavior

Explicit **Check for updates** refreshes metadata under existing maintenance coordination, then reads the actual binary-owning package and effective libapt candidate. Debian epoch/distro revision comparisons, pins, holds, missing/untrusted candidates and unsupported/manual/modified binaries remain distinct. Reads/polling never refresh APT. Failed/old checks preserve useful prior facts as stale; source-policy drift refuses currentness. The saved cache grants no runtime authority.

The shared dismissible notice offers **View instructions**. Its dismissal binds installed binary/package, candidate and configured source identity. Instructions are text for the operator's SSH session: review policy/holds and simulate first, then deliberately install and test/restart if appropriate. Fluxo never installs/upgrades/removes Nginx, migrates repositories or activates services through discovery.

The existing `/server/nginx/restart` URL remains configuration validation/reload; UI and docs now truthfully call it **Reload Nginx**. `nginx -v` is labeled on-disk version. Running master/worker adoption remains **Not verified**. This notice is separate from the nondismissible actual Ubuntu server-restart-required state.

## Checks

- 11 focused Go tests, full nginxupdate/server suites, focused race and affected vet pass.
- 9 Python policy tests pass: real libapt Debian comparison, mocked effective candidates/origins/holds and private temporary policy-path fixtures.
- 8 console +117 TAP UI/demo tests pass, including8 Nginx tests and1 additional demo case. Actual Vue components compile/render in SSR fixtures; no browser-layout/native result is claimed.
- UI and demo typechecks, UI production build, complete site/functions/SSR/prerender/docs build pass. Pinned Go1.26.3/CGO0 AMD64 development compile passes; no official signed artifact or native run is claimed.
- Initial SSR test incorrectly expected a field hidden while loading. Its failed log is retained; the corrected test asserts the truthful first-render text. No production validation was weakened.

## Limits and preserved boundaries

No new VM, package, service or VPS action was run. Native configured-repository/lock/installed-package observation for this feature remains pending separate authorization. APT candidate availability does not certify a successful dependency transaction; running-binary adoption is intentionally unverified.

Completed System/Network native evidence remains unchanged in [base-system-network-native-acceptance.json](./base-system-network-native-acceptance.json), with its original scoped sources/images; it is not borrowed for this feature. System/Network/analytics/runtime055 code remains preserved. No bridge, installer, workflow, schema/floor/reader contract, main/latest/stable, old immutable tag or asset changed. No new release was created. Only the isolated base branch is pushed.

Machine-readable source/check/log/boundary evidence: [nginx-awareness-source-qualification.json](./nginx-awareness-source-qualification.json). Host logs and the private development build are retained under `.base-validation/nginx-awareness/`.
