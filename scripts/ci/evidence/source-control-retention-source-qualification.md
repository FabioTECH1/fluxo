# Compact Source Control and configurable analytics retention

Bounded source validation passed on `4b69865beb9e1a6c3b0a56327fcfa2768094eaac`.
The [machine-readable record](source-control-retention-source-qualification.json)
binds the source tree, local development binary, unchanged installer and logs.
This extends `6629522` and retains existing base/traffic qualification records.

Source Control uses the approved compact modal and existing components. Retry
remains observational; token replacement keeps its identity review, locks,
existing-key registration and private-key preservation. Optional failure reasons
describe observed evidence. A generic SSH error does not diagnose remote
registration or connectivity; reconnect cannot restore an unavailable private key.
Narrow prior Git-settings corrections preserve saved repository/branch selections.

Traffic history defaults to 30 days on fresh and existing installs. One persisted
server policy supports 30/60/90/180/365 days or manual deletion. Parser and cleanup
honor it; manual retention never evicts aggregates by age. SQLite storage remains
bounded to 256 MiB with 8 MiB ingestion headroom. Capacity pauses collection
visibly; a temporarily busy WAL checkpoint remains retryable. Explicit deletion,
disabled gaps, generation floors, cardinality and report bounds remain intact.
Longer retention cannot recover expired data. Older strict control decoders refuse
the added policy field rather than silently applying their 30-day cleanup policy.

Full Git/server/traffic tests, focused race checks, vet, 35 UI tests, UI production
build and complete website/docs build passed. Real SQLite tests cover policy
persistence, manual/finite cleanup, no resurrection, storage pause/recovery and
existing 8192-byte database pages. One bounded Astra 6.2 Low review found busy-WAL
misclassification and cancellation-as-timeout; both are fixed with regressions.
No broad repeat audit was performed.

No VM was used because the parent owns the reusable fixture for bridge QA. Prior
native AMD64/ARM64 and nine-case base results belong to their recorded sources;
this patch claims source validation, not new native or public-asset acceptance.
No resources were started. Main, release assets, tags and latest were untouched.

Deployment duration remains a render-first proposal awaiting approval of the
latest alignment: one Duration between author and relative date, an increasing
running value without extra wording, preserved commit truncation and aligned log
header controls. No deployment schema, API or runtime timing changes were made.
Existing generic timestamps cannot establish historical execution durations.

Publication remains on hold. An approved version/release build, signed exact
public-asset acceptance and promotion remain separate gates. No future bridge
hashes were guessed.
