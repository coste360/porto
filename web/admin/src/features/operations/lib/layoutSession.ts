// Layout session of a dynamic area (spec 9.4, m3-plan §8.5): a draft of moves kept in React state and in
// sessionStorage (per user, area and night) on top of the server's placements (the base). Save sends every
// touched allocation in one transaction; a 412 reloads the base and re-applies the draft ops, reporting the
// vessels that changed meanwhile. Pure reducer.
import type { AreaLayoutSaveData, AreaPlacementData } from '@porto/shared/api';

export type Placement = { x: number; y?: number; rotation?: number };

export type LayoutVessel = {
  id: string;
  name: string;
  loa_cm: number;
  beam_cm: number;
  hull_type: string;
};

export type LayoutItem = {
  allocationId: string;
  version: number;
  vessel: LayoutVessel;
  /** null = in "Da collocare". */
  placement: Placement | null;
  /** Brought in from another resource during the session. */
  added?: boolean;
};

export type LayoutOp =
  | { type: 'move'; allocationId: string; placement: Placement }
  | { type: 'remove'; allocationId: string }
  | { type: 'add'; item: LayoutItem };

export type LayoutState = {
  areaId: string;
  date: string;
  /** Server placements of the area for the night, by allocation id. */
  base: Readonly<Record<string, LayoutItem>>;
  ops: readonly LayoutOp[];
  /** Allocations whose base changed on the last rebase (version or presence). */
  changed: readonly string[];
};

export type LayoutAction =
  | { type: 'load'; base: readonly LayoutItem[]; ops?: readonly LayoutOp[] }
  | { type: 'move'; allocationId: string; placement: Placement }
  | { type: 'remove'; allocationId: string }
  | { type: 'add'; item: LayoutItem }
  | { type: 'restore' }
  | { type: 'rebase'; base: readonly LayoutItem[] }
  | { type: 'compaction'; placements: readonly { allocation_id: string; x_cm: number }[] };

export function itemsFromPlacements(ps: readonly AreaPlacementData[]): LayoutItem[] {
  return ps.map((p) => ({
    allocationId: p.allocation_id,
    version: p.allocation_version,
    vessel: {
      id: p.vessel.id,
      name: p.vessel.name,
      loa_cm: p.vessel.loa_cm,
      beam_cm: p.vessel.beam_cm,
      hull_type: p.vessel.hull_type,
    },
    placement: {
      x: p.x_cm,
      ...(p.y_cm !== undefined ? { y: p.y_cm } : {}),
      ...(p.rotation_dd !== undefined ? { rotation: p.rotation_dd } : {}),
    },
  }));
}

function byId(items: readonly LayoutItem[]): Record<string, LayoutItem> {
  return Object.fromEntries(items.map((i) => [i.allocationId, i]));
}

export function initialLayout(areaId: string, date: string): LayoutState {
  return { areaId, date, base: {}, ops: [], changed: [] };
}

/** Ops that still apply on a base (moves/removes of vanished allocations are dropped). */
function applicable(
  ops: readonly LayoutOp[],
  base: Readonly<Record<string, LayoutItem>>,
): LayoutOp[] {
  const added = new Set<string>();
  const out: LayoutOp[] = [];
  for (const op of ops) {
    if (op.type === 'add') {
      // Already in the area (dragged inside it from the ops map): the add is a move.
      if (base[op.item.allocationId]) {
        if (op.item.placement) {
          out.push({
            type: 'move',
            allocationId: op.item.allocationId,
            placement: op.item.placement,
          });
        }
        continue;
      }
      added.add(op.item.allocationId);
      out.push(op);
    } else if (base[op.allocationId] || added.has(op.allocationId)) {
      out.push(op);
    }
  }
  return out;
}

