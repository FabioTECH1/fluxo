// Some older endpoints return text; structured API failures carry a message.
// Keep the original payload in the Error for callers that need its code/details.
export function apiErrorMessage(error: unknown, fallback = 'Request failed'): string {
  const message = error instanceof Error ? error.message : typeof error === 'string' ? error : '';
  if (!message.trim()) return fallback;
  try {
    const body: unknown = JSON.parse(message);
    if (body && typeof body === 'object' && 'message' in body
        && typeof body.message === 'string' && body.message.trim()) return body.message;
    return fallback;
  } catch {
    return message;
  }
}
