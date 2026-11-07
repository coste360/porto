// Vessel silhouettes in scale (m3-plan §8.2, D25): a pure SVG path generator in a local frame where the stern
// is at x = 0, the bow at x = loa and the beam spans y in [−beam/2, beam/2]. Coordinates are integer cm
// (rounded half-even), so the same vessel always gives the same string (memoisation by value).
import { roundHalfEven } from './geometry';

export type HullType =
  'monohull_motor' | 'monohull_sail' | 'catamaran' | 'trimaran' | 'rib' | 'gozzo' | 'other';

export type VesselShapeInput = { loa_cm: number; beam_cm: number; hull_type: string };

const r = roundHalfEven;

type P = readonly [number, number];

function pt([x, y]: P): string {
  return `${r(x)} ${r(y)}`;
}

/** A closed monohull outline from stern (x0) to bow (x1), half beam h, centred on cy. */
function hull(
  x0: number,
  x1: number,
  cy: number,
  h: number,
  opts: { sternRatio: number; shoulder: number; pointedStern?: boolean },
): string {
  const len = x1 - x0;
  const sh = x0 + len * opts.shoulder; // where the hull starts narrowing toward the bow
  const sternH = h * opts.sternRatio;
  if (opts.pointedStern) {
    const mid = x0 + len / 2;
    return [
      `M${pt([x0, cy])}`,
      `Q${pt([x0 + len * 0.12, cy - h])} ${pt([mid, cy - h])}`,
      `Q${pt([x1 - len * 0.12, cy - h])} ${pt([x1, cy])}`,
      `Q${pt([x1 - len * 0.12, cy + h])} ${pt([mid, cy + h])}`,
      `Q${pt([x0 + len * 0.12, cy + h])} ${pt([x0, cy])}`,
      'Z',
    ].join(' ');
  }
  return [
    `M${pt([x0, cy - sternH])}`,
    `L${pt([sh, cy - h])}`,
    `Q${pt([x1 - len * 0.08, cy - h])} ${pt([x1, cy])}`,
    `Q${pt([x1 - len * 0.08, cy + h])} ${pt([sh, cy + h])}`,
    `L${pt([x0, cy + sternH])}`,
    'Z',
  ].join(' ');
}

/** A rounded rectangle (deck of a multihull, generic vessel). */
function roundedRect(x0: number, y0: number, x1: number, y1: number, rad: number): string {
  const k = Math.min(rad, (x1 - x0) / 2, (y1 - y0) / 2);
  return [
    `M${pt([x0 + k, y0])}`,
    `L${pt([x1 - k, y0])}`,
    `Q${pt([x1, y0])} ${pt([x1, y0 + k])}`,
    `L${pt([x1, y1 - k])}`,
    `Q${pt([x1, y1])} ${pt([x1 - k, y1])}`,
    `L${pt([x0 + k, y1])}`,
    `Q${pt([x0, y1])} ${pt([x0, y1 - k])}`,
    `L${pt([x0, y0 + k])}`,
    `Q${pt([x0, y0])} ${pt([x0 + k, y0])}`,
    'Z',
  ].join(' ');
}

/**
 * SVG path of a vessel silhouette in cm (local frame, stern at x = 0). Returns '' for invalid dimensions.
 * Multihulls are drawn as their hulls plus a deck between them; ribs and gozzi have their own outlines.
 */
export function vesselPath(v: VesselShapeInput): string {
  const loa = v.loa_cm;
  const beam = v.beam_cm;
  if (!Number.isFinite(loa) || !Number.isFinite(beam) || loa <= 0 || beam <= 0) return '';
  const h = beam / 2;
  switch (v.hull_type as HullType) {
    case 'monohull_motor':
      return hull(0, loa, 0, h, { sternRatio: 0.92, shoulder: 0.55 });
    case 'monohull_sail':
      return hull(0, loa, 0, h, { sternRatio: 0.7, shoulder: 0.45 });
    case 'gozzo':
      return hull(0, loa, 0, h, { sternRatio: 0, shoulder: 0.5, pointedStern: true });
    case 'rib': {
      const tube = Math.max(beam * 0.18, 1);
      return [
        hull(0, loa, 0, h, { sternRatio: 1, shoulder: 0.65 }),
        hull(tube, loa - tube * 1.5, 0, h - tube, { sternRatio: 1, shoulder: 0.65 }),
      ].join(' ');
    }
    case 'catamaran':
    case 'trimaran': {
      const n = v.hull_type === 'catamaran' ? 2 : 3;
      const hullW = beam * (n === 2 ? 0.26 : 0.2);
      const centres =
        n === 2 ? [-h + hullW / 2, h - hullW / 2] : [-h + hullW / 2, 0, h - hullW / 2];
      const parts = centres.map((cy, i) =>
        hull(0, loa, cy, (n === 3 && i === 1 ? hullW * 0.6 : hullW) / 2, {
          sternRatio: 0.8,
          shoulder: 0.6,
        }),
      );
      parts.push(roundedRect(loa * 0.08, -h + hullW / 2, loa * 0.7, h - hullW / 2, beam * 0.05));
      return parts.join(' ');
    }
    default:
      return roundedRect(0, -h, loa, h, Math.min(beam, loa) * 0.3);
  }
}

/** Every number of a path (tests and parity). */
export function pathNumbers(d: string): number[] {
  return (d.match(/-?\d+(\.\d+)?/g) ?? []).map(Number);
}
