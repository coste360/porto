import { describe, expect, it } from 'vitest';

import { ProblemError } from '@porto/shared/api';

import { codeComplete, signingView } from './signingState';

const link = {
  signer_status: 'invited' as const,
  request_status: 'sent' as const,
  attempts_left: 5,
};

describe('signing page states', () => {
  it('starts with the identity confirmation, then the code input with attempts left', () => {
    expect(signingView({ link, codeSent: false })).toEqual({ kind: 'confirm' });
    expect(signingView({ link, codeSent: true })).toEqual({
      kind: 'code',
      invalid: false,
      attemptsLeft: 5,
    });
  });

  it('shows the attempts left of an otp_invalid answer', () => {
    const err = new ProblemError({
      type: 'otp_invalid',
      status: 422,
      details: { attempts_left: 2 },
    });
    expect(signingView({ link, codeSent: true, lastError: err })).toEqual({
      kind: 'code',
      invalid: true,
      attemptsLeft: 2,
    });
  });

  it('shows locked and rate limited states', () => {
    expect(
      signingView({
        link,
        codeSent: true,
        lastError: new ProblemError({ type: 'otp_locked', status: 409 }),
      }),
    ).toEqual({ kind: 'locked' });
    expect(
      signingView({
        link,
        codeSent: false,
        lastError: new ProblemError({
          type: 'otp_rate_limited',
          status: 429,
          retryAfterSeconds: 120,
        }),
      }),
    ).toEqual({ kind: 'rateLimited', retryAfterSeconds: 120 });
  });

  it('ends on signed (with the deposit), declined, closed or waiting for the previous signer', () => {
    const deposit = { amount_cents: 90_000, due_on: '2026-11-14' };
    expect(
      signingView({
        link,
        codeSent: true,
        lastResult: { signer_status: 'signed', request_status: 'signed', deposit },
      }),
    ).toEqual({ kind: 'signed', deposit });
    expect(signingView({ link: { ...link, signer_status: 'declined' }, codeSent: false })).toEqual({
      kind: 'declined',
    });
    expect(signingView({ link: { ...link, request_status: 'expired' }, codeSent: false })).toEqual({
      kind: 'closed',
      status: 'expired',
    });
    expect(signingView({ link: { ...link, signer_status: 'waiting' }, codeSent: false })).toEqual({
      kind: 'waiting',
    });
  });

  it('accepts six digits only', () => {
    expect(codeComplete('123456')).toBe(true);
    expect(codeComplete('12345')).toBe(false);
    expect(codeComplete('12345a')).toBe(false);
  });
});
