import { describe, expect, it } from 'vitest';

import {
  formatCents,
  formatCm,
  formatDateTime,
  formatE7,
  formatIsoDate,
  parseDecimalToE7,
  parseMetresToCm,
} from './format';

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

describe('integer parsers', () => {
  it('parses metres to centimetres', () => {
    expect(parseMetresToCm('12,50')).toBe(1250);
    expect(parseMetresToCm('12.5')).toBe(1250);
    expect(parseMetresToCm('7')).toBe(700);
    expect(parseMetresToCm(' 0,05 ')).toBe(5);
    expect(parseMetresToCm('12,345')).toBeNull();
    expect(parseMetresToCm('1e3')).toBeNull();
    expect(parseMetresToCm('')).toBeNull();
  });

  it('formats and parses E7 coordinates without floats', () => {
    expect(formatE7(123456789)).toBe('12.3456789');
    expect(formatE7(-5)).toBe('-0.0000005');
    expect(parseDecimalToE7('12.3456789')).toBe(123456789);
    expect(parseDecimalToE7('45,4')).toBe(454000000);
    expect(parseDecimalToE7('-0.00000005')).toBe(0);
    expect(parseDecimalToE7('0.00000015')).toBe(2);
    expect(parseDecimalToE7('0.000000151')).toBe(2);
    expect(parseDecimalToE7('0.00000025')).toBe(2);
    expect(parseDecimalToE7('abc')).toBeNull();
  });
});
