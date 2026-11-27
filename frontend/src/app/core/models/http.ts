/**
 * The request/response shapes used across the app. They mirror the backend
 * models' JSON exactly; commands return these as plain JSON, and the
 * wrapper services cast to them.
 */

/** The HTTP methods a request can use, in menu order. */
export const HTTP_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];

/** A lightweight request projection for list views (no body or headers). */
export interface HttpRequestSummary {
  id: number;
  collection_id: number;
  project_id: number;
  name: string;
  url: string;
  method: string;
  status_code: number;
  response_id: number;
  user_sort_order: number;
  user_color: string | null;
  last_used_at: number;
  /** The request group it is in (collections only); absent or null = none. */
  group_id?: number | null;
}

/** How much a request is used lately (request.list_usage): a fading score and its level. */
export interface RequestUsage {
  id: number;
  score: number;
  level: UsageLevel;
}

export type UsageLevel = 'high' | 'medium' | 'low';

/** A saved REST request. */
export interface HttpRequest {
  id: number;
  collection_id: number;
  project_id: number;
  name: string;
  url: string;
  method: string;
  body: string;
  body_type: string;
  body_form: string;
  request_headers: string;
  status_code: number;
  response_id: number;
  user_sort_order: number;
  user_color: string | null;
  last_used_at: number;
}

/** A saved response (or an in-flight pending one). */
export interface HttpResponse {
  id: number;
  request_id: number;
  headers: string;
  status_code: number;
  body: string;
  created_at: string;
  duration_ms: number;
  /** What was sent, secrets hidden; empty on older rows that never stored it. */
  method: string;
  url: string;
  /** What the request's scripts did in this send, in run order (null when none ran). */
  script_log: ScriptRun[] | null;
}

/** One script in one phase of a send (see backend/scripting). */
export interface ScriptRun {
  script_id: number;
  script: string;
  phase: 'beforeRequest' | 'afterResponse';
  duration_ms: number;
  console: { level: 'log' | 'info' | 'warn' | 'error'; text: string }[] | null;
  tests: { name: string; passed: boolean; error?: string }[] | null;
  error?: string;
}

/** A request sitting in the trash bin. */
export interface HttpBinnedRequestSummary {
  id: number;
  original_request_id: number;
  project_id: number;
  collection_id: number;
  original_collection_id: number;
  original_collection_name: string;
  name: string;
  url: string;
  method: string;
  status_code: number;
  response_id: number;
  deleted_at: string;
}
