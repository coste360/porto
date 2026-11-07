import type { Lens, LensStyle } from './types';

/** Whole days from a to b (YYYY-MM-DD), b − a. */
export function daysBetween(a: string, b: string): number {
  const [ay, am, ad] = a.split('-').map(Number);
  const [by, bm, bd] = b.split('-').map(Number);
  return Math.round((Date.UTC(by!, bm! - 1, bd!) - Date.UTC(ay!, am! - 1, ad!)) / 86_400_000);
}

/** Scadenze: contract end, insurance and documents of the vessel or the customer. */
export const deadlinesLens: Lens = (el, ctx): LensStyle => {
  const d = el.deadlines;
  if (!d) return { colorToken: 'free', legendKey: 'none' };
  const ends = d.contract_ends_on ? daysBetween(ctx.date, d.contract_ends_on) : null;
  if (
    d.insurance === 'expired' ||
    d.insurance === 'missing' ||
    d.documents === 'expired' ||
    (ends !== null && ends < 0)
  ) {
    return { colorToken: 'bad', glyph: 'calendar', legendKey: 'bad' };
  }
  if (
    d.insurance === 'expiring' ||
    d.documents === 'expiring' ||
    (ends !== null && ends <= ctx.deadlineWarnDays)
  ) {
    return {
      colorToken: 'warn',
      glyph: 'calendar',
      ...(ends !== null && ends <= ctx.deadlineWarnDays ? { badge: String(ends) } : {}),
      legendKey: 'warn',
    };
  }
  return { colorToken: 'ok', legendKey: 'ok' };
};
