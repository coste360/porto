import { describe, expect, it } from 'vitest';

import { initialState, renewalBody, shiftIsoDate, stepErrors, suggestCode } from './renewal';

describe('renewal', () => {
  it('shifts dates by years and moves 29 February to 28 February', () => {
    expect(shiftIsoDate('2027-06-01', 1)).toBe('2028-06-01');
    expect(shiftIsoDate('2028-02-29', 1)).toBe('2029-02-28');
    expect(shiftIsoDate('2028-02-29', 4)).toBe('2032-02-29');
    expect(shiftIsoDate('2027-01-01', 0)).toBe('2027-01-01');
  });

  it('suggests the next code', () => {
    expect(suggestCode('T-2027', 1)).toBe('T-2028');
    expect(suggestCode('LISTINO', 1)).toBe('LISTINO-R');
    expect(suggestCode('T-2027', 0)).toBe('T-2027-R');
  });

  it('validates each step', () => {
    const s = initialState({ code: 'T-2027', name: { it: 'Transiti' } });
    expect(stepErrors(s, 1)).toEqual({});
    expect(stepErrors({ ...s, shift_years: 6 }, 1)).toEqual({
      shift_years: 'pricing:renew.validation.shift',
    });
    expect(stepErrors({ ...s, code: '' }, 1)).toHaveProperty('code');
    const row = {
      scope: 'item' as const,
      item_id: '',
      category: '',
      kind: 'percent' as const,
      percent_bp: 500,
      amount_cents: null,
    };
    expect(stepErrors({ ...s, adjustments: [row] }, 2)).toEqual({
      'adjustments.0.item_id': 'common:validation.required',
    });
    expect(stepErrors({ ...s, istat_bp: 10001 }, 2)).toHaveProperty('istat_bp');
  });

  it('builds the request with integers only', () => {
    const s = {
      ...initialState({ code: 'T-2027', name: { it: 'Transiti', en: 'Transits' } }),
      istat_bp: 180,
      amount_rounding: 'ten_cents' as const,
      adjustments: [
        {
          scope: 'all' as const,
          item_id: 'x',
          category: 'y',
          kind: 'percent' as const,
          percent_bp: 500,
          amount_cents: null,
        },
        {
          scope: 'category' as const,
          item_id: '',
          category: 'L12',
          kind: 'amount' as const,
          percent_bp: null,
          amount_cents: 2500,
        },
      ],
    };
    expect(renewalBody(s)).toEqual({
      code: 'T-2028',
      name: { it: 'Transiti', en: 'Transits' },
      shift_years: 1,
      istat_bp: 180,
      adjustments: [
        { scope: 'all', percent_bp: 500 },
        { scope: 'category', category: 'L12', amount_cents: 2500 },
      ],
      amount_rounding: 'ten_cents',
    });
  });
});
