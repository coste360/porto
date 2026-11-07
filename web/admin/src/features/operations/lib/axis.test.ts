import { describe, expect, it } from 'vitest';

import { axisWidth, freeSpace, snap10, snapAxis } from './axis';

describe('linear axis', () => {
  it('snaps to 10 cm half-even and clamps to the area', () => {
    expect(snap10(1234)).toBe(1230);
    expect(snap10(1235)).toBe(1240);
    expect(snap10(1225)).toBe(1220);
    const s = snapAxis({ rawX: -300, w: 500, length: 3000, margin: 100, neighbours: [] });
    expect(s.x).toBe(50);
    expect(snapAxis({ rawX: 9999, w: 500, length: 3005, margin: 100, neighbours: [] }).x).toBe(
      2500,
    );
  });

  it('snaps to the edge of a neighbour within the snap distance', () => {
    const neighbours = [{ x: 1000, w: 450 }];
    const right = snapAxis({ rawX: 1480, w: 450, length: 5000, margin: 100, neighbours });
    expect(right).toMatchObject({ x: 1450, snapped: 'neighbour', freeLeft: 0 });
    const left = snapAxis({ rawX: 520, w: 450, length: 5000, margin: 100, neighbours });
    expect(left).toMatchObject({ x: 550, snapped: 'neighbour', freeRight: 0 });
    const far = snapAxis({ rawX: 2200, w: 450, length: 5000, margin: 100, neighbours });
    expect(far).toMatchObject({ x: 2200, snapped: 'grid', freeLeft: 750, freeRight: 2350 });
  });

  it('measures the footprint like layout.Footprint', () => {
    expect(axisWidth({ loa: 1200, beam: 400 }, 'stern_to', 100)).toBe(500);
    expect(axisWidth({ loa: 1200, beam: 400 }, 'alongside', 100)).toBe(1300);
    expect(
      freeSpace(1000, 500, 4000, [
        { x: 0, w: 600 },
        { x: 2000, w: 500 },
      ]),
    ).toEqual({ freeLeft: 400, freeRight: 500 });
  });
});
