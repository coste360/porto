import { describe, expect, it } from 'vitest';

import { resources } from '../i18n/resources';
import { PROBLEM_TYPES, ProblemError, problemSlug, toProblem } from './problem';

describe('ProblemError', () => {
  it('parses a problem+json body', () => {
    const p = ProblemError.from(400, {
      type: 'validation_failed',
      title: 'Validation failed',
      status: 400,
      detail: 'bad',
      errors: [{ field: 'email', code: 'invalid' }, { nope: true }],
    });
    expect(p.type).toBe('validation_failed');
    expect(p.status).toBe(400);
    expect(p.detail).toBe('bad');
    expect(p.errors).toEqual([{ field: 'email', code: 'invalid' }]);
    expect(p.i18nKey).toBe('errors:validation_failed');
  });

  it('accepts URI types', () => {
    expect(problemSlug('https://porto.example/problems/slug_taken')).toBe('slug_taken');
    expect(ProblemError.from(409, { type: 'urn:problem#last_owner' }).type).toBe('last_owner');
  });

  it('falls back on the status when the body is not a problem', () => {
    expect(ProblemError.from(404, '<html>').type).toBe('not_found');
    expect(ProblemError.from(502, undefined).type).toBe('internal_error');
  });

  it('reads Retry-After', () => {
    const p = ProblemError.from(
      429,
      { type: 'rate_limited' },
      new Headers({ 'Retry-After': '30' }),
    );
    expect(p.retryAfterSeconds).toBe(30);
  });

  it('maps unknown slugs to the generic message', () => {
    expect(ProblemError.from(400, { type: 'brand_new' }).i18nKey).toBe('errors:internal_error');
  });

  it('parses a Response', async () => {
    const response = new Response(JSON.stringify({ type: 'forbidden', status: 403 }), {
      status: 403,
      headers: { 'Content-Type': 'application/problem+json' },
    });
    expect((await ProblemError.fromResponse(response)).type).toBe('forbidden');
  });

  it('normalises anything thrown', () => {
    expect(toProblem(new Error('x')).type).toBe('internal_error');
  });
});

describe('errors namespace', () => {
  it.each(['it', 'en'] as const)('has a message for every problem type in %s', (locale) => {
    const errors = resources[locale].errors as Record<string, string>;
    for (const type of PROBLEM_TYPES) {
      expect(errors[type], `${locale} errors.${type}`).toBeTruthy();
    }
  });
});
