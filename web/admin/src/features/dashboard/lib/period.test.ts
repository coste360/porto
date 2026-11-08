import { describe, expect, it } from 'vitest';

import { isComparableTile, isPreset, milestoneOf, presetPeriod, validPeriod } from './period';

describe('dashboard periods', () => {
  it('builds every preset ending today', () => {
    expect(presetPeriod('mtd', '2026-11-08')).toEqual({ from: '2026-11-01', to: '2026-11-08' });
    expect(presetPeriod('ytd', '2026-11-08')).toEqual({ from: '2026-01-01', to: '2026-11-08' });
    expect(presetPeriod('last_30', '2026-03-01')).toEqual({ from: '2026-01-31', to: '2026-03-01' });
    expect(presetPeriod('last_12m', '2026-11-08')).toEqual({
      from: '2025-11-09',
      to: '2026-11-08',
    });
    // Leap day: one year back clamps to Feb 28, the period starts the day after.
    expect(presetPeriod('last_12m', '2028-02-29')).toEqual({
      from: '2027-03-01',
      to: '2028-02-29',
    });
  });

  it('validates custom periods against the API limit', () => {
    expect(validPeriod({ from: '2026-01-01', to: '2026-01-01' })).toBe(true);
    expect(validPeriod({ from: '2026-01-02', to: '2026-01-01' })).toBe(false);
    expect(validPeriod({ from: '2025-01-01', to: '2026-02-04' })).toBe(true); // 400 nights
    expect(validPeriod({ from: '2025-01-01', to: '2026-02-05' })).toBe(false);
    expect(validPeriod({ from: '2026-01-01' })).toBe(false);
  });

  it('recognises presets, milestones and comparable tiles', () => {
    expect(isPreset('ytd')).toBe(true);
    expect(isPreset('week')).toBe(false);
    expect(milestoneOf('milestone_m10')).toBe('M10');
    expect(milestoneOf(null)).toBe('');
    expect(isComparableTile('occupancy')).toBe(true);
    expect(isComparableTile('vessels_hosted')).toBe(false);
    expect(isComparableTile('receivables_open')).toBe(false);
  });
});
