import { apiErrorMessage } from './apiError.ts';
import type { TrafficAPIBucket, TrafficAPISettings, TrafficBucket, TrafficGap, TrafficSiteStatus } from '../types/trafficAnalytics.ts';

export const trafficCoverageNote = 'Based on requests reaching this server. Visits served entirely from a CDN or browser cache are not included.';
export const trafficMilliseconds = (value: number) => `${new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format(value)} ms`;
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
  return apiErrorMessage(error, 'Could not load traffic analytics.');
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

export const trafficCollected = (bucket: TrafficBucket | TrafficAPIBucket) => !!bucket.metrics && bucket.collection !== 'disabled' && bucket.collection !== 'unknown';

export function trafficAPISettings(prefixesText: string, routesText: string): { settings: TrafficAPISettings; errors: { prefixes: string; routes: string } } {
  const lines = (text: string) => text.split(/\r?\n/).map(line => line.trim()).filter(Boolean);
  const settings = { prefixes: lines(prefixesText), routes: lines(routesText) };
  const errors = { prefixes: '', routes: '' };
  for (const key of ['prefixes', 'routes'] as const) {
    const values = settings[key];
    const limit = key === 'prefixes' ? 16 : 32;
    if (values.length > limit) errors[key] = `Use at most ${limit} ${key}.`;
    else if (new Set(values).size !== values.length) errors[key] = `Remove duplicate ${key}.`;
    else for (const value of values) {
      const invalid = !value.startsWith('/') || new TextEncoder().encode(value).length > 512 || /[\s\u0000-\u001f\u007f-\u009f?#\\*{}\[\]]/.test(value) || value.includes('//') || value.split('/').some(segment => segment === '.' || segment === '..') || (value.length > 1 && value.endsWith('/'));
      const parameters = value.split('/').filter(segment => segment.includes(':'));
      if (invalid || (key === 'prefixes' ? parameters.length > 0 : parameters.some(segment => !/^:[A-Za-z_][A-Za-z0-9_]*$/.test(segment)) || new Set(parameters).size !== parameters.length)) {
        errors[key] = key === 'prefixes' ? 'Use literal paths beginning with /, without queries, spaces, wildcards or trailing slashes (except /).' : 'Use paths beginning with / and whole :parameter segments, without queries, spaces, wildcards or trailing slashes (except /). Parameter names must be unique.';
        break;
      }
    }
  }
  if (!errors.routes) {
    const routes = settings.routes.map(route => route.split('/'));
    for (const [index, route] of routes.entries()) {
      const literals = route.filter(segment => !segment.startsWith(':')).length;
      if (routes.slice(0, index).some(prior => prior.length === route.length && prior.filter(segment => !segment.startsWith(':')).length === literals && prior.every((segment, part) => segment.startsWith(':') || route[part]!.startsWith(':') || segment === route[part]))) {
        errors.routes = 'Routes with the same specificity must not overlap. Use one template per endpoint group.';
        break;
      }
    }
  }
  return { settings, errors };
}

// Null and disabled buckets deliberately split the line: a collection gap is never zero traffic.
export function trafficSeries(buckets: (TrafficBucket | TrafficAPIBucket)[], key: 'requests' | 'page_views' | 'http_errors', from: string, to: string, scaleMax?: number) {
  const start = Date.parse(from);
  const end = Date.parse(to);
  const width = 660;
  const height = 152;
  const metricValue = (bucket: TrafficBucket | TrafficAPIBucket) => {
    if (!bucket.metrics) return 0;
    if (key === 'http_errors') return 'status_4xx' in bucket.metrics ? bucket.metrics.status_4xx + bucket.metrics.status_5xx : 0;
    if (key === 'page_views') return 'page_views' in bucket.metrics ? bucket.metrics.page_views : 0;
    return bucket.metrics.requests;
  };
  const max = Math.max(1, scaleMax ?? Math.max(0, ...buckets.filter(trafficCollected).map(metricValue)));
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
    const value = Math.max(0, metricValue(bucket));
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

export const trafficRetentionOptions = [
  { value: 30, label: '30 days' },
  { value: 60, label: '60 days' },
  { value: 90, label: '90 days' },
  { value: 180, label: '180 days' },
  { value: 365, label: '365 days' },
  { value: 0, label: 'Keep until manually deleted' },
] as const;

export function trafficRetentionLabel(days: number): string {
  return days === 0 ? 'Keep until manually deleted' : `${days}-day retention`;
}
