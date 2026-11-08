import type { UtilityMeter } from '@porto/shared/api';

/** Days used by the expected-range hint when the meter has no daily threshold of its own. */
const DEFAULT_DAILY_MILLI: Record<string, number> = { power: 30_000, water: 1_000 };

/**
 * Expected range of a new reading: from the previous value up to previous + daily threshold × days since it (no
 * upper bound without a threshold). Pure, exported for the tests.
 */
export function expectedRange(
  meter: Pick<UtilityMeter, 'kind' | 'daily_threshold_milli' | 'last_reading'>,
  today: string,
): { min: number; max: number | null } | null {
  const last = meter.last_reading;
  if (!last) return null;
  const daily = meter.daily_threshold_milli ?? DEFAULT_DAILY_MILLI[meter.kind];
  if (!daily) return { min: last.value_milli, max: null };
  const days = Math.max(1, Math.round((Date.parse(today) - Date.parse(last.read_on)) / 86_400_000));
  return { min: last.value_milli, max: last.value_milli + daily * days };
}
