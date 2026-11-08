import { BACKOFF_CAP_MS, backoffMs, classify, retryAfterMs } from './classify';

describe('classify', () => {
  const cases: [string, Parameters<typeof classify>[0], string][] = [
    ['201', { status: 201 }, 'done'],
    ['200 replayed', { status: 200 }, 'done'],
    ['network error', { status: 0 }, 'retry'],
    ['408', { status: 408 }, 'retry'],
    ['429', { status: 429 }, 'retry'],
    ['500', { status: 500 }, 'retry'],
    ['502', { status: 502 }, 'retry'],
    ['503', { status: 503 }, 'retry'],
    ['504', { status: 504 }, 'retry'],
    ['409 in progress', { status: 409, problemType: 'idempotency_in_progress' }, 'retry'],
    ['409 concurrent', { status: 409, problemType: 'concurrent_update' }, 'retry'],
    ['401', { status: 401, problemType: 'unauthorized' }, 'auth'],
    ['403', { status: 403, problemType: 'forbidden' }, 'conflict'],
    ['404', { status: 404, problemType: 'not_found' }, 'conflict'],
    ['409 allocation', { status: 409, problemType: 'allocation_changed' }, 'conflict'],
    ['409 transition', { status: 409, problemType: 'invalid_transition' }, 'conflict'],
    ['409 match', { status: 409, problemType: 'match_exceeds' }, 'conflict'],
    ['409 no type', { status: 409 }, 'conflict'],
    ['412', { status: 412 }, 'conflict'],
    ['413', { status: 413 }, 'conflict'],
    ['415', { status: 415 }, 'conflict'],
    ['422', { status: 422, problemType: 'offline_not_allowed' }, 'conflict'],
    ['501', { status: 501 }, 'conflict'],
  ];
  it.each(cases)('%s → %s', (_name, answer, kind) => {
    expect(classify(answer, 1, 0.5).kind).toBe(kind);
  });
});

describe('backoff', () => {
  it('doubles from 1 s up to 5 min, with ±20 % jitter', () => {
    expect(backoffMs(1, 0.5)).toBe(1_000);
    expect(backoffMs(2, 0.5)).toBe(2_000);
    expect(backoffMs(5, 0.5)).toBe(16_000);
    expect(backoffMs(30, 0.5)).toBe(BACKOFF_CAP_MS);
    expect(backoffMs(3, 0)).toBe(3_200);
    expect(backoffMs(3, 0.999999)).toBe(4_800);
    expect(backoffMs(40, 0.999999)).toBe(BACKOFF_CAP_MS);
  });

  it('honours Retry-After when longer than the backoff', () => {
    expect(retryAfterMs('7')).toBe(7_000);
    expect(retryAfterMs('Wed, 21 Oct 2027 07:28:00 GMT')).toBeUndefined();
    expect(retryAfterMs(null)).toBeUndefined();
    const r = classify({ status: 503, retryAfter: '30' }, 1, 0.5);
    expect(r).toEqual({ kind: 'retry', afterMs: 30_000 });
    const short = classify({ status: 429, retryAfter: '1' }, 4, 0.5);
    expect(short).toEqual({ kind: 'retry', afterMs: 8_000 });
    expect(classify({ status: 503, retryAfter: '999999' }, 1, 0.5)).toEqual({
      kind: 'retry',
      afterMs: BACKOFF_CAP_MS,
    });
  });
});
