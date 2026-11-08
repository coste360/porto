import { describe, expect, it } from 'vitest';

import type { ComparisonResult } from '@porto/shared/api';

import {
  allowedIndicators,
  bucketLabel,
  comparisonFileName,
  comparisonRows,
  parseComparisonSearch,
} from './comparison';

const result: ComparisonResult = {
  indicator: 'production',
  unit: 'cents',
  align: 'weekday',
  granularity: 'month',
  years_back: 1,
  filters: {},
  current: { from: '2026-01-01', to: '2026-03-31', value: 600 },
  previous: { from: '2025-01-02', to: '2025-04-01', value: 250 },
  delta: 350,
  delta_bp: 14000,
  series: [
    { bucket: '2026-01-01', current: 100, previous: 50 },
    { bucket: '2026-02-01', current: 200, previous: null },
    { bucket: '2026-03-01', current: 300, previous: 200 },
  ],
};

describe('comparison helpers', () => {
  it('maps the series to rows keeping the server alignment', () => {
    expect(comparisonRows(result)).toEqual([
      { bucket: '2026-01-01', current: 100, previous: 50, delta: 50 },
      { bucket: '2026-02-01', current: 200, previous: null, delta: null },
      { bucket: '2026-03-01', current: 300, previous: 200, delta: 100 },
    ]);
    const rows = comparisonRows(result);
    expect(rows.reduce((s, r) => s + (r.current ?? 0), 0)).toBe(result.current.value);
    expect(rows.reduce((s, r) => s + (r.previous ?? 0), 0)).toBe(result.previous.value);
  });

  it('parses the page search, dropping unknown values', () => {
    expect(
      parseComparisonSearch({
        indicator: 'occupancy',
        from: '2026-01-01',
        to: 'x',
        years_back: '3',
        align: 'weekday',
        granularity: 'week',
      }),
    ).toEqual({
      indicator: 'occupancy',
      from: '2026-01-01',
      years_back: 3,
      align: 'weekday',
      granularity: 'week',
    });
    expect(
      parseComparisonSearch({ indicator: 'receivables_open', years_back: '9', align: 'x' }),
    ).toEqual({});
  });

  it('restricts indicators by permission', () => {
    expect(allowedIndicators(true, true)).toHaveLength(13);
    expect(allowedIndicators(false, true)).toEqual([
      'occupancy',
      'efficiency',
      'arrivals',
      'departures',
      'lost_demand_count',
    ]);
    expect(allowedIndicators(false, false)).toEqual([]);
  });

  it('labels buckets and files', () => {
    expect(bucketLabel('2026-03-02', 'week', 'it')).toBe('02/03/2026');
    expect(bucketLabel('2026-11-01', 'month', 'it')).toMatch(/nov.*2026/);
    expect(comparisonFileName('2026-01-01', '2026-03-31', 'csv')).toBe(
      'porto-comparison-2026-01-01-2026-03-31.csv',
    );
  });
});
