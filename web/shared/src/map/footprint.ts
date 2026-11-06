// TypeScript mirror of layout.Footprint, layout.AreaOf and layout.CellCount (m1-plan §2.4, ADR 0003).
// Integers only: every input is integer cm, divisions are floor/ceil on integers, and the shared vectors in
// testdata/footprint/ keep this file and the Go package in step (footprint.parity.test.ts).

export type AreaKind = 'linear' | 'surface';
export type MooringMode = 'stern_to' | 'alongside';

export type FootprintVessel = { loa: number; beam: number };
export type FootprintPlacement = { x: number; y: number; rotation: number };

export type FootprintArea = {
  kind: AreaKind;
  /** Linear: To − From. */
  length: number;
  mode: MooringMode;
  cell: number;
  margin: number;
  /** Surface grid size. */
  cols: number;
  rows: number;
  /** Surface: cell fully inside the outline and not blocked (length cols × rows). */
  usable: boolean[];
  /** Surface: blocked cell indexes (to tell blocked cells from cells outside the outline). */
  blocked?: number[];
};

export type Cells = { first: number; last: number; indexes: number[] };

export type FootprintErrorCode =
  'unsnapped' | 'outside_area' | 'blocked_cell' | 'invalid_vessel' | 'invalid_area';

export class FootprintError extends Error {
  readonly code: FootprintErrorCode;
  constructor(code: FootprintErrorCode) {
    super(code);
    this.code = code;
  }
}

/** Snap step of a placement (cm). */
export const PLACEMENT_SNAP_CM = 10;

function floorDiv(a: number, b: number): number {
  return Math.floor(a / b);
}

function ceilDiv(a: number, b: number): number {
  return -Math.floor(-a / b);
}

function isInt(n: number): boolean {
  return Number.isSafeInteger(n);
}

/**
 * Cells occupied by a vessel placed in an area. Linear: the interval [x, x + w) with w = beam + margin
 * (stern_to) or loa + margin (alongside). Surface: the rectangle (loa + margin) × (beam + margin) along the
 * rotation axis, top-left at (x, y) in grid space.
 */
export function footprint(v: FootprintVessel, p: FootprintPlacement, a: FootprintArea): Cells {
  if (!isInt(v.loa) || !isInt(v.beam) || v.loa <= 0 || v.beam <= 0) {
    throw new FootprintError('invalid_vessel');
  }
  if (a.cell <= 0 || a.margin < 0) throw new FootprintError('invalid_area');
  if (a.kind === 'linear') {
    const w = (a.mode === 'alongside' ? v.loa : v.beam) + a.margin;
    if (!isInt(p.x) || p.x % PLACEMENT_SNAP_CM !== 0) throw new FootprintError('unsnapped');
    if (p.x < floorDiv(a.margin, 2) || p.x + w > a.length) throw new FootprintError('outside_area');
    const first = floorDiv(p.x, a.cell);
    const last = ceilDiv(p.x + w, a.cell) - 1;
    const indexes: number[] = [];
    for (let i = first; i <= last; i++) indexes.push(i);
    return { first, last, indexes };
  }
  // Surface placements are free in grid space; only the rotation must be a multiple of 90°.
  if (![0, 900, 1800, 2700].includes(p.rotation)) throw new FootprintError('unsnapped');
  const along = v.loa + a.margin;
  const across = v.beam + a.margin;
  const horizontal = p.rotation === 0 || p.rotation === 1800;
  const wx = horizontal ? along : across;
  const wy = horizontal ? across : along;
  const gridW = a.cols * a.cell;
  const gridH = a.rows * a.cell;
  if (p.x < 0 || p.y < 0 || p.x + wx > gridW || p.y + wy > gridH) {
    throw new FootprintError('outside_area');
  }
  const c0 = floorDiv(p.x, a.cell);
  const c1 = ceilDiv(p.x + wx, a.cell) - 1;
  const r0 = floorDiv(p.y, a.cell);
  const r1 = ceilDiv(p.y + wy, a.cell) - 1;
  const blocked = new Set(a.blocked ?? []);
  const indexes: number[] = [];
  // The first cell in index order that is not usable decides the error (as layout.Footprint).
  for (let r = r0; r <= r1; r++) {
    for (let c = c0; c <= c1; c++) {
      const idx = r * a.cols + c;
      if (idx >= a.usable.length || !a.usable[idx]) {
        throw new FootprintError(blocked.has(idx) ? 'blocked_cell' : 'outside_area');
      }
      indexes.push(idx);
    }
  }
  return { first: indexes[0] ?? 0, last: indexes[indexes.length - 1] ?? 0, indexes };
}

