import { describe, expect, it } from 'vitest';

import {
  applyCsvToGrid,
  checkGrid,
  checkShares,
  equalShares,
  gridEntries,
  parseMillesimalCsv,
  parseMillis,
} from './shares';

describe('holders Σ‰ validation', () => {
  it('accepts shares summing to exactly 1000 millis', () => {
    expect(
      checkShares([
        { customerId: 'a', millis: 500 },
        { customerId: 'b', millis: 500 },
      ]),
    ).toEqual({ sum: 1000, remaining: 0, errors: [], ok: true });
  });

  it('reports the remainder, out-of-range shares, duplicates and missing customers', () => {
    const c = checkShares([
      { customerId: 'a', millis: 333 },
      { customerId: 'b', millis: 333 },
    ]);
    expect(c).toMatchObject({ sum: 666, remaining: 334, ok: false, errors: ['sum'] });
    expect(
      checkShares([
        { customerId: 'a', millis: 0 },
        { customerId: 'a', millis: 1000 },
        { customerId: '', millis: null },
      ]).errors,
    ).toEqual(['customer', 'duplicate', 'share_range']);
    expect(checkShares([]).errors).toEqual(['none']);
  });

  it('parses integer millis only and splits equal shares exactly', () => {
    expect(parseMillis('250')).toBe(250);
    expect(parseMillis(' 1000 ')).toBe(1000);
    expect(parseMillis('1001')).toBeNull();
    expect(parseMillis('33,3')).toBeNull();
    expect(equalShares(3)).toEqual([334, 333, 333]);
    for (let n = 1; n <= 40; n++) {
      expect(equalShares(n).reduce((a, b) => a + b, 0)).toBe(1000);
    }
  });
});

describe('millesimal grid live Σ and CSV parse', () => {
  const rows = [
    { berthId: 'b1', berthCode: 'A01', text: '400' },
    { berthId: 'b2', berthCode: 'A02', text: '' },
    { berthId: 'b3', berthCode: 'A03', text: '600' },
  ];

  it('sums the grid live and is complete only at 1000 with no invalid cell', () => {
    expect(checkGrid(rows)).toEqual({ sum: 1000, remaining: 0, invalid: [], complete: true });
    const bad = rows.map((r, i) => (i === 1 ? { ...r, text: '1,5' } : r));
    expect(checkGrid(bad)).toMatchObject({ invalid: [1], complete: false });
    expect(gridEntries(rows)).toEqual([
      { berth_id: 'b1', millis: 400 },
      { berth_id: 'b3', millis: 600 },
    ]);
  });

  it('parses a semicolon CSV with a header, rejecting bad rows by line', () => {
    const csv = 'Posto;Millesimi\r\nA01;250\r\nA02;abc\r\nA03\r\na01;10\r\nA04;750\r\n';
    expect(parseMillesimalCsv(csv)).toEqual({
      entries: [
        { code: 'A01', millis: 250 },
        { code: 'A04', millis: 750 },
      ],
      rejected: [
        { row: 2, code: 'millis' },
        { row: 3, code: 'columns' },
        { row: 4, code: 'duplicate' },
      ],
    });
  });

  it('parses a headerless comma CSV and applies it to the grid by code', () => {
    const parsed = parseMillesimalCsv('a02,1000\nZ99,0\n');
    expect(parsed.rejected).toEqual([]);
    const applied = applyCsvToGrid(rows, parsed.entries);
    expect(applied.unknown).toEqual(['Z99']);
    expect(applied.rows[1]!.text).toBe('1000');
    expect(checkGrid(applied.rows).sum).toBe(2000);
  });
});
