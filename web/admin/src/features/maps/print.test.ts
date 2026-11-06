import { describe, expect, it } from 'vitest';

import { printLayout, printScaleBar } from './print';

describe('printLayout', () => {
  it('fits an 800 × 500 m map on A3 landscape', () => {
    const l = printLayout(80_000, 50_000, 'A3', 'landscape', 'fit');
    expect(l.denominator).toBeGreaterThan(1000);
    expect(l.mapWmm).toBeLessThanOrEqual(420 - 24);
    expect(l.cropped).toBe(false);
  });

  it('crops at a fixed scale that does not fit', () => {
    const l = printLayout(80_000, 50_000, 'A4', 'portrait', 500);
    expect(l.denominator).toBe(500);
    expect(l.cropped).toBe(true);
    expect(l.viewWcm).toBeCloseTo((l.mapWmm * 500) / 10);
  });

  it('picks a nice scale bar', () => {
    expect(printScaleBar(1000)).toEqual({ mm: 50, cm: 5000 });
    expect(printScaleBar(2000).cm).toBe(10_000);
  });
});
