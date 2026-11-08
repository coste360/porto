import type { StorageLayoutItemInput } from '@porto/shared/api';

// Layout draft of the storage planner and of a scenario editor (m9-plan §12.1): positions of the boats of one
// area in integer cm (top-left of the footprint box in grid space, snapped to cells) and rotation in tenths of a
// degree (0, 900, 1800, 2700). Pure reducer: the server validates every draft (evaluate) and is the authority.

export type Rotation = 0 | 900 | 1800 | 2700;

export type Position = { x: number; y: number; rotation: Rotation };

export type DraftItem = {
  allocationId: string;
  loa: number;
  beam: number;
  /** null = unplaced (in the "Da collocare" tray). */
  at: Position | null;
  pinned: boolean;
};

export type DraftGrid = { cellCm: number; cols: number; rows: number };

export type DraftState = {
  grid: DraftGrid;
  items: Readonly<Record<string, DraftItem>>;
  /** Positions when loaded (restore and dirty checks). */
  original: Readonly<Record<string, Position | null>>;
  order: readonly string[];
  selected?: string;
};

export type DraftAction =
  | { type: 'load'; grid: DraftGrid; items: DraftItem[] }
  | { type: 'select'; id?: string }
  | { type: 'moveBy'; id: string; cols: number; rows: number }
  | { type: 'moveTo'; id: string; x: number; y: number }
  | { type: 'rotate'; id: string }
  | { type: 'unplace'; id: string }
  | { type: 'togglePin'; id: string }
  | { type: 'fill'; placements: { allocationId: string; at: Position | null }[] }
  | { type: 'restore' };

/** Cells of the footprint box along x and y (LOA along x at 0/1800, along y at 900/2700; margins excluded). */
export function boxCells(
  item: Pick<DraftItem, 'loa' | 'beam'>,
  rotation: Rotation,
  cellCm: number,
) {
  const along = Math.max(1, Math.ceil(item.loa / cellCm));
  const across = Math.max(1, Math.ceil(item.beam / cellCm));
  return rotation === 900 || rotation === 2700 ? { w: across, h: along } : { w: along, h: across };
}

/** Snaps a cm coordinate to the cell grid (floor) and keeps the box inside [0, cells − size]. */
function snap(v: number, cellCm: number, cells: number, size: number): number {
  const c = Math.floor(v / cellCm);
  return Math.max(0, Math.min(c, Math.max(0, cells - size))) * cellCm;
}

function withItem(s: DraftState, id: string, f: (it: DraftItem) => DraftItem): DraftState {
  const it = s.items[id];
  if (!it) return s;
  const next = f(it);
  return next === it ? s : { ...s, items: { ...s.items, [id]: next } };
}

function place(s: DraftState, it: DraftItem, x: number, y: number, rotation: Rotation): DraftItem {
  const { w, h } = boxCells(it, rotation, s.grid.cellCm);
  return {
    ...it,
    at: {
      x: snap(x, s.grid.cellCm, s.grid.cols, w),
      y: snap(y, s.grid.cellCm, s.grid.rows, h),
      rotation,
    },
  };
}

