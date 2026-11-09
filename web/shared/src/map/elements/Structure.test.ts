import { describe, expect, it } from 'vitest';

import { structureLabelFit } from './Structure';

const rect = (w: number, h: number) => [
  { x: 0, y: 0 },
  { x: w, y: 0 },
  { x: w, y: h },
  { x: 0, y: h },
];

describe('structure label fit', () => {
  it('shrinks a code that would leave its closed outline', () => {
    const fit = structureLabelFit(
      { closed: true, code: 'IMBOCCATURA', kind: 'channel' },
      rect(10_000, 7_000),
      1_500,
    );
    expect(fit.rotate).toBe(false);
    expect(fit.font).toBeLessThan(1_500);
    expect(fit.font * 11 * 0.68).toBeLessThanOrEqual(10_000 * 0.9 + 1e-6);
    expect(fit.pill).toBeUndefined();
  });

  it('turns the code along a tall narrow outline', () => {
    const fit = structureLabelFit(
      { closed: true, code: 'DIGA', kind: 'breakwater' },
      rect(1_500, 27_000),
      1_200,
    );
    expect(fit.rotate).toBe(true);
    expect(fit.font).toBeLessThanOrEqual(1_500 * 0.8);
  });

  it('puts the name of a line on a background pill wider than the text', () => {
    const fit = structureLabelFit(
      { closed: false, code: 'BANCHINA', kind: 'quay' },
      [
        { x: 0, y: 0 },
        { x: 60_000, y: 0 },
      ],
      500,
    );
    expect(fit.font).toBe(500);
    expect(fit.pill!.w).toBeGreaterThan(8 * 0.68 * 500);
    expect(fit.pill!.h).toBeGreaterThan(500);
  });
});
