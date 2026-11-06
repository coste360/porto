import type { PriceClassRule } from '@porto/shared/api';

/** Mirror of layout.PriceClassFor: the first band (sorted by max) whose max ≥ width, else the fallback. */
export function priceClassFor(widthCm: number, rule: PriceClassRule | undefined): string {
  if (!rule) return '';
  const bands = [...rule.bands].sort((a, b) => a.max_cm - b.max_cm);
  for (const b of bands) if (widthCm <= b.max_cm) return b.class;
  return rule.fallback;
}
