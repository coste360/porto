import { describe, expect, it } from 'vitest';

import type { RentalFormula, RentalPlanner } from '@porto/shared/api';

import { gridOf, preselect, rangeText, suggestFormula, suggestKind, viewWindow } from './plan';

const bands = [
  { code: 'morning', from_slot: 16, to_slot: 26 },
  { code: 'afternoon', from_slot: 28, to_slot: 38 },
];
const r = (d0: string, s0: number, d1: string, s1: number) => ({
  start: { date: d0, slot: s0 },
  end: { date: d1, slot: s1 },
});

function formula(
  code: string,
  kind: RentalFormula['kind'],
  extra: Partial<RentalFormula> = {},
): RentalFormula {
  return { id: code, code, kind, active: true, categories: [], ...extra } as RentalFormula;
}

describe('rental planner lib', () => {
  it('suggests the formula kind from the range', () => {
    expect(suggestKind(r('2027-06-12', 16, '2027-06-12', 26), 16, 38, bands)).toEqual({
      kind: 'half_day',
      band: 'morning',
    });
    expect(suggestKind(r('2027-06-12', 28, '2027-06-12', 38), 16, 38, bands)).toEqual({
      kind: 'half_day',
      band: 'afternoon',
    });
    expect(suggestKind(r('2027-06-12', 16, '2027-06-12', 38), 16, 38, bands)).toEqual({
      kind: 'day',
    });
    expect(suggestKind(r('2027-06-12', 20, '2027-06-12', 26), 16, 38, bands)).toEqual({
      kind: 'hourly',
    });
    expect(suggestKind(r('2027-06-12', 16, '2027-06-14', 38), 16, 38, bands)).toEqual({
      kind: 'multi_day',
    });
    expect(suggestKind(r('2027-06-05', 16, '2027-06-13', 38), 16, 38, bands)).toEqual({
      kind: 'week',
    });
  });

  it('picks a formula of the kind, band and category', () => {
    const fs = [
      formula('ORARIA', 'hourly', { categories: ['rib'] }),
      formula('MATTINA', 'half_day', { band_code: 'morning' }),
      formula('POMERIGGIO', 'half_day', { band_code: 'afternoon' }),
      formula('PIU', 'multi_day'),
      formula('OLD', 'day', { active: false }),
    ];
    expect(suggestFormula(fs, 'rib', { kind: 'hourly' })?.code).toBe('ORARIA');
    expect(suggestFormula(fs, 'sail', { kind: 'hourly' })).toBeUndefined();
    expect(suggestFormula(fs, 'rib', { kind: 'half_day', band: 'afternoon' })?.code).toBe(
      'POMERIGGIO',
    );
    expect(suggestFormula(fs, 'rib', { kind: 'week' })?.code).toBe('PIU');
    expect(suggestFormula(fs, 'rib', { kind: 'day' })).toBeUndefined();
  });

  it('builds the windows, texts and grid rows', () => {
    expect(viewWindow('hours', '2027-06-12')).toEqual({ from: '2027-06-12', to: '2027-06-12' });
    expect(viewWindow('half_days', '2027-06-12').to).toBe('2027-06-18');
    expect(viewWindow('days', '2027-06-12').to).toBe('2027-06-25');
    expect(rangeText(r('2027-06-12', 20, '2027-06-12', 26))).toBe('10:00-13:00');
    const p = {
      rows: [
        {
          id: 'u2',
          kind: 'unit',
          code: 'VELA',
          category: 'sail',
          status: 'available',
          segments: [],
        },
        {
          id: 'u1',
          kind: 'unit',
          code: 'GOM',
          category: 'rib',
          status: 'maintenance',
          segments: [
            {
              booking_id: 'b1',
              kind: 'booked',
              status: 'confirmed',
              number: 'NL2027-000001',
              from: { date: '2027-06-12', slot: 20 },
              to: { date: '2027-06-12', slot: 26 },
            },
            {
              booking_id: 'b1',
              kind: 'buffer',
              status: 'confirmed',
              from: { date: '2027-06-12', slot: 26 },
              to: { date: '2027-06-12', slot: 27 },
            },
          ],
        },
        { id: 's1', kind: 'skipper', code: 'Marco', segments: [] },
      ],
    } as unknown as RentalPlanner;
    const g = gridOf(p, {
      status: (s) => `st:${s}`,
      kind: (k) => `k:${k}`,
      reason: (x) => `r:${x}`,
    });
    expect(g.rows.map((x) => x.id)).toEqual(['u1', 'u2', 's1']);
    expect(g.rows[0]).toMatchObject({ readOnly: true, sublabel: 'st:maintenance' });
    expect(g.rows[2]?.readOnly).toBe(true);
    expect(g.segments[0]).toMatchObject({
      bookingId: 'b1',
      label: 'NL2027-000001',
      ariaLabel: 'GOM: NL2027-000001 st:confirmed, 10:00-13:00',
    });
    expect(g.segments[1]?.bookingId).toBeUndefined();
  });

  it('preselects the suggested formula when it prices, else the cheapest', () => {
    const o = (id: string, ok: boolean, total: number) =>
      ({ formula_id: id, ok, total_cents: total }) as never;
    const opts = [o('a', true, 9000), o('b', true, 6000), o('c', false, 1000)];
    expect(preselect(opts, 'a')).toBe('a');
    expect(preselect(opts, 'c')).toBe('b');
    expect(preselect(opts)).toBe('b');
    expect(preselect([o('c', false, 1)])).toBeUndefined();
  });
});
