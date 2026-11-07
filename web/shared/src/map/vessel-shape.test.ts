import { describe, expect, it } from 'vitest';

import { pathNumbers, vesselPath } from './vessel-shape';

const HULLS = [
  'monohull_motor',
  'monohull_sail',
  'catamaran',
  'trimaran',
  'rib',
  'gozzo',
  'other',
] as const;

/** Pairs (x, y) of the path, in order. */
function points(d: string): [number, number][] {
  const n = pathNumbers(d);
  const out: [number, number][] = [];
  for (let i = 0; i + 1 < n.length; i += 2) out.push([n[i]!, n[i + 1]!]);
  return out;
}

describe('vesselPath', () => {
  it.each(HULLS)('draws %s with integer coordinates inside loa × beam', (hull) => {
    for (const [loa, beam] of [
      [1200, 400],
      [855, 293],
      [3001, 1201],
      [450, 175],
    ] as const) {
      const d = vesselPath({ loa_cm: loa, beam_cm: beam, hull_type: hull });
      expect(d).toMatch(/^M/);
      expect(d.endsWith('Z')).toBe(true);
      for (const [x, y] of points(d)) {
        expect(Number.isInteger(x) && Number.isInteger(y)).toBe(true);
        expect(x).toBeGreaterThanOrEqual(0);
        expect(x).toBeLessThanOrEqual(loa);
        expect(Math.abs(y)).toBeLessThanOrEqual(Math.ceil(beam / 2));
      }
      // The bow reaches the full length and the hull spans the beam.
      const xs = points(d).map((p) => p[0]);
      expect(Math.max(...xs)).toBe(loa);
    }
  });

  it('is deterministic and differs between hull types', () => {
    const shapes = HULLS.map((h) => vesselPath({ loa_cm: 1200, beam_cm: 400, hull_type: h }));
    expect(new Set(shapes).size).toBe(HULLS.length);
    expect(vesselPath({ loa_cm: 1200, beam_cm: 400, hull_type: 'catamaran' })).toBe(shapes[2]);
  });

  it('draws multihulls as several closed sub-paths', () => {
    const cat = vesselPath({ loa_cm: 1200, beam_cm: 600, hull_type: 'catamaran' });
    const tri = vesselPath({ loa_cm: 1200, beam_cm: 600, hull_type: 'trimaran' });
    expect(cat.match(/M/g)).toHaveLength(3);
    expect(tri.match(/M/g)).toHaveLength(4);
  });

  it('treats an unknown hull type as other and rejects invalid sizes', () => {
    expect(vesselPath({ loa_cm: 1000, beam_cm: 300, hull_type: 'submarine' })).toBe(
      vesselPath({ loa_cm: 1000, beam_cm: 300, hull_type: 'other' }),
    );
    expect(vesselPath({ loa_cm: 0, beam_cm: 300, hull_type: 'other' })).toBe('');
    expect(vesselPath({ loa_cm: 1000, beam_cm: -1, hull_type: 'rib' })).toBe('');
  });
});
