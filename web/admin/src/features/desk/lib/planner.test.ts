import { describe, expect, it } from 'vitest';

import { COL_PX, LABEL_PX, MAX_COL_PX, columnAt, dragColumns, fitColumnPx } from './planner';

describe('fitColumnPx', () => {
  it('stretches 14 and 31 nights to the available width', () => {
    expect(fitColumnPx(LABEL_PX + 14 * 70, 14)).toBe(70);
    expect(fitColumnPx(LABEL_PX + 31 * 40 + 20, 31)).toBe(40);
  });

  it('never goes below COL_PX (62 nights scroll) nor above MAX_COL_PX', () => {
    expect(fitColumnPx(800, 62)).toBe(COL_PX);
    expect(fitColumnPx(5000, 14)).toBe(MAX_COL_PX);
  });

  it('keeps COL_PX before the width is known', () => {
    expect(fitColumnPx(0, 14)).toBe(COL_PX);
  });

  it('maps pointer positions with the fitted width', () => {
    expect(columnAt(LABEL_PX + 150, 14, 70)).toBe(2);
    expect(dragColumns(-100, 70)).toBe(-1);
  });
});
