// Render and pointer geometry of the harbour map (m1-plan §5.2). Map units are integer centimetres, y points down
// and angles are tenths of a degree (0 = +x, 900 = +y, clockwise on screen), as internal/geo. Floats are allowed
// here only for rendering and pointer maths: every value that is stored or sent goes through roundHalfEven.

export { formatCm } from '../lib/format';

export type Pt = { x: number; y: number };
export type Box = { minX: number; minY: number; maxX: number; maxY: number };
export type RectGeom = { anchor: Pt; width_cm: number; length_cm: number; rotation_dd: number };

/** Smallest side of a berth or land resource rectangle in cm (layout.MinRectSide, OpenAPI RectSideCm). */
export const MIN_RECT_SIDE_CM = 2;

/**
 * Smallest usual berth in cm (layout.MinBerthWidth / MinBerthLength): below either side a non-buoy berth gets the
 * non-blocking `berth_below_min_size` warning of the server validation. geometry.parity.test.ts checks the values
 * against internal/domain/layout/model.go.
 */
export const MIN_BERTH_WIDTH_CM = 150;
export const MIN_BERTH_LENGTH_CM = 300;

/** Nearest integer, ties to even (the only rounding used for values the client stores or sends). */
export function roundHalfEven(value: number): number {
  if (!Number.isFinite(value)) return 0;
  const floor = Math.floor(value);
  const diff = value - floor;
  if (diff > 0.5) return floor + 1;
  if (diff < 0.5) return floor;
  return floor % 2 === 0 ? floor : floor + 1;
}

/** Integer division num/den rounded half-even (den > 0), exact for safe integers. */
export function divHalfEven(num: number, den: number): number {
  const q = Math.floor(num / den);
  const r = num - q * den;
  const twice = 2 * r;
  if (twice > den || (twice === den && q % 2 !== 0)) return q + 1;
  return q;
}

/** Rounds a point to integer cm. */
export function roundPoint(p: Pt): Pt {
  return { x: roundHalfEven(p.x), y: roundHalfEven(p.y) };
}

/** Normalizes tenths of a degree to 0..3599. */
export function normalizeDd(dd: number): number {
  const m = Math.round(dd) % 3600;
  return m < 0 ? m + 3600 : m;
}

/** Unit direction of an angle in tenths of a degree. */
export function direction(dd: number): Pt {
  const rad = (dd / 10) * (Math.PI / 180);
  return { x: Math.cos(rad), y: Math.sin(rad) };
}

/** Angle of a vector in tenths of a degree (0..3599), rounded half-even. */
export function angleDdOf(dx: number, dy: number): number {
  if (dx === 0 && dy === 0) return 0;
  const deg = (Math.atan2(dy, dx) * 180) / Math.PI;
  return normalizeDd(roundHalfEven(deg * 10));
}

export function distance(a: Pt, b: Pt): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

/**
 * Corners of a rotated rect: anchor-left, anchor-right, far-right, far-left, where left and right are seen
 * looking from the anchor along the rotation (same order as geo.Rect.Corners). Floats, render only.
 */
export function rectCorners(r: RectGeom): [Pt, Pt, Pt, Pt] {
  const u = direction(r.rotation_dd);
  const left = { x: u.y, y: -u.x };
  const hw = r.width_cm / 2;
  const l = r.length_cm;
  const corner = (side: number, far: number): Pt => ({
    x: r.anchor.x + side * left.x * hw + far * u.x * l,
    y: r.anchor.y + side * left.y * hw + far * u.y * l,
  });
  return [corner(1, 0), corner(-1, 0), corner(-1, 1), corner(1, 1)];
}

/** Centre of a rotated rect. */
export function rectCentre(r: RectGeom): Pt {
  const u = direction(r.rotation_dd);
  return { x: r.anchor.x + (u.x * r.length_cm) / 2, y: r.anchor.y + (u.y * r.length_cm) / 2 };
}

