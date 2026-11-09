import type { Schemas } from '@porto/shared/api';

import { addDays, daysBetween } from './dates';

// Pure geometry of the berth calendar (spec 9.7, m3-plan §8.1): rows = resources of a map, columns = nights,
// one bar per allocation. Everything is integer columns and pixels; the SVG takes them as attributes.

type S = Schemas;
export type MapAllocation = S['MapAllocation'];
export type ResourceType = S['ResourceType'];

export const PLANNER_SPANS = [14, 31, 62] as const;
export type PlannerSpan = (typeof PLANNER_SPANS)[number];

export const COL_PX = 32;
export const ROW_PX = 28;
export const LANE_PX = 22;
export const LABEL_PX = 96;
export const HEADER_PX = 36;

export type PlannerRow = {
  resource_type: ResourceType;
  resource_id: string;
  code: string;
  structure_id?: string;
  group_ids: string[];
};

/** Rows of the planner from a map document: berths (by structure and position), dynamic areas, land resources. */
export function plannerRows(
  doc: Pick<S['MapDocument'], 'berths' | 'dynamic_areas' | 'land_resources'>,
  filter: { structureId?: string; groupId?: string } = {},
): PlannerRow[] {
  const rows: PlannerRow[] = [];
  const berths = [...doc.berths]
    .filter((b) => b.active && b.id)
    .sort(
      (a, b) =>
        (a.structure_id ?? '').localeCompare(b.structure_id ?? '') ||
        a.position - b.position ||
        a.code.localeCompare(b.code),
    );
  for (const b of berths)
    rows.push({
      resource_type: 'berth',
      resource_id: b.id!,
      code: b.code,
      ...(b.structure_id ? { structure_id: b.structure_id } : {}),
      group_ids: b.group_ids,
    });
  for (const a of doc.dynamic_areas)
    if (a.active && a.id)
      rows.push({
        resource_type: 'dynamic_area',
        resource_id: a.id,
        code: a.code,
        ...(a.structure_id ? { structure_id: a.structure_id } : {}),
        group_ids: a.group_ids,
      });
  for (const l of doc.land_resources)
    if (l.active && l.id)
      rows.push({
        resource_type: 'land_resource',
        resource_id: l.id,
        code: l.code,
        group_ids: l.group_ids,
      });
  return rows.filter(
    (r) =>
      (!filter.structureId || r.structure_id === filter.structureId) &&
      (!filter.groupId || r.group_ids.includes(filter.groupId)),
  );
}

export type Bar = {
  allocation: MapAllocation;
  /** First and last visible column (0-based, inclusive). */
  startCol: number;
  endCol: number;
  /** The allocation continues before / after the visible range. */
  clippedStart: boolean;
  clippedEnd: boolean;
  lane: number;
};

/** Visible columns of an allocation in [from, from + days − 1], or null when outside. */
export function barColumns(
  a: Pick<MapAllocation, 'first_night' | 'last_night'>,
  from: string,
  days: number,
): { startCol: number; endCol: number; clippedStart: boolean; clippedEnd: boolean } | null {
  const s = daysBetween(from, a.first_night);
  const e = daysBetween(from, a.last_night);
  if (e < 0 || s > days - 1) return null;
  return {
    startCol: Math.max(0, s),
    endCol: Math.min(days - 1, e),
    clippedStart: s < 0,
    clippedEnd: e > days - 1,
  };
}

/**
 * Bars of every row, active allocations only. Overlapping bars of one row (allocations of a dynamic area) go on
 * separate lanes, assigned greedily by start column; `lanes` is the lane count per row (at least 1).
 */
export function layoutBars(
  allocations: readonly MapAllocation[],
  from: string,
  days: number,
): { bars: Map<string, Bar[]>; lanes: Map<string, number> } {
  const byRow = new Map<string, Bar[]>();
  for (const a of allocations) {
    if (a.status !== 'active') continue;
    const cols = barColumns(a, from, days);
    if (!cols) continue;
    const list = byRow.get(a.resource_id) ?? [];
    list.push({ allocation: a, ...cols, lane: 0 });
    byRow.set(a.resource_id, list);
  }
  const lanes = new Map<string, number>();
  for (const [rowId, list] of byRow) {
    list.sort((x, y) => x.startCol - y.startCol || x.allocation.id.localeCompare(y.allocation.id));
    const laneEnds: number[] = [];
    for (const bar of list) {
      let lane = laneEnds.findIndex((end) => end < bar.startCol);
      if (lane === -1) {
        lane = laneEnds.length;
        laneEnds.push(bar.endCol);
      } else laneEnds[lane] = bar.endCol;
      bar.lane = lane;
    }
    lanes.set(rowId, Math.max(1, laneEnds.length));
  }
  return { bars: byRow, lanes };
}

/** Pixel rectangle of a bar inside its row (x from the label column). */
export function barRect(bar: Bar, rowY: number, col: number = COL_PX) {
  return {
    x: LABEL_PX + bar.startCol * col + 1,
    y: rowY + 3 + bar.lane * LANE_PX,
    width: (bar.endCol - bar.startCol + 1) * col - 2,
    height: LANE_PX - 4,
  };
}

