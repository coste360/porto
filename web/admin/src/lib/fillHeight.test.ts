import { describe, expect, it } from 'vitest';

import { FILL_MAX_UNITS, FILL_MIN_UNITS, fillUnits, heightClass } from './fillHeight';

describe('fillUnits', () => {
  it('fills the viewport below the top, rounded down to the step', () => {
    // 1200 - 300 - 24 = 876 px = 219 units, rounded down to 216.
    expect(fillUnits(1200, 300, 24, 160)).toBe(216);
  });

  it('keeps the minimum on short windows and the maximum on very tall ones', () => {
    expect(fillUnits(900, 300, 24, 160)).toBe(160);
    expect(fillUnits(800, 600, 160)).toBe(FILL_MIN_UNITS);
    expect(fillUnits(10_000, 0, 0)).toBe(FILL_MAX_UNITS);
  });

  it('is null before the element is measured', () => {
    expect(fillUnits(0, 0, 24)).toBeNull();
  });

  it('builds the class with its variant', () => {
    expect(heightClass(216, 'lg:')).toBe('lg:h-216');
    expect(heightClass(64)).toBe('h-64');
  });
});
