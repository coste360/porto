import type { Box } from './geometry';

/** Default cell of the uniform grid index (cm). */
export const INDEX_CELL_CM = 1000;

export type Indexed<T> = { box: Box; item: T };

/**
 * Uniform grid index over element bounds (m1-plan §5.2), built once per document version. `query` returns the
 * items whose bounds intersect a box (the view box plus a margin for culling, or a marquee).
 */
export class SpatialIndex<T> {
  private readonly cells = new Map<string, number[]>();
  private readonly entries: Indexed<T>[];
  private readonly cell: number;

  constructor(entries: readonly Indexed<T>[], cell = INDEX_CELL_CM) {
    this.cell = cell;
    this.entries = [...entries];
    this.entries.forEach((entry, i) => {
      this.forCells(entry.box, (key) => {
        const list = this.cells.get(key);
        if (list) list.push(i);
        else this.cells.set(key, [i]);
      });
    });
  }

  get size(): number {
    return this.entries.length;
  }

  private forCells(box: Box, fn: (key: string) => void) {
    const c0 = Math.floor(box.minX / this.cell);
    const c1 = Math.floor(box.maxX / this.cell);
    const r0 = Math.floor(box.minY / this.cell);
    const r1 = Math.floor(box.maxY / this.cell);
    // Very large boxes (whole-map polygons) would visit too many cells: cap them into one shared bucket.
    if ((c1 - c0 + 1) * (r1 - r0 + 1) > 4096) {
      fn('*');
      return;
    }
    for (let r = r0; r <= r1; r++) for (let c = c0; c <= c1; c++) fn(`${c}:${r}`);
  }

  /** Items whose bounds intersect `box`, in insertion order. */
  query(box: Box): T[] {
    const hit = new Set<number>();
    const visit = (key: string) => {
      for (const i of this.cells.get(key) ?? []) hit.add(i);
    };
    let huge = false;
    this.forCells(box, (key) => {
      if (key === '*') huge = true;
      else visit(key);
    });
    visit('*');
    // A query box larger than the cell budget (zoomed far out) scans every entry instead.
    const candidates = huge ? this.entries.map((_e, i) => i) : [...hit].sort((a, b) => a - b);
    const out: T[] = [];
    for (const i of candidates) {
      const e = this.entries[i]!;
      if (
        e.box.minX <= box.maxX &&
        box.minX <= e.box.maxX &&
        e.box.minY <= box.maxY &&
        box.minY <= e.box.maxY
      ) {
        out.push(e.item);
      }
    }
    return out;
  }
}