export function layoutReducer(state: LayoutState, action: LayoutAction): LayoutState {
  switch (action.type) {
    case 'load': {
      const base = byId(action.base);
      return { ...state, base, ops: applicable(action.ops ?? [], base), changed: [] };
    }
    case 'move':
      return {
        ...state,
        ops: [
          ...state.ops,
          { type: 'move', allocationId: action.allocationId, placement: action.placement },
        ],
      };
    case 'remove':
      return {
        ...state,
        ops: [...state.ops, { type: 'remove', allocationId: action.allocationId }],
      };
    case 'add':
      return {
        ...state,
        ops: applicable(
          [...state.ops, { type: 'add', item: { ...action.item, added: true } }],
          state.base,
        ),
      };
    case 'restore':
      return { ...state, ops: [], changed: [] };
    case 'rebase': {
      const base = byId(action.base);
      const changed = new Set<string>();
      for (const [id, old] of Object.entries(state.base)) {
        const now = base[id];
        if (!now || now.version !== old.version) changed.add(id);
      }
      for (const id of Object.keys(base)) if (!state.base[id]) changed.add(id);
      return { ...state, base, ops: applicable(state.ops, base), changed: [...changed].sort() };
    }
    case 'compaction': {
      const draft = draftItems(state);
      const ops: LayoutOp[] = [];
      for (const p of action.placements) {
        const cur = draft.find((d) => d.allocationId === p.allocation_id);
        if (!cur || cur.placement?.x === p.x_cm) continue;
        ops.push({
          type: 'move',
          allocationId: p.allocation_id,
          placement: { ...cur.placement, x: p.x_cm },
        });
      }
      return { ...state, ops: [...state.ops, ...ops] };
    }
  }
}

/** The draft: base with the ops replayed, in base order then added items. */
export function draftItems(state: LayoutState): LayoutItem[] {
  const items = new Map<string, LayoutItem>(
    Object.entries(state.base).map(([k, v]) => [k, { ...v }]),
  );
  for (const op of state.ops) {
    if (op.type === 'add') items.set(op.item.allocationId, { ...op.item });
    else {
      const cur = items.get(op.allocationId);
      if (!cur) continue;
      items.set(op.allocationId, { ...cur, placement: op.type === 'move' ? op.placement : null });
    }
  }
  return [...items.values()];
}

function samePlacement(a: Placement | null, b: Placement | null): boolean {
  if (!a || !b) return a === b;
  return a.x === b.x && (a.y ?? 0) === (b.y ?? 0) && (a.rotation ?? 0) === (b.rotation ?? 0);
}

/** Allocations whose draft placement differs from the base (or added). */
export function touched(state: LayoutState): LayoutItem[] {
  return draftItems(state).filter((d) => {
    const b = state.base[d.allocationId];
    return !b || !samePlacement(b.placement, d.placement);
  });
}

export function unplaced(state: LayoutState): LayoutItem[] {
  return draftItems(state).filter((d) => d.placement === null);
}

export function isDirty(state: LayoutState): boolean {
  return touched(state).length > 0;
}

/**
 * Body of saveAreaLayout: every base allocation and every added one with its version, and the placement of every
 * vessel of the draft (m3-plan §3.5: a vessel of the area without a placement fails the save as `unplaced`; the
 * server skips the ones that did not move).
 */
export function saveBody(state: LayoutState, fromNight?: string): AreaLayoutSaveData {
  const draft = draftItems(state);
  return {
    date: state.date,
    ...(fromNight ? { from_night: fromNight } : {}),
    base: draft.map((d) => ({ allocation_id: d.allocationId, version: d.version })),
    placements: draft
      .filter((d) => d.placement)
      .map((d) => ({
        allocation_id: d.allocationId,
        x_cm: d.placement!.x,
        ...(d.placement!.y !== undefined ? { y_cm: d.placement!.y } : {}),
        ...(d.placement!.rotation !== undefined ? { rotation_dd: d.placement!.rotation } : {}),
      })),
  };
}

// ---- sessionStorage (per-viewer convenience; safe to lose) ----

export function draftKey(userId: string, areaId: string, date: string): string {
  return `porto:ops:layout:${userId}:${areaId}:${date}`;
}

type Stored = { ops: readonly LayoutOp[] };

export function loadDraft(key: string): LayoutOp[] {
  try {
    const raw = window.sessionStorage.getItem(key);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as Stored;
    return Array.isArray(parsed.ops) ? [...parsed.ops] : [];
  } catch {
    return [];
  }
}

export function storeDraft(key: string, ops: readonly LayoutOp[]): void {
  try {
    if (ops.length === 0) window.sessionStorage.removeItem(key);
    else window.sessionStorage.setItem(key, JSON.stringify({ ops } satisfies Stored));
  } catch {
    // Storage unavailable (private mode): the draft lives in memory only.
  }
}

/** Queues an "add" op for an area before navigating to its layout session (drop from the ops map). */
export function queueAdd(key: string, item: LayoutItem): void {
  storeDraft(key, [...loadDraft(key), { type: 'add', item }]);
}
