import { describe, expect, it } from 'vitest';

import type { PriceListSummary } from '@porto/shared/api';

import { monthCells, rowsByContractType, timelineYears } from './timeline';

const list = (over: Partial<PriceListSummary>): PriceListSummary => ({
  id: 'a',
  code: 'A',
  name: { it: 'A' },
  status: 'active',
  valid_from: '2027-01-01',
  valid_to: '2027-12-31',
  contract_types: ['transit'],
  rule_count: 1,
  version: 1,
  ...over,
});

describe('validity timeline', () => {
  it('spans the non-archived lists, at most five years', () => {
    expect(
      timelineYears([
        list({}),
        list({ valid_from: '2028-01-01', valid_to: '2028-06-30', status: 'draft' }),
      ]),
    ).toEqual({ start: 2027, years: 2 });
    expect(timelineYears([list({ status: 'archived' })])).toBeNull();
    expect(timelineYears([list({ valid_to: '2040-01-01' })])?.years).toBe(5);
  });

  it('marks the months touched by a list', () => {
    const cells = monthCells({ valid_from: '2027-03-15', valid_to: '2027-05-01' }, 2027, 1);
    expect(cells.filter((c) => c.covered).map((c) => c.month)).toEqual([
      '2027-03',
      '2027-04',
      '2027-05',
    ]);
  });

  it('groups lists by contract type, archived excluded', () => {
    const rows = rowsByContractType(
      [
        list({ id: 'b', valid_from: '2028-01-01', contract_types: ['transit', 'annual'] }),
        list({ id: 'a' }),
        list({ id: 'z', status: 'archived' }),
      ],
      ['annual', 'seasonal', 'transit'],
    );
    expect(rows.map((r) => [r.contractType, r.lists.map((l) => l.id)])).toEqual([
      ['annual', ['b']],
      ['transit', ['a', 'b']],
    ]);
  });
});
