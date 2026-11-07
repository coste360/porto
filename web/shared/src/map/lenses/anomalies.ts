import type { Lens, LensStyle } from './types';

/** Anomalie: computed by the server in the day view; the badge is the number of anomalies. */
export const anomaliesLens: Lens = (el): LensStyle => {
  const n = el.anomalies.length;
  if (n === 0) return { colorToken: 'muted', legendKey: 'none' };
  return { colorToken: 'bad', glyph: 'alert', badge: String(n), legendKey: 'anomaly' };
};
