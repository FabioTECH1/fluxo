# Traffic analytics: bounded native candidate qualification

Four native cases passed: AMD64 Ubuntu 24.04 and ARM64 Ubuntu 22.04, 24.04 and 26.04. This qualifies the bounded source candidates; it does not qualify signed public release assets or authorize publication.

| Native case | Exact source | Candidate SHA-256 | Ordinary update | Run |
|---|---|---|---:|---|
| amd64 Ubuntu 24.04 | `b036c8f` | `05fcf50d02b87a12ed9813eb0d3552069b48ca477ca122aa83e0230b46e2d95c` | 18.412s | [PASS](https://github.com/FabioTECH1/fluxo/actions/runs/44606677927) |
| arm64 Ubuntu 22.04 | `426492a` | `629f45d25011e2c7eda9fd02c465fc3075f0ab041caac337de5a79eb10693a6b` | 22.774s | [PASS](https://github.com/FabioTECH1/fluxo/actions/runs/44604143252) |
| arm64 Ubuntu 24.04 | `426492a` | `629f45d25011e2c7eda9fd02c465fc3075f0ab041caac337de5a79eb10693a6b` | 20.281s | [PASS](https://github.com/FabioTECH1/fluxo/actions/runs/44604143252) |
| arm64 Ubuntu 26.04 | `5a32081` | `fab6dbe6179d665ae41fa2e187ea8c6b661e418a98ff0aae90ba9cad5c8c30c9` | 20.508s | [PASS](https://github.com/FabioTECH1/fluxo/actions/runs/44606055203) |

Every subject was built natively with Go 1.26.3, CGO disabled, all 19 official toolchain/installer assignments and clean VCS metadata. Downloaded images/installers were rehashed against their exact build manifests. The full source, tree, image, installer, report and manifest digests are in [the JSON record](traffic-native-source-qualification.json).

The cases verified default OFF without analytics storage creation; authenticated/site-scoped APIs; request, estimated-page-view, 404 and body-byte metrics; query/referrer privacy; failed-Nginx-validation rollback; master/site switches; disabled gaps without backfill; checkpoint recovery without duplicate counts; real Nginx log rotation with the new writer active and old inode quiet; bounded 98-row hourly dimensions with explicit incompleteness; and active-history deletion without backlog resurrection. Normal installer updates retained analytics generation, cursor and aggregates, along with original application data, credentials, keys, execution history, HTML and SSH access. Genuine empty-renewal root jobs completed before analytics, while enabled, and after the update.

The conservative production/build-input scope is byte-identical from `d73c04d5f90399db3806cac1b38617a44e17ee90` through the recorded `b036c8ffefbbc54e67dabc8871b8b1f7e571bd08` and each executed subject. CI fixtures, documentation and website/demo changes outside that scope are not presented as exact-image execution. This evidence-only record changes none of those inputs.

The original ARM26 attempt failed at a burst response and did not record its status; its original cause remains unconfirmed. The fixture was amended to wait for natural retirement of every prior custom-vhost worker before starting concurrent requests and to record future response status/index. The focused rerun passed. The initial failure and its hashes remain recorded. A queued AMD attempt was cancelled before native work to fix CI serialization; only the replacement AMD attempt supplies qualification.

The observed 18–23 second ordinary-update samples used local-SHA candidate transport on hosted runners. They exclude public candidate download/attestation and are not a universal time or controlled speedup claim. Named light-VPS resource measurements are [separate](traffic-native-resource-measurements.json).

No public asset/provenance acceptance, live ACME, optional Nginx-plugin parser qualification or new bridge-handoff claim is made here. Prior base/Node reports retain their own sources. The user-skipped historical interruption test was not run. Main, latest, tags and public release assets were untouched.
