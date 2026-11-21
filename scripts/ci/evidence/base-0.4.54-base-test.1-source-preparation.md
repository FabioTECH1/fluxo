# Corrective base v0.4.54-base-test.1 preparation

The corrective source is prepared, **not tagged or published**. Its [JSON record](base-0.4.54-base-test.1-source-preparation.json) binds the shared commits, test logs and remaining release gates. No final public-artifact acceptance is claimed.

The PHP fix uses authenticated managed-Sury package records across maintainer build-label changes, rather than requiring every current suffix to appear in a spelling whitelist. Ubuntu series, pinned authorization, trusted origin, exact package/source/version/hash/size, installed continuity, holds, dependencies and no-downgrade checks remain. Conservative historical spelling checks remain for old installed packages absent from authenticated indexes. The [signed index evidence](php-sury-packaging-provenance.json) includes the exact newly reported revision on both architectures.

The Node review now presents one issue summary with expandable original diagnostics and a job-review link. Service/site summaries and maintenance details are compact; blocked submission, restart/rollback warnings and all backend authority controls remain. Unknown missing-job intent is not converted into permission to enable a job. Customer-specific remaining Node evidence is being investigated separately and is not a new base entry prerequisite.

Base picks `dfa14b3` and `48bd10a` correspond to original shared commits `43d64837e00b1f1eb604907fb94fe42d0b001eee` and `573a40a620af070ae5b90653fb34deeea579719e`. No historical cleanup conversion writer was imported.

Passed locally: 30 planner and 12 repository tests, the real temporary APT policy fixture, frontend console/TAP tests, embedded UI and website/docs production builds, affected PHP/CLI Go suites, affected vet, and 83 CI-harness tests. UI was built before Go. Native/public qualification of the corrected final release remains pending.

The current admission policy still uses the qualified immutable v0.4.53 bridge. Only after v0.4.54's public signed identities are verified will the gate, exact recommended bridge command and update-channel floor change. No future digest or unknown-image admission was inserted.

One bounded [ARM diagnostic](https://github.com/FabioTECH1/fluxo/actions/runs/44892398856) established the separate upstream blocker: packages.sury.org returned a 142048-byte index when signed metadata required 143608 bytes. APT correctly refused. Only redacted APT stderr was exported from private external tracing; cleanup passed. No unchanged retry or trust relaxation followed. A coherent future signed index/payload response is required before final native acceptance.

The [prior public build record](base-0.4.53-base-test.1-release.md) retains its successful AMD64 routes, failed ARM attempts and exact artifacts. Those assets/tags remain immutable. The base is off main/latest; no customer VPS was changed and all three local VMs remain stopped.
