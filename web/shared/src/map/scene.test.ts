import { describe, expect, it } from 'vitest';

import type { DynamicArea, Structure } from '../api/schema';
import { buildScene, linearAreaPolygon, structureEdges } from './scene';
import { berth } from './testFixtures';

const pier: Structure = {
  key: 'p',
  kind: 'pier',
  code: 'A',
  points: [
    { x: 0, y: 0 },
    { x: 0, y: 10_000 },
  ],
  width_cm: 300,
  sides: ['left', 'right'],
};

describe('scene', () => {
  it('builds pier edges at ±width/2 (left = counter-clockwise on screen)', () => {
    const edges = structureEdges(pier);
    // Travelling +y with y down, left is +x.
    expect(edges.find((e) => e.side === 'left')?.points[0]?.x).toBeCloseTo(150);
    expect(edges.find((e) => e.side === 'right')?.points[0]?.x).toBeCloseTo(-150);
  });

  it('computes bounds for berths, buoys and band structures', () => {
    const scene = buildScene([
      { kind: 'structure', el: pier },
      { kind: 'berth', el: berth() },
      {
        kind: 'berth',
        el: berth({
          key: 'buoy',
          mooring_type: 'buoy',
          swing_radius_cm: 1000,
          anchor: { x: 5000, y: 5000 },
        }),
      },
    ]);
    expect(scene[0]?.box).toEqual({ minX: -150, minY: -150, maxX: 150, maxY: 10_150 });
    expect(scene[1]?.box.maxX).toBeCloseTo(1350);
    expect(scene[2]?.box).toEqual({ minX: 4000, minY: 4000, maxX: 6000, maxY: 6000 });
  });

  it('draws a linear area band on the structure side', () => {
    const area: DynamicArea = {
      key: 'a',
      kind: 'linear',
      code: 'T',
      structure_id: 'p',
      side: 'left',
      from_cm: 1000,
      to_cm: 3000,
      mooring_mode: 'stern_to',
      max_beam_cm: 500,
      blocked_cells: [],
      exits: [],
      grid_rotation_dd: 0,
      active: true,
      group_ids: [],
    };
    const poly = linearAreaPolygon(area, pier);
    const xs = poly.map((p) => Math.round(p.x));
    expect(Math.min(...xs)).toBe(150);
    expect(Math.max(...xs)).toBe(650);
  });
});
