import { describe, expect, it } from 'vitest';

import {
  allocationOfRef,
  berthVesselPose,
  daySubjects,
  indexDayView,
  linearVesselPose,
  placementRef,
  schemaMinTileCm,
  schemaTiles,
} from './dayView';
import { makeDayView } from './lenses/testFixtures';
import { berth } from './testFixtures';

describe('day view index', () => {
  it('indexes elements by resource and placements by area sorted by x', () => {
    const idx = indexDayView(makeDayView());
    expect(idx.byResource.get('a2')?.vessel?.name).toBe('Aurora');
    expect(idx.placementsByArea.get('area-1')?.map((p) => p.allocation_id)).toEqual([
      'al-8',
      'al-9',
    ]);
    expect(idx.version).toContain('2026-11-07');
  });

  it('subjects skip area elements and add one per placed vessel', () => {
    const refs = daySubjects(makeDayView()).map((s) => s.ref);
    expect(refs).not.toContain('area-1');
    expect(refs).toContain(placementRef('al-9'));
    expect(allocationOfRef(placementRef('al-9'))).toBe('al-9');
    expect(allocationOfRef('a1')).toBeNull();
  });
});

describe('vessel poses', () => {
  it('puts the bow on the structure edge, heading back to it', () => {
    const pose = berthVesselPose(
      { anchor: { x: 100, y: 500 }, width_cm: 400, length_cm: 1200, rotation_dd: 0 },
      1000,
    );
    expect(pose.origin.x).toBeCloseTo(1100);
    expect(pose.origin.y).toBeCloseTo(500);
    expect(pose.rotationDd).toBe(1800);
  });

  it('places a stern-to vessel across a linear area band', () => {
    const quay = {
      id: 'q',
      kind: 'quay' as const,
      code: 'Q',
      points: [
        { x: 0, y: 0 },
        { x: 10_000, y: 0 },
      ],
      sides: ['right' as const],
    };
    const pose = linearVesselPose(
      { from_cm: 1000, to_cm: 5000, side: 'right', mooring_mode: 'stern_to', margin_cm: 100 },
      quay,
      { loa: 1200, beam: 400 },
      200,
    );
    // Footprint [200, 700) from 1000: centre at 1450; right of travel is +y; stern 1200 cm out.
    expect(pose!.origin.x).toBeCloseTo(1450);
    expect(pose!.origin.y).toBeCloseTo(1200);
    expect(pose!.rotationDd).toBe(2700);
  });
});

describe('schema tiles', () => {
  it('keeps the order along the structure and widens narrow berths', () => {
    const bs = [
      berth({
        id: 'b2',
        key: undefined,
        structure_id: 's',
        position: 20,
        anchor: { x: 0, y: 300 },
        width_cm: 300,
      }),
      berth({
        id: 'b1',
        key: undefined,
        structure_id: 's',
        position: 10,
        anchor: { x: 0, y: 0 },
        width_cm: 300,
      }),
      berth({
        id: 'b3',
        key: undefined,
        structure_id: 's',
        position: 30,
        anchor: { x: 0, y: 600 },
        width_cm: 300,
      }),
    ];
    const min = schemaMinTileCm(0.02);
    expect(min).toBe(Math.ceil(11 / (0.42 * 0.02)));
    const tiles = schemaTiles(bs, min);
    const ys = ['b1', 'b2', 'b3'].map((id) => tiles.get(id)!.anchor.y);
    expect(ys[0]).toBeLessThan(ys[1]!);
    expect(ys[1]).toBeLessThan(ys[2]!);
    expect(ys[1]! - ys[0]!).toBeCloseTo(min);
    for (const id of ['b1', 'b2', 'b3']) expect(tiles.get(id)!.width_cm).toBe(min);
    // Already wide enough at a large zoom: geometry unchanged.
    const scale = schemaTiles(bs, 10);
    expect(scale.get('b2')!.anchor.y).toBeCloseTo(300);
  });
});
