import type { TrafficBucket, TrafficGap, TrafficSiteStatus } from '../types/trafficAnalytics.ts';

export const trafficCoverageNote = 'Based on requests reaching this server. Visits served entirely from a CDN or browser cache are not included.';
export const trafficCount = (value: number) => new Intl.NumberFormat('en').format(Math.max(0, value));

export function trafficBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  const exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${new Intl.NumberFormat('en', { maximumFractionDigits: exponent ? 1 : 0 }).format(value / 1024 ** exponent)} ${units[exponent]}`;
}

export function trafficDate(value?: string | null): string {
  if (!value || !Number.isFinite(Date.parse(value))) return 'Not yet processed';
  return new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(value)) + ' UTC';
}

export function trafficError(error: unknown): string {
  const message = error instanceof Error ? error.message : 'Could not load traffic analytics.';
  try {
    const detail = JSON.parse(message);
    return detail.message || detail.error || message;
  } catch { return message; }
}

export function trafficStatus(site: TrafficSiteStatus): { label: string; variant: 'green' | 'red' | 'yellow' | 'gray' } {
  if (site.pending || ['pending', 'applying', 'draining', 'enabling', 'disabling'].includes(site.status)) {
    return { label: site.status === 'draining' ? 'Draining' : 'Applying change', variant: 'yellow' };
  }
  if (site.error || ['error', 'failed', 'blocked'].includes(site.status)) return { label: 'Needs attention', variant: 'red' };
  if (site.incomplete) return { label: 'Incomplete data', variant: 'yellow' };
  if (site.backlog_bytes > 0) return { label: 'Processing backlog', variant: 'yellow' };
  if (trafficIsDelayed(site)) return { label: 'Collection delayed', variant: 'yellow' };
  if (site.effective_enabled) return { label: 'Collecting', variant: 'green' };
  return { label: site.enabled ? 'Server collection off' : 'Disabled', variant: 'gray' };
}

export function trafficIsDelayed(site: TrafficSiteStatus, now = Date.now()): boolean {
  if (!site.effective_enabled) return false;
  const checked = Date.parse(site.last_checked_at || site.last_processed_at || '');
  return site.backlog_bytes > 0 || (Number.isFinite(checked) && now - checked > 90_000);
}

export function trafficPollDelay(pending: boolean, enabled: boolean): number | null {
  return pending ? 15_000 : enabled ? 30_000 : null;
}

// Null and disabled buckets deliberately split the line: a collection gap is never zero traffic.
export function trafficSeries(buckets: TrafficBucket[], key: 'requests' | 'page_views', from: string, to: string) {
  const start = Date.parse(from);
  const end = Date.parse(to);
  const width = 660;
  const height = 152;
  const max = Math.max(1, ...buckets.filter(bucket => bucket.collection !== 'disabled' && bucket.collection !== 'unknown').map(bucket => bucket.metrics?.[key] || 0));
  const points: { x: number; y: number; value: number; hour: string; partial: boolean }[] = [];
  const runs: string[] = [];
  let run: string[] = [];
  let previous = Number.NaN;
  const flush = () => { if (run.length) runs.push(run.join(' ')); run = []; };
  for (const bucket of buckets) {
    const hour = Date.parse(bucket.hour);
    if (!bucket.metrics || bucket.collection === 'disabled' || bucket.collection === 'unknown' || !Number.isFinite(hour) || end <= start) {
      flush(); previous = Number.NaN; continue;
    }
    if (hour >= end || hour + 3_600_000 <= start) continue;
    const partial = bucket.incomplete || bucket.collection === 'partial';
    if (partial || hour - previous !== 3_600_000) flush();
    const center = (Math.max(start, hour) + Math.min(end, hour + 3_600_000)) / 2;
    const x = 44 + Math.max(0, Math.min(1, (center - start) / (end - start))) * width;
    const value = Math.max(0, bucket.metrics[key]);
    const y = 172 - value / max * height;
    points.push({ x, y, value, hour: bucket.hour, partial });
    run.push(`${x.toFixed(2)},${y.toFixed(2)}`);
    previous = partial ? Number.NaN : hour;
    if (partial) flush();
  }
  flush();
  return { max, points, runs };
}

export function trafficGapBands(gaps: TrafficGap[], from: string, to: string) {
  const start = Date.parse(from);
  const duration = Date.parse(to) - start;
  if (!Number.isFinite(duration) || duration <= 0) return [];
  return gaps.flatMap(gap => {
    const left = Math.max(0, (Date.parse(gap.from) - start) / duration);
    const right = Math.min(1, (Date.parse(gap.to) - start) / duration);
    return Number.isFinite(left) && Number.isFinite(right) && right > left
      ? [{ ...gap, x: 44 + left * 660, width: (right - left) * 660 }]
      : [];
  });
}
