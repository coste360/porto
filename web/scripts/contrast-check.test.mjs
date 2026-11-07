import { describe, expect, it } from 'vitest';

import { block, check, ratio } from './contrast-check.mjs';

describe('contrast-check', () => {
  it('computes WCAG ratios', () => {
    expect(ratio([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 0);
    expect(ratio([255, 255, 255], [255, 255, 255])).toBeCloseTo(1, 5);
  });

  it('reports low-contrast harbor tokens', () => {
    const css =
      ':root {\n  --background: #ffffff;\n  --chart-2: #0369a1;\n  --harbor-free: #f8fafc;\n  --harbor-annual: #0b2542;\n}';
    const errors = check('light', block(css, ':root'));
    expect(errors).toHaveLength(1);
    expect(errors[0]).toContain('--harbor-free');
  });
});
