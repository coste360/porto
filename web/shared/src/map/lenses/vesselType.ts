import type { HarborToken } from './tokens';
import type { Lens, LegendKey, LensStyle } from './types';

const HULL_TOKEN: Record<string, HarborToken> = {
  monohull_motor: 'owner',
  monohull_sail: 'seasonal',
  catamaran: 'monthly',
  trimaran: 'override',
  rib: 'transit',
  gozzo: 'period',
  other: 'muted',
};

/** Tipo di imbarcazione: the hull type of the vessel on the element (the silhouette is the glyph). */
export const vesselTypeLens: Lens = (el): LensStyle => {
  const hull = el.vessel?.hull_type;
  if (!hull) return { colorToken: 'free', legendKey: 'none' };
  const key = (hull in HULL_TOKEN ? hull : 'other') as LegendKey;
  return { colorToken: HULL_TOKEN[key] ?? 'muted', legendKey: key };
};
