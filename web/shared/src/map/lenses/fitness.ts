import type { Lens, LensStyle } from './types';

/** Fill thresholds of the Adeguatezza lens (spec 9.2): green ≥ 85 %, yellow 60–85 %, red < 60 %. */
export const FIT_OK_BP = 8500;
export const FIT_WARN_BP = 6000;

/** Adeguatezza: vessel footprint over berth size, violet when over the limits with an override. */
export const fitnessLens: Lens = (el): LensStyle => {
  const f = el.fit;
  if (!f || !el.vessel) return { colorToken: 'free', legendKey: 'none' };
  const badge = `${Math.floor(f.fill_bp / 100)}%`;
  if (f.override) return { colorToken: 'override', glyph: 'alert', badge, legendKey: 'override' };
  if (f.blocking_codes.length > 0) {
    return { colorToken: 'bad', glyph: 'alert', badge, legendKey: 'bad' };
  }
  if (f.fill_bp >= FIT_OK_BP) return { colorToken: 'ok', badge, legendKey: 'ok' };
  if (f.fill_bp >= FIT_WARN_BP) return { colorToken: 'warn', badge, legendKey: 'warn' };
  return { colorToken: 'bad', badge, legendKey: 'bad' };
};
