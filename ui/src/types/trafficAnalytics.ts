export type TrafficRange = 'today' | '7d' | '30d';

export interface TrafficSiteStatus {
  site_id: number;
  domain: string;
  enabled: boolean;
  effective_enabled: boolean;
  status: string;
  pending: boolean;
  error?: string;
  last_processed_at?: string | null;
  last_checked_at?: string | null;
  backlog_bytes: number;
  incomplete: boolean;
  dropped_bytes: number;
}

export interface TrafficState {
  master_enabled: boolean;
  pending: boolean;
  status: string;
  error?: string;
  retention_days: number;
  freshness_seconds: number;
  sites: TrafficSiteStatus[];
}

export interface TrafficMetrics {
  requests: number;
  page_views: number;
  not_found: number;
  other_4xx: number;
  server_errors: number;
  response_bytes: number;
  avg_request_time_ms: number;
}

export interface TrafficBucket {
  hour: string;
  metrics: TrafficMetrics | null;
  collection: 'enabled' | 'disabled' | 'partial' | 'unknown';
  incomplete: boolean;
}

export interface TrafficAPIMetrics {
  requests: number;
  status_1xx: number;
  status_2xx: number;
  status_3xx: number;
  status_4xx: number;
  status_5xx: number;
  response_bytes: number;
  avg_request_time_ms: number;
}

export interface TrafficAPIBucket extends Omit<TrafficBucket, 'metrics'> {
  metrics: TrafficAPIMetrics | null;
}

export interface TrafficAPISettings { prefixes: string[]; routes: string[] }
export interface TrafficAPISettingsState { settings: TrafficAPISettings; effective_from: string | null }
export interface TrafficAPIReport extends TrafficAPISettingsState {
  available_from: string | null;
  totals: TrafficAPIMetrics;
  buckets: TrafficAPIBucket[];
  endpoints: { method: string; path: string; metrics: TrafficAPIMetrics }[];
  gaps: TrafficGap[];
  notes: string[];
  incomplete: boolean;
}

export interface TrafficRank { value: string; requests: number }
export interface TrafficGap { from: string; to: string; reason: string }

export interface TrafficReport {
  range: TrafficRange;
  from: string;
  to: string;
  master_enabled: boolean;
  site: TrafficSiteStatus;
  totals: TrafficMetrics;
  buckets: TrafficBucket[];
  top_paths: TrafficRank[];
  referrers: TrafficRank[];
  gaps: TrafficGap[];
  notes: string[];
  api: TrafficAPIReport;
}
