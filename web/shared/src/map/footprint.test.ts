import { describe, expect, it } from 'vitest';

import {
  FootprintError,
  cellCount,
  footprint,
  surfaceAreaOf,
  type FootprintArea,
} from './footprint';

const linear = (over: Partial<FootprintArea> = {}): FootprintArea => ({
  kind: 'linear',
  length: 1000,
  mode: 'stern_to',
  cell: 50,
  margin: 50,
  cols: 0,
  rows: 0,
  usable: [],
  ...over,
});

function code(fn: () => unknown): string | null {
  try {
    fn();
    return null;
  } catch (err) {
    return err instanceof FootprintError ? err.code : 'other';
  }
}

describe('footprint (linear)', () => {
  it.each([
    // [name, vessel, x, mode, expected first..last]
    ['stern_to at the margin start', { loa: 1000, beam: 350 }, 30, 'stern_to', [0, 8]],
    ['cell boundary hit exactly', { loa: 1000, beam: 350 }, 100, 'stern_to', [2, 9]],
    ['alongside uses the length', { loa: 1000, beam: 350 }, 30, 'alongside', [0, 21]],
  ] as const)('%s', (_name, vessel, x, mode, [first, last]) => {
    const area = linear({ mode, length: 2000 });
    const cells = footprint(vessel, { x, y: 0, rotation: 0 }, area);
    expect([cells.first, cells.last]).toEqual([first, last]);
    expect(cells.indexes).toHaveLength(last - first + 1);
  });

  it('rejects unsnapped, outside and invalid vessels', () => {
    const area = linear();
    expect(
      code(() => footprint({ loa: 1000, beam: 350 }, { x: 35, y: 0, rotation: 0 }, area)),
    ).toBe('unsnapped');
    expect(
      code(() => footprint({ loa: 1000, beam: 350 }, { x: 20, y: 0, rotation: 0 }, area)),
    ).toBe('outside_area');
    expect(
      code(() => footprint({ loa: 1000, beam: 350 }, { x: 700, y: 0, rotation: 0 }, area)),
    ).toBe('outside_area');
    expect(code(() => footprint({ loa: 1000, beam: 0 }, { x: 100, y: 0, rotation: 0 }, area))).toBe(
      'invalid_vessel',
    );
  });

  it('fits exactly at the end', () => {
    const cells = footprint({ loa: 800, beam: 350 }, { x: 600, y: 0, rotation: 0 }, linear());
    expect(cells.last).toBe(19);
  });
});

describe('surfaceAreaOf and footprint (surface)', () => {
  const square = [
    { x: 0, y: 0 },
    { x: 1000, y: 0 },
    { x: 1000, y: 500 },
    { x: 0, y: 500 },
  ];

  it('builds the grid and marks blocked cells', () => {
    const area = surfaceAreaOf({
      outline: square,
      grid_rotation_dd: 0,
      cell_cm: 100,
      margin_cm: 0,
      blocked_cells: [3],
    });
    expect([area.cols, area.rows]).toEqual([10, 5]);
    expect(cellCount(area)).toBe(50);
    expect(area.usable.filter(Boolean)).toHaveLength(49);
    expect(area.usable[3]).toBe(false);
  });

  it('rotates the outline by a multiple of 90°', () => {
    const area = surfaceAreaOf({
      outline: square,
      grid_rotation_dd: 900,
      cell_cm: 100,
      margin_cm: 0,
      blocked_cells: [],
    });
    expect([area.cols, area.rows]).toEqual([5, 10]);
  });

  it('marks cells crossing a concave outline as not usable', () => {
    const l = [
      { x: 0, y: 0 },
      { x: 200, y: 0 },
      { x: 200, y: 100 },
      { x: 100, y: 100 },
      { x: 100, y: 200 },
      { x: 0, y: 200 },
    ];
    const area = surfaceAreaOf({
      outline: l,
      grid_rotation_dd: 0,
      cell_cm: 100,
      margin_cm: 0,
      blocked_cells: [],
    });
    expect(area.usable).toEqual([true, true, true, false]);
  });

  it('places a vessel and reports blocked or outside cells', () => {
    const area = surfaceAreaOf({
      outline: square,
      grid_rotation_dd: 0,
      cell_cm: 100,
      margin_cm: 0,
      blocked_cells: [12],
    });
    const cells = footprint({ loa: 300, beam: 200 }, { x: 0, y: 200, rotation: 0 }, area);
    expect(cells.indexes).toEqual([20, 21, 22, 30, 31, 32]);
    expect(code(() => footprint({ loa: 300, beam: 200 }, { x: 0, y: 0, rotation: 0 }, area))).toBe(
      'blocked_cell',
    );
    expect(
      code(() => footprint({ loa: 300, beam: 200 }, { x: 900, y: 0, rotation: 0 }, area)),
    ).toBe('outside_area');
    const rotated = footprint({ loa: 300, beam: 100 }, { x: 500, y: 0, rotation: 900 }, area);
    expect(rotated.indexes).toEqual([5, 15, 25]);
  });
});
