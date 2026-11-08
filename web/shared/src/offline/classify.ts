// PURE: what the queue does with the answer to a replayed operation (m6b-plan §4.3).
//
//   2xx                                                    → done (result stored)
//   network error (status 0), 408, 429, 500–504,
//   409 idempotency_in_progress, 409 concurrent_update      → retry with the same key (backoff, Retry-After)
//   401                                                    → auth: the runner pauses until the same user signs in
//   403, 404, 409 (other), 412, 413, 415, 422, other 4xx   → conflict: kept with its problem, shown to the user
//
// Expiry (an op older than 6 days is never resent: the server keeps offline keys 7 days) is decided by the queue.

export type Outcome =
  { kind: 'done' } | { kind: 'retry'; afterMs: number } | { kind: 'conflict' } | { kind: 'auth' };

export type Answer = {
  /** HTTP status, 0 for a network error or an aborted request. */
  status: number;
  /** Problem slug of a problem+json body, when any. */
  problemType?: string;
  /** Retry-After header value (seconds), when any. */
  retryAfter?: string | null;
};

const TRANSIENT_STATUS = new Set([0, 408, 429, 500, 502, 503, 504]);
const TRANSIENT_409 = new Set(['idempotency_in_progress', 'concurrent_update']);

export const BACKOFF_BASE_MS = 1_000;
export const BACKOFF_CAP_MS = 5 * 60_000;
const JITTER = 0.2;

/** Backoff before attempt n+1 (n = attempts already made, ≥ 1): 1 s × 2ⁿ⁻¹, capped at 5 min, ±20 % jitter.
 * `random` is in [0, 1). */
export function backoffMs(attempts: number, random: number): number {
  const exp = Math.min(BACKOFF_CAP_MS, BACKOFF_BASE_MS * 2 ** Math.max(0, attempts - 1));
  const jitter = 1 + JITTER * (2 * random - 1);
  return Math.min(BACKOFF_CAP_MS, Math.round(exp * jitter));
}

/** Retry-After in milliseconds (delta-seconds only; HTTP dates are ignored), or undefined. */
export function retryAfterMs(value: string | null | undefined): number | undefined {
  if (!value || !/^\d{1,6}$/.test(value.trim())) return undefined;
  return Number(value.trim()) * 1_000;
}

/** Classifies an answer. `attempts` counts this attempt; `random` drives the jitter. */
export function classify(a: Answer, attempts: number, random: number): Outcome {
  if (a.status >= 200 && a.status < 300) return { kind: 'done' };
  if (a.status === 401) return { kind: 'auth' };
  const transient =
    TRANSIENT_STATUS.has(a.status) ||
    (a.status === 409 && a.problemType !== undefined && TRANSIENT_409.has(a.problemType));
  if (transient) {
    const backoff = backoffMs(attempts, random);
    const hinted = retryAfterMs(a.retryAfter);
    return { kind: 'retry', afterMs: Math.min(BACKOFF_CAP_MS, Math.max(backoff, hinted ?? 0)) };
  }
  return { kind: 'conflict' };
}
