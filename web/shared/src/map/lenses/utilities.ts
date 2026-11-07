import type { Lens, LensStyle } from './types';

/** Utenze (static in M3): pedestal power in amperes, else water, else none. */
export const utilitiesLens: Lens = (el): LensStyle => {
  const u = el.utilities;
  if (u?.power_a) {
    return { colorToken: 'owner', glyph: 'bolt', badge: `${u.power_a}A`, legendKey: 'power' };
  }
  if (u?.water) return { colorToken: 'arrival', glyph: 'drop', legendKey: 'water' };
  return { colorToken: 'muted', legendKey: 'none' };
};
