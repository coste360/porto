import type { Schemas } from '@porto/shared/api';

// Enum values of the pricing contract used by the tool pages, in display order. Labels come from pricingEnums.

export const CONTRACT_TYPES = [
  'transit',
  'monthly',
  'seasonal',
  'annual',
  'storage',
  'charter',
] as const satisfies readonly Schemas['ContractType'][];

export const CHANNELS = [
  'desk',
  'online',
  'api',
  'external',
] as const satisfies readonly Schemas['Channel'][];

export const CUSTOMER_CATEGORIES = [
  'member',
  'resident',
  'affiliated',
  'charter',
  'yard',
] as const satisfies readonly Schemas['CustomerCategory'][];

export const HULL_TYPES = [
  'monohull_motor',
  'monohull_sail',
  'catamaran',
  'trimaran',
  'rib',
  'gozzo',
  'other',
] as const satisfies readonly Schemas['HullType'][];

/** Hull types priced as multihulls by the usual modifiers (shown with a marker). */
export const MULTIHULLS: readonly Schemas['HullType'][] = ['catamaran', 'trimaran'];

/** No-price reasons with a label in pricingEnums:noPriceReasons. */
export const NO_PRICE_REASONS = [
  'no_rule',
  'no_band',
  'no_amount',
  'outside_validity',
  'no_primary_item',
  'unit_missing',
  'no_active_list',
] as const;

export function isNoPriceReason(r: unknown): r is (typeof NO_PRICE_REASONS)[number] {
  return (NO_PRICE_REASONS as readonly unknown[]).includes(r);
}

/** Matrix cell error `no_price:<reason>` → its reason, when known. */
export function cellErrorReason(
  error: string | undefined,
): (typeof NO_PRICE_REASONS)[number] | null {
  if (!error) return null;
  const reason = error.startsWith('no_price:') ? error.slice('no_price:'.length) : error;
  return isNoPriceReason(reason) ? reason : null;
}
