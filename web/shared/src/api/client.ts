import createClient, { type Client, type Middleware } from 'openapi-fetch';

import { readCookie } from '../lib/cookies';
import { ProblemError } from './problem';
import type { paths } from './schema';

/** Cookie set by the server at login, echoed back in X-CSRF-Token (double submit). */
export const CSRF_COOKIE = 'porto_csrf';
export const CSRF_HEADER = 'X-CSRF-Token';

const UNSAFE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

/** Endpoints where a 401 is an expected answer, not an expired session. */
const ANONYMOUS_PATHS = [
  '/api/admin/v1/auth/login',
  '/api/admin/v1/auth/me',
  '/api/admin/v1/auth/password-reset/',
  // Desktop API (M6a/M6b): a 401 is a wrong app PIN or owner password (app_pin_invalid), not an expired session.
  '/api/desktop/v1/',
];

export type ApiClient = Client<paths>;

export type ApiClientOptions = {
  baseUrl?: string;
  fetch?: typeof fetch;
  /** Called on a 401 from an authenticated endpoint (session expired or revoked). */
  onUnauthorized?: () => void;
  /** Cookie source, injectable for tests. */
  readCsrf?: () => string | undefined;
};

/** Adds the CSRF header on unsafe methods, when the cookie exists and no header was set. */
export function csrfMiddleware(readCsrf: () => string | undefined): Middleware {
  return {
    onRequest({ request }) {
      if (!UNSAFE_METHODS.has(request.method.toUpperCase())) return undefined;
      if (request.headers.has(CSRF_HEADER)) return undefined;
      const token = readCsrf();
      if (token) request.headers.set(CSRF_HEADER, token);
      return request;
    },
  };
}

export function unauthorizedMiddleware(onUnauthorized: () => void): Middleware {
  return {
    onResponse({ response, request }) {
      if (response.status !== 401) return undefined;
      const path = new URL(request.url, 'http://local').pathname;
      if (ANONYMOUS_PATHS.some((p) => path.startsWith(p))) return undefined;
      onUnauthorized();
      return undefined;
    },
  };
}

export function createApiClient(options: ApiClientOptions = {}): ApiClient {
  const client = createClient<paths>({
    baseUrl: options.baseUrl ?? '',
    credentials: 'same-origin',
    headers: { Accept: 'application/json, application/problem+json' },
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
  client.use(csrfMiddleware(options.readCsrf ?? (() => readCookie(CSRF_COOKIE))));
  if (options.onUnauthorized) client.use(unauthorizedMiddleware(options.onUnauthorized));
  return client;
}

/** Minimal shape of an openapi-fetch result. */
type FetchResult = { data?: unknown; error?: unknown; response: Response };
type DataOf<R extends FetchResult> = Exclude<R['data'], undefined>;

async function settle<R extends FetchResult>(promise: Promise<R>): Promise<R> {
  let result: R;
  try {
    result = await promise;
  } catch {
    // Network failure, aborted request or a middleware error.
    throw new ProblemError({ type: 'internal_error', status: 0 });
  }
  if (result.error !== undefined || !result.response.ok) {
    throw ProblemError.from(result.response.status, result.error, result.response.headers);
  }
  return result;
}

/**
 * Returns `data` or throws a ProblemError built from the problem+json body.
 * Use inside TanStack Query functions so errors reach `error` as ProblemError.
 */
export async function unwrap<R extends FetchResult>(promise: Promise<R>): Promise<DataOf<R>> {
  const result = await settle(promise);
  return result.data as DataOf<R>;
}

/** Same as unwrap, also returning the response (ETag, Location). */
export async function unwrapWithResponse<R extends FetchResult>(
  promise: Promise<R>,
): Promise<{ data: DataOf<R>; response: Response }> {
  const result = await settle(promise);
  return { data: result.data as DataOf<R>, response: result.response };
}

/** `If-Match` value for a versioned resource (the server sends `ETag: "<version>"`). */
export function ifMatch(version: number | string): string {
  return `"${version}"`;
}

/** A fresh Idempotency-Key; create one per user action and reuse it on retries. */
export function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export type UploadBinaryOptions = {
  /** PUT (document file) or POST (map background). */
  method: 'PUT' | 'POST';
  /** Declared content type of the file (image/png, image/jpeg, image/svg+xml, application/pdf). */
  contentType: string;
  /** Extra headers (If-Match, X-File-Name). */
  headers?: Record<string, string>;
  fetch?: typeof fetch;
  readCsrf?: () => string | undefined;
  signal?: AbortSignal;
};

/**
 * Uploads a raw binary body (m1-plan §4.1: uploads are not multipart) to an admin API path such as
 * `/api/admin/v1/marinas/{m}/documents/{d}/file`, with the CSRF header. Returns the JSON response, or throws a
 * ProblemError (415 unsupported_media_type, 413 file_too_large, 400 invalid_file, ...).
 */
export async function uploadBinary<T>(
  path: string,
  body: Blob | ArrayBuffer | Uint8Array,
  options: UploadBinaryOptions,
): Promise<{ data: T; response: Response }> {
  const headers = new Headers({
    Accept: 'application/json, application/problem+json',
    'Content-Type': options.contentType,
    ...options.headers,
  });
  const token = (options.readCsrf ?? (() => readCookie(CSRF_COOKIE)))();
  if (token) headers.set(CSRF_HEADER, token);
  const doFetch = options.fetch ?? fetch;
  let response: Response;
  try {
    response = await doFetch(path, {
      method: options.method,
      body: body as BodyInit,
      headers,
      credentials: 'same-origin',
      signal: options.signal,
    });
  } catch {
    throw new ProblemError({ type: 'internal_error', status: 0 });
  }
  if (!response.ok) throw await ProblemError.fromResponse(response);
  const text = await response.text();
  return { data: (text ? JSON.parse(text) : undefined) as T, response };
}