/** Number of cells of an area: linear ⌈Length / Cell⌉, surface Cols × Rows. */
export function cellCount(
  a: Pick<FootprintArea, 'kind' | 'length' | 'cell' | 'cols' | 'rows'>,
): number {
  if (a.cell <= 0) return 0;
  return a.kind === 'linear' ? Math.max(0, ceilDiv(a.length, a.cell)) : a.cols * a.rows;
}

type IntPt = { x: number; y: number };

/** Rotates p around c by −rotation, rotation a multiple of 900 (exact on integers). */
function rotateBack(p: IntPt, c: IntPt, rotation: number): IntPt {
  const dx = p.x - c.x;
  const dy = p.y - c.y;
  switch (((rotation % 3600) + 3600) % 3600) {
    case 900: // undo +90° (clockwise on screen): (dx, dy) -> (dy, -dx)
      return { x: c.x + dy, y: c.y - dx };
    case 1800:
      return { x: c.x - dx, y: c.y - dy };
    case 2700:
      return { x: c.x - dy, y: c.y + dx };
    default:
      return { x: p.x, y: p.y };
  }
}

/** Inclusive point-in-polygon on integers (same predicate as geo.Polygon.Contains). */
function containsInclusive(poly: readonly IntPt[], p: IntPt): boolean {
  const n = poly.length;
  if (n < 3) return false;
  let inside = false;
  for (let i = 0, j = n - 1; i < n; j = i++) {
    const a = poly[i]!;
    const b = poly[j]!;
    const cross = (b.x - a.x) * (p.y - a.y) - (b.y - a.y) * (p.x - a.x);
    if (
      cross === 0 &&
      p.x >= Math.min(a.x, b.x) &&
      p.x <= Math.max(a.x, b.x) &&
      p.y >= Math.min(a.y, b.y) &&
      p.y <= Math.max(a.y, b.y)
    ) {
      return true;
    }
    if (a.y > p.y !== b.y > p.y) {
      const lhs = (p.x - a.x) * (b.y - a.y);
      const rhs = (b.x - a.x) * (p.y - a.y);
      if (b.y > a.y ? lhs < rhs : lhs > rhs) inside = !inside;
    }
  }
  return inside;
}

export type SurfaceAreaInput = {
  outline: readonly IntPt[];
  grid_rotation_dd: number;
  cell_cm: number;
  margin_cm: number;
  blocked_cells: readonly number[];
};

/**
 * Footprint view of a surface area: the outline rotated by −grid_rotation around its first vertex, grid origin
 * at the minimum of the rotated bounds, cols/rows rounded up, a cell usable when its four corners are inside
 * the rotated outline and it is not blocked.
 */
export function surfaceAreaOf(
  d: SurfaceAreaInput,
): FootprintArea & { originX: number; originY: number } {
  const pivot = d.outline[0] ?? { x: 0, y: 0 };
  const rotated = d.outline.map((p) => rotateBack(p, pivot, d.grid_rotation_dd));
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const p of rotated) {
    minX = Math.min(minX, p.x);
    minY = Math.min(minY, p.y);
    maxX = Math.max(maxX, p.x);
    maxY = Math.max(maxY, p.y);
  }
  const cell = d.cell_cm;
  if (rotated.length < 3 || cell <= 0) {
    return {
      kind: 'surface',
      length: 0,
      mode: 'stern_to',
      cell,
      margin: d.margin_cm,
      cols: 0,
      rows: 0,
      usable: [],
      blocked: [...d.blocked_cells],
      originX: 0,
      originY: 0,
    };
  }
  const cols = ceilDiv(maxX - minX, cell);
  const rows = ceilDiv(maxY - minY, cell);
  const blocked = new Set(d.blocked_cells);
  const usable: boolean[] = new Array<boolean>(cols * rows).fill(false);
  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < cols; c++) {
      const x0 = minX + c * cell;
      const y0 = minY + r * cell;
      const idx = r * cols + c;
      usable[idx] =
        !blocked.has(idx) &&
        containsInclusive(rotated, { x: x0, y: y0 }) &&
        containsInclusive(rotated, { x: x0 + cell, y: y0 }) &&
        containsInclusive(rotated, { x: x0 + cell, y: y0 + cell }) &&
        containsInclusive(rotated, { x: x0, y: y0 + cell });
    }
  }
  return {
    kind: 'surface',
    length: 0,
    mode: 'stern_to',
    cell,
    margin: d.margin_cm,
    cols,
    rows,
    usable,
    blocked: [...d.blocked_cells],
    originX: minX,
    originY: minY,
  };
}
