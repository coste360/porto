// Go ↔ TypeScript parity of Footprint and AreaOf (m1-plan §2.8): this test and the Go test
// TestFootprint_ParityVectors read the same files under testdata/footprint/ at the repo root.
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import {
  FootprintError,
  cellCount,
  footprint,
  surfaceAreaOf,
  type FootprintArea,
} from './footprint';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', '..');
const CASES = join(ROOT, 'testdata', 'footprint', 'cases.json');
const AREA_CELLS = join(ROOT, 'testdata', 'footprint', 'area-cells.json');

type VectorArea = {
  kind: 'linear' | 'surface';
  length_cm?: number;
  mode?: 'stern_to' | 'alongside';
  cell_cm: number;
  margin_cm?: number;
  cols?: number;
  rows?: number;
  usable?: boolean[];
  blocked?: number[];
};

type Vector = {
  name: string;
  area: VectorArea;
  vessel: { loa_cm: number; beam_cm: number };
  placement: { x_cm: number; y_cm?: number; rotation_dd?: number };
  expect:
    | { cells: { first: number; last: number } | { indexes: number[] } }
    | { error: 'unsnapped' | 'outside_area' | 'blocked_cell' | 'invalid_vessel' | 'invalid_area' };
};

function toArea(a: VectorArea): FootprintArea {
  return {
    kind: a.kind,
    length: a.length_cm ?? 0,
    mode: a.mode ?? 'stern_to',
    cell: a.cell_cm,
    margin: a.margin_cm ?? 0,
    cols: a.cols ?? 0,
    rows: a.rows ?? 0,
    usable: a.usable ?? [],
    ...(a.blocked ? { blocked: a.blocked } : {}),
  };
}

function run(v: Vector) {
  try {
    const cells = footprint(
      { loa: v.vessel.loa_cm, beam: v.vessel.beam_cm },
      { x: v.placement.x_cm, y: v.placement.y_cm ?? 0, rotation: v.placement.rotation_dd ?? 0 },
      toArea(v.area),
    );
    return { cells };
  } catch (err) {
    if (err instanceof FootprintError) return { error: err.code };
    throw err;
  }
}

// The vector files are required: a missing file fails here instead of silently skipping the TS side of the
// Go <-> TS parity check (INV-02).
describe('parity vector files', () => {
  it.each([CASES, AREA_CELLS])('%s exists', (path) => {
    expect(existsSync(path)).toBe(true);
  });
});

describe('footprint parity vectors', () => {
  const file = existsSync(CASES)
    ? (JSON.parse(readFileSync(CASES, 'utf8')) as { version: number; cases: Vector[] })
    : { version: 1, cases: [] };

  it('has the expected format and at least 40 cases', () => {
    expect(file.version).toBe(1);
    expect(file.cases.length).toBeGreaterThanOrEqual(40);
  });

  it.each(file.cases.map((c) => [c.name, c] as const))('%s', (_name, v) => {
    const got = run(v);
    if ('error' in v.expect) {
      expect(got).toEqual({ error: v.expect.error });
      return;
    }
    expect('cells' in got).toBe(true);
    if (!('cells' in got) || !got.cells) return;
    const want = v.expect.cells;
    if ('indexes' in want) {
      expect(got.cells.indexes).toEqual(want.indexes);
    } else {
      expect({ first: got.cells.first, last: got.cells.last }).toEqual(want);
    }
  });
});

type AreaVector = {
  name: string;
  kind: 'linear' | 'surface';
  outline?: { x: number; y: number }[];
  grid_rotation_dd?: number;
  cell_cm: number;
  from_cm?: number;
  to_cm?: number;
  blocked?: number[];
  expect: { cols?: number; rows?: number; usable?: boolean[]; cell_count: number };
};

describe('area cells parity vectors', () => {
  const file = existsSync(AREA_CELLS)
    ? (JSON.parse(readFileSync(AREA_CELLS, 'utf8')) as { cases: AreaVector[] })
    : { cases: [] };

  it.each(file.cases.map((c) => [c.name, c] as const))('%s', (_name, v) => {
    if (v.kind === 'linear') {
      const length = (v.to_cm ?? 0) - (v.from_cm ?? 0);
      expect(cellCount({ kind: 'linear', length, cell: v.cell_cm, cols: 0, rows: 0 })).toBe(
        v.expect.cell_count,
      );
      return;
    }
    const area = surfaceAreaOf({
      outline: v.outline ?? [],
      grid_rotation_dd: v.grid_rotation_dd ?? 0,
      cell_cm: v.cell_cm,
      margin_cm: 0,
      blocked_cells: v.blocked ?? [],
    });
    expect({
      cols: area.cols,
      rows: area.rows,
      usable: area.usable,
      cell_count: cellCount(area),
    }).toEqual(v.expect);
  });
});