export function boundsOf(points: readonly Pt[]): Box {
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const p of points) {
    if (p.x < minX) minX = p.x;
    if (p.y < minY) minY = p.y;
    if (p.x > maxX) maxX = p.x;
    if (p.y > maxY) maxY = p.y;
  }
  if (minX === Infinity) return { minX: 0, minY: 0, maxX: 0, maxY: 0 };
  return { minX, minY, maxX, maxY };
}

export function expandBox(b: Box, margin: number): Box {
  return {
    minX: b.minX - margin,
    minY: b.minY - margin,
    maxX: b.maxX + margin,
    maxY: b.maxY + margin,
  };
}

export function unionBox(a: Box, b: Box): Box {
  return {
    minX: Math.min(a.minX, b.minX),
    minY: Math.min(a.minY, b.minY),
    maxX: Math.max(a.maxX, b.maxX),
    maxY: Math.max(a.maxY, b.maxY),
  };
}

export function boxesIntersect(a: Box, b: Box): boolean {
  return a.minX <= b.maxX && b.minX <= a.maxX && a.minY <= b.maxY && b.minY <= a.maxY;
}

export function boxContains(outer: Box, inner: Box): boolean {
  return (
    inner.minX >= outer.minX &&
    inner.maxX <= outer.maxX &&
    inner.minY >= outer.minY &&
    inner.maxY <= outer.maxY
  );
}

export function boxCentre(b: Box): Pt {
  return { x: (b.minX + b.maxX) / 2, y: (b.minY + b.maxY) / 2 };
}

/** Sum of the segment lengths of a polyline (render only). */
export function polylineLength(points: readonly Pt[]): number {
  let total = 0;
  for (let i = 1; i < points.length; i++) total += distance(points[i - 1]!, points[i]!);
  return total;
}

/** Point at distance s along a polyline and the direction (dd) of its segment; clamped to the ends. */
export function pointAt(
  points: readonly Pt[],
  s: number,
): { point: Pt; dd: number; segment: number } {
  if (points.length === 0) return { point: { x: 0, y: 0 }, dd: 0, segment: 0 };
  if (points.length === 1) return { point: { ...points[0]! }, dd: 0, segment: 0 };
  let rest = Math.max(0, s);
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1]!;
    const b = points[i]!;
    const len = distance(a, b);
    if (rest <= len || i === points.length - 1) {
      const t = len === 0 ? 0 : Math.min(1, rest / len);
      return {
        point: { x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t },
        dd: angleDdOf(b.x - a.x, b.y - a.y),
        segment: i - 1,
      };
    }
    rest -= len;
  }
  return { point: { ...points[points.length - 1]! }, dd: 0, segment: points.length - 2 };
}

/** Sub-path of a polyline between distances from and to (render only). */
export function subPolyline(points: readonly Pt[], from: number, to: number): Pt[] {
  if (points.length < 2 || to <= from) return [];
  const out: Pt[] = [pointAt(points, from).point];
  let acc = 0;
  for (let i = 1; i < points.length; i++) {
    acc += distance(points[i - 1]!, points[i]!);
    if (acc > from && acc < to) out.push({ ...points[i]! });
  }
  out.push(pointAt(points, to).point);
  return out;
}

export type Projection = { point: Pt; t: number; dist: number };

/** Projection of p onto the segment a-b (t in 0..1). */
export function projectOntoSegment(p: Pt, a: Pt, b: Pt): Projection {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len2 = dx * dx + dy * dy;
  const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / len2));
  const point = { x: a.x + dx * t, y: a.y + dy * t };
  return { point, t, dist: distance(p, point) };
}

/** Nearest point of a polyline to p, with the distance along it. */
export function projectOntoPolyline(
  p: Pt,
  points: readonly Pt[],
): (Projection & { s: number; segment: number }) | null {
  let best: (Projection & { s: number; segment: number }) | null = null;
  let acc = 0;
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1]!;
    const b = points[i]!;
    const proj = projectOntoSegment(p, a, b);
    const len = distance(a, b);
    if (!best || proj.dist < best.dist) best = { ...proj, s: acc + proj.t * len, segment: i - 1 };
    acc += len;
  }
  return best;
}

