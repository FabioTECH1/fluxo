# v0.4.58-test.2 opt-in release

Published as an immutable prerelease, off main and off latest. User VPS testing and explicit approval are still required before stable promotion.

Source: `5c017c8ac1b0925b73c1b9ce1b52ccbc92ed704d`. Release: [461462886](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.58-test.2). [Official build](https://github.com/FabioTECH1/fluxo/actions/runs/46133295012): success.

## Included changes

- Searchable site title switcher: framework/PHP/repository metadata, keyboard navigation, compatible-route preservation, old-site response guards and unsaved-change guards.
- Persistent restart-required warning with a Show details callback and actual reasons/packages in the shared modal.
- One details/confirmation/restarting/waiting/boot-check/service-result flow, minimisation, manual status checks, authentication and ambiguity handling, and completed-result dismissal.
- Bounded shared service table with sticky header; existing expected-service inventory, startup grace and root maintenance/reboot authority remain unchanged.
- PHP/Node detail modals with exact installed/offered upstream and package revisions, sources and bundled npm; optional fixed official highlights and fresh guarded review handoff, never automatic apply.
- Compact Node updates card with standard blue Update and secondary Check actions, truthful freshness/current/error states and eligible retained rollback presentation.
- PHP Version helper links to Runtime / PHP through router navigation and existing guards.

## Validation and limits

- 169 frontend TAP tests and eight existing console checks pass, including branch selection/polling, compact Node states, guarded reviews, site navigation guards, reboot ambiguity and returned-service status.
- One additional actual-dashboard RouterView/KeepAlive regression passes against unchanged test.2 production source: sections keep separate caches and changing site remounts the child view. It is recorded separately from the 169 tagged tests.
- Dashboard typecheck/build and website/functions/demo/SSR/prerender/docs builds pass.
- Full Go suite passes. Focused release-note/server race and vet checks pass. Local Go checks bind the production-identical test.1 backend; the official test.2 build reruns the full suite on the release source.
- Both live optional official-note readers returned the exact requested target dates and six bounded highlights. Availability and update authority remain independent.
- All eight public byte identities/checksums, four source/tag/official-hosted attestations, both Linux ELF/Go1.26.3/CGo0 images and all 19 tool pins pass. The actual AMD64 version and base/qualified-bridge contract match. The branded tagged installer matches the published script.
- No user VPS install/reboot, local VM use, native ARM acceptance or browser pixel proof is claimed. Existing reboot/package/maintenance guards were retained.

## Superseded first test

Test.1 remains immutable at source `92a54090cc64dc418c0e3e827723da6401376b27`. Its workflow was cancelled, but publication had already completed. No tag or asset was replaced. Test.2 corrects the initial PHP selector state, preserves the selected branch across discovery changes, and avoids repeated note reads during status polling. Use test.2 for new trials.

## Install this exact test

```bash
curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.58-test.2/install.sh -o /tmp/fluxo-0.4.58-test.2-install.sh && sudo env FLUXO_VERSION=v0.4.58-test.2 bash /tmp/fluxo-0.4.58-test.2-install.sh
```

Main remains `3c17dff85ceea6ad1be9fd1a216e46d260e52b4a`; latest stable remains v0.4.57. Bridge source, installer copies, workflows, tool pins, skill/MCP/auth work and old immutable assets are unchanged.

The adjacent JSON contains exact public subjects, selected reference hashes and source-scoped local evidence hashes.
