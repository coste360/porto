import { describe, expect, it } from 'vitest';

import type { MapAllocationData, MapOccupancyData } from '@porto/shared/api';
import { checkFit } from '@porto/shared/map';

import { buildBusyIndex, overlap, previewDrop, subtractReleased } from './busy';

const alloc = (id: string, over: Partial<MapAllocationData> = {}): MapAllocationData => ({
  id,
  resource_type: 'berth',
  resource_id: 'b1',
  first_night: '2026-11-01',
  last_night: '2026-11-30',
  kind: 'contract',
  status: 'active',
  version: 1,
  released: [],
  vessel: {
    id: `v-${id}`,
    name: `Boat ${id}`,
    loa_cm: 1000,
    beam_cm: 350,
    hull_type: 'monohull_sail',
  },
  ...over,
});

const occ = (allocations: MapAllocationData[]): MapOccupancyData => ({
  from: '2026-11-01',
  to: '2026-12-31',
  allocations,
});

const fitOk = checkFit(
  { loa: 1000, beam: 350, hull: 'monohull_sail', insuranceUntil: '2027-01-01' },
  {
    kind: 'berth',
    maxLoa: 1200,
    maxBeam: 400,
    length: 1100,
    width: 380,
    multihullOk: false,
    overhangAllowed: false,
  },
  '2026-11-20',
);

describe('busy index', () => {
  it('removes released nights from an allocation', () => {
    expect(
      subtractReleased('2026-11-01', '2026-11-30', [{ from: '2026-11-10', to: '2026-11-12' }]),
    ).toEqual([
      { from: '2026-11-01', to: '2026-11-09' },
      { from: '2026-11-13', to: '2026-11-30' },
    ]);
    expect(
      subtractReleased('2026-11-01', '2026-11-05', [{ from: '2026-10-01', to: '2026-12-01' }]),
    ).toEqual([]);
    expect(
      subtractReleased('2026-11-01', '2026-11-05', [{ from: '2026-11-05', to: '2026-11-09' }]),
    ).toEqual([{ from: '2026-11-01', to: '2026-11-04' }]);
  });

  it('skips the moving allocation and inactive allocations', () => {
    const idx = buildBusyIndex(
      occ([alloc('a'), alloc('b', { status: 'superseded' }), alloc('c', { resource_id: 'b2' })]),
      new Set(['c']),
    );
    expect(idx.get('b1')?.map((e) => e.allocationId)).toEqual(['a']);
    expect(idx.get('b2')).toBeUndefined();
  });

  it('reports the overlapping nights', () => {
    const [e] = buildBusyIndex(
      occ([alloc('a', { first_night: '2026-11-10', last_night: '2026-11-15' })]),
    ).get('b1')!;
    expect(overlap(e!, '2026-11-14', '2026-11-20')).toEqual({
      from: '2026-11-14',
      to: '2026-11-15',
    });
    expect(overlap(e!, '2026-11-16', '2026-11-20')).toBeNull();
  });
});

describe('drag preview', () => {
  const idx = buildBusyIndex(
    occ([
      alloc('a', { first_night: '2026-11-10', last_night: '2026-11-15' }),
      alloc('r', { resource_id: 'b3', released: [{ from: '2026-11-01', to: '2026-11-30' }] }),
      alloc('l1', {
        resource_type: 'dynamic_area',
        resource_id: 'area',
        cells: { first: 0, last: 4 },
      }),
      alloc('s1', {
        resource_type: 'dynamic_area',
        resource_id: 'surf',
        cells: { indexes: [10, 11, 20, 21] },
      }),
    ]),
  );

  it('is ok on a free berth, warn with fit warnings, conflict on a busy one', () => {
    expect(
      previewDrop({
        index: idx,
        resourceId: 'b2',
        from: '2026-11-07',
        to: '2026-11-20',
        fit: fitOk,
      }).state,
    ).toBe('ok');
    const warn = checkFit(
      { loa: 1000, beam: 350, hull: 'monohull_sail' },
      {
        kind: 'berth',
        maxLoa: 0,
        maxBeam: 0,
        length: 1000,
        width: 350,
        multihullOk: false,
        overhangAllowed: false,
      },
      '2026-11-20',
    );
    expect(
      previewDrop({ index: idx, resourceId: 'b2', from: '2026-11-07', to: '2026-11-20', fit: warn })
        .state,
    ).toBe('warn');
    const p = previewDrop({
      index: idx,
      resourceId: 'b1',
      from: '2026-11-07',
      to: '2026-11-20',
      fit: fitOk,
    });
    expect(p.state).toBe('conflict');
    expect(p.conflicts).toEqual([
      { allocationId: 'a', vesselName: 'Boat a', from: '2026-11-10', to: '2026-11-15', cells: [] },
    ]);
  });

  it('treats released nights as free and a blocking fit as a conflict', () => {
    expect(
      previewDrop({
        index: idx,
        resourceId: 'b3',
        from: '2026-11-07',
        to: '2026-11-20',
        fit: fitOk,
      }).state,
    ).toBe('ok');
    const blocked = checkFit(
      { loa: 1500, beam: 350, hull: 'monohull_sail', insuranceUntil: '2027-01-01' },
      {
        kind: 'berth',
        maxLoa: 1200,
        maxBeam: 400,
        length: 1100,
        width: 380,
        multihullOk: false,
        overhangAllowed: false,
      },
      '2026-11-20',
    );
    const p = previewDrop({
      index: idx,
      resourceId: 'b2',
      from: '2026-11-07',
      to: '2026-11-20',
      fit: blocked,
    });
    expect(p).toMatchObject({ state: 'conflict', conflicts: [] });
  });

  it('compares cells inside dynamic areas', () => {
    const near = previewDrop({
      index: idx,
      resourceId: 'area',
      from: '2026-11-07',
      to: '2026-11-20',
      cells: [5, 6, 7],
      fit: fitOk,
    });
    expect(near.state).toBe('ok');
    const hit = previewDrop({
      index: idx,
      resourceId: 'area',
      from: '2026-11-07',
      to: '2026-11-20',
      cells: [4, 5, 6],
      fit: fitOk,
    });
    expect(hit.state).toBe('conflict');
    expect([...hit.conflictCells]).toEqual([4]);
    const surf = previewDrop({
      index: idx,
      resourceId: 'surf',
      from: '2026-11-07',
      to: '2026-11-07',
      cells: [21, 22],
      fit: fitOk,
    });
    expect(surf.conflicts[0]?.cells).toEqual([21]);
  });

  it('is invalid when the placement is refused', () => {
    expect(
      previewDrop({
        index: idx,
        resourceId: 'area',
        from: '2026-11-07',
        to: '2026-11-07',
        fit: null,
        placementError: 'outside_area',
      }).state,
    ).toBe('invalid');
  });
});
