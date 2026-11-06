// Background calibration (spec 8.2, m1-plan §5.4, §11 D21).
import type { MapBackground } from '@porto/shared/api';
import { roundHalfEven, type Pt } from '@porto/shared/map';

/** Initial scale of a new background: 1 px = 10 cm. */
export const INITIAL_SCALE_MCM_PER_PX = 10_000;

/** Image pixels between two map points under the current calibration (rotation does not change it). */
export function pixelDistance(a: Pt, b: Pt, scaleMcmPerPx: number): number {
  return (Math.hypot(b.x - a.x, b.y - a.y) * 1000) / scaleMcmPerPx;
}

/** scale_mcm_per_px = roundHalfEven(distance_cm × 1000 / pixelDistance), at least 1. */
export function scaleFromTwoPoints(pixels: number, distanceCm: number): number | null {
  if (!(pixels > 0) || !(distanceCm > 0)) return null;
  return Math.max(1, roundHalfEven((distanceCm * 1000) / pixels));
}

/**
 * New calibration after picking two points on the image (map cm, current calibration) and typing their real
 * distance. The first point stays where it is on the map, so the image grows or shrinks around it.
 */
export function calibrate(
  bg: MapBackground,
  a: Pt,
  b: Pt,
  distanceCm: number,
): MapBackground | null {
  const px = pixelDistance(a, b, bg.scale_mcm_per_px);
  const scale = scaleFromTwoPoints(px, distanceCm);
  if (scale === null) return null;
  const k = scale / bg.scale_mcm_per_px;
  const origin = {
    x: roundHalfEven(a.x + (bg.origin.x - a.x) * k),
    y: roundHalfEven(a.y + (bg.origin.y - a.y) * k),
  };
  return { ...bg, scale_mcm_per_px: scale, origin };
}

/** Degrees typed with one decimal ("12,5") → rotation_dd 0..3599. */
export function parseDegreesToDd(text: string): number | null {
  const m = /^\s*(-?)(\d{1,4})(?:[.,](\d))?\s*$/.exec(text);
  if (!m) return null;
  const dd = Number(m[2]) * 10 + Number(m[3] ?? 0);
  const signed = m[1] === '-' ? -dd : dd;
  return ((signed % 3600) + 3600) % 3600;
}

export function formatDd(dd: number): string {
  return `${Math.trunc(dd / 10)},${dd % 10}`;
}
