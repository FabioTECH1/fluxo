# Base dashboard polish qualification

All ten approved fixes are implemented on **baseline/clean-base**. Final UI/source **8f5bf765fd1a5ab7cd5c7a168ab34fbacb98a58a** follows implementation **d5c3153fb67fe99dd8125d294b5b1133e20eae39**; all Go/cmd/go.mod/go.sum bytes are identical between them. No release, tag, main/latest/stable promotion, bridge, installer, schema, admission or toolpin change was made.

| Approved item | Implementation and coverage |
| --- | --- |
| 1. API errors | Shared readable presentation across 55 files plus request/login/download boundaries. Original Error.message/rawMessage/payload/status retained for predicates. JSON message/error/validation, text, network, malformed/empty/HTML, bracket prefixes, Vue escaping and non-error literals tested. Explicit technical operation/recovery/terminal/log output stays raw. Full audited/migrated inventory in JSON. |
| 2. Wildcard/panel | Protected enabled managed exact panel coexists; exact-site priority, HTTP/TLS/SNI/cert and IPv4/6 retained. Covered old panel move/remove409 before mutation; explicit disable first. Custom exact collision and unfinished Network publication refuse safely. Existing recovery/revision/custom contracts retained. 27 real native checks. |
| 3. Application type | Existing AppTypeIcon + corresponding plain name, no pill; read-only/immutable. Actual browser appearance verified. |
| 4. System | Hide routine Successful chip; keep date/Enabled/failure/unknown. Actual nullable running_kernel from protected proc, Unavailable on missing/poll error, existing reconnect refresh. Native matches uname; API docs/type/demo and component tests updated. |
| 5. Activity | Suppress pure PHP check/repository review from visible completion/feed; keep operational events, actual mutations/failures/security and all old rows. Shared global/site count/filter tested. Node already omits checks; Nginx/Fluxo writers audited. |
| 6. Network | Nested Settings after Environment/before Vhost. Old query/hash deep links redirect. Both demo/production routers and links/docs updated; Domains/SSL unchanged. |
| 7. Shorter side groups | Shared CSS sticky for all global/site navigation and both Overview information groups; measured header clearance, bounded nav scrolling, tall-info/mobile natural flow and observer cleanup. Actual light/dark/mobile/short-height/scaled checks passed. |
| 8. Retention | Existing Card/FormGroup, 40px select/default-md Save button, responsive controls. Existing policy/state/validation/deletion semantics preserved. Actual heights verified. |
| 9. Observe order | Global keeps Monitoring first then Logs → Activity → Traffic Analytics; site is Logs → Activity → Traffic Analytics. Both real arrays and browser checked. |
| 10. Traffic copy | Concise shared typography and context; origin/CDN, estimates, missing≠zero, HTTP≠application success, UTC/freshness and consequential history rules retained. Long generated/technical notes in closed details. Main actions 40px/default-md; compact range filters/table-row links intentionally retained. |

## Source checks

**8 console + 130 TAP tests**, UI typecheck/Vite, full site/functions/SSR/prerender/docs build passed. Full hostsystem/PHP/Node/Nginx/server suites, affected race and vet passed; final backend server full 33.833s/race 36.436s. Existing bundle-size warning remains informational. Local raw logs are retained privately with SHA256 references in JSON. No local VM, package/service operation or duplicate delegate was run by the implementing agent.

Initial fixture/import/browser findings are retained in JSON: direct Node TS imports and router globals needed fixture corrections; valid in-memory PEM was needed to reach the HTTP routing guard instead of the correct 422 validation refusal. No production validation was weakened. The genuine site Observe ordering/action-size misses were fixed. The dev-ESM cycle was observed on d5; d5 built boot passed and baseline55 already had those eager imports. Lazy routes in both real routers fix the unbundled cycle. Source route stubs are not labeled browser proof; an optional actual installed-Chrome/private-profile/loopback boot regression is included.

## Real integration, with distinct subjects

Root native **27/27 PASS at d5**, using production initialization/handlers/middleware and genuine HTTP bootstrap through a guarded private Go 1.26.3 CGO0 driver with real Nginx/TLS. It covers actual kernel==uname, exact panel/child certificate priority and wildcard fallback on IPv4/6, protected panel lifecycle/refusals/restoration, and pure status Activity. Native report SHA **4873cdf481bbbefff9068b5918dc06380366b02a76c7a6762fbc321f0f86f352**; controller **4adaa5b79e471d166a2fabfbdb8b462ae07837f38e58c2d98b277f2b39f65f95**; driver **fabb0e012200d381e481a6c447de0f06d43aa4e9d40dcbf49ab131c413a570c2**. No backend bytes changed in the later UI fix, so no duplicate VM run.

Actual Root browser at **8f5bf765**: **28/28 built-demo**, **1/1 dev cold boot**, and **5/5 scaled checks** (startup plus 125%/200% effective CSS viewport/deviceScale). These are not browser-toolbar zoom or live-backend/public-release proof; displayed demo data is synthetic. Hashes: built **ddf16e822d885824fa2e49ba37b304f0d16cc2f5afb494d98b7a25cd2576213a**; dev **26b13c26001d511365c6d3b1180e54b502601b86271b44208d0817e2cc46a8e5**; scaled **feabdaff0f94546503950bd821af976f927b88cc6cda9763658d822762abfdec**. The safe reports/checks and screenshot paths are embedded in JSON. Root disclosed two failed shared-browser snapshot attempts and the permitted installed headless-Chrome fallback. Earlier failed selector/closed-disclosure assumptions remain attributed to their original reports.

Root restored original fluxo-dev, deleted only its task snapshot and left it **Stopped, snapshot_count0, original image matched**. Owned Chrome profiles/tabs and Vite5198/preview5199 were closed/stopped. No real VPS was touched.

This is source/native-handler/UI qualification only: no new public binary, normal startup/installer/admission/ordinary base update, genuine Scheduler, package transaction, reboot, ACME, runtime apply or ARM claim. Existing System/Network native and immutable public .2 evidence remain byte-identical; the older .2 release still belongs to 82c22057 and its historical Sury blocker. No release permission is inferred.
