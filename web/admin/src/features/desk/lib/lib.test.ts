import { describe, expect, it } from 'vitest';

import type { Schemas } from '@porto/shared/api';

import { defaultEndsOn, endsOnRequired } from './contract';
import { addDays, addMonths, daysBetween, isIsoDate, lastNightOf, nightsOf } from './dates';
import {
  barColumns,
  barFillClass,
  barRect,
  columnAt,
  COL_PX,
  dragColumns,
  isFree,
  LABEL_PX,
  layoutBars,
  moveForDrop,
  plannerRows,
  selectionStay,
  stayChangeForDrag,
  type MapAllocation,
} from './planner';
import { emptySearchForm, solutionCodes, solutionPlacements, toSearchRequest } from './search';
import { contractActions, statusKey, transitActions } from './status';

describe('dates', () => {
  it('does day arithmetic across months, leap years and DST without shifting', () => {
    expect(addDays('2026-03-28', 2)).toBe('2026-03-30');
    expect(addDays('2028-02-28', 1)).toBe('2028-02-29');
    expect(addDays('2026-01-01', -1)).toBe('2025-12-31');
    expect(daysBetween('2026-10-24', '2026-10-27')).toBe(3);
    expect(lastNightOf('2026-07-04')).toBe('2026-07-03');
    expect(nightsOf('2026-07-30', '2026-08-02')).toEqual([
      '2026-07-30',
      '2026-07-31',
      '2026-08-01',
      '2026-08-02',
    ]);
  });

  it('clamps month additions to the month end', () => {
    expect(addMonths('2026-01-31', 1)).toBe('2026-02-28');
    expect(addMonths('2028-02-29', 12)).toBe('2029-02-28');
    expect(addMonths('2026-11-15', 2)).toBe('2027-01-15');
  });

  it('validates calendar dates', () => {
    expect(isIsoDate('2026-02-29')).toBe(false);
    expect(isIsoDate('2028-02-29')).toBe(true);
    expect(isIsoDate('2026-7-1')).toBe(false);
    expect(isIsoDate(null)).toBe(false);
  });
});

describe('contract defaults per term', () => {
  it.each([
    ['annual', '2026-04-01', '2027-04-01'],
    ['annual', '2028-02-29', '2029-02-28'],
    ['monthly', '2026-01-31', '2026-02-28'],
    ['seasonal', '2026-05-01', null],
    ['period', '2026-05-01', null],
  ] as const)('%s from %s ends on %s', (term, start, end) => {
    expect(defaultEndsOn(term, start)).toBe(end);
  });

  it('asks for the end date only for period contracts', () => {
    expect(endsOnRequired('period')).toBe(true);
    expect(endsOnRequired('annual')).toBe(false);
  });
});

describe('search form → request', () => {
  const today = '2026-07-10';

  it('maps dimensions in integer centimetres and the options', () => {
    const f = {
      ...emptySearchForm(today),
      loaCm: 1134,
      beamCm: 380,
      draftCm: 180,
      hullType: 'monohull_sail' as const,
      departure: '2026-07-13',
      considerOut: true,
      maxSegments: 1,
      powerA: 32,
      water: true,
    };
    const r = toSearchRequest(f);
    expect(r).toEqual({
      ok: true,
      request: {
        purpose: 'transit',
        arrival: '2026-07-10',
        departure: '2026-07-13',
        resource_types: ['berth', 'dynamic_area'],
        channel: 'desk',
        vessel: { loa_cm: 1134, beam_cm: 380, hull_type: 'monohull_sail', draft_cm: 180 },
        consider_out: true,
        max_segments: 1,
        prefs: { power_a: 32, water: true },
      },
    });
    if (r.ok) expect(Number.isInteger(r.request.vessel!.loa_cm)).toBe(true);
  });

  it('uses an existing vessel and the term for contracts', () => {
    const r = toSearchRequest({
      ...emptySearchForm(today),
      purpose: 'contract',
      term: 'seasonal',
      vesselId: 'v1',
    });
    expect(r.ok && r.request).toMatchObject({
      vessel_id: 'v1',
      term: 'seasonal',
      purpose: 'contract',
    });
    expect(r.ok && r.request.vessel).toBeUndefined();
  });

  it('refuses missing dimensions, empty stays and no resource types', () => {
    expect(toSearchRequest(emptySearchForm(today))).toEqual({
      ok: false,
      error: 'desk:search.errors.vessel',
    });
    const v = { ...emptySearchForm(today), loaCm: 900, beamCm: 300 };
    expect(toSearchRequest({ ...v, departure: today })).toEqual({
      ok: false,
      error: 'desk:search.errors.dates',
    });
    expect(toSearchRequest({ ...v, resourceTypes: [] })).toEqual({
      ok: false,
      error: 'desk:search.errors.resourceTypes',
    });
  });

  it('turns solution segments into placements', () => {
    const s: Schemas['OccupancySolution'] = {
      key: 'k',
      score: 10,
      fit: { ok: true, overridable: false, fill_bp: 8000, blocking: [], warnings: [] },
      segments: [
        {
          resource_type: 'berth',
          resource_id: 'b1',
          code: 'A01',
          map_id: 'm',
          first_night: '2026-07-10',
          last_night: '2026-07-11',
        },
        {
          resource_type: 'dynamic_area',
          resource_id: 'a1',
          code: 'L1',
          map_id: 'm',
          x_cm: 1250,
          first_night: '2026-07-12',
          last_night: '2026-07-12',
        },
      ],
    };
    expect(solutionPlacements(s)).toEqual([
      {
        resource_type: 'berth',
        resource_id: 'b1',
        first_night: '2026-07-10',
        last_night: '2026-07-11',
      },
      {
        resource_type: 'dynamic_area',
        resource_id: 'a1',
        first_night: '2026-07-12',
        last_night: '2026-07-12',
        x_cm: 1250,
      },
    ]);
    expect(solutionCodes(s)).toBe('A01 → L1');
  });
});

