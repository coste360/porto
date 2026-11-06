import { describe, expect, it } from 'vitest';

import { fitToView, mapToScreen, panBy, pxPerCm, resize, screenToMap, zoomAt } from './viewport';

describe('viewport', () => {
  const content = { minX: 0, minY: 0, maxX: 80_000, maxY: 50_000 };

  it('fits the content with the screen aspect ratio', () => {
    const vp = fitToView(content, 800, 400, 0);
    expect(vp.w / vp.h).toBeCloseTo(2);
    expect(vp.h).toBeCloseTo(50_000);
    expect(pxPerCm(vp)).toBeCloseTo(0.008);
  });

  it('zooms keeping the point under the cursor', () => {
    const vp = fitToView(content, 800, 400, 0);
    const at = { x: 200, y: 100 };
    const before = screenToMap(vp, at);
    const z = zoomAt(vp, 2, at);
    const after = screenToMap(z, at);
    expect(after.x).toBeCloseTo(before.x);
    expect(after.y).toBeCloseTo(before.y);
    expect(pxPerCm(z)).toBeCloseTo(pxPerCm(vp) * 2);
  });

  it('pans and converts both ways', () => {
    const vp = panBy(fitToView(content, 800, 400, 0), 80, 0);
    const p = { x: 1234, y: 5678 };
    const back = screenToMap(vp, mapToScreen(vp, p));
    expect(back.x).toBeCloseTo(p.x);
    expect(back.y).toBeCloseTo(p.y);
    const r = resize(vp, 400, 400);
    expect(pxPerCm(r)).toBeCloseTo(pxPerCm(vp));
  });
});
