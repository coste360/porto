import type {
  StorageArea,
  StorageMoveEnd,
  StoragePlanItem,
  StorageScenarioComparison,
  StorageScenarioItem,
} from '@porto/shared/api';

import { asRotation, type DraftGrid, type DraftItem } from './draft';

// Conversions between the API shapes of the planner and the layout draft, the timeline rows and the comparison
// rows (m9-plan §12.1). Dates are YYYY-MM-DD strings compared as strings (ISO order).

export function gridOf(area: Pick<StorageArea, 'cell_cm' | 'cols' | 'rows'>): DraftGrid {
  return { cellCm: area.cell_cm, cols: area.cols, rows: area.rows };
}

type Positioned = {
  allocation_id: string;
  vessel?: { loa_cm: number; beam_cm: number } | undefined;
  x_cm?: number;
  y_cm?: number;
  rotation_dd?: number;
};

function draftItem(i: Positioned, pinned: boolean): DraftItem {
  const placed = i.x_cm !== undefined && i.y_cm !== undefined;
  return {
    allocationId: i.allocation_id,
    loa: i.vessel?.loa_cm ?? 100,
    beam: i.vessel?.beam_cm ?? 100,
    at: placed ? { x: i.x_cm!, y: i.y_cm!, rotation: asRotation(i.rotation_dd) } : null,
    pinned,
  };
}

/** Draft items of the boats of a plan (blocks and maintenance rows are obstacles, not items). */
export function planDraftItems(items: readonly StoragePlanItem[]): DraftItem[] {
  return items
    .filter((i) => i.kind !== 'block' && i.kind !== 'maintenance')
    .map((i) => draftItem(i, false));
}

/** Draft items of a scenario working copy. */
export function scenarioDraftItems(items: readonly StorageScenarioItem[]): DraftItem[] {
  return items.map((i) => draftItem(i, i.pinned));
}

/** Whether a stay covers the night (first ≤ date ≤ last). */
export function presentOn(i: { first_night: string; last_night: string }, date: string): boolean {
  return i.first_night <= date && date <= i.last_night;
}

export type TimelineRow = {
  allocationId: string;
  label: string;
  arrival: string;
  exit: string;
  exitBlocked: boolean;
};

/** One row per boat from arrival to exit, sorted by exit date then label (spec 17.2). */
export function timelineRows(
  items: readonly {
    allocation_id: string;
    vessel?: { name: string } | undefined;
    contract?: { code: string } | undefined;
    arrival_on: string;
    exit_on: string;
  }[],
  blocked: ReadonlySet<string>,
): TimelineRow[] {
  return items
    .map((i) => ({
      allocationId: i.allocation_id,
      label: i.vessel?.name ?? i.contract?.code ?? i.allocation_id.slice(0, 8),
      arrival: i.arrival_on,
      exit: i.exit_on,
      exitBlocked: blocked.has(i.allocation_id),
    }))
    .sort((a, b) =>
      a.exit === b.exit ? a.label.localeCompare(b.label) : a.exit < b.exit ? -1 : 1,
    );
}

/** Days between two ISO dates (b − a). */
export function daysBetween(a: string, b: string): number {
  return Math.round((Date.parse(`${b}T00:00:00Z`) - Date.parse(`${a}T00:00:00Z`)) / 86_400_000);
}

/** The ISO date n days after d. */
export function addDays(d: string, n: number): string {
  const t = new Date(Date.parse(`${d}T00:00:00Z`) + n * 86_400_000);
  return t.toISOString().slice(0, 10);
}

export type CompareRow = {
  allocationId: string;
  vesselId?: string;
  left?: StorageMoveEnd;
  right?: StorageMoveEnd;
  /** moved on the same area, moved to another resource, only on one side. */
  change: 'moved' | 'rotated' | 'other_resource' | 'only_left' | 'only_right';
};

/** Rows of the moved boats of a comparison, by change then id. */
export function compareRows(c: StorageScenarioComparison | undefined): CompareRow[] {
  if (!c) return [];
  const rank = { moved: 0, rotated: 1, other_resource: 2, only_left: 3, only_right: 4 } as const;
  return c.diffs
    .map((d): CompareRow => {
      let change: CompareRow['change'] = 'moved';
      if (!d.left) change = 'only_right';
      else if (!d.right) change = 'only_left';
      else if (d.left.resource_id !== d.right.resource_id) change = 'other_resource';
      else if (d.left.x_cm === d.right.x_cm && d.left.y_cm === d.right.y_cm) change = 'rotated';
      return {
        allocationId: d.allocation_id,
        ...(d.vessel_id ? { vesselId: d.vessel_id } : {}),
        ...(d.left ? { left: d.left } : {}),
        ...(d.right ? { right: d.right } : {}),
        change,
      };
    })
    .sort(
      (a, b) => rank[a.change] - rank[b.change] || a.allocationId.localeCompare(b.allocationId),
    );
}

/** Text of a position: "x 12,0 m · y 4,0 m · 90°" (metres with one decimal, locale numbers). */
export function positionText(
  p: { x_cm?: number; y_cm?: number; rotation_dd?: number } | undefined,
  locale: string,
): string {
  if (!p || p.x_cm === undefined || p.y_cm === undefined) return '—';
  const m = new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return `x ${m.format(p.x_cm / 100)} m · y ${m.format(p.y_cm / 100)} m · ${(p.rotation_dd ?? 0) / 10}°`;
}
