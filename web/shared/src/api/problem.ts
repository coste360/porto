/** RFC 9457 problem+json errors as returned by the Porto API (plan §4.4). */

import type { MapIssue } from './schema';

export type ProblemFieldError = { field: string; code: string; message?: string };

/** Stable problem slugs (plan §4.4); the UI shows each through `errors:<slug>`. */
export const PROBLEM_TYPES = [
  'bad_request',
  'validation_failed',
  'weak_password',
  'invalid_reset_token',
  'invalid_totp',
  'pairing_code_invalid',
  'invalid_scope',
  'invalid_webhook_url',
  'unknown_event_type',
  'unauthorized',
  'invalid_credentials',
  'totp_required',
  'forbidden',
  'csrf_failed',
  'totp_enrolment_required',
  'totp_required_by_policy',
  'not_found',
  'method_not_allowed',
  'conflict',
  'slug_taken',
  'already_member',
  'last_owner',
  'totp_already_enabled',
  'totp_not_enrolled',
  'endpoint_disabled',
  'idempotency_in_progress',
  'version_conflict',
  'unsupported_media_type',
  'idempotency_key_reused',
  'precondition_required',
  'rate_limited',
  'internal_error',
  'not_ready',
  // M1
  'map_invalid',
  'map_not_empty',
  'map_not_georeferenced',
  'invalid_file',
  'file_too_large',
  'csv_invalid',
  'customer_anonymized',
  'customer_merged',
  'download_link_expired',
] as const;

export type ProblemType = (typeof PROBLEM_TYPES)[number];

/** Slug used when the response is not a problem document (network failure, proxy page). */
export const UNKNOWN_PROBLEM = 'internal_error';

const STATUS_FALLBACK: Record<number, ProblemType> = {
  400: 'bad_request',
  401: 'unauthorized',
  403: 'forbidden',
  404: 'not_found',
  405: 'method_not_allowed',
  409: 'conflict',
  412: 'version_conflict',
  415: 'unsupported_media_type',
  428: 'precondition_required',
  429: 'rate_limited',
  503: 'not_ready',
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Extracts the slug from a problem `type`, which may be a bare slug or a URI ending in it. */
export function problemSlug(type: string): string {
  const trimmed = type.replace(/[/#]+$/, '');
  const idx = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('#'));
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed;
}

export class ProblemError extends Error {
  readonly type: string;
  readonly status: number;
  readonly title: string;
  readonly detail: string | undefined;
  readonly errors: ProblemFieldError[];
  readonly retryAfterSeconds: number | undefined;
  /** Map validation issues of a map_invalid problem (M1). */
  readonly issues: MapIssue[];

  constructor(init: {
    type: string;
    status: number;
    title?: string;
    detail?: string | undefined;
    errors?: ProblemFieldError[];
    retryAfterSeconds?: number | undefined;
    issues?: MapIssue[];
  }) {
    super(init.title ?? init.type);
    this.name = 'ProblemError';
    this.type = init.type;
    this.status = init.status;
    this.title = init.title ?? init.type;
    this.detail = init.detail;
    this.errors = init.errors ?? [];
    this.retryAfterSeconds = init.retryAfterSeconds;
    this.issues = init.issues ?? [];
  }

  /** i18n key for this problem, always present in the `errors` namespace. */
  get i18nKey(): string {
    const known = (PROBLEM_TYPES as readonly string[]).includes(this.type);
    return `errors:${known ? this.type : UNKNOWN_PROBLEM}`;
  }

  is(type: ProblemType): boolean {
    return this.type === type;
  }

  /** Builds a ProblemError from an HTTP status and a parsed (or raw) body. */
  static from(status: number, body: unknown, headers?: Headers): ProblemError {
    const retry = headers?.get('Retry-After');
    const retryAfterSeconds = retry && /^\d+$/.test(retry) ? Number(retry) : undefined;
    if (isRecord(body) && typeof body.type === 'string') {
      const errors = Array.isArray(body.errors)
        ? body.errors.filter(
            (e): e is ProblemFieldError =>
              isRecord(e) && typeof e.field === 'string' && typeof e.code === 'string',
          )
        : [];
      return new ProblemError({
        type: problemSlug(body.type),
        status: typeof body.status === 'number' ? body.status : status,
        title: typeof body.title === 'string' ? body.title : undefined,
        detail: typeof body.detail === 'string' ? body.detail : undefined,
        errors,
        retryAfterSeconds,
        issues: Array.isArray(body.issues)
          ? body.issues.filter(
              (i): i is MapIssue =>
                isRecord(i) && typeof i.code === 'string' && Array.isArray(i.refs),
            )
          : [],
      });
    }
    return new ProblemError({
      type: STATUS_FALLBACK[status] ?? UNKNOWN_PROBLEM,
      status,
      retryAfterSeconds,
    });
  }

  /** Parses a fetch Response carrying (or not) a problem+json body. */
  static async fromResponse(response: Response): Promise<ProblemError> {
    let body: unknown;
    try {
      const text = await response.text();
      body = text ? JSON.parse(text) : undefined;
    } catch {
      body = undefined;
    }
    return ProblemError.from(response.status, body, response.headers);
  }
}

export function isProblem(error: unknown, type?: ProblemType): error is ProblemError {
  return error instanceof ProblemError && (type === undefined || error.type === type);
}

/** Normalises anything thrown by a query or mutation into a ProblemError. */
export function toProblem(error: unknown): ProblemError {
  if (error instanceof ProblemError) return error;
  return new ProblemError({ type: UNKNOWN_PROBLEM, status: 0 });
}
