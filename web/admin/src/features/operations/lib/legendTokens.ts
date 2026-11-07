import type { LensStyle } from '@porto/shared/map';

/** One representative token per legend key (the swatch). */
export function legendTokens(styles: Iterable<LensStyle>): Map<string, LensStyle['colorToken']> {
  const out = new Map<string, LensStyle['colorToken']>();
  for (const s of styles) if (!out.has(s.legendKey)) out.set(s.legendKey, s.colorToken);
  return out;
}
