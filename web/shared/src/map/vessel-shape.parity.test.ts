// Go ↔ TypeScript parity of the vessel silhouette (m4-plan D30): the map print draws vessels with a Go mirror of
// vessel-shape.ts; this test and TestSilhouette_ParityVectors read the same files under testdata/silhouette/ at
// the repo root. Each file is a JSON array of {loa_cm, beam_cm, hull_type, path} where `path` is the exact SVG path
// string of vesselPath (integer cm, half-even rounding; '' for an invalid vessel).
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { vesselPath } from './vessel-shape';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', '..');
const DIR = join(ROOT, 'testdata', 'silhouette');

type Vector = { loa_cm: number; beam_cm: number; hull_type: string; path: string };

function load(): Vector[] {
  if (!existsSync(DIR)) return [];
  return readdirSync(DIR)
    .filter((f) => f.endsWith('.json'))
    .sort()
    .flatMap((f) => JSON.parse(readFileSync(join(DIR, f), 'utf8')) as Vector[]);
}

const vectors = load();

describe.skipIf(vectors.length === 0)('vessel silhouette parity vectors', () => {
  it.each(vectors.map((v) => [`${v.hull_type} ${v.loa_cm}×${v.beam_cm}`, v] as const))(
    '%s',
    (_name, v) => {
      expect(vesselPath(v)).toBe(v.path);
    },
  );

  it('covers every hull type and an invalid vessel', () => {
    const hulls = new Set(vectors.map((v) => v.hull_type));
    for (const h of [
      'monohull_motor',
      'monohull_sail',
      'catamaran',
      'trimaran',
      'rib',
      'gozzo',
      'other',
    ]) {
      expect(hulls.has(h)).toBe(true);
    }
    expect(vectors.some((v) => v.path === '')).toBe(true);
  });
});
