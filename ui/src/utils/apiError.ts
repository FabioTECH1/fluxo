// Wire diagnostics remain intact for recovery/feature predicates. Presentation
// deliberately reads only public message/validation fields, never code/details.
export class APIError extends Error {
  readonly status: number;
  readonly rawMessage: string;
  readonly payload: unknown;
  constructor(raw: string, status: number, fallback = 'Request failed') {
    super(raw || fallback);
    this.name = 'APIError';
    this.status = status;
    this.rawMessage = raw;
    try { this.payload = JSON.parse(raw); } catch { this.payload = null; }
  }
}

export function apiErrorMessage(error: unknown, fallback = 'Request failed'): string {
  const clean = (value: unknown): string => typeof value === 'string' ? value.trim().slice(0, 1000) : '';
  const validation = (value: unknown, depth = 0): string[] => {
    if (depth > 3) return [];
    if (typeof value === 'string') return clean(value) ? [clean(value)] : [];
    if (Array.isArray(value)) return value.slice(0, 5).flatMap(v => validation(v, depth + 1)).slice(0, 3);
    if (value && typeof value === 'object') return Object.values(value).slice(0, 5).flatMap(v => validation(v, depth + 1)).slice(0, 3);
    return [];
  };
  const structured = (body: unknown): string => {
    if (typeof body === 'string') return clean(body);
    if (!body || typeof body !== 'object' || Array.isArray(body)) return '';
    const v = body as Record<string, unknown>;
    return clean(v.message) || clean(v.error) || validation(v.errors).join(' ');
  };
  if (error && typeof error === 'object' && !(error instanceof Error)) return structured(error) || fallback;
  const message = error instanceof Error ? error.message.trim() : typeof error === 'string' ? error.trim() : '';
  if (!message) return fallback;
  if (/^(?:TypeError: )?(?:Failed to fetch|NetworkError when attempting to fetch resource\.?|Load failed)$/i.test(message)) return 'Could not reach the server. Check your connection and try again.';
  // HTML proxy pages and malformed/empty JSON are not useful human messages.
  if (/^<|<!doctype|<html[\s>]/i.test(message)) return fallback;
  try { return structured(JSON.parse(message)) || fallback; }
  catch {
    // Brackets are also normal diagnostic prefixes ([Errno13], [::1]).
    // Only recognizable JSON array syntax is treated as a broken payload;
    // a completed bracket prefix followed by prose remains plain text.
    const bracketProse = /^\[[^\]\r\n]*\](?:\s+\S|:\d)/.test(message);
    const jsonArray = /^\[\s*(?:["{\[\d-]|true\b|false\b|null\b|$)/.test(message);
    return message.startsWith('{') || (jsonArray && !bracketProse) ? fallback : message.slice(0, 1000);
  }
}