/** Theme token class of a bar, by kind and contract term (lens Tipo di contratto). */
export function barFillClass(a: MapAllocation): string {
  switch (a.kind) {
    case 'hold':
      return 'fill-harbor-hold';
    case 'transit':
      return 'fill-harbor-transit';
    case 'block':
      return 'fill-harbor-blocked';
    case 'maintenance':
      return 'fill-harbor-maintenance';
    case 'owner':
      return 'fill-harbor-owner';
    default:
      switch (a.contract?.term) {
        case 'seasonal':
          return 'fill-harbor-seasonal';
        case 'monthly':
          return 'fill-harbor-monthly';
        case 'period':
          return 'fill-harbor-period';
        default:
          return 'fill-harbor-annual';
      }
  }
}

/** Column under a pointer x (relative to the SVG), clamped to the range; null over the label column. */
export function columnAt(x: number, days: number, col: number = COL_PX): number | null {
  if (x < LABEL_PX) return null;
  return Math.min(days - 1, Math.max(0, Math.floor((x - LABEL_PX) / col)));
}

/** Whole columns moved by a horizontal drag of dx pixels (half a column rounds). */
export function dragColumns(dx: number, col: number = COL_PX): number {
  return Math.round(dx / col);
}

/** Widest column a night may take: 31 and 14 nights stretch up to this to fill the width. */
export const MAX_COL_PX = 96;

/** Column width that fills `available` pixels (label column included) with `days` nights, never narrower than
 * COL_PX (the grid scrolls) nor wider than MAX_COL_PX; COL_PX before the width is known. */
export function fitColumnPx(available: number, days: number): number {
  if (available <= 0 || days <= 0) return COL_PX;
  const col = Math.floor((available - LABEL_PX) / days);
  return Math.max(COL_PX, Math.min(MAX_COL_PX, col));
}

export type DragMode = 'move' | 'resize-end';

/**
 * Stay change of a transit for a horizontal drag of `delta` nights: `resize-end` moves the departure, `move`
 * shifts arrival and departure. Returns null when nothing changes, the stay would have no nights, or the move
 * would change the arrival of a transit already arrived (arrival is frozen, m3-plan §4.2).
 */
export function stayChangeForDrag(
  a: Pick<MapAllocation, 'first_night' | 'last_night'>,
  delta: number,
  mode: DragMode,
  opts: { arrivalFrozen: boolean },
): { arrival?: string; departure: string } | null {
  if (delta === 0) return null;
  const departure = addDays(a.last_night, 1 + delta);
  if (mode === 'resize-end') {
    if (daysBetween(a.first_night, departure) < 1) return null;
    return { departure };
  }
  if (opts.arrivalFrozen) return null;
  return { arrival: addDays(a.first_night, delta), departure };
}

/**
 * Move request for a vertical drop of an allocation on another row (berths and land resources only: placing in
 * a dynamic area needs a position and goes through the layout session of the operational map). Past nights
 * never move, so the move starts at max(today, first night).
 */
export function moveForDrop(
  a: Pick<
    MapAllocation,
    'id' | 'version' | 'resource_id' | 'resource_type' | 'first_night' | 'last_night'
  >,
  target: Pick<PlannerRow, 'resource_type' | 'resource_id'>,
  today: string,
): S['MoveRequest'] | null {
  if (target.resource_id === a.resource_id) return null;
  if (target.resource_type === 'dynamic_area' || target.resource_type !== a.resource_type)
    return null;
  if (a.last_night < today) return null;
  return {
    allocation_id: a.id,
    version: a.version,
    target: { resource_type: target.resource_type, resource_id: target.resource_id },
    from_night: a.first_night > today ? a.first_night : today,
  };
}

/** Stay [arrival, departure) of a free-space selection from column c1 to c2 (any order). */
export function selectionStay(from: string, c1: number, c2: number) {
  const lo = Math.min(c1, c2);
  const hi = Math.max(c1, c2);
  return { arrival: addDays(from, lo), departure: addDays(from, hi + 1) };
}

/** Whether the nights [first, last] of a row are free of active allocations (berths and land only). */
export function isFree(
  bars: readonly Bar[] | undefined,
  startCol: number,
  endCol: number,
): boolean {
  return !(bars ?? []).some((b) => b.startCol <= endCol && b.endCol >= startCol);
}

/** Vertical layout of the rows: each row is tall enough for its lanes. */
export function rowLayout(
  rows: readonly PlannerRow[],
  lanes: ReadonlyMap<string, number>,
): { row: PlannerRow; y: number; h: number }[] {
  const out: { row: PlannerRow; y: number; h: number }[] = [];
  let y = HEADER_PX;
  for (const row of rows) {
    const h = Math.max(ROW_PX, (lanes.get(row.resource_id) ?? 1) * LANE_PX + 6);
    out.push({ row, y, h });
    y += h;
  }
  return out;
}
