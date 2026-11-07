import { describe, expect, it } from 'vitest';

import { coverage } from '../test/fixtures';
import { NO_FILTERS, filterGaps, filterOptions, gapRow } from './coverageFilter';

describe('coverage filters', () => {
  it('offers the distinct values of the gaps', () => {
    expect(filterOptions(coverage.gaps)).toEqual({
      contractTypes: ['annual', 'transit'],
      periods: ['ALTA', 'BASSA'],
      categories: ['L10', 'L12', 'L14'],
    });
  });

  it('filters by contract type, period and category or band', () => {
    expect(filterGaps(coverage.gaps, NO_FILTERS)).toHaveLength(3);
    expect(filterGaps(coverage.gaps, { ...NO_FILTERS, contractType: 'transit' })).toHaveLength(2);
    expect(
      filterGaps(coverage.gaps, { contractType: 'transit', period: 'BASSA', category: '' }).map(
        gapRow,
      ),
    ).toEqual(['L10']);
    expect(filterGaps(coverage.gaps, { ...NO_FILTERS, category: 'L12' })).toEqual([
      coverage.gaps[2],
    ]);
    expect(filterGaps(coverage.gaps, { ...NO_FILTERS, period: 'ESTATE' })).toEqual([]);
  });
});
