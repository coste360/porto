import type { Lens, LensStyle } from './types';

/** Presenze: in port, out (long absences apart), expected arrivals and departures of the day. */
export const presenceLens: Lens = (el, ctx): LensStyle => {
  if (!el.vessel) return { colorToken: 'free', legendKey: 'free' };
  const state = el.presence?.state;
  if (state === 'in_port' && el.departure_today) {
    return { colorToken: 'departure', glyph: 'arrowOut', legendKey: 'departure' };
  }
  if (state !== 'in_port' && el.arrival_today) {
    return { colorToken: 'arrival', glyph: 'arrowIn', legendKey: 'arrival' };
  }
  if (state === 'in_port') return { colorToken: 'in', glyph: 'arrowIn', legendKey: 'in' };
  if (state === 'out' || state === 'ashore' || state === 'yard') {
    if ((el.presence?.away_days ?? 0) > ctx.absentDaysThreshold) {
      return {
        colorToken: 'away-long',
        glyph: 'arrowOut',
        badge: String(el.presence?.away_days ?? 0),
        legendKey: 'awayLong',
      };
    }
    return { colorToken: 'out', glyph: 'arrowOut', legendKey: 'out' };
  }
  // A vessel on the berth whose presence was never recorded.
  return { colorToken: 'muted', legendKey: 'unrecorded' };
};
