// Snapping (spec 8.1, m1-plan §5.3): vertices, structure edges (anchor on the edge, rotation perpendicular
// to it, outward), alignment with other berths, grid. Pointer maths in floats; the output is integer cm.
import {
  normalizeDd,
  projectOntoPolyline,
  rectCorners,
  roundHalfEven,
  structureEdges,
  type Pt,
  type SceneItem,
} from '@porto/shared/map';

export type SnapKind = 'none' | 'grid' | 'edge' | 'vertex' | 'align';

export type SnapOptions = { grid: boolean; edges: boolean; vertices: boolean; align: boolean };

export const DEFAULT_SNAP: SnapOptions = { grid: true, edges: true, vertices: true, align: true };

export type SnapContext = {
  items: readonly SceneItem[];
  gridCm: number;
  /** Snap distance in cm (a few screen pixels at the current zoom). */
  toleranceCm: number;
  options: SnapOptions;
  /** Alt held: no snapping at all. */
  disabled?: boolean;
  /** Refs to ignore (the elements being dragged). */
  exclude?: ReadonlySet<string>;
};

export type Guide = { a: Pt; b: Pt };

export type SnapResult = {
  point: Pt;
  kind: SnapKind;
  /** Edge snap: rotation of a berth anchored there (perpendicular to the edge, outward). */
  rotation_dd?: number;
  structureRef?: string;
  side?: 'left' | 'right';
  /** Edge snap: distance along the edge from its start (cm). */
  s?: number;
  guides: Guide[];
};

function snapToGrid(v: number, grid: number): number {
  return grid > 0 ? roundHalfEven(v / grid) * grid : roundHalfEven(v);
}

function vertexCandidates(item: SceneItem): Pt[] {
  switch (item.kind) {
    case 'structure':
      return item.el.points ?? [];
    case 'area':
      return item.el.outline ?? [];
    case 'berth':
    case 'land':
      return item.kind === 'berth' && item.el.mooring_type === 'buoy'
        ? [item.el.anchor]
        : rectCorners(item.el);
    default:
      return [];
  }
}

function near(item: SceneItem, p: Pt, tol: number): boolean {
  const b = item.box;
  return p.x >= b.minX - tol && p.x <= b.maxX + tol && p.y >= b.minY - tol && p.y <= b.maxY + tol;
}

/** Snaps a map point; the result is always integer cm. */
export function snapPoint(p: Pt, ctx: SnapContext): SnapResult {
  const raw = { x: roundHalfEven(p.x), y: roundHalfEven(p.y) };
  if (ctx.disabled) return { point: raw, kind: 'none', guides: [] };
  const tol = ctx.toleranceCm;
  const candidates = ctx.items.filter((i) => !ctx.exclude?.has(i.ref) && near(i, p, tol));

  if (ctx.options.vertices) {
    let best: { p: Pt; d: number } | null = null;
    for (const item of candidates) {
      for (const v of vertexCandidates(item)) {
        const d = Math.hypot(v.x - p.x, v.y - p.y);
        if (d <= tol && (!best || d < best.d)) best = { p: v, d };
      }
    }
    if (best)
      return {
        point: { x: roundHalfEven(best.p.x), y: roundHalfEven(best.p.y) },
        kind: 'vertex',
        guides: [],
      };
  }

  if (ctx.options.edges) {
    let best: (SnapResult & { d: number }) | null = null;
    for (const item of candidates) {
      if (item.kind !== 'structure') continue;
      for (const edge of structureEdges(item.el)) {
        const proj = projectOntoPolyline(p, edge.points);
        if (!proj || proj.dist > tol || (best && proj.dist >= best.d)) continue;
        const a = edge.points[proj.segment]!;
        const b = edge.points[proj.segment + 1]!;
        const dd = (Math.atan2(b.y - a.y, b.x - a.x) * 1800) / Math.PI;
        const outward = edge.side === 'left' ? dd - 900 : dd + 900;
        best = {
          point: { x: roundHalfEven(proj.point.x), y: roundHalfEven(proj.point.y) },
          kind: 'edge',
          rotation_dd: normalizeDd(roundHalfEven(outward)),
          structureRef: item.ref,
          side: edge.side,
          s: roundHalfEven(proj.s),
          guides: [],
          d: proj.dist,
        };
      }
    }
    if (best) {
      const { d: _d, ...result } = best;
      return result;
    }
  }

  let x = p.x;
  let y = p.y;
  let aligned = false;
  const guides: Guide[] = [];
  if (ctx.options.align) {
    let bx: { v: number; d: number; from: Pt } | null = null;
    let by: { v: number; d: number; from: Pt } | null = null;
    for (const item of ctx.items) {
      if (item.kind !== 'berth' || ctx.exclude?.has(item.ref)) continue;
      const a = item.el.anchor;
      const dx = Math.abs(a.x - p.x);
      const dy = Math.abs(a.y - p.y);
      if (dx <= tol && (!bx || dx < bx.d)) bx = { v: a.x, d: dx, from: a };
      if (dy <= tol && (!by || dy < by.d)) by = { v: a.y, d: dy, from: a };
    }
    if (bx) {
      x = bx.v;
      aligned = true;
      guides.push({ a: bx.from, b: { x: bx.v, y: p.y } });
    }
    if (by) {
      y = by.v;
      aligned = true;
      guides.push({ a: by.from, b: { x: p.x, y: by.v } });
    }
  }
  if (aligned) {
    return {
      point: {
        x: roundHalfEven(x === p.x && ctx.options.grid ? snapToGrid(x, ctx.gridCm) : x),
        y: roundHalfEven(y === p.y && ctx.options.grid ? snapToGrid(y, ctx.gridCm) : y),
      },
      kind: 'align',
      guides,
    };
  }
  if (ctx.options.grid && ctx.gridCm > 0) {
    return {
      point: { x: snapToGrid(p.x, ctx.gridCm), y: snapToGrid(p.y, ctx.gridCm) },
      kind: 'grid',
      guides: [],
    };
  }
  return { point: raw, kind: 'none', guides: [] };
}

/** Snaps a delta (drag of a selection) to the grid, as integer cm. */
export function snapDelta(dx: number, dy: number, gridCm: number, disabled: boolean): Pt {
  if (disabled || gridCm <= 0) return { x: roundHalfEven(dx), y: roundHalfEven(dy) };
  return { x: snapToGrid(dx, gridCm), y: snapToGrid(dy, gridCm) };
}
