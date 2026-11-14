export type HttpStatusClass = '2xx' | '3xx' | '4xx' | '5xx';

/** Buckets an HTTP status code so the UI can colour it by class (theme tokens --status-*). */
export function httpStatusClass(code: number): HttpStatusClass {
  if (code >= 500) return '5xx';
  if (code >= 400) return '4xx';
  if (code >= 300) return '3xx';
  return '2xx';
}
