// Busy index of the drag preview (m3-plan §8.5): the allocations of the current map for the moving
// allocation's remaining nights, by resource, with their released nights and cells. Advisory only: the
// database constraints decide (INV-01).
import type { MapAllocationData, MapOccupancyData } from '@porto/shared/api';
import type { FitOutcome } from '@porto/shared/map';

import { addDays, maxDate, minDate } from './dates';

export type NightRange = { from: string; to: string };

export type BusyEntry = {
  allocationId: string;
  vesselId?: string;
  vesselName?: string;
  /** Nights of the allocation minus its released ranges. */
  busy: NightRange[];
  /** Linear: first..last cell; surface: indexes; undefined for berths and land. */
  cells?: { first?: number; last?: number; indexes?: number[] };
  xCm?: number;
  yCm?: number;
  rotationDd?: number;
  loa?: number;
  beam?: number;
};

export type BusyIndex = ReadonlyMap<string, readonly BusyEntry[]>;

/** [first, last] minus the released ranges (sorted, disjoint). */
export function subtractReleased(
  first: string,
  last: string,
  released: readonly NightRange[],
): NightRange[] {
  const out: NightRange[] = [];
  let cursor = first;
  for (const r of [...released].sort((a, b) => (a.from < b.from ? -1 : 1))) {
    if (r.to < cursor || r.from > last) continue;
    if (r.from > cursor) out.push({ from: cursor, to: minDate(addDays(r.from, -1), last) });
    cursor = maxDate(cursor, addDays(r.to, 1));
    if (cursor > last) return out;
  }
  if (cursor <= last) out.push({ from: cursor, to: last });
  return out;
}

function entryOf(a: MapAllocationData): BusyEntry {
  return {
    allocationId: a.id,
    ...(a.vessel
      ? {
          vesselId: a.vessel.id,
          vesselName: a.vessel.name,
          loa: a.vessel.loa_cm,
          beam: a.vessel.beam_cm,
        }
      : {}),
    busy: subtractReleased(a.first_night, a.last_night, a.released),
    ...(a.cells ? { cells: a.cells } : {}),
    ...(a.x_cm !== undefined ? { xCm: a.x_cm } : {}),
    ...(a.y_cm !== undefined ? { yCm: a.y_cm } : {}),
    ...(a.rotation_dd !== undefined ? { rotationDd: a.rotation_dd } : {}),
  };
}

/** Busy index of a map occupancy, without the moving allocation(s) and non-active allocations. */
export function buildBusyIndex(
  occ: MapOccupancyData,
  exclude: ReadonlySet<string> = new Set(),
): BusyIndex {
  const out = new Map<string, BusyEntry[]>();
  for (const a of occ.allocations) {
    if (a.status !== 'active' || exclude.has(a.id)) continue;
    const list = out.get(a.resource_id) ?? [];
    list.push(entryOf(a));
    out.set(a.resource_id, list);
  }
  return out;
}

/** First busy range of e intersecting [from, to], or null. */
export function overlap(e: BusyEntry, from: string, to: string): NightRange | null {
  for (const r of e.busy) {
    if (r.to < from || r.from > to) continue;
    return { from: maxDate(r.from, from), to: minDate(r.to, to) };
  }
  return null;
}

function cellSet(c: BusyEntry['cells']): Set<number> {
  const s = new Set<number>();
  if (!c) return s;
  if (c.indexes) for (const i of c.indexes) s.add(i);
  else if (c.first !== undefined && c.last !== undefined)
    for (let i = c.first; i <= c.last; i++) s.add(i);
  return s;
}

export type PreviewConflict = {
  allocationId: string;
  vesselName?: string;
  from: string;
  to: string;
  cells: number[];
};

export type PreviewState = 'ok' | 'warn' | 'conflict' | 'invalid';

export type Preview = {
  state: PreviewState;
  conflicts: PreviewConflict[];
  /** Candidate cells in conflict. */
  conflictCells: Set<number>;
  fit: FitOutcome | null;
  placementError?: string;
};

/**
 * Drag preview state machine (m3-plan §8.5): invalid when the placement is not valid, conflict when a busy
 * allocation overlaps (same berth, or shared cells of an area) or the fit blocks, warn on fit warnings, else
 * ok. `cells` are the candidate's cells in an area (undefined on berths and land).
 */
export function previewDrop(p: {
  index: BusyIndex;
  resourceId: string;
  from: string;
  to: string;
  cells?: readonly number[];
  fit: FitOutcome | null;
  placementError?: string;
}): Preview {
  if (p.placementError) {
    return {
      state: 'invalid',
      conflicts: [],
      conflictCells: new Set(),
      fit: p.fit,
      placementError: p.placementError,
    };
  }
  const conflicts: PreviewConflict[] = [];
  const conflictCells = new Set<number>();
  for (const e of p.index.get(p.resourceId) ?? []) {
    const nights = overlap(e, p.from, p.to);
    if (!nights) continue;
    if (p.cells) {
      const theirs = cellSet(e.cells);
      const shared = p.cells.filter((c) => theirs.has(c));
      if (shared.length === 0) continue;
      for (const c of shared) conflictCells.add(c);
      conflicts.push({
        allocationId: e.allocationId,
        vesselName: e.vesselName,
        ...nights,
        cells: shared,
      });
    } else {
      conflicts.push({
        allocationId: e.allocationId,
        vesselName: e.vesselName,
        ...nights,
        cells: [],
      });
    }
  }
  const state: PreviewState =
    conflicts.length > 0 || (p.fit && !p.fit.ok)
      ? 'conflict'
      : p.fit && p.fit.warnings.length > 0
        ? 'warn'
        : 'ok';
  return { state, conflicts, conflictCells, fit: p.fit };
}
