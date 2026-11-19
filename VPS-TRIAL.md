# Fluxo v0.4.52-base-test.2 VPS trial

This prerelease comes from `test/base-vps-trial` and includes the clean base, Node toolchain preservation and opt-in site traffic analytics with Overview, Pages and API traffic tabs. Production `main` and the latest stable release remain unchanged. Installing this test version is an explicit choice.

## Install from GitHub

```bash
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.52-base-test.2/install.sh \
  | sudo env FLUXO_VERSION=v0.4.52-base-test.2 bash
```

The installer selects the correct AMD64 or ARM64 release binary and verifies its checksum and signed GitHub build provenance. No manual artifact upload or local development tools are required. Existing runtime choices are retained; the installer's usual component flags remain available.

Existing installations must first complete the published **v0.4.51 bridge**. The base preflight checks this automatically. Fresh installations use the direct base path. Preserve a VPS backup before testing the update.

## Check the trial

```bash
sudo fluxo --version
systemctl is-active fluxo.service
```

Confirm existing sites, deployments, scheduled jobs and runtime pages still work. Traffic analytics is **disabled by default** on a fresh installation. An update preserves existing collection selections and retained Overview history. To try it, open **Observe > Traffic**, enable server collection, select a site and view its **Observe > Traffic Analytics** page. Reports normally refresh in 30–60 seconds. API history begins with API-capable collection; older overall summaries cannot reconstruct API requests. Configure API prefixes and explicit patterns in the API traffic tab; the default `/api` prefix matches `/api` and `/api/...`, not `/apiculture`. All three tabs share the same date range and collection setting. See the [analytics guide](ui-site/docs/operations/traffic-analytics.md) for metric definitions, collection gaps, retention and storage limits.

`fluxo --version` reports `0.4.52-base-test.2`. The normal website installer continues to select the latest stable release. Broader stable promotion and final production acceptance remain separate from this opt-in VPS trial.

Candidate-source native evidence and resource measurements are retained in [native qualification](scripts/ci/evidence/traffic-native-source-qualification.md) and [resource qualification](scripts/ci/evidence/traffic-native-resource-measurements.md). They retain their original source and image identities; verification of this signed prerelease is recorded separately.

API-extension qualification and resource limits are recorded in [API native qualification](scripts/ci/evidence/api-traffic-source-qualification.md). The immutable `v0.4.52-base-test.2` downloads, signatures, source binding, official pins and embedded UI were independently verified in [public artifact verification](scripts/ci/evidence/base-test-0.4.52.2-release.json). These source and integrity checks make this an opt-in VPS trial; they do not claim final public-artifact runtime acceptance or stable promotion.