describe('status actions', () => {
  it('follows the M3 transit table', () => {
    expect(transitActions('held', '2026-07-10', '2026-07-12')).toMatchObject({
      confirm: true,
      cancel: true,
      noShow: false,
    });
    expect(transitActions('confirmed', '2026-07-12', '2026-07-12').noShow).toBe(true);
    expect(transitActions('confirmed', '2026-07-11', '2026-07-12').noShow).toBe(false);
    expect(transitActions('arrived', '2026-07-12', '2026-07-10')).toMatchObject({
      cancel: false,
      stay: true,
      arrivalEditable: false,
    });
    expect(transitActions('departed', '2026-07-12', '2026-07-10').stay).toBe(false);
  });

  it('follows the M3 mooring table and labels by kind', () => {
    expect(contractActions('draft')).toEqual({ activate: true, cancel: true, terminate: false });
    expect(contractActions('active')).toEqual({ activate: false, cancel: false, terminate: true });
    expect(statusKey('transit', 'cancelled')).toBe('occupancyEnums:transitStatus.cancelled');
    expect(statusKey('mooring', 'cancelled')).toBe('occupancyEnums:contractStatus.cancelled');
  });
});

const alloc = (o: Partial<MapAllocation>): MapAllocation => ({
  id: 'a1',
  resource_type: 'berth',
  resource_id: 'b1',
  first_night: '2026-07-10',
  last_night: '2026-07-12',
  kind: 'transit',
  status: 'active',
  version: 3,
  released: [],
  ...o,
});

