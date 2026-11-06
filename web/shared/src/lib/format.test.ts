import { describe, expect, it } from 'vitest';

import { formatCents, formatCm, formatDateTime, formatIsoDate } from './format';

describe('format', () => {
  it('formats ISO dates without timezone shifts', () => {
    expect(formatIsoDate('2026-11-06')).toBe('06/11/2026');
    expect(formatIsoDate('nope')).toBe('nope');
  });

  it('formats centimetres as metres', () => {
    expect(formatCm(1250)).toBe('12,50 m');
    expect(formatCm(5)).toBe('0,05 m');
    expect(formatCm(-1250)).toBe('-12,50 m');
    expect(formatCm(123456)).toBe('1.234,56 m');
    expect(formatCm(1250, 'en')).toBe('12.50 m');
  });

  it('formats cents', () => {
    expect(formatCents(123456)).toBe('1.234,56 €');
    expect(formatCents(123456, 'EUR', 'en')).toBe('€1,234.56');
  });

  it('rejects non-integers', () => {
    expect(() => formatCm(1.5)).toThrow(RangeError);
  });

  it('formats instants in a timezone', () => {
    expect(formatDateTime('2026-11-06T10:00:00Z', 'it', 'Europe/Rome')).toContain('11:00');
  });
});
