/**
 * The request/response shapes used across the app. They mirror the backend
 * models' JSON exactly; commands return these as plain JSON, and the
 * wrapper services cast to them.
 */

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
}

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
  request_headers: string;
  status_code: number;
  response_id: number;
  user_sort_order: number;
  user_color: string | null;
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
