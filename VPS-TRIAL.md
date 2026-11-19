# Base candidate VPS trial

Branch: `test/base-vps-trial`. The candidate includes the clean base, Node toolchain preservation and opt-in site traffic analytics. Production `main` and the latest published release remain unchanged.

## Get the tested build

Open the matching Actions run while signed in to GitHub, download its artifact and extract it. Copy the extracted `traffic-build` directory to the VPS. This supplies the compiled binary, matching installer and exact-source manifest; no Go or Node build tools are needed on the VPS.

| VPS | Actions run | Artifact to choose | Binary SHA-256 |
|---|---|---|---|
| AMD64 Ubuntu 24.04 | [Verified AMD64 run](https://github.com/FabioTECH1/fluxo/actions/runs/44606677927) | `traffic-b036c8ffefbbc54e67dabc8871b8b1f7e571bd08-amd64-24.04-44606677927` | `05fcf50d02b87a12ed9813eb0d3552069b48ca477ca122aa83e0230b46e2d95c` |
| ARM64 Ubuntu 22.04 / 24.04 | [Verified ARM64 run](https://github.com/FabioTECH1/fluxo/actions/runs/44604143252) | The `22.04` or `24.04` artifact matching the VPS | `629f45d25011e2c7eda9fd02c465fc3075f0ab041caac337de5a79eb10693a6b` |
| ARM64 Ubuntu 26.04 | [Verified ARM64 26.04 rerun](https://github.com/FabioTECH1/fluxo/actions/runs/44606055203) | `traffic-5a320816b287829988e76ef1643e93812977597a-arm64-26.04-44606055203` | `fab6dbe6179d665ae41fa2e187ea8c6b661e418a98ff0aae90ba9cad5c8c30c9` |

These retained builds report `0.0.0-base.dev`. Their production inputs match this branch; their exact source/build identities are recorded in [native qualification](scripts/ci/evidence/traffic-native-source-qualification.md). Actions artifacts have limited retention. They are development builds supplied for this trial, with explicit checksums, rather than signed public release assets.

## Install on the VPS

Existing installations must first complete the published **v0.4.51 bridge**. The base installer's preflight checks this automatically. Fresh installations use the direct base path. Preserve a VPS backup before testing the update.

For the AMD64 build above, from the uploaded `traffic-build` directory:

```bash
sudo env FLUXO_LOCAL_BINARY_SHA256=05fcf50d02b87a12ed9813eb0d3552069b48ca477ca122aa83e0230b46e2d95c \
  bash ./install.sh --local-binary="$PWD/fluxo"
```

For ARM64, replace the checksum with the matching value in the table. The normal installer retains existing runtime choices; its usual component flags remain available.

Downloading `install.sh` alone from this branch still selects a published release. Use the matched binary and `--local-binary` command above to test the base candidate.

## Check the trial

```bash
sudo fluxo --version
systemctl is-active fluxo.service
```

Confirm existing sites, deployments, scheduled jobs and runtime pages still work. Traffic analytics starts **disabled**. To try it, open **Observe > Traffic**, enable server collection, select a site and view its **Observe > Traffic Analytics** page. Reports normally refresh in 30–60 seconds. See the [analytics guide](ui-site/docs/operations/traffic-analytics.md) for metric definitions, collection gaps, retention and storage limits.

No stable tag or latest promotion is part of this branch push. Signed public-artifact acceptance remains a separate release gate.
