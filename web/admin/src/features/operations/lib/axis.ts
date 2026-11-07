// Sliding along a linear dynamic area (spec 9.4, m3-plan §8.5): 10 cm snapping, snap to the neighbour's edge
// (the footprint already includes the margin), clamped to the area, and the free space on both sides. Integers.
import { PLACEMENT_SNAP_CM, roundHalfEven } from '@porto/shared/map';

export type AxisSpan = { x: number; w: number };

/** Distance (cm) within which the vessel snaps to a neighbour's edge. */
export const NEIGHBOUR_SNAP_CM = 60;

export function snap10(x: number): number {
  return roundHalfEven(x / PLACEMENT_SNAP_CM) * PLACEMENT_SNAP_CM;
}

/** Footprint width along the axis: beam + margin (stern-to) or loa + margin (alongside), as layout.Footprint. */
export function axisWidth(
  v: { loa: number; beam: number },
  mode: string | undefined,
  margin: number,
): number {
  return (mode === 'alongside' ? v.loa : v.beam) + margin;
}

export type AxisSnap = {
  x: number;
  snapped: 'grid' | 'neighbour';
  freeLeft: number;
  freeRight: number;
};

/**
 * Snapped x of a footprint of width w dragged at rawX in an area of `length` cm with margin `margin`, given the
 * neighbours' footprints for the same nights.
 */
export function snapAxis(p: {
  rawX: number;
  w: number;
  length: number;
  margin: number;
  neighbours: readonly AxisSpan[];
  snapCm?: number;
}): AxisSnap {
  const minX = Math.ceil(Math.floor(p.margin / 2) / PLACEMENT_SNAP_CM) * PLACEMENT_SNAP_CM;
  const maxX = Math.floor((p.length - p.w) / PLACEMENT_SNAP_CM) * PLACEMENT_SNAP_CM;
  const clamp = (x: number) => Math.max(minX, Math.min(maxX, x));
  let x = clamp(snap10(p.rawX));
  let snapped: AxisSnap['snapped'] = 'grid';
  const limit = p.snapCm ?? NEIGHBOUR_SNAP_CM;
  let best = limit + 1;
  for (const n of p.neighbours) {
    for (const cand of [n.x + n.w, n.x - p.w]) {
      if (cand % PLACEMENT_SNAP_CM !== 0 || cand < minX || cand > maxX) continue;
      const d = Math.abs(cand - p.rawX);
      if (d <= limit && d < best) {
        best = d;
        x = cand;
        snapped = 'neighbour';
      }
    }
  }
  return { x, snapped, ...freeSpace(x, p.w, p.length, p.neighbours) };
}

/** Free cm between the footprint [x, x+w) and the nearest neighbour (or the area end) on each side. */
export function freeSpace(
  x: number,
  w: number,
  length: number,
  neighbours: readonly AxisSpan[],
): { freeLeft: number; freeRight: number } {
  let left = 0;
  let right = length;
  for (const n of neighbours) {
    const centre = n.x + n.w / 2;
    if (centre <= x + w / 2) left = Math.max(left, n.x + n.w);
    else right = Math.min(right, n.x);
  }
  return { freeLeft: Math.max(0, x - left), freeRight: Math.max(0, right - (x + w)) };
}