describe('planner geometry', () => {
  const from = '2026-07-08';

  it('places bars on night columns and clips them to the range', () => {
    expect(barColumns(alloc({}), from, 14)).toEqual({
      startCol: 2,
      endCol: 4,
      clippedStart: false,
      clippedEnd: false,
    });
    expect(
      barColumns(alloc({ first_night: '2026-07-01', last_night: '2026-08-30' }), from, 14),
    ).toEqual({
      startCol: 0,
      endCol: 13,
      clippedStart: true,
      clippedEnd: true,
    });
    expect(
      barColumns(alloc({ first_night: '2026-07-22', last_night: '2026-07-23' }), from, 14),
    ).toBeNull();
    expect(
      barColumns(alloc({ first_night: '2026-07-01', last_night: '2026-07-07' }), from, 14),
    ).toBeNull();
  });

  it('stacks overlapping allocations of one row on lanes and skips inactive ones', () => {
    const { bars, lanes } = layoutBars(
      [
        alloc({
          id: 'x',
          resource_id: 'area',
          first_night: '2026-07-08',
          last_night: '2026-07-10',
        }),
        alloc({
          id: 'y',
          resource_id: 'area',
          first_night: '2026-07-09',
          last_night: '2026-07-11',
        }),
        alloc({
          id: 'z',
          resource_id: 'area',
          first_night: '2026-07-11',
          last_night: '2026-07-12',
        }),
        alloc({ id: 'gone', resource_id: 'area', status: 'cancelled' }),
      ],
      from,
      14,
    );
    expect(bars.get('area')!.map((b) => [b.allocation.id, b.lane])).toEqual([
      ['x', 0],
      ['y', 1],
      ['z', 0],
    ]);
    expect(lanes.get('area')).toBe(2);
    const rect = barRect(bars.get('area')![1]!, 100);
    expect(rect).toEqual({
      x: LABEL_PX + COL_PX + 1,
      y: 100 + 3 + 22,
      width: 3 * COL_PX - 2,
      height: 18,
    });
    expect(Object.values(rect).every(Number.isInteger)).toBe(true);
    expect(isFree(bars.get('area'), 6, 9)).toBe(true);
    expect(isFree(bars.get('area'), 4, 9)).toBe(false);
  });

  it('maps pointer positions and drags to whole columns', () => {
    expect(columnAt(LABEL_PX - 1, 14)).toBeNull();
    expect(columnAt(LABEL_PX + COL_PX * 3 + 5, 14)).toBe(3);
    expect(columnAt(LABEL_PX + COL_PX * 99, 14)).toBe(13);
    expect(dragColumns(COL_PX * 2 - 10)).toBe(2);
    expect(dragColumns(-COL_PX / 4)).toBe(-0);
    expect(selectionStay(from, 5, 3)).toEqual({ arrival: '2026-07-11', departure: '2026-07-14' });
  });

  it('turns a horizontal drag into a stay change', () => {
    const a = alloc({});
    expect(stayChangeForDrag(a, 2, 'resize-end', { arrivalFrozen: false })).toEqual({
      departure: '2026-07-15',
    });
    expect(stayChangeForDrag(a, -1, 'move', { arrivalFrozen: false })).toEqual({
      arrival: '2026-07-09',
      departure: '2026-07-12',
    });
    expect(stayChangeForDrag(a, -3, 'resize-end', { arrivalFrozen: false })).toBeNull();
    expect(stayChangeForDrag(a, 1, 'move', { arrivalFrozen: true })).toBeNull();
    expect(stayChangeForDrag(a, 0, 'move', { arrivalFrozen: false })).toBeNull();
  });

  it('turns a vertical drop into a move from today', () => {
    const a = alloc({ first_night: '2026-07-05', last_night: '2026-07-12' });
    expect(moveForDrop(a, { resource_type: 'berth', resource_id: 'b2' }, '2026-07-09')).toEqual({
      allocation_id: 'a1',
      version: 3,
      target: { resource_type: 'berth', resource_id: 'b2' },
      from_night: '2026-07-09',
    });
    expect(
      moveForDrop(alloc({}), { resource_type: 'berth', resource_id: 'b2' }, '2026-07-01')!
        .from_night,
    ).toBe('2026-07-10');
    expect(moveForDrop(a, { resource_type: 'berth', resource_id: 'b1' }, '2026-07-09')).toBeNull();
    expect(
      moveForDrop(a, { resource_type: 'dynamic_area', resource_id: 'x' }, '2026-07-09'),
    ).toBeNull();
    expect(moveForDrop(a, { resource_type: 'berth', resource_id: 'b2' }, '2026-07-20')).toBeNull();
  });

  it('colours bars by kind and term and builds rows by structure and position', () => {
    expect(barFillClass(alloc({ kind: 'hold' }))).toBe('fill-harbor-hold');
    expect(
      barFillClass(
        alloc({
          kind: 'contract',
          contract: { id: 'c', kind: 'mooring', term: 'seasonal', status: 'active', code: 'X' },
        }),
      ),
    ).toBe('fill-harbor-seasonal');
    const berth = (id: string, code: string, position: number, structure_id = 's1') =>
      ({ id, code, position, structure_id, active: true, group_ids: [] }) as never;
    const rows = plannerRows({
      berths: [berth('b2', 'A02', 2), berth('b1', 'A01', 1), berth('b3', 'B01', 1, 's2')],
      dynamic_areas: [{ id: 'a', code: 'L1', active: true, group_ids: ['g'] } as never],
      land_resources: [{ id: 'l', code: 'P1', active: false, group_ids: [] } as never],
    });
    expect(rows.map((r) => r.code)).toEqual(['A01', 'A02', 'B01', 'L1']);
    expect(
      plannerRows(
        {
          berths: [berth('b1', 'A01', 1)],
          dynamic_areas: [{ id: 'a', code: 'L1', active: true, group_ids: ['g'] } as never],
          land_resources: [],
        },
        { groupId: 'g' },
      ).map((r) => r.code),
    ).toEqual(['L1']);
  });
});
