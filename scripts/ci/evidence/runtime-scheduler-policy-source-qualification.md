# Automatic Scheduler policy renewal — source qualification

Source `8f501aa5b84d33ccd454e6fbb190cb6b4c0b378c` adds current-runtime revalidation for supported package changes on the **same live cron engine**. Prior policy hashes, reader floors, roles and schema stay intact. The existing loader ledger archives the transition before writes, preserves predecessors and publishes new nonces without manufacturing acknowledgements. No historical converter, replacement-engine adoption, app stop or missing-job enablement is introduced.

The old fingerprint includes the complete dpkg status database and loader cache, so an ordinary PHP/package transaction can invalidate it. The customer's matching engine identities with different aggregate hashes establish a policy mismatch; they do not identify a specific historical changed file. Their earlier transient `maintenance_busy` cause remains unconfirmed.

## Native result

**PASS** on the preserved Ubuntu 24.04 AMD64 `fluxo-php-patch-test`, using stock Noble cron. An authenticated real public bridge-test.3 Scheduler enable first reproduced HTTP500 `sql: no rows in result set`. The earlier committed-row fix and this policy renewal together enabled Scheduler with HTTP201 after a real signed APT `ncal` installation (one observed request: 11.464s). A genuine cron acknowledgement and hosted-user PHP marker execution followed.

A second real APT transaction installed `lhasa` and its new `liblhasa0` dependency, changing metadata without changing/removing any existing package. With no further enable request or installer run, background revalidation published a new epoch/nonce, obtained a fresh genuine acknowledgement, and the scheduled marker ran again. The same cron PID/start/boot/cgroup/static contract and prior epochs were retained. Duplicate enable was refused with one row. An unknown PAM fragment was refused without publishing or modifying rows. Explicit disable preserved the inactive registration.

The final native binary was `24f2cc6a2e7a405bca5d395c8e46581c928de0d1def0e85b79335b9d68874060` (`0.4.54-runtime-qa.3`, native Go1.26.3, CGO0, all19 official pins). Its 395 production/embedded-UI input hashes match this committed runtime, while build VCS metadata honestly records the preceding commit and a modified tree. This is a checksummed development subject, **not a signed public release**.

## Qualification and retained failures

Full bridge/Scheduler/server/cron Go suites, focused race checks, affected vet and the existing docs/site build passed. Earlier full PHP/Node suites passed before the retry-only followups. No UI source changed.

QA1 normal installation passed but exposed a real cron-worker drain overlap. Final code retries only the existing typed empty-provider observation and protected-directory snapshot churn, discarding the whole attempt and releasing all gates before a bounded pause. Changed files/permissions/inodes/selectors, unsupported profiles, replaced engines and joined failures remain refused. The whole operation is limited to20s.

QA2's normal installer timed out after600s on WP-CLI download and restored the previous binary. Exact-image recovery preparation correctly refused that changed named image. Development replacement left the interrupted installer's durable hold intact; the supported lease/release protocol recovered this fixture before further testing. A later QA2 run exposed transient-directory snapshot churn. Those attempts are retained in the JSON and private archive. They are not installer acceptance passes. The final QA3 runtime test used a checksummed development deployment onto the preserved QA1-qualified fixture; there was no installer or manual authority change after its test package changes.

No native Cronie/ARM64, fresh PHP/Node apply, real Laravel-framework, ACME renewal or public-artifact acceptance is claimed. The controlled artisan marker verifies Scheduler plumbing and execution identity. Customer-reported successful PHP/Node maintenance remains separate evidence.

## Cleanup and release boundary

The original `before-policy-refresh-20261121` snapshot was restored and read back: original binary/version, site/cron/daemon rows, credentials, encryption/TLS/SSH and recorded history matched; original timer states matched. All three local guests are stopped. Private evidence SHA256: `5bb637b8b86d6c1cf40541c5a1af685beac38645f8355b1c9e9fc6b8f825ec69`.

Only isolated branch source/evidence is eligible to push here. Main/latest/stable and existing immutable trial assets remain unchanged. A new installable signed test release and its exact-public acceptance are separate work. See the JSON for exact input, script and log hashes and every retained attempt.
