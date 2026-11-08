import { describe, expect, it } from 'vitest';

import {
  formatCm2,
  formatInteger,
  formatReportValue,
  formatCents,
  formatCm,
  formatDateTime,
  formatBp,
  formatE7,
  formatIsoDate,
  formatQtyMilli,
  parseDecimalToE7,
  parseEuroToCents,
  parseMetresToCm,
  parsePercentToBp,
  parseQtyToMilli,
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

describe('format: pricing inputs', () => {
  it('parses euros to integer cents with it and en separators', () => {
    expect(parseEuroToCents('12,50')).toBe(1250);
    expect(parseEuroToCents('12.5')).toBe(1250);
    expect(parseEuroToCents('€ 1.234,56')).toBe(123456);
    expect(parseEuroToCents('1,234.56 €')).toBe(123456);
    expect(parseEuroToCents('1.234')).toBeNull(); // ambiguous
    expect(parseEuroToCents('1.234.567')).toBe(123456700);
    expect(parseEuroToCents('256,50')).toBe(25650);
    expect(parseEuroToCents('0,07')).toBe(7);
    expect(parseEuroToCents('-3')).toBe(-300);
    expect(parseEuroToCents('12,505')).toBeNull();
    expect(parseEuroToCents('1.23.4')).toBeNull();
    expect(parseEuroToCents('abc')).toBeNull();
    expect(parseEuroToCents('')).toBeNull();
  });

  it('parses and formats percentages as basis points', () => {
    expect(parsePercentToBp('22')).toBe(2200);
    expect(parsePercentToBp('22,5 %')).toBe(2250);
    expect(parsePercentToBp('-10%')).toBe(-1000);
    expect(parsePercentToBp('0.05')).toBe(5);
    expect(parsePercentToBp('1,234')).toBeNull();
    expect(formatBp(2200)).toBe('22%');
    expect(formatBp(1250)).toBe('12,5%');
    expect(formatBp(5, 'en')).toBe('0.05%');
    expect(formatBp(-1000)).toBe('-10%');
  });

  it('parses and formats quantities in thousandths', () => {
    expect(parseQtyToMilli('1,5')).toBe(1500);
    expect(parseQtyToMilli('2')).toBe(2000);
    expect(parseQtyToMilli('1.234,567')).toBe(1234567);
    expect(parseQtyToMilli('1,234')).toBe(1234);
    expect(parseQtyToMilli('0,0005')).toBeNull();
    expect(formatQtyMilli(1500)).toBe('1,5');
    expect(formatQtyMilli(2000)).toBe('2');
    expect(formatQtyMilli(1234567)).toBe('1.234,567');
    expect(formatQtyMilli(1500, 'en')).toBe('1.5');
  });

  it('parses quantities as the exact inverse of formatQtyMilli', () => {
    for (const locale of ['it', 'en']) {
      for (const milli of [0, 1, 1500, 999999, 1000000, 1500000, 1234567, -2500]) {
        expect(parseQtyToMilli(formatQtyMilli(milli, locale), locale)).toBe(milli);
      }
    }
    expect(parseQtyToMilli('1.500', 'it')).toBe(1500000);
    expect(parseQtyToMilli('1.500')).toBe(1500000);
    expect(parseQtyToMilli('1,500', 'en')).toBe(1500000);
    expect(parseQtyToMilli('1.5', 'en')).toBe(1500);
    expect(parseQtyToMilli('1.5', 'it')).toBeNull(); // '.' only groups thousands in it
    expect(parseQtyToMilli('1,5', 'en')).toBeNull();
    expect(parseQtyToMilli('1,5,0', 'it')).toBeNull();
    expect(parseQtyToMilli('12.34', 'it')).toBeNull();
    expect(parseQtyToMilli('1,', 'it')).toBeNull();
  });
});

describe('report formats (M7)', () => {
  it('formats square centimetres as square metres, half-even', () => {
    expect(formatCm2(125000)).toBe('12,50 m²');
    expect(formatCm2(150)).toBe('0,02 m²'); // 1.5 hundredths → 2 (half-even)
    expect(formatCm2(250)).toBe('0,02 m²'); // 2.5 hundredths → 2 (half-even)
    expect(formatCm2(0)).toBe('0,00 m²');
    expect(formatCm2(-125000)).toBe('-12,50 m²');
  });

  it('formats integers and values by unit', () => {
    expect(formatInteger(1234567)).toBe('1.234.567');
    expect(formatReportValue(null, 'bp')).toBe('—');
    expect(formatReportValue(8250, 'bp')).toBe('82,5%');
    expect(formatReportValue(123456, 'cents')).toBe('1.234,56 €');
    expect(formatReportValue(1250, 'cm')).toBe('12,50 m');
    expect(formatReportValue(42, 'berth_night')).toBe('42');
  });
});
