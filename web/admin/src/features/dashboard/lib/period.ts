import { addDays, addMonths, daysBetween } from '../../desk/lib/dates';

// Period presets of the manager dashboard (m7-plan §4, D18): year to date by default; every preset ends today
// (marina time zone) and stays within the 400-day limit of the API.

export const PERIOD_PRESETS = ['mtd', 'ytd', 'last_30', 'last_12m', 'custom'] as const;
export type PeriodPreset = (typeof PERIOD_PRESETS)[number];

export type Period = { from: string; to: string };

/** Longest period the API accepts (nights, inclusive). */
export const MAX_PERIOD_DAYS = 400;

/** The period of a preset ending on `today` (custom: year to date, the caller overrides it). */
export function presetPeriod(preset: PeriodPreset, today: string): Period {
  switch (preset) {
    case 'mtd':
      return { from: `${today.slice(0, 7)}-01`, to: today };
    case 'last_30':
      return { from: addDays(today, -29), to: today };
    case 'last_12m':
      return { from: addDays(addMonths(today, -12), 1), to: today };
    case 'ytd':
    case 'custom':
      return { from: `${today.slice(0, 4)}-01-01`, to: today };
  }
}

/** Whether a custom period is acceptable to the API (from ≤ to, at most MAX_PERIOD_DAYS nights). */
export function validPeriod(p: Partial<Period>): p is Period {
  if (!p.from || !p.to) return false;
  const nights = daysBetween(p.from, p.to) + 1;
  return nights >= 1 && nights <= MAX_PERIOD_DAYS;
}

export function isPreset(v: unknown): v is PeriodPreset {
  return typeof v === 'string' && (PERIOD_PRESETS as readonly string[]).includes(v);
}

/** Milestone that brings a seam tile (OpenAPI TileReason milestone_m10 → "M10"). */
export function milestoneOf(reason: string | null | undefined): string {
  const m = /^milestone_(m\d+)$/.exec(reason ?? '');
  return m?.[1]?.toUpperCase() ?? '';
}

/** Tiles that the period comparison accepts as indicators (m7-plan §5). */
export const COMPARABLE_TILES = [
  'production',
  'charged',
  'collected',
  'occupancy',
  'efficiency',
  'berth_revenue_per_m',
  'transit_avg_night',
  'new_customers',
  'renewal_rate',
] as const;

export function isComparableTile(key: string): key is (typeof COMPARABLE_TILES)[number] {
  return (COMPARABLE_TILES as readonly string[]).includes(key);
}