export function draftReducer(s: DraftState, a: DraftAction): DraftState {
  switch (a.type) {
    case 'load': {
      const items: Record<string, DraftItem> = {};
      const original: Record<string, Position | null> = {};
      for (const it of a.items) {
        items[it.allocationId] = it;
        original[it.allocationId] = it.at;
      }
      return { grid: a.grid, items, original, order: a.items.map((i) => i.allocationId) };
    }
    case 'select':
      return { ...s, selected: a.id };
    case 'moveBy':
      return withItem(s, a.id, (it) =>
        it.pinned || !it.at
          ? it
          : place(
              s,
              it,
              it.at.x + a.cols * s.grid.cellCm,
              it.at.y + a.rows * s.grid.cellCm,
              it.at.rotation,
            ),
      );
    case 'moveTo':
      return withItem(s, a.id, (it) =>
        it.pinned ? it : place(s, it, a.x, a.y, it.at?.rotation ?? 0),
      );
    case 'rotate':
      return withItem(s, a.id, (it) =>
        it.pinned || !it.at
          ? it
          : place(s, it, it.at.x, it.at.y, ((it.at.rotation + 900) % 3600) as Rotation),
      );
    case 'unplace':
      return withItem(s, a.id, (it) => (it.pinned || !it.at ? it : { ...it, at: null }));
    case 'togglePin':
      return withItem(s, a.id, (it) => ({ ...it, pinned: !it.pinned }));
    case 'fill': {
      let next = s;
      for (const p of a.placements) {
        next = withItem(next, p.allocationId, (it) =>
          it.pinned
            ? it
            : p.at
              ? place(next, it, p.at.x, p.at.y, p.at.rotation)
              : { ...it, at: null },
        );
      }
      return next;
    }
    case 'restore': {
      const items: Record<string, DraftItem> = {};
      for (const id of s.order) {
        const it = s.items[id]!;
        items[id] = { ...it, at: s.original[id] ?? null };
      }
      return { ...s, items };
    }
  }
}

export const EMPTY_DRAFT: DraftState = {
  grid: { cellCm: 100, cols: 0, rows: 0 },
  items: {},
  original: {},
  order: [],
};

/** Same rectangle: 0/1800 and 900/2700 are not moves (D13). */
export function samePosition(a: Position | null, b: Position | null): boolean {
  if (!a || !b) return a === b;
  return a.x === b.x && a.y === b.y && a.rotation % 1800 === b.rotation % 1800;
}

/** Ids whose position differs from the loaded one. */
export function changedIds(s: DraftState): string[] {
  return s.order.filter((id) => !samePosition(s.items[id]!.at, s.original[id] ?? null));
}

export function isDirty(s: DraftState): boolean {
  return changedIds(s).length > 0;
}

/** Items in the tray (unplaced), in load order. */
export function trayIds(s: DraftState): string[] {
  return s.order.filter((id) => !s.items[id]!.at);
}

/** The draft as the API layout input (evaluate, arrange, scenario items). */
export function toLayoutInputs(s: DraftState): StorageLayoutItemInput[] {
  return s.order.map((id) => {
    const it = s.items[id]!;
    return {
      allocation_id: id,
      x_cm: it.at ? it.at.x : null,
      y_cm: it.at ? it.at.y : null,
      rotation_dd: it.at ? it.at.rotation : null,
      pinned: it.pinned,
    };
  });
}

/** Cell indexes (row-major, cols wide) covered by the box of an item (rendering and client overlap hints). */
export function boxIndexes(it: DraftItem, grid: DraftGrid): number[] {
  if (!it.at) return [];
  const { w, h } = boxCells(it, it.at.rotation, grid.cellCm);
  const c0 = Math.floor(it.at.x / grid.cellCm);
  const r0 = Math.floor(it.at.y / grid.cellCm);
  const out: number[] = [];
  for (let r = r0; r < r0 + h && r < grid.rows; r++) {
    for (let c = c0; c < c0 + w && c < grid.cols; c++) out.push(r * grid.cols + c);
  }
  return out;
}

/** Ids of the placed items whose boxes overlap another one (client hint; the server issues are authoritative). */
export function overlappingIds(s: DraftState): Set<string> {
  const owner = new Map<number, string>();
  const out = new Set<string>();
  for (const id of s.order) {
    for (const c of boxIndexes(s.items[id]!, s.grid)) {
      const o = owner.get(c);
      if (o && o !== id) {
        out.add(o);
        out.add(id);
      } else owner.set(c, id);
    }
  }
  return out;
}

/** A rotation value from the API (anything else reads as 0). */
export function asRotation(v: number | null | undefined): Rotation {
  return v === 900 || v === 1800 || v === 2700 ? v : 0;
}