/** Offsets a polyline sideways by d (positive = right of the direction of travel, negative = left). */
export function offsetPolyline(points: readonly Pt[], d: number): Pt[] {
  if (points.length < 2) return points.map((p) => ({ ...p }));
  const normals: Pt[] = [];
  for (let i = 1; i < points.length; i++) {
    const u = direction(
      angleDdOf(points[i]!.x - points[i - 1]!.x, points[i]!.y - points[i - 1]!.y),
    );
    normals.push({ x: -u.y, y: u.x }); // right of travel (y down)
  }
  return points.map((p, i) => {
    const n0 = normals[Math.max(0, i - 1)]!;
    const n1 = normals[Math.min(normals.length - 1, i)]!;
    const nx = (n0.x + n1.x) / 2;
    const ny = (n0.y + n1.y) / 2;
    const len = Math.hypot(nx, ny) || 1;
    // Miter length: keep the offset distance on both segments (bounded for sharp turns).
    const cos = Math.max(0.25, nx * n1.x + ny * n1.y) / len;
    return { x: p.x + (nx / len) * (d / cos), y: p.y + (ny / len) * (d / cos) };
  });
}

/** Whether p is inside the polygon or on its border (exact for integer input). */
export function pointInPolygon(p: Pt, polygon: readonly Pt[]): boolean {
  const n = polygon.length;
  if (n < 3) return false;
  let inside = false;
  for (let i = 0, j = n - 1; i < n; j = i++) {
    const a = polygon[i]!;
    const b = polygon[j]!;
    const cross = (b.x - a.x) * (p.y - a.y) - (b.y - a.y) * (p.x - a.x);
    if (
      cross === 0 &&
      p.x >= Math.min(a.x, b.x) &&
      p.x <= Math.max(a.x, b.x) &&
      p.y >= Math.min(a.y, b.y) &&
      p.y <= Math.max(a.y, b.y)
    ) {
      return true; // on the border
    }
    if (a.y > p.y !== b.y > p.y) {
      // x of the edge at p.y compared without division: sign depends on the edge direction.
      const lhs = (p.x - a.x) * (b.y - a.y);
      const rhs = (b.x - a.x) * (p.y - a.y);
      if (b.y > a.y ? lhs < rhs : lhs > rhs) inside = !inside;
    }
  }
  return inside;
}

/** Rotates p around c by dd tenths of a degree (clockwise on screen). Floats. */
export function rotatePoint(p: Pt, c: Pt, dd: number): Pt {
  const u = direction(dd);
  const dx = p.x - c.x;
  const dy = p.y - c.y;
  return { x: c.x + dx * u.x - dy * u.y, y: c.y + dx * u.y + dy * u.x };
}

/** Reflects p across the line through a and b. Floats. */
export function reflectPoint(p: Pt, a: Pt, b: Pt): Pt {
  const proj = projectOntoLine(p, a, b);
  return { x: 2 * proj.x - p.x, y: 2 * proj.y - p.y };
}

function projectOntoLine(p: Pt, a: Pt, b: Pt): Pt {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len2 = dx * dx + dy * dy;
  if (len2 === 0) return { ...a };
  const t = ((p.x - a.x) * dx + (p.y - a.y) * dy) / len2;
  return { x: a.x + dx * t, y: a.y + dy * t };
}

/** Points as the SVG `points` attribute. */
export function svgPoints(points: readonly Pt[]): string {
  return points.map((p) => `${p.x},${p.y}`).join(' ');
}

/** Polygon area (shoelace, absolute), cm². */
export function polygonArea(points: readonly Pt[]): number {
  let twice = 0;
  for (let i = 0, j = points.length - 1; i < points.length; j = i++) {
    twice += points[j]!.x * points[i]!.y - points[i]!.x * points[j]!.y;
  }
  return Math.abs(twice) / 2;
}
