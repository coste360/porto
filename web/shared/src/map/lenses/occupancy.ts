import type { Lens, LensStyle } from './types';

/** Occupazione (default lens): the night's status, contracts coloured by term. */
export const occupancyLens: Lens = (el): LensStyle => {
  switch (el.status) {
    case 'contract': {
      const term = el.contract?.term;
      if (term === 'annual') return { colorToken: 'annual', glyph: 'star', legendKey: 'annual' };
      if (term === 'seasonal') return { colorToken: 'seasonal', legendKey: 'seasonal' };
      if (term === 'monthly') return { colorToken: 'monthly', legendKey: 'monthly' };
      if (term === 'period') return { colorToken: 'period', legendKey: 'period' };
      return { colorToken: 'annual', legendKey: 'contract' };
    }
    case 'transit':
      return { colorToken: 'transit', glyph: 'suitcase', legendKey: 'transit' };
    case 'hold':
      return { colorToken: 'hold', glyph: 'clock', legendKey: 'hold' };
    case 'released':
      return { colorToken: 'released', glyph: 'release', legendKey: 'released' };
    case 'block':
      return { colorToken: 'blocked', glyph: 'lock', legendKey: 'blocked' };
    case 'closed':
      return { colorToken: 'blocked', glyph: 'lock', legendKey: 'closed' };
    case 'maintenance':
      return { colorToken: 'maintenance', glyph: 'wrench', legendKey: 'maintenance' };
    case 'owner':
      return { colorToken: 'owner', legendKey: 'owner' };
    default:
      return { colorToken: 'free', legendKey: 'free' };
  }
};
