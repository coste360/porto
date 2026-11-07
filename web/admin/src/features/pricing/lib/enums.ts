import type { I18nText, Schemas } from '@porto/shared/api';

// Enum values of the pricing contract, in display order. Labels come from the `pricingEnums` namespace.

export const CONTRACT_TYPES = [
  'annual',
  'seasonal',
  'monthly',
  'transit',
  'storage',
  'charter',
] as const satisfies readonly Schemas['ContractType'][];

export const ITEM_TYPES = [
  'berth_fee',
  'service',
  'utility',
  'product',
  'labour',
  'material',
  'fee',
  'deposit',
] as const satisfies readonly Schemas['ItemType'][];

export const ITEM_UNITS = [
  'night',
  'month',
  'season',
  'year',
  'm',
  'm2',
  'kwh',
  'm3',
  'hour',
  'piece',
  'lump',
] as const satisfies readonly Schemas['ItemUnit'][];

export const VAT_NATURES = [
  'N1',
  'N2.1',
  'N2.2',
  'N3.1',
  'N3.2',
  'N3.3',
  'N3.4',
  'N3.5',
  'N3.6',
  'N4',
  'N5',
  'N6.1',
  'N6.2',
  'N6.3',
  'N6.4',
  'N6.5',
  'N6.6',
  'N6.7',
  'N6.8',
  'N6.9',
  'N7',
] as const satisfies readonly Schemas['VatNature'][];

/** Italian VAT rates offered in the select (basis points); any other rate goes through "custom". */
export const VAT_PRESETS_BP = [2200, 1000, 500, 400, 0] as const;

export const STAGES = [
  'base',
  'modifier',
  'discount',
] as const satisfies readonly Schemas['PricingStage'][];

export const BASES = [
  'per_berth',
  'length_band',
  'per_meter',
  'per_m2',
  'per_m2_berth',
  'flat',
  'metered',
] as const satisfies readonly Schemas['PricingBasis'][];

export const TIME_UNITS = [
  'night',
  'week',
  'month',
  'season',
  'year',
  'stay',
] as const satisfies readonly Schemas['TimeUnit'][];

export const SPLIT_RULES = [
  'pro_rata_daily',
  'whole_months',
  'best_combination',
] as const satisfies readonly Schemas['SplitRule'][];

export const TOTAL_ROUNDINGS = [
  'none',
  'euro',
  'five_euro',
] as const satisfies readonly Schemas['TotalRounding'][];

export const AMOUNT_ROUNDINGS = [
  'none',
  'ten_cents',
  'euro',
] as const satisfies readonly Schemas['AmountRounding'][];

export const DIMENSION_SOURCES = [
  'vessel',
  'berth',
  'max',
] as const satisfies readonly Schemas['DimensionSource'][];

export const LENGTH_STEPS = [1, 10, 50, 100] as const;
export const LENGTH_MODES = ['up', 'half_even'] as const;

export const CHANNELS = [
  'desk',
  'online',
  'api',
  'external',
] as const satisfies readonly Schemas['Channel'][];

export const DISCOUNT_KINDS = [
  'customer',
  'promo',
  'automatic',
] as const satisfies readonly Schemas['DiscountKind'][];

export const PERIOD_KINDS = ['season', 'event'] as const satisfies readonly Schemas['PeriodKind'][];

export const PERIOD_COLORS = [
  'chart-1',
  'chart-2',
  'chart-3',
  'chart-4',
  'chart-5',
] as const satisfies readonly Schemas['PeriodColor'][];

export const PRICE_LIST_STATUSES = [
  'draft',
  'active',
  'archived',
] as const satisfies readonly Schemas['PriceListStatus'][];

export const CATEGORY_TARGETS = [
  'berth',
  'dynamic_area',
  'land_resource',
] as const satisfies readonly Schemas['CategoryTarget'][];

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

/** Multihulls get the multihull modifier (spec 12.4). */
export const MULTIHULLS: readonly Schemas['HullType'][] = ['catamaran', 'trimaran'];

export const ADJUSTMENT_SCOPES = ['all', 'item', 'category'] as const;

/** Background class of a period colour token (literal strings so Tailwind generates them). */
export const PERIOD_BG: Record<Schemas['PeriodColor'], string> = {
  'chart-1': 'bg-chart-1',
  'chart-2': 'bg-chart-2',
  'chart-3': 'bg-chart-3',
  'chart-4': 'bg-chart-4',
  'chart-5': 'bg-chart-5',
};

/** Name in the UI language (Italian when English is missing). */
export function localText(name: I18nText | undefined | null, lang: string): string {
  if (!name) return '';
  return (lang.startsWith('en') ? name.en : undefined) || name.it;
}
