---
title: Traffic Analytics
description: Collect origin requests, read Traffic reports and manage retained history.
---

# Traffic analytics

Traffic Analytics summarizes requests reaching each site's Nginx origin.
Requests served entirely from a CDN or browser cache are not included.

## Enable collection

1. Open **Observe > Traffic Analytics** and enable server collection.
2. Select sites there, or enable a site in **Site > Observe > Traffic Analytics**.
3. Wait for **Applying change** or **Draining** to finish.

Both server and site collection must be enabled. Turning the server switch off
preserves site selections. Up to **32 sites** can collect simultaneously.
Collection starts from enablement; it does not import ordinary access logs or
backfill disabled periods. After a failed change, refresh the saved state before
retrying.

## Read a report

The site's report has three tabs:

| Tab | Shows |
|---|---|
| Overview | Origin requests, estimated page views, API requests and HTTP errors. |
| Pages | Estimated page views, popular paths and referring domains. |
| API traffic | Requests matching configured API paths, status classes, response bytes, timing and endpoint groups. |

Tabs share the same collection settings, date range and retained history.
Reports usually appear within **30–60 seconds**. Check the last processed time
and any backlog or incomplete-data indicator. Refresh reads saved aggregates;
it does not rescan old logs. Active reports poll while visible; disabled
reports can be refreshed manually.

### Date ranges

**Range** follows the server's retention policy: Today, Last 7 days, then
30/60/90/180/365 days up to the saved limit. **Keep until manually deleted** also
offers **All retained history**. Reducing retention replaces an unavailable
selection with the longest permitted range.

Ranges include today and use UTC calendar boundaries. The displayed from/to
timestamps identify the covered interval. Charts use hourly periods up to
30 days, daily periods up to 180 days, then weekly or wider periods, with at most
720 periods. The final period may still be in progress. Selecting a longer range
does not recover deleted or uncollected data.

### Metrics

| Metric | Meaning |
|---|---|
| Requests | All observed requests, including assets, bots and APIs; not visits or people. |
| Estimated page views | Successful or redirected `GET` page requests, excluding known bots, common assets and configured API paths. Classification is approximate. |
| Popular page paths | Estimated page-view paths without query strings; overflow may be grouped as `(other)`. |
| Referrer domains | Available referring hostnames, without full URLs. Direct/private requests may have no referrer. |
| 404 / other 4xx / 5xx | HTTP response counts; other 4xx excludes 404. |
| Nginx response bytes | Origin response-body bytes, excluding headers and CDN delivery. |
| Avg Nginx request time | Request-weighted milliseconds including upstream work and response transmission; not browser rendering time. |

## Configure API traffic

The default prefix `/api` matches `/api` and `/api/...`, not `/apiculture`.
Use **Configure API** to add literal prefixes or route patterns. For example,
`/api/users/:id` groups one path segment; an explicit `/api/users/me` rule can
take priority. Endpoint groups remain separated by HTTP method. Explicit patterns
can also classify paths outside the prefixes. Unmatched prefix paths retain
their sanitized path, subject to endpoint limits.

Rules apply from their saved effective time. They do not reclassify stored
history; queued requests use the rules in effect when recorded. Matching API
requests are excluded from estimated page views. Fluxo does not infer routes
from source code or inspect request/response bodies.

API totals include all matching methods and bots. The error line counts
**4xx + 5xx**; redirects are not errors. Averages are weighted by request count.
**HTTP 200 does not establish application success**, including for GraphQL.
Older overall summaries cannot reconstruct missing API history.

## Missing or incomplete data

A gap means unavailable data, not zero requests. A quiet observed period can be
zero; disabled collection, deleted history or periods before collection began
remain gaps. Partial periods may contain both requests and missing coverage.
Use the values table and **Collection gaps** to inspect them.

Backlogs, malformed records, log rotation and storage pressure can leave reports
incomplete. Address the displayed cause before re-enabling collection. Retained
aggregates remain available under the retention policy.

### Collection limits

| Limit | Value |
|---|---|
| Collection work per pass | 4 MiB, 32,768 records and a one-second budget across sites. |
| Page/referrer dimensions | 64 paths and 32 referrers per site/hour, plus `(other)`; shared 50,000-row budget. Reports rank up to 20 values. |
| API rules | 16 prefixes and 32 explicit patterns per site; ambiguous patterns are rejected. |
| API endpoint dimensions | 64 method/path pairs per site/hour and 50,000 retained rows, plus `(other)`. Reports rank up to 100 groups. |
| API rule history | 128 intervals / 64 KiB per site; further changes are refused rather than discarding needed rules. |
| Saved API preferences | Up to 256 sites. |
| Analytics database | 256 MiB main database, with 8 MiB reserved for control/checkpoint work. WAL checkpoints target 8 MiB and may temporarily exceed it. |
| Current-log rotation target | 4 MiB after the previous interval drains; this is not a hard file-size cap. |
| Log-pressure cutoff | 16 MiB per site or 128 MiB across the server's analytics logs. |
| Free-space cutoff | Server collection stops below 128 MiB on the analytics filesystem. |

At a storage cutoff, collection pauses and drains. Free space or explicitly
delete unneeded history, then enable collection again. Log files can temporarily
exceed rotation targets; processing limits do not guarantee a fixed CPU or disk
footprint.

## Retention and deletion

Under **Observe > Traffic Analytics > Traffic history retention**, select **30 days**
(default), **60**, **90**, **180**, **365**, or **Keep until manually deleted**.
The server-wide policy survives restarts and applies while collection is off.
Finite policies expire older aggregates during bounded hourly cleanup.

- Shortening retention makes older history eligible for deletion.
- Lengthening it expands available view ranges but cannot recover deleted data.
- **Keep until manually deleted** disables age-based expiry, not storage limits.
  At capacity, collection pauses instead of evicting retained history.

**Delete analytics history** requires confirmation and removes that site's
retained analytics. It preserves the collection setting and API preferences;
an enabled site continues collecting new requests. Earlier queued requests do
not recreate deleted history. Preferences for deleted sites are reclaimed;
their retained aggregates and rules still follow retention.

## Data and privacy

The analytics stream records bounded paths without query strings, methods,
statuses, timing, response bytes, classification flags and referring domains.
It does not store IP addresses, cookies, sessions, full referrer URLs, raw
user-agent strings or location identifiers. There is no unique-visitor profile.

Ordinary Nginx logs are separate. Analytics live in
`/var/lib/fluxo/traffic/analytics.db` (or `traffic/analytics.db` under a custom
Fluxo data directory). See [Paths and services](../reference/paths-services)
and the [Traffic Analytics API](../reference/api#traffic-analytics).
