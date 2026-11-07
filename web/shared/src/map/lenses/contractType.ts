import type { Lens, LensStyle } from './types';

/** Tipo di contratto: mooring terms, transits and use rights, with the initial of the type as badge. */
export const contractTypeLens: Lens = (el): LensStyle => {
  if (el.status === 'owner' || el.kind === 'owner') {
    return { colorToken: 'owner', badge: 'D', legendKey: 'owner' };
  }
  const c = el.contract;
  if (!c) {
    if (el.status === 'hold') return { colorToken: 'hold', glyph: 'clock', legendKey: 'hold' };
    return { colorToken: 'free', legendKey: 'none' };
  }
  if (c.kind === 'transit') return { colorToken: 'transit', badge: 'T', legendKey: 'transit' };
  if (c.kind !== 'mooring') return { colorToken: 'muted', legendKey: 'other' };
  switch (c.term) {
    case 'annual':
      return { colorToken: 'annual', badge: 'A', legendKey: 'annual' };
    case 'seasonal':
      return { colorToken: 'seasonal', badge: 'S', legendKey: 'seasonal' };
    case 'monthly':
      return { colorToken: 'monthly', badge: 'M', legendKey: 'monthly' };
    default:
      return { colorToken: 'period', badge: 'P', legendKey: 'period' };
  }
};
