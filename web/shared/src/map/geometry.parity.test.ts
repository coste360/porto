// Go ↔ TypeScript parity of the geometry constants the web repeats: the values of geometry.ts must match the Go
// constants of internal/domain/layout/model.go (no shared JSON file: they are plain numbers).
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { MIN_BERTH_LENGTH_CM, MIN_BERTH_WIDTH_CM, MIN_RECT_SIDE_CM } from './geometry';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', '..');
const MODEL_GO = readFileSync(join(ROOT, 'internal', 'domain', 'layout', 'model.go'), 'utf8');

function goConst(name: string): number {
  const m = MODEL_GO.match(new RegExp(`\\b${name}\\s+geo\\.Cm\\s*=\\s*([\\d_]+)`));
  if (!m?.[1]) throw new Error(`${name} not found in layout/model.go`);
  return Number(m[1].replace(/_/g, ''));
}

describe('geometry constants parity', () => {
  it.each([
    ['MinRectSide', MIN_RECT_SIDE_CM],
    ['MinBerthWidth', MIN_BERTH_WIDTH_CM],
    ['MinBerthLength', MIN_BERTH_LENGTH_CM],
  ] as const)('%s matches the Go constant', (name, value) => {
    expect(goConst(name)).toBe(value);
  });
});
